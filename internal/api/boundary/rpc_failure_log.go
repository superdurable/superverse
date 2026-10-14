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
	"encoding/json"
	"log/slog"
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LogUnaryFailure records only the public status and bounded identity of a failed call.
// Configuration host errors are HTTP documents, so they need a separate check.
func (b *Boundary) LogUnaryFailure(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	response, err := handler(ctx, request)
	if err != nil {
		b.logRPCFailure(ctx, info.FullMethod, request, err)
	} else if strings.HasPrefix(info.FullMethod, "/superverse.v2.ConnectorService/") || strings.HasPrefix(info.FullMethod, "/superverse.v2.ApplicationEnvironmentService/") {
		if result, ok := response.(interface{ GetDocument() *pb.FlowApiDocument }); ok {
			b.logConfigurationFailure(ctx, info.FullMethod, request, result.GetDocument())
		}
	}
	return response, err
}

// LogStreamFailure records a failed Watch once, after its handler terminates.
func (b *Boundary) LogStreamFailure(service any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	tracked := &failureLogStream{ServerStream: stream}
	err := handler(service, tracked)
	b.logRPCFailure(stream.Context(), info.FullMethod, tracked.request, err)
	return err
}

type failureLogStream struct {
	grpc.ServerStream
	request any
}

func (s *failureLogStream) RecvMsg(message any) error {
	err := s.ServerStream.RecvMsg(message)
	if err == nil && s.request == nil {
		s.request = message
	}
	return err
}

func (b *Boundary) logRPCFailure(ctx context.Context, method string, request any, err error) {
	if err == nil {
		return
	}
	code := status.Code(err)
	if code == codes.Canceled || ctx.Err() == context.Canceled {
		return
	}
	level := slog.LevelWarn
	if code == codes.Internal || code == codes.Unknown || code == codes.DataLoss {
		level = slog.LevelError
	}
	fields := []any{"method", method, "code", code.String(), "message", boundedLogValue(status.Convert(err).Message(), 512)}
	fields = appendRequestIdentity(fields, request)
	b.Logger.Log(ctx, level, "Business gRPC call failed", fields...)
}

func (b *Boundary) logConfigurationFailure(ctx context.Context, method string, request any, document *pb.FlowApiDocument) {
	if document == nil || document.GetHttpStatus() < 400 {
		return
	}
	var coded struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	// Error documents are small. Refuse to parse a larger response rather than log its body.
	if len(document.GetJson()) <= 4096 {
		_ = json.Unmarshal(document.GetJson(), &coded)
	}
	level := slog.LevelWarn
	if document.GetHttpStatus() >= 500 {
		level = slog.LevelError
	}
	fields := []any{"method", method, "http_status", document.GetHttpStatus(), "code", boundedLogValue(coded.Code, 128), "message", boundedLogValue(coded.Error, 512)}
	fields = appendRequestIdentity(fields, request)
	b.Logger.Log(ctx, level, "Business configuration call failed", fields...)
}

func appendRequestIdentity(fields []any, request any) []any {
	if withCaller, ok := request.(interface{ GetCaller() *pb.CallerContext }); ok {
		if account := withCaller.GetCaller().GetAccountId(); account != "" {
			fields = append(fields, "account", boundedLogValue(account, 128))
		}
	}
	var project string
	if withProject, ok := request.(interface{ GetProjectId() string }); ok {
		project = withProject.GetProjectId()
	} else if withContext, ok := request.(interface{ GetContext() *pb.FlowApiContext }); ok {
		project = withContext.GetContext().GetTarget().GetProjectId()
	} else if withTarget, ok := request.(interface {
		GetTarget() *pb.ProjectConfigurationTarget
	}); ok {
		project = withTarget.GetTarget().GetProjectId()
	}
	if project != "" {
		fields = append(fields, "project", boundedLogValue(project, 128))
	}
	return fields
}

func boundedLogValue(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	if len(value) > limit {
		return strings.ToValidUTF8(value[:limit], "")
	}
	return value
}
