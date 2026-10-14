// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
	projectconfig "github.com/superdurable/superversev2/internal/projects/config"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/runs"
)

// maximumOutputBytes bounds one tool result, which is recorded as it is
// produced: a result that is too large keeps fewer of its items or a shorter
// log tail, and says what it left out.
const maximumOutputBytes = 16 << 10

// ProjectDiagnostics runs the Agent's read-only platform tools on the server,
// for the Project of the calling Session only. It implements
// session.ProjectDiagnosticsReader; the composition root assigns its
// dependencies. It reads through Flow clients, the platform Dex, the Live
// engine that Runs resolves and the Live serving, and changes nothing.
type ProjectDiagnostics struct {
	Projects      *projects.ProjectFlowClient
	Previews      *preview.PreviewFlowClient
	Releases      *releases.ProjectReleaseFlowClient
	Deployments   *deployments.ProjectDeployFlowClient
	Configuration *projectconfig.Configuration
	Runs          *runs.Runs
	// Platform reads the platform Dex, which every Project shares: only the
	// Flows that platformFlowOf admits for the Session.
	Platform dexpb.FlowServiceClient
	// Serving reads the Live application's environment and log, and Redactor
	// hides the project's internal resource names.
	Serving  deployments.Serving
	Redactor infra.Redactor
	// Publishing reads a failed build's redacted log, sharing the replica's
	// log reads with Studio.
	Publishing *deployments.PublishingFlowService
}

// callFailureError is a read the tool refuses or cannot make because of the call:
// the model sees its code and message and corrects its call.
type callFailureError struct {
	code    toolfailure.Code
	message string
}

func (f *callFailureError) Error() string { return f.code.String() + ": " + f.message }

// ReadDiagnostics runs one platform tool call for identity's Project. Every
// string of the result is redacted, and the result is at most 16 KiB.
func (d *ProjectDiagnostics) ReadDiagnostics(ctx context.Context, identity agentloop.SessionIdentity, call conversation.ToolCall) (agentloop.ToolExecutionResult, error) {
	var output []byte
	var err error
	switch call.Name {
	case conversation.ToolReadDeliveryStatus:
		output, err = d.readDeliveryStatus(ctx, identity, call.Arguments.String())
	case conversation.ToolReadPlatformFlow:
		output, err = d.readPlatformFlow(ctx, identity, call.Arguments.String())
	case conversation.ToolReadLiveAppRuns:
		output, err = d.readLiveAppRuns(ctx, identity, call.Arguments.String())
	default:
		panic(fmt.Sprintf("%q is not a platform tool", call.Name))
	}
	redaction := newRedactor(identity.ProjectID, d.Redactor)
	var refusal *callFailureError
	switch {
	case err == nil:
		return agentloop.ToolExecutionResult{Content: string(output), Outcome: conversation.ToolOutcomeSucceeded}, nil
	case errors.As(err, &refusal):
		return agentloop.FailureResult(conversation.ToolFailure{Code: refusal.code, Effect: toolfailure.EffectNone, Message: redaction.text(refusal.message)}), nil
	case sandboxd.IsTransient(err) || errors.Is(err, context.Canceled):
		// Dex repeats the read within the retry budget of the tool.
		return agentloop.ToolExecutionResult{}, fmt.Errorf("%s: %w", call.Name, err)
	default:
		// A read changes nothing, so the failure leaves no effect.
		return agentloop.FailureResult(conversation.ToolFailure{Code: toolfailure.ToolError, Effect: toolfailure.EffectNone,
			Message: "The tool failed with an unexpected error.", Detail: redaction.text(err.Error())}), nil
	}
}

// decodeArguments decodes a call's arguments with its strict schema; invalid
// arguments are a failure the model corrects.
func decodeArguments[T any](decode func(string) (T, error), arguments string) (T, error) {
	input, err := decode(arguments)
	if err != nil {
		return input, &callFailureError{code: toolfailure.InvalidArguments, message: err.Error()}
	}
	return input, nil
}

// notFound turns a missing Flow into a call failure and keeps any other
// error.
func notFound(err error, what string) error {
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) || errors.Is(err, postgres.ErrNotFound) {
		return &callFailureError{code: toolfailure.FlowNotFound, message: what + " does not exist"}
	}
	return err
}

// redactor applies the configuration's output redaction to every string a
// tool returns: the configured values a read can reach and the Project's
// internal resource names.
type redactor struct{ output projectconfig.OutputRedaction }

// newRedactor is the redactor of project's internal names.
func newRedactor(project string, infrastructure infra.Redactor) redactor {
	return redactor{output: projectconfig.OutputRedaction{Project: project, Infrastructure: infrastructure}}
}

func (r redactor) text(text string) string { return r.output.Text(text) }

// encode returns document as JSON with every string, object keys included,
// redacted. Strings are redacted before encoding, so an escape sequence never
// hides a value.
func (r redactor) encode(document any) []byte {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	comm.MustDo(encoder.Encode(document))
	var decoded any
	comm.MustDo(comm.DecodeStrictJSONNumbers(encoded.Bytes(), &decoded))
	encoded.Reset()
	comm.MustDo(encoder.Encode(r.walk(decoded)))
	return bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
}

func (r redactor) walk(value any) any {
	switch current := value.(type) {
	case string:
		return r.text(current)
	case map[string]any:
		redacted := make(map[string]any, len(current))
		for key, entry := range current {
			redacted[r.text(key)] = r.walk(entry)
		}
		return redacted
	case []any:
		for index, entry := range current {
			current[index] = r.walk(entry)
		}
		return current
	default:
		return value
	}
}

// fitted encodes the document that build returns for the largest count of
// its items, at most count, whose redacted encoding stays within
// maximumOutputBytes; the document reports the items it left out. Every item
// is bounded far below that, so one item always fits.
func (r redactor) fitted(count int, build func(int) any) []byte {
	for {
		encoded := r.encode(build(count))
		if len(encoded) <= maximumOutputBytes || count <= 1 {
			return encoded
		}
		count = count * 3 / 4
	}
}

// failure is the model's view of a recorded Flow failure.
type failure struct {
	Code    string `json:"code"`
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
}

// maximumFailureMessageBytes bounds a failure message in a document that
// lists several resources.
const maximumFailureMessageBytes = 1 << 10

// failure is the redacted view of a recorded failure.
func (r redactor) failure(recorded *dexflow.Failure) *failure {
	if recorded == nil {
		return nil
	}
	return &failure{Code: recorded.Code, Phase: recorded.Phase, Message: comm.TruncateUTF8(r.text(recorded.Message), maximumFailureMessageBytes)}
}
