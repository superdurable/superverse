// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package localgit

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// The limits of the objects one push may store. go-git's parser holds every
// object it stores, and the base and target of every delta, in memory, so the
// limits are checked on the received pack before the parser reads it.
const (
	// maximumObjectBytes bounds one object, inflated, as GitHub bounds a file.
	maximumObjectBytes = 100 << 20
	// maximumPushInflatedBytes bounds the inflated objects of one push together.
	maximumPushInflatedBytes = 1 << 30
	// maximumPushObjects bounds the objects of one push, which the parser
	// indexes in memory.
	maximumPushObjects = 100_000
)

// pushRefusedError is a pack that the host refuses by its own rule; its status
// is what the pusher reads.
type pushRefusedError struct{ status pushStatus }

func (e pushRefusedError) Error() string { return string(e.status) }

// checkPackLimits reads the header of every object of a received pack, and the
// start of every delta's data, and refuses the pack before anything is
// allocated for its objects: an object that inflates to more than
// maximumObjectBytes, a delta whose target would, more than maximumPushObjects
// objects, or more than maximumPushInflatedBytes together. It leaves the file
// at its start, for the parser.
func checkPackLimits(pack *os.File) error {
	scanner := packfile.NewScanner(pack)
	_, count, err := scanner.Header()
	if err != nil {
		return err
	}
	if count > maximumPushObjects {
		return pushRefusedError{statusTooManyObjects}
	}
	var total int64
	var header *packfile.ObjectHeader
	for range count {
		if header, err = scanner.NextObjectHeader(); err != nil {
			return err
		}
		// The declared size bounds what the object inflates to; a delta's is the
		// size of its instructions.
		size := header.Length
		if size > maximumObjectBytes {
			return pushRefusedError{statusObjectTooLarge}
		}
		if header.Type.IsDelta() {
			if size, err = deltaTargetSize(scanner); err != nil {
				return err
			}
			if size > maximumObjectBytes {
				return pushRefusedError{statusObjectTooLarge}
			}
		}
		total += size
		if total > maximumPushInflatedBytes {
			return pushRefusedError{statusPushTooLarge}
		}
	}
	_, err = pack.Seek(0, io.SeekStart)
	return err
}

// errMalformedDelta reports delta data that does not start with the sizes of
// its base and its target.
var errMalformedDelta = errors.New("the delta does not start with its sizes")

// deltaTargetSize reads the size of the object a delta produces: its data
// starts with the size of its base and then the size of its target, each a
// base-128 varint. The rest of the data is inflated and discarded.
func deltaTargetSize(scanner *packfile.Scanner) (int64, error) {
	var start deltaStart
	if _, _, err := scanner.NextObject(&start); err != nil {
		return 0, err
	}
	_, baseLength := binary.Uvarint(start.data)
	if baseLength <= 0 {
		return 0, errMalformedDelta
	}
	target, targetLength := binary.Uvarint(start.data[baseLength:])
	switch {
	case targetLength <= 0:
		return 0, errMalformedDelta
	case target > math.MaxInt64:
		return math.MaxInt64, nil
	default:
		return int64(target), nil
	}
}

// deltaStart keeps the first bytes of a delta's data, which hold its two sizes,
// and discards the rest.
type deltaStart struct{ data []byte }

