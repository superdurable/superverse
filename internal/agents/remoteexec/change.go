// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteexec

import (
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

const maximumBindingBytes = 8 << 10

// SetRequest is authenticated by the injected Session service. BasisEpoch
// comes from the connection's pairing record or the browser's exact snapshot.
type SetRequest struct {
	ProjectID    string             `json:"project_id"`
	BasisEpoch   uint64             `json:"basis_epoch"`
	Mode         Mode               `json:"mode"`
	ConnectionID string             `json:"connection_id,omitempty"`
	Owner        conversation.Actor `json:"owner"`
	Binding      Binding            `json:"binding"`
	Reason       HandoffReason      `json:"reason,omitempty"`
}

// SetResult preserves an explicit CAS refusal through the Dex transport.
type SetResult struct {
	Executor Executor `json:"executor"`
	Accepted bool     `json:"accepted"`
	Changed  bool     `json:"changed"`
}

func (request SetRequest) Validate() error {
	if request.ProjectID == "" {
		return errors.New("executor selection requires a Project")
	}
	switch request.Mode {
	case ModeRemote:
		if request.ConnectionID == "" || len(request.ConnectionID) > 128 || request.Owner.AccountID == "" || len(request.Owner.AccountID) > 262 || request.Owner.Email == "" || len(request.Owner.Email) > 254 {
			return errors.New("remote executor requires a bounded authenticated connection and owner")
		}
		binding, err := json.Marshal(request.Binding)
		if err != nil || len(binding) > maximumBindingBytes {
			return errors.New("executor binding exceeds the activation body bound")
		}
	case ModePlatform:
		if request.Reason != HandoffSwitched && request.Reason != HandoffConnectionRemoved {
			return errors.New("platform selection requires a declared handoff reason")
		}
	default:
		return errors.New("unknown executor mode")
	}
	return nil
}

// Change computes an epoch CAS under the Session executor lock. Only a retry
// of the same current connection can join a selection whose basis is old.
func (executor Executor) Change(request SetRequest, now time.Time) SetResult {
	result := SetResult{Executor: executor}
	if executor.Mode == ModeRemote && executor.Remote != nil && request.Mode == ModeRemote && executor.Remote.ConnectionID == request.ConnectionID {
		result.Accepted = true
		return result
	}
	if request.BasisEpoch != executor.Epoch || executor.Epoch == ^uint64(0) || executor.Generation == ^uint64(0) {
		return result
	}
	result.Accepted = true
	if executor.Mode == request.Mode && request.Mode == ModePlatform {
		return result
	}
	executor.Epoch++
	executor.Generation++
	if request.Mode == ModeRemote {
		executor.Mode = ModeRemote
		executor.Remote = &RemoteExecutor{ConnectionID: request.ConnectionID, Owner: request.Owner, Binding: request.Binding, ActivatedAt: now.UTC()}
	} else {
		handoff := PlatformHandoff{Epoch: executor.Epoch, From: now.UTC(), To: now.UTC(), Reason: request.Reason}
		if executor.Handoff != nil {
			handoff.From, handoff.Owners = executor.Handoff.From, slices.Clone(executor.Handoff.Owners)
		}
		if executor.Remote != nil {
			if handoff.From.After(executor.Remote.ActivatedAt) {
				handoff.From = executor.Remote.ActivatedAt
			}
			if len(handoff.Owners) == 0 || handoff.Owners[len(handoff.Owners)-1] != executor.Remote.Owner {
				handoff.Owners = append(handoff.Owners, executor.Remote.Owner)
			}
		}
		handoff.Owners = handoff.Owners[max(0, len(handoff.Owners)-maximumHandoffOwners):]
		executor.Mode, executor.Remote, executor.Handoff = ModePlatform, nil, &handoff
	}
	result.Executor, result.Changed = executor, true
	return result
}
