// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package comm

// Must returns v, or panics with err when err is not nil.
//
// Use it only for calls that can fail solely through a programming error or
// corrupt data, such as a Dex Attribute Get/Set, Channel Publish or Stream
// Write, or a helper that does nothing else. The Dex SDK recovers a panic in a
// Step, WaitFor or RPC handler and the business gRPC recovery interceptor
// recovers one in a request handler; both turn it into an error. Real I/O and
// errors that callers classify with errors.Is or errors.As keep explicit
// handling.
func Must[T any](v T, err error) T {
	MustDo(err)
	return v
}

// MustDo panics with err when err is not nil, under the same policy as Must.
func MustDo(err error) {
	if err != nil {
		panic(err)
	}
}
