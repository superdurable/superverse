// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowid

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
)

// alphabet is the character set of every generated ID.
const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// RandomID returns length uniformly random lowercase letters and digits.
func RandomID(length int) string {
	id := make([]byte, 0, length)
	buffer := make([]byte, length*2)
	for len(id) < length {
		// crypto/rand.Read never returns an error.
		comm.Must(rand.Read(buffer))
		id = appendIDCharacters(id, length, buffer)
	}
	return string(id)
}

// ProjectCreationIDs assigns the Project and Setup IDs of one logical create
// reproducibly from the caller's idempotency key. A replay, also after response
// loss or login replacement, addresses the same Project without a lookup store;
// ProjectFlow's full input fingerprint rejects a short-ID collision.
func ProjectCreationIDs(accountID, requestID string, projectIDLength int) (projectID, setupID string) {
	const domain = "superverse-v2-project-creation-v1"
	return creationID(domain, "project", accountID, requestID, projectIDLength), creationID(domain, "setup", accountID, requestID, 8)
}

// TeamSpaceCreationID assigns the TeamSpace ID of one logical create
// reproducibly from the caller's idempotency key, as ProjectCreationIDs does
// for a Project; TeamSpaceFlow's start fingerprint rejects a collision.
func TeamSpaceCreationID(accountID, requestID string) string {
	return creationID("superverse-v2-teamspace-creation-v1", "teamspace", accountID, requestID, teamSpaceIDLength)
}

// TeamSpaceConnectionCreationID assigns the ID of a TeamSpace connection
// reproducibly from its creator's idempotency key, so a replay addresses the
// same connection; the connection's creation fingerprint rejects a replay
// with other content. It is c followed by eleven lowercase letters and
// digits, such as ck3n8q5r1m2p.
func TeamSpaceConnectionCreationID(accountID, requestID string) string {
	return "c" + creationID("superverse-v2-teamspace-connection-creation-v1", "connection", accountID, requestID, 11)
}

// agentIDLength is the length of a subagent's ID.
const agentIDLength = 8

// AgentTaskID derives the ID of the subagent that the tool call callID of the
// Session sessionFlowID starts, for example k3n8q5r1: eight lowercase letters
// and digits, the same for every retry of the call and distinct for every
// call.
func AgentTaskID(sessionFlowID, callID string) string {
	return creationID("superverse-v2-agent-task-v1", "agent", sessionFlowID, callID, agentIDLength)
}

// IsAgentID reports whether value has the form of an ID AgentTaskID derives.
func IsAgentID(value string) bool {
	return len(value) == agentIDLength && IsLowerAlphanumeric(value)
}

// subagentTaskPrefix begins the task ID of every subagent.
const subagentTaskPrefix = "agent-"

// SubagentTaskID is the task ID that the model and the user read of the
// subagent agentID, for example agent-k3n8q5r1.
func SubagentTaskID(agentID string) string { return subagentTaskPrefix + agentID }

// ParseSubagentTaskID returns the agent ID a task ID names, agent-<agent ID>
// or the bare agent ID, and false for anything else, such as the task ID of a
// command.
func ParseSubagentTaskID(taskID string) (string, bool) {
	agentID := strings.TrimPrefix(strings.TrimSpace(taskID), subagentTaskPrefix)
	return agentID, IsAgentID(agentID)
}

// creationID expands the hash of its inputs with a counter until it yields
// size characters.
func creationID(domain, purpose, accountID, requestID string, size int) string {
	id := make([]byte, 0, size)
	for counter := uint32(0); len(id) < size; counter++ {
		// The tags repeat the field names: the encoding is part of every ID.
		sum := sha256.Sum256(comm.Must(json.Marshal(struct {
			Domain  string `json:"Domain"`
			Purpose string `json:"Purpose"`
			Account string `json:"Account"`
			Request string `json:"Request"`
			Counter uint32 `json:"Counter"`
		}{domain, purpose, accountID, requestID, counter})))
		id = appendIDCharacters(id, size, sum[:])
	}
	return string(id)
}

// appendIDCharacters maps source bytes onto the alphabet until id holds size
// characters. Bytes from 252 up are skipped so every character stays equally
// likely instead of favouring the start of the alphabet.
func appendIDCharacters(id []byte, size int, source []byte) []byte {
	for _, b := range source {
		if len(id) == size {
			break
		}
		if b < 252 {
			id = append(id, alphabet[int(b)%len(alphabet)])
		}
	}
	return id
}
