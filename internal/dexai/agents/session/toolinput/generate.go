// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package toolinput

// All input schemas are generated together so optional scalar helpers are shared.
// tool_inputs.json is a generation-only index; it is never a tool or stored payload.
//go:generate go tool jschemagen -target generated/tool_inputs_gen.go -package generated -typename ToolInputs tool_inputs.json
