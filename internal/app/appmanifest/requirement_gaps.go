// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package appmanifest

import (
	"fmt"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
)

// A requirement gap is one reason why a scope's stored configuration cannot
// start the application a source declares: a connection, its authorization,
// a trigger binding, a Step's operation use or an environment variable that is
// missing or invalid.
// The configuration host reports every gap at once. A gap holds names only,
// never a value, a credential or a field's content.

// RequirementGapKind is what a gap lacks. It is a struct that string literals
// cannot convert to: the kinds below are its only values, persisted by their
// names.
type RequirementGapKind struct{ definition *requirementGapDefinition }

type requirementGapDefinition struct {
	name string
	// phrase is how a gap's name says what it lacks.
	phrase string
	// wire is the kind in the business API.
	wire pb.ConfigurationRequirementGap_Kind
}

var (
	// GapConnectionRequired: a declared connection is not configured for its
	// connector release and authentication method.
	GapConnectionRequired = RequirementGapKind{&requirementGapDefinition{"CONNECTION_REQUIRED", "connection required", pb.ConfigurationRequirementGap_KIND_CONNECTION_REQUIRED}}
	// GapAuthorizationRequired: the connection has no READY credential of its
	// authentication method.
	GapAuthorizationRequired = RequirementGapKind{&requirementGapDefinition{"AUTHORIZATION_REQUIRED", "authorization required", pb.ConfigurationRequirementGap_KIND_AUTHORIZATION_REQUIRED}}
	// GapConnectionIncomplete: the connection's configuration fields are
	// incomplete or invalid.
	GapConnectionIncomplete = RequirementGapKind{&requirementGapDefinition{"CONNECTION_INCOMPLETE", "configuration incomplete", pb.ConfigurationRequirementGap_KIND_CONNECTION_INCOMPLETE}}
	// GapTriggerBindingRequired: a declared trigger binding is not configured,
	// or its saved configuration lacks a value for a required unit.
	GapTriggerBindingRequired = RequirementGapKind{&requirementGapDefinition{"TRIGGER_BINDING_REQUIRED", "trigger binding required", pb.ConfigurationRequirementGap_KIND_TRIGGER_BINDING_REQUIRED}}
	// GapTriggerBindingInvalid: a configured trigger binding is invalid.
	GapTriggerBindingInvalid = RequirementGapKind{&requirementGapDefinition{"TRIGGER_BINDING_INVALID", "trigger binding invalid", pb.ConfigurationRequirementGap_KIND_TRIGGER_BINDING_INVALID}}
	// GapOperationConfigurationRequired: a Step's connector operation use lacks
	// a value for a required unit, saved or not.
	GapOperationConfigurationRequired = RequirementGapKind{&requirementGapDefinition{"OPERATION_CONFIGURATION_REQUIRED", "operation configuration required", pb.ConfigurationRequirementGap_KIND_OPERATION_CONFIGURATION_REQUIRED}}
	// GapEnvironmentRequired: a declared environment variable is missing or
	// invalid, or the stored environment holds one the source no longer
	// declares, which a save of the environment removes.
	GapEnvironmentRequired = RequirementGapKind{&requirementGapDefinition{"ENVIRONMENT_REQUIRED", "environment required", pb.ConfigurationRequirementGap_KIND_ENVIRONMENT_REQUIRED}}
)

var requirementGapKinds = []RequirementGapKind{
	GapConnectionRequired, GapAuthorizationRequired, GapConnectionIncomplete, GapTriggerBindingRequired, GapTriggerBindingInvalid, GapOperationConfigurationRequired,
	GapEnvironmentRequired,
}

// String is the persisted spelling of the kind.
func (kind RequirementGapKind) String() string { return kind.definition.name }

// MarshalText writes the persisted spelling.
func (kind RequirementGapKind) MarshalText() ([]byte, error) { return []byte(kind.String()), nil }

// UnmarshalText reads a persisted spelling.
func (kind *RequirementGapKind) UnmarshalText(text []byte) error {
	for _, declared := range requirementGapKinds {
		if declared.definition.name == string(text) {
			*kind = declared
			return nil
		}
	}
	return fmt.Errorf("unknown requirement gap kind %q", text)
}

// FollowRefusal is why an item of Live's next publish can never follow
// Preview. It is a struct that string literals cannot convert to: the zero
// value is no refusal, and the refusals below are its only other values,
// persisted by their names.
type FollowRefusal struct{ definition *followRefusalDefinition }

type followRefusalDefinition struct {
	name string
	// wire is the refusal in the business API.
	wire pb.ConfigurationFollowRefusal
}

var (
	// FollowRefusedSecret: a secret environment value, which is never copied
	// across scopes.
	FollowRefusedSecret = FollowRefusal{&followRefusalDefinition{"SECRET", pb.ConfigurationFollowRefusal_CONFIGURATION_FOLLOW_REFUSAL_SECRET}}
	// FollowRefusedEndpointBound: a connection tied to one address, which
	// cannot serve both Preview's and Live's addresses.
	FollowRefusedEndpointBound = FollowRefusal{&followRefusalDefinition{"ENDPOINT_BOUND", pb.ConfigurationFollowRefusal_CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND}}
)

