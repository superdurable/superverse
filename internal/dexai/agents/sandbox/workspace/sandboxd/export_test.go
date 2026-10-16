// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxd

import "context"

// The tests name these constants.
const (
	ProcessEventStdoutForTest = processEventStdout
	ProcessEventExitForTest   = processEventExit
)

// The tests name these values.
var (
	ErrProcessTimeoutForTest    = errProcessTimeout
	ErrProcessStopFailedForTest = errProcessStopFailed
)

// ProcessStreamForTest is processStream for the tests.
type ProcessStreamForTest = processStream

// RecvForTest is recv for the tests.
func (r *processStream) RecvForTest() (processEvent, error) {
	return r.recv()
}

// StartForTest is start for the tests.
func (r *SandboxClient) StartForTest(ctx context.Context, request StartRequest) (*processStream, error) {
	return r.start(ctx, request)
}

// WithWorkingDirectoryForTest is the request with its working directory set.
func (r StartRequest) WithWorkingDirectoryForTest(directory WorkspacePath) StartRequest {
	r.workingDirectory = directory
	return r
}
