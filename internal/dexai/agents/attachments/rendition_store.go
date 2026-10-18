// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package attachments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// maximumParallelBinds bounds the attachments one Resolve binds at once.
const maximumParallelBinds = 4

// RenditionStore resolves the attachment references of the Sessions' model
// requests from the platform object storage. The first resolve of an
// attachment in a Project binds it: it copies the attachment's staged objects
// by their exact versions, inside storage, to
// <projectID>/attachments/<id>/<role>/<digest> and writes the manifest last,
// which marks the attachment bound. A resolve reads the bound copy, verified
// by its digest; an attachment that is neither bound nor staged any more
// resolves to the frozen expired placeholder. Only Session Steps write under
// that prefix, and the Project's deletion removes it after its Session
// completed, so a bound attachment resolves to the same bytes on every call.
// The cache holds resolved blocks of this replica only; correctness never
// depends on it.
type RenditionStore struct {
	objects *objectstore.Store
	cache   *renditionCache
}

// NewRenditionStore resolves from objects, which presigns downloads, and keeps
// at most cacheBytes of resolved blocks.
func NewRenditionStore(objects *objectstore.Store, cacheBytes int64) *RenditionStore {
	return &RenditionStore{objects: objects, cache: &renditionCache{limit: cacheBytes, entries: map[string]*cachedBlock{}}}
}

// downloadValidity is how long a presigned download stays valid.
const downloadValidity = 5 * time.Minute

// binding is the manifest of an attachment bound to a Project: its label and
// redaction count and the exact Project copies of its objects by role,
// written after every copy.
type binding struct {
	Label      conversation.AttachmentLabel `json:"label"`
	Redactions int                          `json:"redactions,omitempty"`
	Original   *objectstore.ObjectRef       `json:"original,omitempty"`
	Rendition  objectstore.ObjectRef        `json:"rendition"`
	Brief      *objectstore.ObjectRef       `json:"brief,omitempty"`
	Bundle     *objectstore.ObjectRef       `json:"bundle,omitempty"`
}

