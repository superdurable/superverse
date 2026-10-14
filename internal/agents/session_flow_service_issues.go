// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"

	"github.com/superdurable/superversev2/internal/agents/platformissue"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
)

// ReportPlatformIssue starts the report of a platform fault of a Session: the
// PlatformIssueFlow of its signature files it as one GitHub issue, or only
// logs it where no issue repository is configured. A report of the signature
// from another call or Session may already run it, and this one joins it. A
// failed start is logged and returned, so the Step asks again. It reads no
// Flow and never changes the Session.
func (s *SessionFlowService) ReportPlatformIssue(ctx context.Context, id agentloop.SessionIdentity, incident agentloop.PlatformIncident) error {
	report, err := s.Issues.Report(ctx, platformissue.Incident{
		SessionFlowID: string(agentloop.SessionFlowID(id.ProjectID)), CallID: string(incident.CallID), Tool: string(incident.Tool), Code: incident.Code,
		Owner: incident.Owner, Effect: incident.Effect, ErrorType: incident.ErrorType, OccurredAt: incident.At,
	})
	attributes := []any{"project_id", id.ProjectID, "call_id", string(incident.CallID), "tool", string(incident.Tool), "code", incident.Code, "signature", incident.Signature()}
	switch {
	case err != nil:
		s.Logger.WarnContext(ctx, "platform incident report not started", append(attributes, "error", err.Error())...)
		return err
	case report.Joined:
		s.Logger.InfoContext(ctx, "platform incident joined a running report", append(attributes, "flow_id", report.FlowID)...)
	}
	return nil
}