func (s *deltaStart) Write(p []byte) (int, error) {
	if room := 2*binary.MaxVarintLen64 - len(s.data); room > 0 {
		s.data = append(s.data, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// ingestPack stores the objects of a pack that passed checkPackLimits. The
// generic parser, not the pack writer of the storage, resolves a thin pack's
// deltas against objects the repository already has, and it reads the deltas
// again from the file instead of keeping them. It still holds an object, or a
// delta's base and target, in memory, which is why the host handles the
// objects of one push at a time.
func ingestPack(storage *filesystem.Storage, pack *os.File) error {
	parser, err := packfile.NewParserWithStorage(packfile.NewScanner(pack), storage)
	if err != nil {
		return err
	}
	_, err = parser.Parse()
	return err
}

// errMissingObject reports an object that the history of a push reaches and
// the repository does not have.
var errMissingObject = errors.New("a reachable object is missing")

// connectivity checks that the history a push adds is complete before any
// reference points to it. It does not walk below the commits that the
// repository's references reach: they passed this check when they were pushed.
type connectivity struct {
	storage *filesystem.Storage
	// complete holds the commits the references reach and every object of a
	// history a check found complete.
	complete map[plumbing.Hash]bool
}

// reachable is an object of a pushed history and the type its referrer names;
// AnyObject when the referrer is a reference.
type reachable struct {
	hash plumbing.Hash
	kind plumbing.ObjectType
}

// newConnectivity marks the commits the repository's branches and tags reach,
// through their tags and parents.
func newConnectivity(storage *filesystem.Storage) (*connectivity, error) {
	references, err := storage.IterReferences()
	if err != nil {
		return nil, err
	}
	var pending []plumbing.Hash
	err = references.ForEach(func(reference *plumbing.Reference) error {
		if reference.Type() == plumbing.HashReference && (reference.Name().IsBranch() || reference.Name().IsTag()) {
			pending = append(pending, reference.Hash())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	c := &connectivity{storage: storage, complete: map[plumbing.Hash]bool{}}
	var decoded object.Object
	for len(pending) > 0 {
		hash := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if c.complete[hash] {
			continue
		}
		c.complete[hash] = true
		if decoded, err = c.decode(hash); err != nil {
			return nil, fmt.Errorf("read the history the references reach: %w", err)
		}
		switch value := decoded.(type) {
		case *object.Commit:
			pending = append(pending, value.ParentHashes...)
		case *object.Tag:
			pending = append(pending, value.Target)
		}
	}
	return c, nil
}

// check reports errMissingObject when an object that tip reaches is missing:
// a commit's tree and parents, a tree's trees and blobs, a tag's target. It
// decodes the commits, trees and tags and checks that each has the type its
// referrer names; a blob only has to exist. A gitlink names a commit of
// another repository and is not followed.
func (c *connectivity) check(tip plumbing.Hash) error {
	walked := map[plumbing.Hash]bool{}
	pending := []reachable{{hash: tip, kind: plumbing.AnyObject}}
	for len(pending) > 0 {
		next := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if c.complete[next.hash] || walked[next.hash] {
			continue
		}
		walked[next.hash] = true
		if next.kind == plumbing.BlobObject {
			if err := c.storage.HasEncodedObject(next.hash); err != nil {
				return objectError(next.hash, err)
			}
			continue
		}
		decoded, err := c.decode(next.hash)
		if err != nil {
			return err
		}
		if next.kind != plumbing.AnyObject && decoded.Type() != next.kind {
			return fmt.Errorf("object %s is a %s, not a %s", next.hash, decoded.Type(), next.kind)
		}
		switch value := decoded.(type) {
		case *object.Commit:
			pending = append(pending, reachable{hash: value.TreeHash, kind: plumbing.TreeObject})
			for _, parent := range value.ParentHashes {
				pending = append(pending, reachable{hash: parent, kind: plumbing.CommitObject})
			}
		case *object.Tree:
			for _, entry := range value.Entries {
				switch entry.Mode {
				case filemode.Submodule:
				case filemode.Dir:
					pending = append(pending, reachable{hash: entry.Hash, kind: plumbing.TreeObject})
				default:
					pending = append(pending, reachable{hash: entry.Hash, kind: plumbing.BlobObject})
				}
			}
		case *object.Tag:
			pending = append(pending, reachable{hash: value.Target, kind: value.TargetType})
		}
	}
	for hash := range walked {
		c.complete[hash] = true
	}
	return nil
}

// decode reads and decodes one object; a missing one is errMissingObject.
func (c *connectivity) decode(hash plumbing.Hash) (object.Object, error) {
	encoded, err := c.storage.EncodedObject(plumbing.AnyObject, hash)
	if err != nil {
		return nil, objectError(hash, err)
	}
	return object.DecodeObject(c.storage, encoded)
}

// objectError names the object a read failed for, and marks a missing one.
func objectError(hash plumbing.Hash, err error) error {
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return fmt.Errorf("object %s: %w", hash, errMissingObject)
	}
	return fmt.Errorf("read object %s: %w", hash, err)
}
