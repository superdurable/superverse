// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package localgit

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// maximumPushBytes bounds the body of one push.
const maximumPushBytes = 512 << 20

// maximumCommandBytes bounds the reference updates that precede the pack of a
// push, which the host decodes into memory.
const maximumCommandBytes = 1 << 20

// pushStatus is what the report of a push says about its pack or about one of
// its reference updates: ok, or the reason Git shows the pusher. Every status
// is one of these constants, so no error text reaches the pusher.
type pushStatus string

const (
	statusOK pushStatus = "ok"
	// statusUnpackFailed refuses a pack that cannot be read or stored; only the
	// log keeps the cause.
	statusUnpackFailed pushStatus = "the pack cannot be read or stored"
	// The limits of checkPackLimits.
	statusObjectTooLarge pushStatus = "an object is larger than 100 MiB"
	statusPushTooLarge   pushStatus = "the objects of the push are larger than 1 GiB together"
	statusTooManyObjects pushStatus = "the push holds more than 100000 objects"
	// statusUnpackerError refuses every reference update of a push whose pack
	// was refused, as Git's own receive-pack does.
	statusUnpackerError       pushStatus = "unpacker error"
	statusInvalidName         pushStatus = "the reference name is not valid"
	statusNotBranchOrTag      pushStatus = "only branches and tags can be pushed"
	statusDeleteRefused       pushStatus = "references cannot be deleted"
	statusReferenceUnreadable pushStatus = "the reference cannot be read"
	statusReferenceExists     pushStatus = "the reference already exists"
	statusReferenceChanged    pushStatus = "the reference changed since it was fetched"
	statusTagMoved            pushStatus = "a tag cannot be moved"
	// statusMissingObjects is Git's own reason for a history that reaches an
	// object the repository does not have.
	statusMissingObjects     pushStatus = "missing necessary objects"
	statusHistoryUnreadable  pushStatus = "the pushed history cannot be read"
	statusBranchNotCommit    pushStatus = "a branch must point to a commit"
	statusPreviousUnreadable pushStatus = "the previous commit cannot be read"
	statusNotConnected       pushStatus = "the new commit does not connect to the previous one"
	statusNonFastForward     pushStatus = "non-fast-forward"
	statusUpdateFailed       pushStatus = "the reference could not be updated"
)

// errArchived refuses a push to a repository that was archived.
var errArchived = errors.New("the repository is archived")

