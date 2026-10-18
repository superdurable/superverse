// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package orca

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/superversev2/cmd/dexai-connect/relay"
	"github.com/superdurable/superversev2/cmd/dexai-connect/state"
)

// History directions.
const (
	directionTail  = "tail"
	directionAfter = "after"
)

const (
	// maxHistoryLimit is Orca's largest history page.
	maxHistoryLimit = 200
	// pollInterval is how often the events are read. It stays short while
	// idle too, so a turn the owner starts in Orca is seen before its first
	// tool call needs it.
	pollInterval = time.Second
	// maxSeenItems bounds the items a subscription remembers.
	maxSeenItems = 4096
)

// Journal body kinds, message roles and turn states the Bridge reads.
const (
	bodyMessage        = "message"
	bodyTurn           = "turn"
	bodyToolCall       = "tool-call"
	blockSubagentGroup = "subagent-group"
	bodyApproval       = "approval"
	roleUser           = "user"
	roleAssistant      = "assistant"
	stateRunning       = "running"
	// A turn is over only when it completed or was interrupted; any other
	// state, unverifiable included, may still run.
	stateCompleted    = "completed"
	stateInterrupted  = "interrupted"
	resolutionPending = "pending"
	blockText         = "text"
	// superverseServer is the Bridge's own MCP server, whose calls the
	// platform reports itself.
	superverseServer = "superverse"
)

// Orca can settle a recovered subagent as unverifiable without settledAt.
// Its other subagent states stay open strings in the journal schema.
type subagentState string

const subagentStateUnverifiable subagentState = "unverifiable"

type cursor struct {
	Epoch    string `json:"epoch"`
	Sequence uint64 `json:"sequence"`
}

func (c *cursor) position() state.Position {
	if c == nil {
		return state.Position{}
	}
	return state.Position{Epoch: c.Epoch, Sequence: c.Sequence}
}

type historyAnswer struct {
	OK              bool   `json:"ok"`
	Reset           string `json:"reset"`
	Fence           int64  `json:"fence"`
	ProviderSession *struct {
		ID string `json:"id"`
	} `json:"providerSession"`
	Page historyPage `json:"page"`
}

type historyPage struct {
	Epoch  string       `json:"epoch"`
	Fence  int64        `json:"fence"`
	Items  []renderItem `json:"items"`
	Window struct {
		NextCursor *cursor `json:"nextCursor"`
	} `json:"window"`
	LiveCursor *cursor `json:"liveCursor"`
	HasNewer   bool    `json:"hasNewer"`
	LatestTurn *struct {
		ItemID string `json:"itemId"`
		Turn   struct {
			TurnID string `json:"turnId"`
			State  string `json:"state"`
		} `json:"turn"`
	} `json:"latestTurn"`
}

// latestTurn reads latestTurn ({itemId, turn}), or else the page's newest
// root turn item, so that one missing field cannot hide a running turn; an
// empty ID is a conversation without turns.
func (page historyPage) latestTurn() (turnID, turnState string) {
	itemID, _, turnState := page.latestTurnIdentity()
	return itemID, turnState
}

// latestTurnIdentity is latestTurn with the turn's own ID, which a cancel
// names so that Orca cancels that turn and no other.
func (page historyPage) latestTurnIdentity() (itemID, orcaTurnID, turnState string) {
	if page.LatestTurn != nil {
		return page.LatestTurn.ItemID, page.LatestTurn.Turn.TurnID, page.LatestTurn.Turn.State
	}
	for i := len(page.Items) - 1; i >= 0; i-- {
		if item := page.Items[i]; item.Body.Kind == bodyTurn && item.root() {
			return item.ItemID, item.Body.TurnID, item.Body.State
		}
	}
	return "", "", ""
}

func (page historyPage) turnRuns() bool {
	_, turnState := page.latestTurn()
	return turnState == stateRunning
}

// idle is a conversation whose latest turn is positively over, or that has
// none.
func (page historyPage) idle() bool {
	turnID, turnState := page.latestTurn()
	return turnID == "" || turnOver(turnState)
}

func turnOver(turnState string) bool {
	return turnState == stateCompleted || turnState == stateInterrupted
}

type renderItem struct {
	// AgentID names the subagent that produced a row of the shared journal;
	// it is absent or empty for the conversation's own (root) rows.
	AgentID  string `json:"agentId"`
	ItemID   string `json:"itemId"`
	Revision int64  `json:"revision"`
	Sequence uint64 `json:"sequence"`
	// ObservedAt is when Orca wrote the row's latest revision, in Unix ms.
	ObservedAt int64    `json:"observedAt"`
	Body       itemBody `json:"body"`
	TurnScope  *struct {
		TurnItemID string `json:"turnItemId"`
	} `json:"turnScope"`
}

