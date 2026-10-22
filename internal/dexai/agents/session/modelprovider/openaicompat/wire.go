// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package openaicompat

// EventType is the closed set of accepted Responses stream event identities.
type EventType struct{ id string }

func (kind EventType) String() string { return kind.id }

var (
	EventResponseCreated                = EventType{"response.created"}
	EventResponseInProgress             = EventType{"response.in_progress"}
	EventResponseOutputItemAdded        = EventType{"response.output_item.added"}
	EventResponseOutputItemDone         = EventType{"response.output_item.done"}
	EventResponseFunctionArgumentsDelta = EventType{"response.function_call_arguments.delta"}
	EventResponseFunctionArgumentsDone  = EventType{"response.function_call_arguments.done"}
	EventResponseOutputTextDelta        = EventType{"response.output_text.delta"}
	EventResponseOutputTextDone         = EventType{"response.output_text.done"}
	EventResponseContentPartAdded       = EventType{"response.content_part.added"}
	EventResponseContentPartDone        = EventType{"response.content_part.done"}
	EventResponseCompleted              = EventType{"response.completed"}
	EventResponseFailed                 = EventType{"response.failed"}
	EventResponseIncomplete             = EventType{"response.incomplete"}
	EventResponseReasoningTextDelta     = EventType{"response.reasoning_text.delta"}
	EventResponseReasoningSummaryDelta  = EventType{"response.reasoning_summary_text.delta"}
	EventResponseRefusalDelta           = EventType{"response.refusal.delta"}
	EventResponseRefusalDone            = EventType{"response.refusal.done"}
	EventProviderError                  = EventType{"error"}
)

// ItemType names the wire content and function items shared with the mock encoder.
type ItemType struct{ id string }

func (kind ItemType) String() string { return kind.id }

var (
	ItemFunctionCall   = ItemType{"function_call"}
	ItemFunctionOutput = ItemType{"function_call_output"}
	ItemReasoning      = ItemType{"reasoning"}
	ItemMessage        = ItemType{"message"}
	ItemOutputText     = ItemType{"output_text"}
	ItemInputText      = ItemType{"input_text"}
	ItemInputImage     = ItemType{"input_image"}
	ItemImageURL       = ItemType{"image_url"}
	ItemFunction       = ItemType{"function"}
	ItemRefusal        = ItemType{"refusal"}
)

// Paths are fixed protocol endpoints, never selected after an HTTP failure.
const (
	ResponsesPath      = "/v1/responses"
	ChatPath           = "/v1/chat/completions"
	MockProviderHeader = "Mock-Provider"
)

// FinishReason is the closed Chat completion outcome set accepted by the adapter.
type FinishReason struct{ id string }

func (reason FinishReason) String() string { return reason.id }

var (
	FinishStop          = FinishReason{"stop"}
	FinishToolCalls     = FinishReason{"tool_calls"}
	FinishLength        = FinishReason{"length"}
	FinishContentFilter = FinishReason{"content_filter"}
)
