// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"fmt"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/dexai/agents/attachments"
	"github.com/superdurable/superversev2/internal/dexai/agents/platformissue"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/backgroundcommand"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/repobinding"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// serverFlows are the Flows the server registers with Dex. compose fills them
// with their dependencies; CheckFlowRegistry registers the same types without
// them, since registration reads only each Flow's declarations.
type serverFlows struct {
	teamSpace  *teamspaces.TeamSpaceFlow
	project    *projects.ProjectFlow
	setup      *setup.ProjectSetupFlow
	repository *repobinding.RepositoryBindingFlow
	provision  *provision.SandboxProvisionFlow
	workspace  *sessionworkspace.SessionWorkspaceFlow
	preview    *preview.PreviewFlow
	session    *sessionagent.SessionAgentFlow
	subagent   *subagent.SubagentFlow
	issue      *platformissue.PlatformIssueFlow
	background *backgroundcommand.BackgroundCommandFlow
	// contribution is the Flow of one connector a Session contributes.
	contribution *contribution.ConnectorContributionFlow
	release      *releases.ProjectReleaseFlow
	deployment   *deployments.ProjectDeployFlow
	// attachment is the Flow of one uploaded file of a TeamSpace.
	attachment *attachments.AttachmentFlow
	// evolve is the goal measurement of one Project.
	evolve *evolve.EvolveFlow
}

// list is every server Flow; a Flow left out is nil, which the registry refuses.
func (flows serverFlows) list() []dex.Flow {
	return []dex.Flow{flows.teamSpace, flows.project, flows.setup, flows.repository, flows.workspace, flows.provision, flows.preview, flows.session, flows.subagent, flows.background, flows.issue, flows.contribution, flows.release, flows.deployment, flows.attachment, flows.evolve}
}

// CheckFlowRegistry builds the actual SDK registry of every server Flow, as
// the server does before its Dex client and Worker start, and returns the
// registered Flow types. It is a static check: registration validates each
// Flow's primitives, Steps, RPCs and StepOptions with their failure routes
// and calls nothing outside the process. It also checks that the child Flow
// types a Project admits are the types Dex gives those Flows.
func CheckFlowRegistry() ([]string, error) {
	server := serverFlows{
		teamSpace: &teamspaces.TeamSpaceFlow{}, project: &projects.ProjectFlow{}, setup: &setup.ProjectSetupFlow{},
		repository: &repobinding.RepositoryBindingFlow{}, workspace: &sessionworkspace.SessionWorkspaceFlow{}, provision: &provision.SandboxProvisionFlow{}, preview: &preview.PreviewFlow{}, session: &sessionagent.SessionAgentFlow{},
		subagent: &subagent.SubagentFlow{}, background: &backgroundcommand.BackgroundCommandFlow{}, issue: &platformissue.PlatformIssueFlow{}, contribution: &contribution.ConnectorContributionFlow{}, release: &releases.ProjectReleaseFlow{}, deployment: &deployments.ProjectDeployFlow{},
		attachment: &attachments.AttachmentFlow{}, evolve: &evolve.EvolveFlow{},
	}
	flows := server.list()
	if _, err := dex.NewRegistry(flows); err != nil {
		return nil, err
	}
	children := map[projects.ChildFlow]dex.Flow{projects.ChildSetup: server.setup, projects.ChildSession: server.session, projects.ChildRelease: server.release, projects.ChildDeploy: server.deployment, projects.ChildEvolve: server.evolve}
	for child, flow := range children {
		if child.FlowType() != dex.GetFinalFlowType(flow) {
			return nil, fmt.Errorf("a Project admits the child Flow type %s, but Dex names the Flow %s", child.FlowType(), dex.GetFinalFlowType(flow))
		}
	}
	types := make([]string, 0, len(flows))
	for _, flow := range flows {
		types = append(types, dex.GetFinalFlowType(flow))
	}
	return types, nil
}