// receivePack accepts a push: the reference updates, then the pack of new
// objects. It stores a pack Git sent thin, completed from the repository's own
// objects, and it only grows a repository: a branch moves forward from the
// commit the client saw, a tag is never moved, and nothing is deleted. A
// repository that was archived refuses the push.
func (h *Host) receivePack(w http.ResponseWriter, r *http.Request, found repository) {
	body := http.MaxBytesReader(w, r.Body, maximumPushBytes)
	defer func() { _ = body.Close() }()
	// The decoder reads the reference updates line by line and leaves the pack
	// in the body.
	commands := &io.LimitedReader{R: body, N: maximumCommandBytes}
	request := packp.NewReferenceUpdateRequest()
	if err := request.Decode(commands); err != nil {
		if commands.N == 0 {
			http.Error(w, "the push updates too many references", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "malformed push request: "+err.Error(), http.StatusBadRequest)
		return
	}
	// The pack is read in full before the repository's lock is taken, so a slow
	// client never holds the lock.
	pack, err := h.store.receive(body)
	var tooLarge *http.MaxBytesError
	var local *fs.PathError
	switch {
	case errors.As(err, &tooLarge):
		http.Error(w, "the push is larger than "+strconv.Itoa(maximumPushBytes>>20)+" MiB", http.StatusRequestEntityTooLarge)
		return
	case errors.As(err, &local):
		h.fail(w, r, err)
		return
	case err != nil:
		http.Error(w, "the pushed pack cannot be read", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = pack.Close()
		_ = os.Remove(pack.Name())
	}()
	report, err := h.push(r.Context(), found, request, pack)
	switch {
	case errors.Is(err, errArchived):
		http.Error(w, errArchived.Error(), http.StatusForbidden)
		return
	case r.Context().Err() != nil:
		return // The client is gone.
	case err != nil:
		h.fail(w, r, err)
		return
	}
	if !request.Capabilities.Supports(capability.ReportStatus) {
		return
	}
	w.Header().Set("Content-Type", "application/x-git-receive-pack-result")
	if err = report.Encode(w); err != nil {
		h.logger.WarnContext(r.Context(), "local Git host could not write a push report", "error", err)
	}
}

// push applies a received push and returns its report. The limits of the pack
// are checked without the repository's lock; under it, the pack is stored and
// each reference update applied.
func (h *Host) push(ctx context.Context, found repository, request *packp.ReferenceUpdateRequest, pack *os.File) (*packp.ReportStatus, error) {
	writesPack := slices.ContainsFunc(request.Commands, func(command *packp.Command) bool { return command.Action() != packp.Delete })
	if writesPack {
		if err := checkPackLimits(pack); err != nil {
			return h.refusedPack(ctx, found, request, err), nil
		}
	}
	// One push at a time per repository, and none after the archive took effect.
	release, err := h.store.lock(ctx, found.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	current, err := h.store.find(found.FullName)
	if err != nil {
		return nil, err
	}
	if current.Archived {
		return nil, errArchived
	}
	opened, storage, err := current.open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = storage.Close() }()
	// go-git holds whole objects in memory while it stores and walks them, so the
	// host handles the objects of one push at a time: the memory of concurrent
	// pushes does not add up.
	select {
	case h.pushing <- struct{}{}:
		defer func() { <-h.pushing }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	target := pushTarget{repository: current, opened: opened, storage: storage}
	if writesPack {
		if err = ingestPack(storage, pack); err != nil {
			return h.refusedPack(ctx, found, request, err), nil
		}
		if target.connected, err = newConnectivity(storage); err != nil {
			return nil, err
		}
	}
	report := packp.NewReportStatus()
	report.UnpackStatus = string(statusOK)
	for _, command := range request.Commands {
		status, cause := target.apply(command)
		switch {
		case status == statusOK:
			h.logger.InfoContext(ctx, "local Git host accepted a push", slog.String("repository", current.FullName), slog.String("ref", command.Name.String()), slog.String("commit", command.New.String()))
		case cause != nil:
			h.logger.WarnContext(ctx, "local Git host refused a reference update", slog.String("repository", current.FullName), slog.String("ref", command.Name.String()), slog.String("status", string(status)), slog.Any("error", cause))
		}
		report.CommandStatuses = append(report.CommandStatuses, &packp.CommandStatus{ReferenceName: command.Name, Status: string(status)})
	}
	return report, nil
}

// refusedPack is the report of a push whose pack is refused: the status of a
// limit, or a fixed reason for a pack that cannot be read or stored, whose
// cause can name the server's paths and only the log keeps. Every reference
// update of the push is refused with it.
func (h *Host) refusedPack(ctx context.Context, found repository, request *packp.ReferenceUpdateRequest, err error) *packp.ReportStatus {
	h.logger.WarnContext(ctx, "local Git host refused a pushed pack", slog.String("repository", found.FullName), slog.Any("error", err))
	report := packp.NewReportStatus()
	report.UnpackStatus = string(statusUnpackFailed)
	var refused pushRefusedError
	if errors.As(err, &refused) {
		report.UnpackStatus = string(refused.status)
	}
	for _, command := range request.Commands {
		report.CommandStatuses = append(report.CommandStatuses, &packp.CommandStatus{ReferenceName: command.Name, Status: string(statusUnpackerError)})
	}
	return report
}

// pushTarget is the repository a push updates, opened under its lock, with the
// check of the history the push adds.
type pushTarget struct {
	repository
	opened    *git.Repository
	storage   *filesystem.Storage
	connected *connectivity
}

// apply applies one reference update and returns statusOK or the reason Git
// shows the pusher, with the cause of a failure for the log. A name Git does
// not allow, such as refs/heads/a..b, a .lock suffix or a branch that starts
// with a dash, is refused first. The history the update adds must be complete
// before the reference points to it: since a reference is never deleted or
// moved back, a missing object would break the repository for good.
func (p pushTarget) apply(command *packp.Command) (pushStatus, error) {
	name := command.Name
	if err := name.Validate(); err != nil {
		return statusInvalidName, err
	}
	if !name.IsBranch() && !name.IsTag() {
		return statusNotBranchOrTag, nil
	}
	if command.Action() == packp.Delete {
		return statusDeleteRefused, nil
	}
	// Under the repository's lock, this read and the write below are one
	// compare-and-swap.
	current, exists, err := p.reference(p.storage, name)
	if err != nil {
		return statusReferenceUnreadable, err
	}
	switch {
	case command.Action() == packp.Create && exists:
		return statusReferenceExists, nil
	case command.Action() == packp.Update && (!exists || current != command.Old):
		return statusReferenceChanged, nil
	case command.Action() == packp.Update && name.IsTag():
		return statusTagMoved, nil
	}
	if err = p.connected.check(command.New); errors.Is(err, errMissingObject) {
		return statusMissingObjects, err
	}
	if err != nil {
		return statusHistoryUnreadable, err
	}
	newCommit, err := p.opened.CommitObject(command.New)
	if err != nil && name.IsBranch() {
		return statusBranchNotCommit, err
	}
	if command.Action() == packp.Update {
		oldCommit, oldErr := p.opened.CommitObject(command.Old)
		if oldErr != nil {
			return statusPreviousUnreadable, oldErr
		}
		forward, ancestorErr := oldCommit.IsAncestor(newCommit)
		if ancestorErr != nil {
			return statusNotConnected, ancestorErr
		}
		if !forward {
			return statusNonFastForward, nil
		}
	}
	if err = p.setReference(name, command.New); err != nil {
		return statusUpdateFailed, err
	}
	return statusOK, nil
}
