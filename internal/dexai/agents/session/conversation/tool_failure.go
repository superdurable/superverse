// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package conversation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// Bounds of the text the model reads. The failure of a source gate carries the
// end of a build's output, so its message may be longer.
const (
	toolFailureMessageBytes     = 1 << 10
	toolFailureGateMessageBytes = 8 << 10
	toolFailureDetailBytes      = 2 << 10
)

// ToolFailure is the model's view of a tool call that failed with a known code,
// which names its owner, and a known effect. Message names the failure; Detail
// is supplementary text that holds no secret, such as the end of a command's
// output.
type ToolFailure struct {
	Code    toolfailure.Code
	Effect  toolfailure.Effect
	Message string
	Detail  string
}

// Outcome is the recorded outcome of the call: unknown when its effect could
// not be observed, a known failure otherwise.
func (failure ToolFailure) Outcome() ToolOutcome {
	if failure.Effect == toolfailure.EffectUnknown {
		return ToolOutcomeUnknown
	}
	return ToolOutcomeKnownFailure
}

// Text is the tool result the model reads, for every failure alike:
//
//	Error: <code> — <message>
//	owner: <owner> · effect: <effect>
//	hint: <what to do>
//	detail: <supplementary text>
//
// The detail line is omitted when there is no detail.
func (failure ToolFailure) Text() string {
	messageBytes := toolFailureMessageBytes
	if failure.Code.IsSourceGate() {
		messageBytes = toolFailureGateMessageBytes
	}
	text := fmt.Sprintf("Error: %s — %s\nowner: %s · effect: %s\nhint: %s",
		failure.Code, boundedFailureText(failure.Message, messageBytes), failure.Code.Owner(), failure.Effect, failure.hint())
	if failure.Detail != "" {
		text += "\ndetail: " + boundedFailureText(failure.Detail, toolFailureDetailBytes)
	}
	return text
}

func boundedFailureText(text string, maximumBytes int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maximumBytes {
		return text
	}
	return comm.TruncateUTF8(text, maximumBytes-len("...")) + "..."
}

// hint is the advice of the code, then of the effect, then of the owner.
func (failure ToolFailure) hint() string {
	advice := []string{failure.Code.Hint(), failure.Effect.Hint(), failure.Code.OwnerHint()}
	return strings.Join(slices.DeleteFunc(advice, func(text string) bool { return text == "" }), " ")
}