var followRefusals = []FollowRefusal{FollowRefusedSecret, FollowRefusedEndpointBound}

// String is the persisted spelling of the refusal, "" for none.
func (refusal FollowRefusal) String() string {
	if refusal.definition == nil {
		return ""
	}
	return refusal.definition.name
}

// Wire is the refusal in the business API, unspecified for none.
func (refusal FollowRefusal) Wire() pb.ConfigurationFollowRefusal {
	if refusal.definition == nil {
		return pb.ConfigurationFollowRefusal_CONFIGURATION_FOLLOW_REFUSAL_UNSPECIFIED
	}
	return refusal.definition.wire
}

// IsZero reports no refusal, which omitzero also leaves out of JSON.
func (refusal FollowRefusal) IsZero() bool { return refusal.definition == nil }

// MarshalText writes the persisted spelling.
func (refusal FollowRefusal) MarshalText() ([]byte, error) { return []byte(refusal.String()), nil }

// UnmarshalText reads a persisted spelling; "" is no refusal.
func (refusal *FollowRefusal) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*refusal = FollowRefusal{}
		return nil
	}
	for _, declared := range followRefusals {
		if declared.definition.name == string(text) {
			*refusal = declared
			return nil
		}
	}
	return fmt.Errorf("unknown follow refusal %q", text)
}

// RequirementGap is one gap: its kind and the names of what it concerns. A
// connection gap names the connection and its connector, a trigger gap also
// the trigger and its binding, an operation use gap the Flow, Step and
// operation, and an environment gap the variable, or none
// when the stored environment must only be saved again.
type RequirementGap struct {
	Kind            RequirementGapKind `json:"kind"`
	ConnectionName  string             `json:"connectionName,omitempty"`
	ConnectorID     string             `json:"connectorId,omitempty"`
	TriggerName     string             `json:"triggerName,omitempty"`
	BindingName     string             `json:"bindingName,omitempty"`
	EnvironmentName string             `json:"environmentName,omitempty"`
	// UnitLabel names the required Connector UI unit a saved trigger binding
	// or an operation use still lacks a value for.
	UnitLabel   string `json:"unitLabel,omitempty"`
	FlowName    string `json:"flowName,omitempty"`
	StepName    string `json:"stepName,omitempty"`
	OperationID string `json:"operationId,omitempty"`
	// FollowRefusal is set on a Live gap of an item that cannot follow
	// Preview and has no value of Live's own.
	FollowRefusal FollowRefusal `json:"followRefusal,omitzero"`
}

// Name says what the gap concerns and what it lacks, such as
// "gmail (authorization required)", "environment SIGNING_SECRET (environment
// required)", "slack trigger threadReplyCreated/reply (trigger binding
// required: Approval channel)" or "slack step Flow/PostSlackCompletion
// operation postThreadReply (operation configuration required: Completion
// reply)".
func (gap RequirementGap) Name() string {
	var subject string
	switch {
	case gap.TriggerName != "":
		subject = gap.ConnectionName + " trigger " + gap.TriggerName + "/" + gap.BindingName
	case gap.StepName != "":
		subject = gap.ConnectionName + " step " + gap.FlowName + "/" + gap.StepName + " operation " + gap.OperationID
	case gap.ConnectionName != "":
		subject = gap.ConnectionName
	case gap.EnvironmentName != "":
		subject = "environment " + gap.EnvironmentName
	default:
		subject = "application environment"
	}
	if gap.UnitLabel != "" {
		return subject + " (" + gap.Kind.definition.phrase + ": " + gap.UnitLabel + ")"
	}
	return subject + " (" + gap.Kind.definition.phrase + ")"
}

// RequirementGapSnapshots are gaps as the business API shows them, in order;
// nil without gaps.
func RequirementGapSnapshots(gaps []RequirementGap) []*pb.ConfigurationRequirementGap {
	var snapshots []*pb.ConfigurationRequirementGap
	for _, gap := range gaps {
		snapshots = append(snapshots, &pb.ConfigurationRequirementGap{Kind: gap.Kind.definition.wire, ConnectionName: gap.ConnectionName, ConnectorId: gap.ConnectorID,
			TriggerName: gap.TriggerName, BindingName: gap.BindingName, EnvironmentName: gap.EnvironmentName, UnitLabel: gap.UnitLabel,
			FlowName: gap.FlowName, StepName: gap.StepName, OperationId: gap.OperationID, FollowRefusal: gap.FollowRefusal.Wire()})
	}
	return snapshots
}

// RequirementGapNames are the names of gaps, in order.
func RequirementGapNames(gaps []RequirementGap) []string {
	names := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		names = append(names, gap.Name())
	}
	return names
}
