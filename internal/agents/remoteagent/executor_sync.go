// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

var errDeliveryRetentionMoved = errors.New("remote retention moved while assembling delivery")

// Read admission covers assembly and the response write; callers release it
// and clear payloads before activity waits. The Bridge owns the delivery cursor.
func (e *Endpoint) readDelivery(ctx context.Context, token string, request bridgeapi.SyncRequest) (bridgeapi.SyncResponse, func(), error) {
	release, err := e.reads.acquire(ctx)
	if err != nil {
		return bridgeapi.SyncResponse{}, nil, readAdmissionError(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		response, err := e.readDeliveryOnce(ctx, token, request)
		if err == nil {
			return response, release, nil
		}
		if !errors.Is(err, errDeliveryRetentionMoved) {
			release()
			return bridgeapi.SyncResponse{}, nil, err
		}
	}
	release()
	return bridgeapi.SyncResponse{}, nil, errDeliveryRetentionMoved
}

func (e *Endpoint) readDeliveryOnce(ctx context.Context, token string, request bridgeapi.SyncRequest) (bridgeapi.SyncResponse, error) {
	cursor := conversation.Sequence(request.After)
	var combined session.RemoteRead
	var entries []conversation.SequencedEntry
	bytes := 2
	for {
		access, err := e.service.authenticate(ctx, token)
		if err != nil {
			return bridgeapi.SyncResponse{}, err
		}
		if err = e.service.currentAccess(ctx, access); err != nil {
			return bridgeapi.SyncResponse{}, err
		}
		read, err := e.service.Sessions.Agent.ReadRemote(ctx, remoteexec.ReadRequest{ProjectID: access.ProjectID, ConnectionID: access.ConnectionID,
			Epoch: request.Epoch, After: cursor, RepositoryContext: request.Context})
		if err != nil {
			return bridgeapi.SyncResponse{}, err
		}
		if combined.Executor.Mode != "" && combined.Delivery.FirstRetainedSequence != read.Delivery.FirstRetainedSequence {
			return bridgeapi.SyncResponse{}, errDeliveryRetentionMoved
		}
		combined = read
		through := max(cursor, read.Delivery.FirstRetainedSequence-1)
		stopped := false
		for _, item := range read.Delivery.Entries {
			wire, err := syncEntry(item)
			if err != nil {
				return bridgeapi.SyncResponse{}, err
			}
			encoded, err := json.Marshal(wire)
			if err != nil {
				return bridgeapi.SyncResponse{}, err
			}
			if len(entries) >= bridgeapi.MaxSyncEntries || len(entries) > 0 && bytes+len(encoded)+1 > bridgeapi.SyncSoftBudgetBytes {
				through, stopped = item.Sequence-1, true
				break
			}
			entries, bytes = append(entries, item), bytes+len(encoded)+1
		}
		if !stopped {
			through = read.Delivery.Through
		}
		if stopped || !read.Delivery.More || len(entries) >= bridgeapi.MaxSyncEntries || bytes > bridgeapi.SyncSoftBudgetBytes {
			combined.Delivery.Entries, combined.Delivery.Through = entries, through
			combined.Delivery.Truncated = conversation.Sequence(request.After) < combined.Delivery.FirstRetainedSequence-1
			response, err := syncResponse(combined, request)
			if err != nil {
				return bridgeapi.SyncResponse{}, err
			}
			// A SQL revocation or role/login change during hydration refuses delivery.
			if _, err = e.service.authenticate(ctx, token); err != nil {
				return bridgeapi.SyncResponse{}, err
			}
			return response, nil
		}
		if through <= cursor {
			return bridgeapi.SyncResponse{}, errors.New("remote delivery made no cursor progress")
		}
		cursor = through
	}
}

func syncChanged(response bridgeapi.SyncResponse, request bridgeapi.SyncRequest) bool {
	return request.Context || len(response.Entries) > 0 || response.Dropped != nil || response.ExaminedThrough != request.After ||
		response.StateDigest != request.StateDigest
}

func (e *Endpoint) serveSync(w http.ResponseWriter, r *http.Request) {
	release, admitted := e.admit(w, r, e.syncSlots)
	if !admitted {
		return
	}
	defer release()
	ctx, cancel := e.requestContext(r, bridgeapi.SyncWait)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if err := http.NewResponseController(w).SetWriteDeadline(deadline); err != nil {
		e.writeError(w, r, err)
		return
	}
	token, err := bearer(r)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	var request bridgeapi.SyncRequest
	if err = decodeBody(w, r, bridgeapi.MaxSyncRequestBytes, &request); err == nil {
		err = validateSync(request)
	}
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	response, releaseRead, err := e.readDelivery(ctx, token, request)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	if syncChanged(response, request) {
		defer releaseRead()
		writeJSON(w, http.StatusOK, response)
		return
	}
	response = bridgeapi.SyncResponse{}
	releaseRead()
	access, err := e.service.authenticate(ctx, token)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	id := agentloop.SessionFlowID(access.ProjectID)
	tail, tailErr := e.service.Sessions.Agent.LatestEventResumeToken(ctx, id, session.EventStreamActivity)
	// Read after capturing a disposable stream tail closes the snapshot-to-tail
	// race; stream loss cannot discard a fact already committed to the Session.
	response, releaseRead, err = e.readDelivery(ctx, token, request)
	if err != nil {
		e.writeError(w, r, err)
		return
	}
	if !syncChanged(response, request) {
		response = bridgeapi.SyncResponse{}
		releaseRead()
		deadline, _ := ctx.Deadline()
		wait, stop := context.WithDeadline(ctx, deadline.Add(-5*time.Second))
		if tailErr == nil {
			_, _ = e.service.Sessions.Agent.ReadEvent(wait, id, session.EventStreamActivity, tail)
		} else {
			<-wait.Done()
		}
		stop()
		response, releaseRead, err = e.readDelivery(ctx, token, request)
		if err != nil {
			e.writeError(w, r, err)
			return
		}
	}
	defer releaseRead()
	writeJSON(w, http.StatusOK, response)
}
