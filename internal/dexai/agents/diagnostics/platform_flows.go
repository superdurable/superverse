// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package diagnostics

import (
	"context"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput/generated"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
)

// platformFlow is a kind of platform Flow that a Session may read: its Flow
// type and the business Attributes its state shows, each with the fields of
// its value that the state keeps, or all of them when none are named. Every
// other Attribute stays hidden: it holds internal resource references,
// receipts or configuration pins.
type platformFlow struct {
	flowType   string
	attributes map[string][]string
}

// statusField is the field of a status record that a read always shows.
const statusField = "Status"

// preparedSourceFields are the fields a read shows of a prepared source, a
// Release's or a Preview's.
var preparedSourceFields = []string{statusField, "SourceCommit", "Failure"}

var (
	projectFlow = platformFlow{flowType: dex.GetFinalFlowType(&projects.ProjectFlow{}), attributes: map[string][]string{
		projects.ProjectStatus.AttributeName():           nil,
		projects.ProjectCurrentLive.AttributeName():      {"ReleaseID", "DeploymentID", "BuildRevisionID", "URL", "Revision", "Ready"},
		projects.ProjectActiveDeployment.AttributeName(): nil,
		projects.ProjectActiveCommand.AttributeName():    nil,
		projects.ProjectLastCommand.AttributeName():      nil,
		projects.ProjectLastFailure.AttributeName():      nil,
	}}
	setupFlow = platformFlow{flowType: dex.GetFinalFlowType(&setup.ProjectSetupFlow{}), attributes: map[string][]string{
		setup.SetupStatus.AttributeName():       nil,
		setup.SetupPhase.AttributeName():        nil,
		setup.SetupSourceCommit.AttributeName(): nil,
		setup.SetupFailure.AttributeName():      nil,
	}}
	releaseFlow = platformFlow{flowType: dex.GetFinalFlowType(&releases.ProjectReleaseFlow{}), attributes: map[string][]string{
		releases.ReleaseStatus.AttributeName():         nil,
		releases.PreparedSource.AttributeName():        preparedSourceFields,
		releases.ActiveBuildRevisionID.AttributeName(): nil,
		releases.ReadyBuildRevisionID.AttributeName():  nil,
		releases.FailureAttribute.AttributeName():      nil,
	}}
	deployFlow = platformFlow{flowType: dex.GetFinalFlowType(&deployments.ProjectDeployFlow{}), attributes: map[string][]string{
		deployments.DeploymentStatus.AttributeName():      nil,
		deployments.ReleaseID.AttributeName():             nil,
		deployments.BuildRevisionID.AttributeName():       nil,
		deployments.ConfigurationRevision.AttributeName(): nil,
		deployments.ProviderOperationRefs.AttributeName(): {"Phase"},
		deployments.FailureAttribute.AttributeName():      nil,
	}}
	previewFlow = platformFlow{flowType: dex.GetFinalFlowType(&preview.PreviewFlow{}), attributes: map[string][]string{
		preview.WorkspaceRequestIDAttribute.AttributeName():    nil,
		preview.WorkspaceStateAttribute.AttributeName():        nil,
		preview.HeadCommitAttribute.AttributeName():            nil,
		preview.PreviewAttribute.AttributeName():               {statusField, "URL", "SourceCommit", "Revision"},
		preview.PreparedPreviewSourceAttribute.AttributeName(): preparedSourceFields,
		preview.PreviewEnvironmentAttribute.AttributeName():    {statusField, "LastStop", "LastStopCause"},
		preview.ActiveCommandAttribute.AttributeName():         {"ID", "Kind"},
		preview.LastCommandAttribute.AttributeName():           {"ID", "Kind"},
		preview.FailureAttribute.AttributeName():               nil,
	}}
	provisionFlow = platformFlow{flowType: dex.GetFinalFlowType(&provision.SandboxProvisionFlow{}), attributes: map[string][]string{
		provision.CheckoutAttribute.AttributeName(): nil,
		provision.OutcomeAttribute.AttributeName():  {statusField, "Failure", "Reported", "ReportCause"},
	}}
	workspaceFlow = platformFlow{flowType: dex.GetFinalFlowType(&sessionworkspace.SessionWorkspaceFlow{}), attributes: map[string][]string{
		sessionworkspace.InstanceAttribute.AttributeName(): {"instance", "workspaceRequestId", "instanceId", "volumeState", "instanceUpdatedAt", "failure"},
		sessionworkspace.OutcomeAttribute.AttributeName():  {"workspaceRequestId", "kind", "failure", "reported"},
	}}
)

