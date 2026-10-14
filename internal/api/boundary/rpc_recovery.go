// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package boundary

import (
	"context"
	"errors"

	"github.com/superdurable/superversev2/internal/api/watch"
	"google.golang.org/grpc"
)

// A panic is a programming error, so handlers do not guard against one. The
// recovery interceptors keep one defect from stopping the process: the caller
// receives a fixed Internal status and the log keeps the value and stack. They
// also log the cause of a failure no mapper classified, whose fixed status
// tells the caller to inspect the server diagnostics.

// RecoverUnary converts panics before the outer logging interceptor records their status.
func (b *Boundary) RecoverUnary(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (_ any, err error) {
	defer watch.RecoverAsError(ctx, b.Logger, info.FullMethod, &err)
	response, err := handler(ctx, request)
	return response, b.logUnclassified(ctx, info.FullMethod, err)
}

// RecoverStream converts panics before the outer logging interceptor records their status.
func (b *Boundary) RecoverStream(service any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	defer watch.RecoverAsError(stream.Context(), b.Logger, info.FullMethod, &err)
	return b.logUnclassified(stream.Context(), info.FullMethod, handler(service, stream))
}

// logUnclassified logs the cause of an unclassified failure and returns only
// its public status.
func (b *Boundary) logUnclassified(ctx context.Context, method string, err error) error {
	var unclassified *unclassifiedError
	if !errors.As(err, &unclassified) {
		return err
	}
	b.Logger.ErrorContext(ctx, "Business gRPC call failed", "method", method, "error", unclassified.cause)
	return errUnclassified
}
