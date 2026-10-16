// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package boundary

import (
	"errors"

	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ConnectionRPCStatus maps the errors of TeamSpace connection requests and of
// connection selections. A refusal carries its reason, which Studio branches
// on; its message names versions and rules, never a credential.
func ConnectionRPCStatus(err error) error {
	if err == nil {
		return nil
	}
	if reason, refused := connections.Refusal(err); refused {
		code := codes.FailedPrecondition
		if reason == connections.ReasonConnectionNameTaken {
			code = codes.AlreadyExists
		}
		return reasonStatus(code, err.Error(), reason.String(), reason.Remedy())
	}
	switch {
	case errors.Is(err, connectiondao.ErrRevisionChanged):
		return revisionChanged("the connection changed; read it again")
	case errors.Is(err, deployments.ErrDeploymentInProgress):
		return withErrorInfo(codes.FailedPrecondition, "a deployment is in progress; release unused connections after it", reasonDeploymentInProgress)
	case errors.Is(err, connections.ErrRequestReused):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, connections.ErrCredentialChangeForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, connections.ErrInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, connections.ErrNotFound):
		return status.Error(codes.NotFound, "connection not found")
	case errors.Is(err, connections.ErrReauthorizationRequired):
		return status.Error(codes.FailedPrecondition, "the connection needs a new authorization")
	}
	return ConfigurationRPCStatus(err)
}
