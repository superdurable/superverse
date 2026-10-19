// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Document is one Studio JSON document with the HTTP status that classifies it
// for the Studio BFF.
type Document struct {
	StatusCode        int
	JSON              []byte
	RetryAfterSeconds uint32
}

// JSONDocument encodes value as a document with status.
func JSONDocument(status int, value any) Document {
	data, err := json.Marshal(value)
	if err != nil {
		// A Dex double can be NaN or infinite, which JSON cannot encode.
		return Document{StatusCode: http.StatusInternalServerError, JSON: []byte(`{"error":"Studio response could not be encoded"}` + "\n")}
	}
	return Document{StatusCode: status, JSON: append(data, '\n')}
}

// CodedErrorDocument is an error document with a stable code that Studio
// recovery behavior branches on.
func CodedErrorDocument(status int, code, message string) Document {
	return JSONDocument(status, errorResponse{Error: message, Code: code})
}

type errorResponse struct {
	Error    string  `json:"error"`
	Code     string  `json:"code,omitempty"`
	GRPCCode *uint32 `json:"grpcCode,omitempty"`
}

func errorDocument(status int, message string) Document {
	return JSONDocument(status, errorResponse{Error: message})
}

// grpcErrorDocument maps a failed Dex call to a document that keeps its gRPC
// code; Studio recognizes an inactive Flow by FailedPrecondition.
func grpcErrorDocument(err error, operation string) Document {
	var unavailable executionDefinitionError
	if errors.As(err, &unavailable) {
		return CodedErrorDocument(http.StatusConflict, "FLOW_EXECUTION_DEFINITION_UNAVAILABLE", unavailable.Error())
	}
	statusError := status.Convert(err)
	httpStatus := http.StatusBadGateway
	switch statusError.Code() {
	case codes.InvalidArgument:
		httpStatus = http.StatusBadRequest
	case codes.AlreadyExists, codes.FailedPrecondition:
		httpStatus = http.StatusConflict
	case codes.NotFound:
		httpStatus = http.StatusNotFound
	case codes.DeadlineExceeded:
		httpStatus = http.StatusRequestTimeout
	case codes.ResourceExhausted:
		httpStatus = http.StatusTooManyRequests
	case codes.Canceled:
		httpStatus = 499
	default:
	}
	message := statusError.Message()
	if message == "" {
		message = operation + " failed"
	}
	grpcCode := uint32(statusError.Code())
	document := JSONDocument(httpStatus, errorResponse{Error: flowRunNotFoundMessage(statusError.Code(), message), GRPCCode: &grpcCode})
	if httpStatus == http.StatusTooManyRequests {
		for _, detail := range statusError.Details() {
			if retry, ok := detail.(*errdetails.RetryInfo); ok && retry.GetRetryDelay().IsValid() && retry.GetRetryDelay().AsDuration() > 0 {
				seconds := math.Ceil(retry.GetRetryDelay().AsDuration().Seconds())
				document.RetryAfterSeconds = uint32(min(seconds, math.MaxUint32))
				break
			}
		}
	}
	return document
}

// flowRunNotFoundMessage rewords Temporal's missing-execution message in Flow terms.
func flowRunNotFoundMessage(code codes.Code, message string) string {
	const temporalPrefix = "workflow execution not found for workflow ID "
	if code != codes.NotFound || !strings.HasPrefix(message, temporalPrefix) {
		return message
	}
	return "Flow run is not found for flow ID " + strings.TrimPrefix(message, temporalPrefix)
}