type itemBody struct {
	Kind string `json:"kind"`
	// TurnID is a turn item's own turn ID.
	TurnID string `json:"turnId"`
	Role   string `json:"role"`
	State  string `json:"state"`
	Blocks []struct {
		Type   string `json:"type"`
		Text   string `json:"text"`
		Agents []struct {
			ID        string        `json:"id"`
			Label     string        `json:"label"`
			State     subagentState `json:"state"`
			SettledAt *int64        `json:"settledAt"`
		} `json:"agents"`
	} `json:"blocks"`
	UserItemID  string `json:"userItemId"`
	Name        string `json:"name"`
	MCPIdentity *struct {
		Server string `json:"server"`
	} `json:"mcpIdentity"`
	Resolution *struct {
		State string `json:"state"`
	} `json:"resolution"`
}

// root reports a row of the conversation itself, as Orca's root predicate
// does; a subagent's rows are never the conversation's messages or turns.
func (item renderItem) root() bool { return item.AgentID == "" }

// turnItemID is the turn item a row belongs to, or empty for a row of the
// thread itself, such as a user message or a turn.
func (item renderItem) turnItemID() string {
	if item.TurnScope == nil {
		return ""
	}
	return item.TurnScope.TurnItemID
}

func (body itemBody) text() string {
	var parts []string
	for _, block := range body.Blocks {
		if block.Type == blockText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// history reads one page of at most limit items. A page larger than Orca's
// response bound is read again with half the limit, down to one item; a
// single item over the bound is an error. It returns the limit that fit.
func (client *Client) history(ctx context.Context, sessionID, direction string, after *cursor, limit int) (historyAnswer, int, error) {
	for {
		params := map[string]any{"sessionId": sessionID, "direction": direction, "limit": limit}
		if after != nil {
			params["cursor"] = after
		}
		var answer historyAnswer
		err := client.read(ctx, methodHistory, params, &answer)
		if errors.Is(err, errAnswerTooLarge) && limit > 1 {
			limit /= 2
			continue
		}
		if errors.Is(err, errAnswerTooLarge) {
			return historyAnswer{}, 0, fmt.Errorf("an Orca history item of session %s exceeds the 4 MiB answer bound: %w", sessionID, err)
		}
		if err != nil {
			return historyAnswer{}, 0, err
		}
		client.noteFence(sessionID, max(answer.Fence, answer.Page.Fence))
		return answer, limit, nil
	}
}

// tail reads the session's newest item, with the turn state and cursors.
func (client *Client) tail(ctx context.Context, sessionID string) (historyAnswer, error) {
	answer, _, err := client.history(ctx, sessionID, directionTail, nil, 1)
	return answer, err
}

// boundTail is tail for a mutation of the binding's conversation: it fails
// with relay.ErrConversationChanged when the session holds another provider
// thread, and with a plain error when it holds none to confirm.
func (client *Client) boundTail(ctx context.Context, sessionID, providerThreadID string) (historyAnswer, error) {
	answer, err := client.tail(ctx, sessionID)
	if err != nil {
		return historyAnswer{}, err
	}
	if answer.ProviderSession == nil || answer.ProviderSession.ID == "" {
		return historyAnswer{}, fmt.Errorf("orca session %s has no Codex thread to confirm before changing it", sessionID)
	}
	if err := sameThread(answer, providerThreadID); err != nil {
		return historyAnswer{}, err
	}
	return answer, nil
}

// sameThread fails when the answer names another provider thread; one that
// names none proves no change.
func sameThread(answer historyAnswer, providerThreadID string) error {
	if answer.ProviderSession != nil && answer.ProviderSession.ID != "" && answer.ProviderSession.ID != providerThreadID {
		return fmt.Errorf("orca session holds another Codex thread: %w", relay.ErrConversationChanged)
	}
	return nil
}

// Conversation reads the session's provider thread and latest turn.
func (client *Client) Conversation(ctx context.Context, sessionID string) (relay.Conversation, error) {
	answer, err := client.tail(ctx, sessionID)
	if err != nil {
		return relay.Conversation{}, err
	}
	conversation := relay.Conversation{Turn: relay.TurnUnknown}
	if answer.ProviderSession != nil {
		conversation.ProviderThreadID = answer.ProviderSession.ID
	}
	conversation.TurnID, _ = answer.Page.latestTurn()
	switch {
	case answer.Page.idle():
		conversation.Turn = relay.TurnIdle
	case answer.Page.turnRuns():
		conversation.Turn = relay.TurnRunning
	}
	return conversation, nil
}

// History returns the session's public user and completed assistant
// messages from its latest page, newest last. The assistant messages of a
// latest turn that is not over may still be revised, so they are left to the
// events, which report them with that turn's completion.
func (client *Client) History(ctx context.Context, sessionID string, limit int) ([]relay.HistoryMessage, error) {
	answer, _, err := client.history(ctx, sessionID, directionTail, nil, min(limit, maxHistoryLimit))
	if err != nil {
		return nil, err
	}
	latestTurn, latestState := answer.Page.latestTurn()
	var messages []relay.HistoryMessage
	for _, item := range answer.Page.Items {
		body := item.Body
		if !item.root() || body.Kind != bodyMessage || body.State == stateRunning || body.text() == "" {
			continue
		}
		if item.turnItemID() != "" && item.turnItemID() == latestTurn && !turnOver(latestState) {
			continue
		}
		switch body.Role {
		case roleUser:
			messages = append(messages, relay.HistoryMessage{ID: item.ItemID, Role: relay.HistoryRoleUser, Text: body.text()})
		case roleAssistant:
			messages = append(messages, relay.HistoryMessage{ID: item.ItemID, Role: relay.HistoryRoleAssistant, Text: body.text()})
		}
	}
	return messages, nil
}

// Subscribe polls the session's history after the position, about once a
// second, and hands each page's new events to handle with the position after
// the page. A reset (a rewound or compacted journal) restarts from the
// current end, skipping what this subscription already reported.
func (client *Client) Subscribe(ctx context.Context, sessionID, providerThreadID string, after state.Position,
	handle func([]relay.Event, state.Position) error) error {
	seen := newSeenItems()
	next := &cursor{Epoch: after.Epoch, Sequence: after.Sequence}
	if after.IsZero() {
		tail, err := client.tail(ctx, sessionID)
		if err != nil {
			return err
		}
		next = tail.Page.LiveCursor
	}
	limit := maxHistoryLimit
	for {
		answer, fit, err := client.history(ctx, sessionID, directionAfter, next, limit)
		if err != nil {
			return err
		}
		if err := sameThread(answer, providerThreadID); err != nil {
			return err
		}
		// A page that needed a smaller limit keeps it for one page, then the
		// limit grows back.
		limit = min(max(2*fit, 1), maxHistoryLimit)
		page := answer.Page
		resume := page.Window.NextCursor
		if !answer.OK && answer.Reset != "" {
			resume = page.LiveCursor
		}
		if resume == nil {
			resume = next
		}
		events, err := seen.events(page, func(turn renderItem) ([]renderItem, error) {
			return client.turnMessages(ctx, sessionID, providerThreadID, page.Epoch, turn)
		})
		if err != nil {
			return err
		}
		if len(events) > 0 || resume.position() != next.position() {
			if err := handle(events, resume.position()); err != nil {
				return err
			}
		}
		next = resume
		if page.HasNewer && answer.OK {
			continue
		}
		if err := pause(ctx, pollInterval); err != nil {
			return err
		}
	}
}

// turnMessages reads back the assistant messages of a turn that is over, in
// the order they were first written, each at its last revision. A Codex
// message has no state that marks it final, but Orca writes a turn's
// completion after its messages' last revisions, so they are final now. The
// read pages from the turn's own first write to the journal's end, so it is
// bounded by the turn's items and what followed it.
func (client *Client) turnMessages(ctx context.Context, sessionID, providerThreadID, epoch string, turn renderItem) ([]renderItem, error) {
	next := &cursor{Epoch: epoch, Sequence: turn.Sequence}
	limit := maxHistoryLimit
	var messages []renderItem
	index := map[string]int{}
	for {
		answer, fit, err := client.history(ctx, sessionID, directionAfter, next, limit)
		if err != nil {
			return nil, err
		}
		if err := sameThread(answer, providerThreadID); err != nil {
			return nil, err
		}
		if !answer.OK {
			return nil, fmt.Errorf("read the messages of Orca turn %s: the journal was reset (%s)", turn.ItemID, answer.Reset)
		}
		for _, item := range answer.Page.Items {
			if !item.root() || item.Body.Kind != bodyMessage || item.Body.Role != roleAssistant || item.turnItemID() != turn.ItemID {
				continue
			}
			// A row revised between two pages comes again; its last revision wins.
			if at, known := index[item.ItemID]; known {
				messages[at] = item
				continue
			}
			index[item.ItemID] = len(messages)
			messages = append(messages, item)
		}
		if !answer.Page.HasNewer || answer.Page.Window.NextCursor == nil {
			return messages, nil
		}
		next, limit = answer.Page.Window.NextCursor, min(max(2*fit, 1), maxHistoryLimit)
	}
}

// seenItems remembers what a subscription reported of each item, so every
// event is emitted once whatever revisions arrive.
type seenItems struct {
	reported map[string]itemProgress
	order    []string
}

type itemProgress struct {
	started, finished bool
}

func newSeenItems() *seenItems { return &seenItems{reported: map[string]itemProgress{}} }

func (seen *seenItems) mark(itemID string, progress itemProgress) {
	if _, known := seen.reported[itemID]; !known {
		seen.order = append(seen.order, itemID)
		if len(seen.order) > maxSeenItems {
			delete(seen.reported, seen.order[0])
			seen.order = seen.order[1:]
		}
	}
	seen.reported[itemID] = progress
}

// events maps a page's items to relay events. A turn's assistant messages
// are read back by turnMessages and reported, final, just before its
// completion, in the same page's events. Reasoning, tool output, diffs and
// the Bridge's own tool calls are never passed on.
func (seen *seenItems) events(page historyPage, turnMessages func(turn renderItem) ([]renderItem, error)) ([]relay.Event, error) {
	var events []relay.Event
	for _, item := range page.Items {
		if !item.root() {
			continue
		}
		progress := seen.reported[item.ItemID]
		body := item.Body
		position := state.Position{Epoch: page.Epoch, Sequence: item.Sequence}
		event := relay.Event{Position: position, TurnID: item.turnItemID(), MessageID: item.ItemID}
		if body.Kind == bodyMessage {
			for _, block := range body.Blocks {
				if block.Type != blockSubagentGroup {
					continue
				}
				for _, agent := range block.Agents {
					if agent.ID == "" {
						continue
					}
					key := item.ItemID + "/" + agent.ID
					activity := seen.reported[key]
					if !activity.started {
						label := strings.Join(strings.Fields(agent.Label), " ")
						if label == "" {
							label = "subagent"
						}
						if characters := []rune(label); len(characters) > 80 {
							label = string(characters[:80])
						}
						events = append(events, relay.Event{Kind: relay.EventActivity, Position: position,
							TurnID: item.turnItemID(), MessageID: item.ItemID, Text: "Codex subagent started: " + label})
						activity.started = true
					}
					if (agent.SettledAt != nil || agent.State == subagentStateUnverifiable) && !activity.finished {
						events = append(events, relay.Event{Kind: relay.EventActivity, Position: position,
							TurnID: item.turnItemID(), MessageID: item.ItemID, Text: "Codex subagent finished"})
						activity.finished = true
					}
					seen.mark(key, activity)
				}
			}
		}
		switch body.Kind {
		case bodyMessage:
			text := body.text()
			if progress.finished || text == "" {
				continue
			}
			switch {
			case body.Role == roleUser:
				event.Kind = relay.EventUserMessage
			case body.Role == roleAssistant && item.turnItemID() == "" && body.State == stateCompleted:
				event.Kind = relay.EventAssistantMessage
			default:
				continue
			}
			event.Text = text
			events = append(events, event)
			progress.finished = true
		case bodyTurn:
			event.TurnID, event.MessageID = item.ItemID, body.UserItemID
			if !progress.started {
				event.Kind = relay.EventTurnStarted
				events = append(events, event)
				progress.started = true
			}
			if turnOver(body.State) && !progress.finished {
				messages, err := turnMessages(item)
				if err != nil {
					return nil, err
				}
				for _, message := range messages {
					if seen.reported[message.ItemID].finished || message.Body.text() == "" {
						continue
					}
					events = append(events, relay.Event{
						Kind: relay.EventAssistantMessage, Position: state.Position{Epoch: page.Epoch, Sequence: message.Sequence},
						TurnID: item.ItemID, MessageID: message.ItemID, Text: message.Body.text(),
					})
					seen.mark(message.ItemID, itemProgress{finished: true})
				}
				event.Kind = relay.EventTurnCompleted
				events = append(events, event)
				progress.finished = true
			}
		case bodyToolCall:
			if progress.started || (body.MCPIdentity != nil && body.MCPIdentity.Server == superverseServer) {
				continue
			}
			event.Kind, event.Text = relay.EventActivity, "Codex used "+body.Name+" on the Builder's machine"
			events = append(events, event)
			progress.started = true
		case bodyApproval:
			if progress.started || body.Resolution == nil || body.Resolution.State != resolutionPending {
				continue
			}
			event.Kind, event.Text = relay.EventActivity, "Codex is waiting for an approval in Orca"
			events = append(events, event)
			progress.started = true
		default:
			continue
		}
		seen.mark(item.ItemID, progress)
	}
	return events, nil
}
