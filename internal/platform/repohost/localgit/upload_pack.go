// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package localgit

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/revlist"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/superdurable/superversev2/internal/comm"
)

// maximumUploadRequestBytes bounds one decoded fetch request, which lists wants
// and haves.
const maximumUploadRequestBytes = 8 << 20

// maximumUploadLines bounds the wants and the haves of one request.
const maximumUploadLines = 100_000

// advertise answers the ref discovery of a fetch or a push: the service line,
// then the refs of the repository and the capabilities this host implements.
func (h *Host) advertise(w http.ResponseWriter, r *http.Request, found repository) {
	service := gitOperation(r.URL.Query().Get("service"))
	if service != operationUploadPack && service != operationReceivePack {
		http.Error(w, "only the smart Git protocol is served", http.StatusForbidden)
		return
	}
	if service == operationReceivePack && found.Archived {
		http.Error(w, "the repository is archived", http.StatusForbidden)
		return
	}
	_, storage, err := found.open()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	defer func() { _ = storage.Close() }()
	advertised, err := advertisedReferences(storage, service)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-"+string(service)+"-advertisement")
	if err = advertised.Encode(w); err != nil {
		h.logger.WarnContext(r.Context(), "local Git host could not write an advertisement", "error", err)
	}
}

// advertisedReferences lists the branches and tags, and for a fetch HEAD and its
// symbolic target, with the capabilities of the service. A fetch may ask for any
// object the repository has, so a client can fetch an exact commit. A capability
// that cannot be declared is a programming error.
func advertisedReferences(storage *filesystem.Storage, service gitOperation) (*packp.AdvRefs, error) {
	advertised := packp.NewAdvRefs()
	advertised.Prefix = [][]byte{[]byte("# service=" + string(service)), pktline.Flush}
	comm.MustDo(advertised.Capabilities.Set(capability.Agent, agentName))
	references, err := storage.IterReferences()
	if err != nil {
		return nil, err
	}
	err = references.ForEach(func(reference *plumbing.Reference) error {
		name := reference.Name()
		if reference.Type() != plumbing.HashReference || (!name.IsBranch() && !name.IsTag()) {
			return nil
		}
		advertised.References[name.String()] = reference.Hash()
		if name.IsTag() {
			if peeled, ok := peelTag(storage, reference.Hash()); ok {
				advertised.Peeled[name.String()] = peeled
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if service == operationReceivePack {
		comm.MustDo(advertised.Capabilities.Set(capability.ReportStatus))
		comm.MustDo(advertised.Capabilities.Set(capability.OFSDelta))
		return advertised, nil
	}
	comm.MustDo(advertised.Capabilities.Set(capability.MultiACKDetailed))
	comm.MustDo(advertised.Capabilities.Set(capability.AllowTipSHA1InWant))
	comm.MustDo(advertised.Capabilities.Set(capability.AllowReachableSHA1InWant))
	head, err := storage.Reference(plumbing.HEAD)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return advertised, nil
	}
	if err != nil {
		return nil, err
	}
	if target, found := advertised.References[head.Target().String()]; found && head.Type() == plumbing.SymbolicReference {
		advertised.Head = &target
		comm.MustDo(advertised.Capabilities.Add(capability.SymRef, "HEAD:"+head.Target().String()))
	}
	return advertised, nil
}

// peelTag returns the object an annotated tag finally points to.
func peelTag(storage *filesystem.Storage, hash plumbing.Hash) (plumbing.Hash, bool) {
	tag, err := object.GetTag(storage, hash)
	if err != nil {
		return plumbing.ZeroHash, false
	}
	if peeled, ok := peelTag(storage, tag.Target); ok {
		return peeled, true
	}
	return tag.Target, true
}

// uploadRequest is one decoded request of the stateless fetch protocol: what the
// client wants, what it already has and whether it is done negotiating.
type uploadRequest struct {
	wants        []plumbing.Hash
	capabilities map[capability.Capability]bool
	haves        []plumbing.Hash
	done         bool
}

// decodeUploadRequest reads wants with the capabilities of the first, a flush,
// then haves until done or the flush that ends a negotiation round. It refuses
// what this host does not advertise: shallow requests and filters.
func decodeUploadRequest(source io.Reader) (uploadRequest, error) {
	request := uploadRequest{capabilities: map[capability.Capability]bool{}}
	scanner := pktline.NewScanner(source)
	inWants := true
	for scanner.Scan() {
		line := strings.TrimSuffix(string(scanner.Bytes()), "\n")
		if len(request.wants)+len(request.haves) > maximumUploadLines {
			return request, errors.New("the request lists too many objects")
		}
		switch {
		case line == "" && inWants:
			inWants = false
		case line == "":
			return request, nil
		case inWants && strings.HasPrefix(line, "want "):
			hash, rest, err := parseObjectLine(strings.TrimPrefix(line, "want "))
			if err != nil {
				return request, err
			}
			if len(request.wants) == 0 {
				for _, name := range strings.Fields(rest) {
					request.capabilities[capability.Capability(strings.SplitN(name, "=", 2)[0])] = true
				}
			}
			request.wants = append(request.wants, hash)
		case !inWants && strings.HasPrefix(line, "have "):
			hash, _, err := parseObjectLine(strings.TrimPrefix(line, "have "))
			if err != nil {
				return request, err
			}
			request.haves = append(request.haves, hash)
		case !inWants && line == "done":
			request.done = true
			return request, nil
		default:
			return request, fmt.Errorf("unsupported request line %q", line)
		}
	}
	return request, scanner.Err()
}

// parseObjectLine reads a 40-digit object ID and returns what follows it.
func parseObjectLine(line string) (plumbing.Hash, string, error) {
	if len(line) < 40 {
		return plumbing.ZeroHash, "", errors.New("malformed object ID")
	}
	if !plumbing.IsHash(line[:40]) {
		return plumbing.ZeroHash, "", errors.New("malformed object ID")
	}
	return plumbing.NewHash(line[:40]), strings.TrimSpace(line[40:]), nil
}

// uploadPack answers one request of the stateless fetch protocol. Until the
// client says done, it only acknowledges the objects both sides have. After
// done it sends a pack of what the wants need and the haves do not cover,
// without deltas, so the pack never refers to an object outside itself.
func (h *Host) uploadPack(w http.ResponseWriter, r *http.Request, found repository) {
	body, err := requestBody(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer func() { _ = body.Close() }()
	request, err := decodeUploadRequest(io.LimitReader(body, maximumUploadRequestBytes))
	if err != nil {
		http.Error(w, "malformed fetch request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(request.wants) == 0 {
		http.Error(w, "the fetch request wants nothing", http.StatusBadRequest)
		return
	}
	_, storage, err := found.open()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	defer func() { _ = storage.Close() }()

	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	encoder := pktline.NewEncoder(w)
	common := commonObjects(storage, request.haves)
	var objects []plumbing.Hash
	if request.done {
		if objects, err = revlist.Objects(storage, request.wants, common); err != nil {
			// A pkt-line ERR before any acknowledgement is how Git reports a refusal.
			_ = encoder.Encodef("ERR %s\n", sanitizeLine(err))
			return
		}
	}
	for _, line := range acknowledgements(request, common) {
		_ = encoder.EncodeString(line + "\n")
	}
	if !request.done {
		return
	}
	if _, err = packfile.NewEncoder(w, storage, false).Encode(objects, 0); err != nil {
		h.logger.WarnContext(r.Context(), "local Git host could not write a pack", "error", err)
	}
}

// acknowledgements answer the haves of a request as Git's upload-pack does
// without the ready and no-done refinements. A client that negotiates with
// multi_ack_detailed hears each common object in every round and the last one
// when it is done; any other client hears the first common object once.
func acknowledgements(request uploadRequest, common []plumbing.Hash) []string {
	detailed := request.capabilities[capability.MultiACKDetailed]
	switch {
	case detailed && !request.done:
		lines := make([]string, 0, len(common)+1)
		for _, hash := range common {
			lines = append(lines, "ACK "+hash.String()+" common")
		}
		return append(lines, "NAK")
	case len(common) == 0:
		return []string{"NAK"}
	case detailed:
		return []string{"ACK " + common[len(common)-1].String()}
	default:
		return []string{"ACK " + common[0].String()}
	}
}

// commonObjects are the objects the client has that the repository has too, in
// the order the client listed them.
func commonObjects(storage *filesystem.Storage, haves []plumbing.Hash) []plumbing.Hash {
	var common []plumbing.Hash
	seen := map[plumbing.Hash]bool{}
	for _, have := range haves {
		if seen[have] {
			continue
		}
		seen[have] = true
		if storage.HasEncodedObject(have) == nil {
			common = append(common, have)
		}
	}
	return common
}

// requestBody returns the request body, decompressed when the client gzipped it
// as Git does for a large fetch request.
func requestBody(w http.ResponseWriter, r *http.Request) (io.ReadCloser, error) {
	body := http.MaxBytesReader(w, r.Body, maximumPushBytes)
	if r.Header.Get("Content-Encoding") != "gzip" {
		return body, nil
	}
	reader, err := gzip.NewReader(body)
	if err != nil {
		return nil, errors.New("the request body is not valid gzip")
	}
	return reader, nil
}

// sanitizeLine makes an error fit one pkt-line of a protocol message.
func sanitizeLine(err error) string {
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(err.Error())
}
