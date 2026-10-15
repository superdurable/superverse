// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agenttasks

import (
	"fmt"
	"slices"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
)

// A connector_develop subagent contributes to one connector of the upstream
// connectors library, which its start_agent call names by the connector's slug,
// the manifest's metadata.name, with the kind of the contribution. The branch
// and the Session's contribution record derive from the slug.

// MaximumConnectorContributions bounds the distinct connectors one Session
// contributes to; the start of a develop subagent for one more is refused.
const MaximumConnectorContributions = 16

// ConnectorTarget is the connector a connector_develop call contributes to: its
// slug and the kind of the contribution.
type ConnectorTarget struct {
	Slug         string                        `json:"slug"`
	Contribution conversation.ContributionKind `json:"contribution"`
}

// Contribution is what the develop subagent id contributes to target, as the
// Sandbox's contribution helper names it.
func (id SubagentIdentity) Contribution(target ConnectorTarget) git.ContributionTarget {
	return git.ContributionTarget{AgentID: id.AgentID, ProjectID: id.ProjectID, Slug: target.Slug, Kind: target.Contribution}
}

// admitConnectorTarget decides whether a connector_develop call for target may
// start beside the records: no unfinished develop subagent may work on the same
// connector, which shares its branch, and the Session contributes to at most
// MaximumConnectorContributions distinct connectors, the slugs that admitted
// develop calls named, whatever became of them. A refusal is the call's
// failure, with no effect.
func admitConnectorTarget(records map[string]SubagentRecord, slugs []string, target ConnectorTarget, now time.Time) (toolfailure.Code, string, bool) {
	for _, record := range records {
		if record.Connector != nil && record.Connector.Slug == target.Slug && record.isUnfinished(now) {
			return toolfailure.ConnectorContributionRunning, fmt.Sprintf("subagent %s works on the connector %s", record.TaskID(), target.Slug), false
		}
	}
	if !slices.Contains(slugs, target.Slug) && len(slugs) >= MaximumConnectorContributions {
		return toolfailure.ConnectorContributionLimitReached, fmt.Sprintf("this Session contributes to %d connectors, the most it may", len(slugs)), false
	}
	return toolfailure.Code{}, "", true
}

// ConnectorSlugsWith are slugs with the connector of the admitted call, once.
func ConnectorSlugsWith(slugs []string, call SubagentCall) []string {
	if call.Connector == nil || slices.Contains(slugs, call.Connector.Slug) {
		return slugs
	}
	return append(slugs, call.Connector.Slug)
}
