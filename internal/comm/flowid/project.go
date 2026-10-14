// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package flowid

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidIdentity classifies a caller-supplied ID that does not have the
// format of the resource it names. Retrying such a request cannot succeed.
var ErrInvalidIdentity = errors.New("invalid resource identity")

// ValidateProjectID checks a caller-supplied Project ID. Every Project ID the
// server assigns has exactly the configured length.
func ValidateProjectID(projectID string, length int) error {
	if len(projectID) != length || !IsLowerAlphanumeric(projectID) {
		return fmt.Errorf("%w: project ID must contain %d lowercase letters or digits", ErrInvalidIdentity, length)
	}
	return nil
}

// ProjectFlowID addresses the ProjectFlow of a Project, for example project-a7k2.
func ProjectFlowID(projectID string) string {
	return "project-" + projectID
}

var childIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,159}$`)

// ProjectChild validates caller-supplied IDs and derives the FlowID
// <kind>-<projectID>-<childID> of a Flow that a Project admits as a cleanup
// child, for example release-a7k2-<commit>. Kind is "setup", "release" or
// "deploy". IDs the server assigned use SetupFlowID instead, and the
// Project's one Session, which has no child ID, is SessionFlowID.
func ProjectChild(kind, projectID, childID string) (string, error) {
	switch kind {
	case "setup", "release", "deploy":
	default:
		panic("unsupported project child kind " + kind)
	}
	if !IsLowerAlphanumeric(projectID) || !childIDPattern.MatchString(childID) {
		return "", ErrInvalidIdentity
	}
	return kind + "-" + projectID + "-" + childID, nil
}

// ParseProjectChild returns the child ID of a FlowID that ProjectChild derives
// for kind and projectID, and false for every other FlowID.
func ParseProjectChild(kind, projectID, flowID string) (string, bool) {
	childID, found := strings.CutPrefix(flowID, kind+"-"+projectID+"-")
	if !found {
		return "", false
	}
	_, err := ProjectChild(kind, projectID, childID)
	return childID, err == nil
}

// SetupFlowID addresses the ProjectSetupFlow of an accepted Project, for
// example setup-a7k2-k3n8q5r1.
func SetupFlowID(projectID, setupID string) string {
	return "setup-" + projectID + "-" + setupID
}

// SessionFlowID addresses the SessionAgentFlow of a Project, its one Session,
// for example session-a7k2. The Project ID is the Session's only business
// identity: no Session ID exists.
func SessionFlowID(projectID string) string {
	return "session-" + projectID
}

// SandboxProvisionFlowID addresses the SandboxProvisionFlow of one Sandbox
// instance of a Project's Session, for example sandbox-provision-a7k2-ws-3.
// The WorkspaceRequestID names the instance, and the Session never repeats
// it; a Project ID contains no "-", so the format is unambiguous.
func SandboxProvisionFlowID(projectID, workspaceRequestID string) string {
	return "sandbox-provision-" + projectID + "-" + workspaceRequestID
}

// SessionWorkspaceFlowID addresses the SessionWorkspaceFlow of a Project, the
// one owner of its Session's Sandbox instances, for example
// session-workspace-a7k2.
func SessionWorkspaceFlowID(projectID string) string {
	return "session-workspace-" + projectID
}

var workspaceRequestIDPattern = regexp.MustCompile(`^ws-[0-9]+$`)

// WorkspaceRequestNumber returns n of a WorkspaceRequestID ws-<n>, which
// orders the requests of one Session, and false for any other value.
func WorkspaceRequestNumber(workspaceRequestID string) (uint64, bool) {
	if !workspaceRequestIDPattern.MatchString(workspaceRequestID) {
		return 0, false
	}
	number, err := strconv.ParseUint(strings.TrimPrefix(workspaceRequestID, "ws-"), 10, 64)
	return number, err == nil
}

// ParseSandboxProvision returns the WorkspaceRequestID of a FlowID that
// SandboxProvisionFlowID derives for the Project, and false for every other
// FlowID.
func ParseSandboxProvision(projectID, flowID string) (string, bool) {
	requestID, found := strings.CutPrefix(flowID, SandboxProvisionFlowID(projectID, ""))
	return requestID, found && workspaceRequestIDPattern.MatchString(requestID)
}

// SubagentFlowID addresses the SubagentFlow of one subagent of the Project's
// Session, for example subagent-a7k2-k3n8q5r1. The agent ID is AgentTaskID of
// the call that started it.
func SubagentFlowID(projectID, agentID string) string {
	return "subagent-" + projectID + "-" + agentID
}

// ParseSubagentFlowID returns the agent ID of a FlowID that SubagentFlowID
// derives for the Project, and false for every other FlowID.
func ParseSubagentFlowID(projectID, flowID string) (string, bool) {
	agentID, found := strings.CutPrefix(flowID, SubagentFlowID(projectID, ""))
	return agentID, found && IsAgentID(agentID)
}

// ConnectorContributionFlowID addresses the ConnectorContributionFlow of one
// connector the Project's Session contributes to the connectors library, for
// example connector-contribution-a7k2-newsapi. The slug is the connector's ID;
// a Project ID contains no "-", so the format is unambiguous.
func ConnectorContributionFlowID(projectID, slug string) string {
	return "connector-contribution-" + projectID + "-" + slug
}

// ParseConnectorContributionFlowID returns the slug of a FlowID that
// ConnectorContributionFlowID derives for the Project, and false for every
// other FlowID.
func ParseConnectorContributionFlowID(projectID, flowID string) (string, bool) {
	slug, found := strings.CutPrefix(flowID, ConnectorContributionFlowID(projectID, ""))
	return slug, found && slug != ""
}

// PreviewFlowID addresses the PreviewFlow of a Project, its one Preview, for
// example preview-a7k2.
func PreviewFlowID(projectID string) string {
	return "preview-" + projectID
}

// RepositoryBindingFlowID addresses the RepositoryBindingFlow of a GitHub
// repository, for example repository-123456789.
func RepositoryBindingFlowID(repositoryID int64) string {
	return "repository-" + strconv.FormatInt(repositoryID, 10)
}

var deploymentIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,34}[a-z0-9])?$`)

// PreviewWorkloadID is the workload ID of the Project's Preview grants. It
// names their generations, app-preview-<generation>, beside the Live
// deployments' app-<deployment>-<generation> in the same Project storage, so
// no deployment may take it.
const PreviewWorkloadID = "preview"

// ValidateDeploymentID bounds the identity that Live resource names carry:
// app-<deployment>-<uint64 generation> stays within the 63-byte Service name
// limit, and it never names the Preview's grants.
func ValidateDeploymentID(deploymentID string) error {
	switch {
	case !deploymentIDPattern.MatchString(deploymentID):
		return fmt.Errorf("%w: deployment ID must be 1 to 36 lowercase letters, digits or interior hyphens", ErrInvalidIdentity)
	case deploymentID == PreviewWorkloadID:
		return fmt.Errorf("%w: deployment ID %q is reserved for the Preview", ErrInvalidIdentity, deploymentID)
	}
	return nil
}

// IsLowerAlphanumeric reports whether value is a non-empty identifier of
// lowercase letters and digits, the form of Project IDs.
func IsLowerAlphanumeric(value string) bool {
	return value != "" && strings.Trim(value, alphabet) == ""
}