// Resolve returns, in order, the block each reference shows in projectID,
// binding each attachment on its first resolve, at most maximumParallelBinds
// at a time.
func (store *RenditionStore) Resolve(ctx context.Context, projectID string, references []conversation.AttachmentReference) ([]json.RawMessage, error) {
	blocks := make([]json.RawMessage, len(references))
	var pending []int
	for index, reference := range references {
		if block, found := store.cache.get(cacheKey(projectID, reference)); found {
			blocks[index] = block
		} else {
			pending = append(pending, index)
		}
	}
	bindings := map[string]*binding{}
	var lock sync.Mutex
	group, binds := errgroup.WithContext(ctx)
	group.SetLimit(maximumParallelBinds)
	for _, index := range pending {
		attachment := references[index].Attachment
		lock.Lock()
		_, started := bindings[attachment.ID]
		bindings[attachment.ID] = nil
		lock.Unlock()
		if started {
			continue
		}
		group.Go(func() error {
			bound, isBound, err := store.bind(binds, projectID, attachment)
			if err != nil || !isBound {
				return err
			}
			lock.Lock()
			bindings[attachment.ID] = &bound
			lock.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	for _, index := range pending {
		reference := references[index]
		bound := bindings[reference.Attachment.ID]
		if bound == nil {
			blocks[index] = conversation.ExpiredAttachmentBlock(reference.Attachment)
			continue
		}
		block, err := store.read(ctx, *bound, reference)
		if err != nil {
			return nil, err
		}
		store.cache.put(cacheKey(projectID, reference), block)
		blocks[index] = block
	}
	return blocks, nil
}

// cacheKey names the block reference shows in projectID.
func cacheKey(projectID string, reference conversation.AttachmentReference) string {
	rendition, _ := reference.Rendition()
	return strings.Join([]string{projectID, reference.Attachment.ID, reference.Variant.String(), rendition.Digest}, "/")
}

// read reads the bound copy of the rendition reference shows by its exact
// version and checks it is the referenced one.
func (store *RenditionStore) read(ctx context.Context, bound binding, reference conversation.AttachmentReference) (json.RawMessage, error) {
	rendition, isShown := reference.Rendition()
	copied := &bound.Rendition
	if reference.Variant == conversation.AttachmentVariantBrief {
		copied = bound.Brief
	}
	if !isShown || copied == nil || copied.Digest != rendition.Digest {
		return nil, fmt.Errorf("the %s reference of attachment %s names no rendition its binding holds", reference.Variant, reference.Attachment.ID)
	}
	block, err := store.objects.Get(ctx, *copied)
	if err != nil {
		return nil, fmt.Errorf("read the %s rendition of attachment %s: %w", reference.Variant, reference.Attachment.ID, err)
	}
	return block, nil
}

// bind returns the manifest of attachment in projectID, copying its staged
// objects and writing the manifest when it has none; false when the
// attachment is not bound and a staged object is gone. Each copy reads before
// it writes, so a retried or concurrent bind writes the same objects.
func (store *RenditionStore) bind(ctx context.Context, projectID string, attachment conversation.AttachmentRef) (binding, bool, error) {
	if bound, found, err := store.manifest(ctx, projectID, attachment.ID); err != nil || found {
		return bound, found, err
	}
	prefix, staged := boundPrefix(projectID, attachment.ID), stagingPrefix(attachment.TeamSpaceID, attachment.ID)
	expired := false
	copyObject := func(role, mediaType string, object *conversation.AttachmentObject) (*objectstore.ObjectRef, error) {
		if object == nil || expired {
			return nil, nil
		}
		source := objectstore.ObjectRef{Key: store.objects.FullKey(objectKey(staged, role, object.Digest)), Version: object.Version, Digest: object.Digest, MediaType: mediaType}
		copied, err := store.objects.CopyVersion(ctx, source, objectKey(prefix, role, object.Digest))
		switch {
		case errors.Is(err, objectstore.ErrNotFound):
			expired = true
			return nil, nil
		case err != nil:
			return nil, fmt.Errorf("copy the %s of attachment %s to Project %s: %w", role, attachment.ID, projectID, err)
		}
		return &copied, nil
	}
	// A JSON file's original is its redacted copy; the raw upload is gone.
	originalRole, originalType := roleOriginal, attachment.MediaType
	if attachment.Kind == conversation.AttachmentKindJSON {
		originalRole, originalType = roleRedacted, objectstore.MediaTypeJSON
	}
	var bound binding
	original, err := copyObject(originalRole, originalType, &attachment.Original)
	if err != nil {
		return binding{}, false, err
	}
	rendition, err := copyObject(roleRendition, objectstore.MediaTypeRendition, &attachment.Rendition.AttachmentObject)
	if err != nil {
		return binding{}, false, err
	}
	var brief *conversation.AttachmentObject
	if attachment.Brief != nil {
		brief = &attachment.Brief.AttachmentObject
	}
	if bound.Brief, err = copyObject(roleBrief, objectstore.MediaTypeRendition, brief); err != nil {
		return binding{}, false, err
	}
	if bound.Bundle, err = copyObject(roleBundle, objectstore.MediaTypeJSON, attachment.Bundle); err != nil {
		return binding{}, false, err
	}
	if expired {
		return binding{}, false, nil
	}
	bound.Label, bound.Original, bound.Rendition = attachment.Label(), original, *rendition
	if attachment.JSON != nil {
		bound.Redactions = attachment.JSON.Redactions
	}
	if _, err := store.objects.PutJSON(ctx, prefix+"manifest.json", bound); err != nil {
		return binding{}, false, fmt.Errorf("write the binding of attachment %s: %w", attachment.ID, err)
	}
	return bound, true, nil
}

// boundPrefix is the prefix of the Project's copy of an attachment:
// <projectID>/attachments/<attachmentID>/.
func boundPrefix(projectID, attachmentID string) string {
	return projectID + "/attachments/" + attachmentID + "/"
}

// manifest reads the binding of an attachment projectID bound; false when it
// bound none of that ID.
func (store *RenditionStore) manifest(ctx context.Context, projectID, attachmentID string) (binding, bool, error) {
	var bound binding
	_, err := store.objects.GetCurrentJSON(ctx, boundPrefix(projectID, attachmentID)+"manifest.json", &bound)
	switch {
	case errors.Is(err, objectstore.ErrNotFound):
		return binding{}, false, nil
	case err != nil:
		return binding{}, false, fmt.Errorf("read the binding of attachment %s: %w", attachmentID, err)
	}
	return bound, true, nil
}

// Download presigns the browser's GET of the Project's copy of an attachment
// projectID bound, valid for five minutes: an image's thumbnail inline, or a
// download named as the file, a JSON file's redacted copy; false when the
// Project bound none of that ID.
func (store *RenditionStore) Download(ctx context.Context, projectID, attachmentID string, inline bool) (objectstore.PresignedRequest, bool, error) {
	bound, found, err := store.manifest(ctx, projectID, attachmentID)
	if err != nil || !found || bound.Original == nil {
		return objectstore.PresignedRequest{}, false, err
	}
	download, err := store.objects.PresignGet(ctx, *bound.Original, bound.Label.Name, inline, downloadValidity)
	return download, err == nil, err
}

// AttachmentSection is one section of a document's sections bundle: its ID
// (s1 to s512), its heading path and its text.
type AttachmentSection struct {
	ID      string `json:"id"`
	Heading string `json:"heading"`
	Text    string `json:"text"`
}

// sectionsBundle is the bundle object a document's preparation stages: every
// section's text in order, at most 512 sections and 2 MiB.
type sectionsBundle struct {
	Sections []AttachmentSection `json:"sections"`
}

// BoundAttachment is what a read finds of an attachment its Project bound:
// its label, a JSON file's redacted copy and a document's sections.
type BoundAttachment struct {
	File     conversation.AttachmentFile
	Sections []AttachmentSection
}

// ReadBound reads an attachment that projectID bound, with a document's
// sections; false when the Project bound none of that ID. It never binds.
func (store *RenditionStore) ReadBound(ctx context.Context, projectID, attachmentID string) (BoundAttachment, bool, error) {
	return store.readBound(ctx, projectID, attachmentID, true)
}

// readBound reads an attachment that projectID bound, its sections only when
// withSections. Only a JSON file's original is read: the tools read no other.
func (store *RenditionStore) readBound(ctx context.Context, projectID, attachmentID string, withSections bool) (BoundAttachment, bool, error) {
	bound, found, err := store.manifest(ctx, projectID, attachmentID)
	if err != nil || !found {
		return BoundAttachment{}, false, err
	}
	read := BoundAttachment{File: conversation.AttachmentFile{Label: bound.Label, Redactions: bound.Redactions}}
	if bound.Original != nil && bound.Label.Kind == conversation.AttachmentKindJSON {
		if read.File.Data, err = store.objects.Get(ctx, *bound.Original); err != nil {
			return BoundAttachment{}, false, fmt.Errorf("read the file of attachment %s: %w", attachmentID, err)
		}
		read.File.Digest = bound.Original.Digest
	}
	if bound.Bundle != nil && withSections {
		var bundle sectionsBundle
		if err = store.objects.GetJSON(ctx, *bound.Bundle, &bundle); err != nil {
			return BoundAttachment{}, false, fmt.Errorf("read the sections of attachment %s: %w", attachmentID, err)
		}
		read.Sections = bundle.Sections
	}
	return read, true, nil
}

// BoundFile reads, for place_attachment, the file of an attachment projectID
// bound: its label, and a JSON file's redacted copy; false when the Project
// bound none of that ID.
func (store *RenditionStore) BoundFile(ctx context.Context, projectID, attachmentID string) (conversation.AttachmentFile, bool, error) {
	read, found, err := store.readBound(ctx, projectID, attachmentID, false)
	return read.File, found, err
}

// renditionCache keeps the replica's most recently resolved blocks within
// limit bytes, evicting the least recently used first. A block enters only
// after its Project copy was read and verified, and callers never modify one.
type renditionCache struct {
	mu             sync.Mutex
	limit, size    int64
	entries        map[string]*cachedBlock
	newest, oldest *cachedBlock
}

// cachedBlock is one cached block, linked from the newest to the oldest.
type cachedBlock struct {
	key          string
	block        json.RawMessage
	newer, older *cachedBlock
}

func (cache *renditionCache) get(key string) (json.RawMessage, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cached, found := cache.entries[key]
	if !found {
		return nil, false
	}
	cache.unlink(cached)
	cache.pushNewest(cached)
	return cached.block, true
}

func (cache *renditionCache) put(key string, block json.RawMessage) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, found := cache.entries[key]; found || int64(len(block)) > cache.limit {
		return
	}
	cached := &cachedBlock{key: key, block: block}
	cache.entries[key] = cached
	cache.pushNewest(cached)
	cache.size += int64(len(block))
	for cache.size > cache.limit {
		evicted := cache.oldest
		cache.unlink(evicted)
		delete(cache.entries, evicted.key)
		cache.size -= int64(len(evicted.block))
	}
}

func (cache *renditionCache) pushNewest(cached *cachedBlock) {
	cached.older, cached.newer = cache.newest, nil
	if cache.newest != nil {
		cache.newest.newer = cached
	}
	cache.newest = cached
	if cache.oldest == nil {
		cache.oldest = cached
	}
}

func (cache *renditionCache) unlink(cached *cachedBlock) {
	if cached.newer != nil {
		cached.newer.older = cached.older
	} else {
		cache.newest = cached.older
	}
	if cached.older != nil {
		cached.older.newer = cached.newer
	} else {
		cache.oldest = cached.newer
	}
	cached.newer, cached.older = nil, nil
}