// platformFlowOf returns the kind of a FlowID that the Session may read: the
// Project's own ProjectFlow, Setup, Releases and Deployments, its Preview, its
// Session's workspace Flow and its Sandbox provisionings. Every other Project's Flows are refused, and so
// is the Session's own SessionAgentFlow, whose history is the conversation
// itself.
func platformFlowOf(identity agentloop.SessionIdentity, flowID string) (platformFlow, bool) {
	project := identity.ProjectID
	switch flowID {
	case flowid.ProjectFlowID(project):
		return projectFlow, true
	case flowid.PreviewFlowID(project):
		return previewFlow, true
	case flowid.SessionWorkspaceFlowID(project):
		return workspaceFlow, true
	}
	if _, provisioning := flowid.ParseSandboxProvision(project, flowID); provisioning {
		return provisionFlow, true
	}
	for kind, flow := range map[string]platformFlow{"setup": setupFlow, "release": releaseFlow, "deploy": deployFlow} {
		if _, child := flowid.ParseProjectChild(kind, project, flowID); child {
			return flow, true
		}
	}
	return platformFlow{}, false
}

// keep shows an allowlisted Attribute with its allowlisted fields.
func (flow platformFlow) keep(key string, value any) (any, bool) {
	fields, shown := flow.attributes[key]
	object, isObject := value.(map[string]any)
	if !shown || fields == nil || !isObject {
		return value, shown
	}
	kept := make(map[string]any, len(fields))
	for _, field := range fields {
		if entry, found := object[field]; found {
			kept[field] = entry
		}
	}
	return kept, true
}

// readPlatformFlow reads one platform Flow of the Session's Project. A FlowID
// outside the Project is refused before any read.
func (d *ProjectDiagnostics) readPlatformFlow(ctx context.Context, identity agentloop.SessionIdentity, arguments string) ([]byte, error) {
	input, err := decodeArguments(toolinput.ReadPlatformFlow.Decode, arguments)
	if err != nil {
		return nil, err
	}
	flow, admitted := platformFlowOf(identity, input.FlowID)
	if !admitted {
		return nil, &callFailureError{code: toolfailure.FlowNotInProject, message: "only this Project's project-, setup-, release- and deploy- Flows and this Session's preview-, session-workspace- and sandbox-provision- Flows are readable"}
	}
	summary, err := readSummary(ctx, d.Platform, input.FlowID)
	if err != nil {
		return nil, err
	}
	if summary.GetFlowType() != flow.flowType {
		return nil, &callFailureError{code: toolfailure.FlowNotInProject, message: input.FlowID + " is not a " + flow.flowType}
	}
	redaction := newRedactor(identity.ProjectID, d.Redactor)
	switch input.View {
	case generated.ReadPlatformFlowViewSummary:
		document, err := summaryOf(ctx, d.Platform, redaction, summary)
		if err != nil {
			return nil, err
		}
		return redaction.encode(document), nil
	case generated.ReadPlatformFlowViewState:
		state, err := readState(ctx, d.Platform, summary)
		if err != nil {
			return nil, err
		}
		return redaction.encode(stateDocument(redaction, summary, state, flow.keep)), nil
	default:
		page, err := readHistory(ctx, d.Platform, redaction, summary, int64(input.AfterEventID.Or(0)), input.PageSize.Or(50))
		if err != nil {
			return nil, err
		}
		return fittedHistory(redaction, page), nil
	}
}
