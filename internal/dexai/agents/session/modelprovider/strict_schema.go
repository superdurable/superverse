// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider

import (
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// StrictInputSchema is the input schema of a strict tool as the Claude API
// takes it. The API guarantees only the shape of a strict input, so it accepts
// a subset of JSON Schema: the SDK's transformation removes every other
// keyword, such as the lengths, ranges and defaults, and appends it to the
// description of the parameter that carried it. The tool's own decoder still
// validates the bounds when the tool_use is complete.
//
// The SDK writes the keys it keeps in extra fields in Go's randomized map
// order, so the same schema encodes differently in each process, and the
// digest of the frozen prompt and the request bytes of the tools would change
// with every server start. Decoding the result and encoding it again lets the
// standard library write every object key in sorted order.
func StrictInputSchema(schema conversation.JSONObject) conversation.JSONObject {
	var decoded map[string]any
	comm.MustDo(json.Unmarshal([]byte(schema), &decoded))
	var transformed any
	comm.MustDo(json.Unmarshal(comm.Must(json.Marshal(anthropic.BetaToolInputSchema(decoded))), &transformed))
	return conversation.MustJSONObject(string(comm.Must(json.Marshal(transformed))))
}
