// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandboxtools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// runPlatformProgram runs a program the platform chose, such as the catalog
// fetch, to its exit in the workspace. The programs only read, so each run has
// a fresh execution identity and a timeout is a transient fault Dex may repeat.
// A program the Sandbox cannot start and an output beyond maximumOutputBytes
// are platform faults, never the call's.
func (execution *Execution) runPlatformProgram(ctx context.Context, purpose string, arguments []string, timeout time.Duration, maximumOutputBytes int64) (sandboxd.ProcessOutput, error) {
	output, err := execution.executor.RunToExit(ctx, sandboxd.StartRequest{
		ExecutionID: sandboxd.PlatformExecutionID(purpose), Arguments: arguments,
		Environment: sandboxd.ToolProcessEnvironment(sandboxd.WorkspaceRoot), Timeout: timeout, MaximumOutputBytes: maximumOutputBytes,
	})
	var failure *sandboxd.ToolFailure
	switch {
	case errors.Is(err, sandboxd.ErrProcessNotStarted):
		return output, sandboxd.NewToolFailure(toolfailure.PlatformCommandUnavailable, fmt.Errorf("the Sandbox cannot start %q: %w", arguments[0], err))
	case errors.As(err, &failure) && failure.Code() == toolfailure.CommandOutputLimitExceeded:
		return output, sandboxd.NewToolFailure(toolfailure.ProcessOutputLimitExceeded, fmt.Errorf("%s printed more than %d bytes: %w", arguments[0], maximumOutputBytes, err))
	}
	return output, err
}
