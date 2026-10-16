// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package boundary

import (
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The API is the trust boundary of request input. The checks below run right
// after authentication, before any Flow client or Service sees the request,
// and return public InvalidArgument statuses directly.

// maxRequestIDBytes bounds the idempotency key that a caller sends with a
// start or command. Start RequestIDs, creation IDs and command receipts are
// derived from it.
const maxRequestIDBytes = 128

// ValidateRequestID rejects a missing or oversized request ID.
func ValidateRequestID(id string) error {
	return InvalidIf(id == "" || len(id) > maxRequestIDBytes, "request ID must contain 1 to 128 bytes")
}

// ValidateMessageID rejects a missing, oversized or malformed user message ID.
func ValidateMessageID(id string) error {
	return InvalidIf(id == "" || len(id) > 256 || !utf8.ValidString(id), "message ID must contain 1 to 256 UTF-8 bytes")
}

// DomainValue is the persisted domain value that an enum value of a request
// names: its value name without the enum prefix. The zero value and a number
// the enum does not declare are invalid input.
func DomainValue[E protoEnum](value E) (string, error) {
	values := value.Descriptor().Values()
	declared := values.ByNumber(value.Number())
	if value == 0 || declared == nil {
		return "", status.Errorf(codes.InvalidArgument, "%s value %d is unspecified or undeclared", value.Descriptor().Name(), value.Number())
	}
	return strings.TrimPrefix(string(declared.Name()), enumPrefix(values)), nil
}

// DomainValues is the DomainValue of every value, such as the statuses of a
// list filter.
func DomainValues[E protoEnum](values []E) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		domain, err := DomainValue(value)
		if err != nil {
			return nil, err
		}
		out = append(out, domain)
	}
	return out, nil
}

// InvalidIf is the InvalidArgument status of a rejected request input.
func InvalidIf(invalid bool, message string) error {
	if invalid {
		return status.Error(codes.InvalidArgument, message)
	}
	return nil
}
