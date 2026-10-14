// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package platform

import (
	"context"
	"errors"
	"net/http"

	"github.com/aws/smithy-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OutcomeUnknownError reports a provider call whose effect could not be
// observed, such as a lost or unreadable response after the request was sent.
// It matches both the provider package's Sentinel and the Cause, so callers keep
// classifying cancellation and deadlines. Error ends with the Cause, which
// providers build only from requests whose credentials travel in headers.
type OutcomeUnknownError struct {
	Sentinel error
	// Message replaces the Sentinel's text in Error when it is not empty.
	Message string
	Cause   error
}

func (e *OutcomeUnknownError) Error() string {
	message := e.Message
	if message == "" {
		message = e.Sentinel.Error()
	}
	return message + ": " + e.Cause.Error()
}

func (e *OutcomeUnknownError) Unwrap() []error { return []error{e.Sentinel, e.Cause} }

// IsCallInterrupted reports whether a call ended before its outcome could be
// observed because the caller's context ended or a gRPC call ended with
// DeadlineExceeded or Canceled. It deliberately ignores OutcomeUnknownError:
// each caller decides which providers' unknown outcomes it can reconcile.
func IsCallInterrupted(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	code := status.Code(err)
	return code == codes.DeadlineExceeded || code == codes.Canceled
}

// IsAccessDenied reports whether a provider refused the caller's credentials or
// authorization: a gRPC PermissionDenied or Unauthenticated status, or an HTTP
// 401 or 403 response as IsHTTPAccessDenied defines it.
func IsAccessDenied(err error) bool {
	code := status.Code(err)
	return code == codes.PermissionDenied || code == codes.Unauthenticated || IsHTTPAccessDenied(err)
}

// IsHTTPAccessDenied reports whether err carries a provider HTTP 401 or 403
// response. Provider client errors expose their response status through an
// HTTPStatusCode method: the AWS SDK response errors, kubeapi.StatusError and
// githubapp.HTTPError.
func IsHTTPAccessDenied(err error) bool {
	var response interface{ HTTPStatusCode() int }
	if !errors.As(err, &response) {
		return false
	}
	code := response.HTTPStatusCode()
	return code == http.StatusUnauthorized || code == http.StatusForbidden
}

// HasAWSErrorCode reports whether err is an AWS API error whose code is one of want.
// S3-compatible stores such as MinIO report the same codes.
func HasAWSErrorCode(err error, want ...string) bool {
	var api smithy.APIError
	if !errors.As(err, &api) {
		return false
	}
	for _, code := range want {
		if api.ErrorCode() == code {
			return true
		}
	}
	return false
}
