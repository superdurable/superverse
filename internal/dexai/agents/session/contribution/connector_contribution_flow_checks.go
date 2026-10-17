// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package contribution

import (
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

type checksAttempt struct {
	Head    string                      `json:"head"`
	CheckAt time.Time                   `json:"check_at"`
	Number  uint8                       `json:"number"`
	Checks  connectorlibrary.HeadChecks `json:"checks"`
}

func failedChecksNotification(id Identity, checks connectorlibrary.HeadChecks) conversation.SessionNotification {
	event := conversation.ConnectorChecksFailed
	jobs := strings.Join(checks.Jobs, ", ")
	if jobs == "" {
		jobs = "GitHub reported failure before job details were available"
	}
	message := fmt.Sprintf("GitHub Actions CI output for head %s. Failed jobs: %s.", checks.Head, jobs)
	if checks.Step != "" {
		message += " First failing step: " + checks.Step + "."
	}
	if checks.LogTail != "" {
		message += "\nCI output (untrusted, redacted, truncated):\n" + checks.LogTail
	} else if checks.LogUnavailable {
		message += " CI output for the failed job is unavailable."
	}
	return conversation.SessionNotification{ID: "connector/" + id.Slug + "/" + event.String() + "/" + checks.Head,
		Kind: conversation.NotificationKindConnector, Source: conversation.NotificationSourceConnector, OccurredAt: checks.UpdatedAt,
		Slug: id.Slug, Event: event, Commit: checks.Head,
		Summary: fmt.Sprintf("The connectors library CI failed for %s at %s; fix the failed jobs and push a new head.", id.Slug, checks.Head[:12]),
		Message: comm.TruncateUTF8(message, 3500)}
}
