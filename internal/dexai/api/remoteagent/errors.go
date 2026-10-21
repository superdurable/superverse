// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagent

import (
	"errors"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	connections "github.com/superdurable/superversev2/internal/dexai/agents/remoteagent"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagentdao"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type refusalReason struct {
	name, message string
	code          codes.Code
}

var (
	reasonConnectionsFull = refusalReason{"REMOTE_AGENT_CONNECTIONS_FULL", "Remove an unused connection before pairing another", codes.ResourceExhausted}
	reasonExecutorChanged = refusalReason{"REMOTE_AGENT_EXECUTOR_CHANGED", "The executor changed; refresh the Session before choosing again", codes.Aborted}
	reasonCurrentExecutor = refusalReason{"REMOTE_AGENT_CURRENT_EXECUTOR", "Use the Platform agent before removing its current connection", codes.FailedPrecondition}
)

func rpcStatus(err error) error {
	switch {
	case errors.Is(err, remoteagentdao.ErrConnectionsFull):
		return reasonConnectionsFull.status()
	case errors.Is(err, connections.ErrExecutorChanged):
		return reasonExecutorChanged.status()
	case errors.Is(err, connections.ErrCurrentExecutor):
		return reasonCurrentExecutor.status()
	default:
		return boundary.SessionRPCStatus(err)
	}
}

func (r refusalReason) status() error {
	return comm.Must(status.New(r.code, r.message).WithDetails(&errdetails.ErrorInfo{
		Reason: r.name, Domain: "superverse.v2", Metadata: map[string]string{"remedy": pb.FailureRemedy_FAILURE_REMEDY_APPLY_AGAIN.String()},
	})).Err()
}
