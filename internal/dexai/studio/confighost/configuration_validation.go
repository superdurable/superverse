// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package confighost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi/fdg"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// ValidationRequest validates a configuration revision against the AppManifest
// it was edited for. Without CheckOnly or Snapshot, a READY configuration is
// frozen: a Live commit's draft under the RequestID of the build that pins it.
type ValidationRequest struct {
	// CheckOnly reports readiness without initializing or freezing anything.
	CheckOnly bool `json:"checkOnly,omitempty"`
	// Snapshot validates an exact frozen version instead of the draft.
	Snapshot              *connectorconfig.SnapshotRef `json:"snapshot,omitempty"`
	RequestID             string                       `json:"requestId,omitempty"`
	AppManifestRevision   uint64                       `json:"appManifestRevision"`
	ManifestDigest        string                       `json:"manifestDigest"`
	ConfigurationRevision uint64                       `json:"configurationRevision"`
}

// notReadyError is why a configuration cannot serve a build yet, as
// opposed to a failed read of stored configuration.
type notReadyError struct {
	status        int
	code, message string
}

func (failure *notReadyError) Error() string { return failure.message }

// handleValidate validates a configuration revision of the request's prepared
// source and, unless asked only to check, freezes it: a Live commit's draft
// only for a build, and Preview's canonical head at the version the revision
// was written as. A build that already froze returns its receipt.
func (host *ConfigurationHost) handleValidate(response http.ResponseWriter, request *http.Request) {
	admission, internal := requireInternal(response, request)
	if !internal {
		return
	}
	ctx := request.Context()
	var validation ValidationRequest
	// Every validation is of a prepared source, whose AppManifest never changes;
	// the next publish is validated as it resolves, never as the layer itself.
	if decodeBody(response, request, 1<<16, &validation) != nil || admission.SourceCommit == "" || admission.nextPublish != nil {
		writeCodedError(response, http.StatusBadRequest, "PROJECT_CONFIGURATION_INVALID", "Configuration validation request is invalid")
		return
	}
	manifest, err := host.project.readManifest(ctx, admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if manifest.Revision != validation.AppManifestRevision || manifest.ManifestDigest != validation.ManifestDigest {
		writeStoreError(response, connectorconfig.ErrConflict)
		return
	}
	freezesBuild := admission.liveDraft && validation.RequestID != ""
	if freezesBuild {
		if !validReleaseBuildID(validation.RequestID) {
			writeCodedError(response, http.StatusBadRequest, "RELEASE_BUILD_INVALID", "An exact build identity is required")
			return
		}
		receipt, readErr := host.project.readBuildFreeze(ctx, host.project.buildFreezeKey(manifest.SourceCommit, validation.RequestID))
		if readErr == nil {
			if receipt.DraftRevision != validation.ConfigurationRevision || receipt.ManifestDigest != validation.ManifestDigest {
				writeStoreError(response, connectorconfig.ErrConflict)
				return
			}
			summary := host.project.summary(manifest, receipt.DraftRevision)
			summary.Status, summary.Snapshot, summary.SnapshotConfigurationRevision = "READY", &receipt.Snapshot, receipt.ConfigurationRevision
			writeJSON(response, http.StatusOK, summary)
			return
		}
		if !errors.Is(readErr, connectorconfig.ErrObjectNotFound) {
			writeStoreError(response, readErr)
			return
		}
	}
	draft, err := host.project.loadConnectionDraft(ctx, admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if validation.Snapshot != nil {
		if draft.document, err = host.project.canonical.ReadSnapshot(ctx, *validation.Snapshot); err != nil {
			writeStoreError(response, err)
			return
		}
	}
	// A Preview start applies exactly the revision its user saw; only the
	// settings refresh its own earlier attempt wrote may stand in for it.
	appliesPreview := !admission.liveDraft && !validation.CheckOnly && validation.Snapshot == nil
	if draft.document.Revision != validation.ConfigurationRevision {
		refreshed := false
		if appliesPreview {
			if refreshed, err = host.project.wroteRefresh(ctx, validation.ConfigurationRevision, draft.document); err != nil {
				writeStoreError(response, err)
				return
			}
		}
		if !refreshed {
			if appliesPreview {
				writeCodedError(response, http.StatusConflict, PreviewRevisionChangedCode, "The configuration changed after the Preview start was requested")
			} else {
				writeStoreError(response, connectorconfig.ErrConflict)
			}
			return
		}
	}
	bundle, err := host.project.preparedBundle(ctx, admission)
	if err != nil {
		writeDefinitionError(response, err)
		return
	}
	if err = host.readiness(ctx, draft, manifest, bundle); err != nil {
		if failure, notReady := errors.AsType[*notReadyError](err); notReady {
			writeCodedError(response, failure.status, failure.code, failure.message)
		} else {
			writeStoreError(response, err)
		}
		return
	}
	summary := host.project.summary(manifest, validation.ConfigurationRevision)
	summary.Status = "READY"
	// A check and an exact snapshot are read-only: refreshing a page or
	// inspecting deployment readiness never initializes or freezes anything.
	if validation.CheckOnly || validation.Snapshot != nil {
		summary.Snapshot = validation.Snapshot
		writeJSON(response, http.StatusOK, summary)
		return
	}
	if draft.document.Revision == 0 {
		if draft.document, _, err = draft.store.WriteConfiguration(ctx, 0, draft.document); err != nil {
			writeStoreError(response, err)
			return
		}
		summary.ConfigurationRevision = draft.document.Revision
	}
	if admission.liveDraft && !freezesBuild {
		// A Live commit's draft is frozen only for a build.
		writeJSON(response, http.StatusOK, summary)
		return
	}
	// A freeze copies the selected connections' current settings into the
	// entries it freezes: a build freezes them with its version, and a
	// Preview start writes them into the head it freezes.
	refreshed, teamSpaceID, selected, err := host.refreshSharedSettings(ctx, draft.document, manifest)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	var snapshot connectorconfig.SnapshotRef
	snapshotRevision := draft.document.Revision
	if freezesBuild {
		snapshot, snapshotRevision, err = host.project.freezeBuild(ctx, manifest, refreshed, validation.RequestID)
	} else {
		if !sameSettings(refreshed, draft.document) {
			// The intent names the write before it happens, so a retry after a lost
			// answer recognizes the head it wrote (wroteRefresh).
			if err = host.project.recordRefresh(ctx, validation.ConfigurationRevision, draft.document.Revision, refreshed); err != nil {
				writeStoreError(response, err)
				return
			}
			if draft.document, _, err = draft.store.WriteConfiguration(ctx, draft.document.Revision, refreshed); err != nil {
				writeStoreError(response, err)
				return
			}
			snapshotRevision = draft.document.Revision
		}
		// Every edit of Preview's canonical head is a new object version, and
		// the running application stays pinned to the version it accepted.
		snapshot, err = draft.store.FreezeConfiguration(ctx, draft.document.Revision)
		if err == nil {
			// The Preview's usages become the connections it now runs with.
			err = host.shared.Usages.ReplaceUsages(ctx, teamSpaceID, selected, host.project.scope.ProjectID, host.project.scopeKind())
		}
		if err == nil {
			// Registrations of connection names the frozen document no longer
			// selects free their slots.
			err = host.shared.Usages.RetainTriggerRegistrations(ctx, host.project.scope.ProjectID, host.project.scopeKind(), SharedSelections(refreshed))
		}
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	summary.Snapshot, summary.SnapshotConfigurationRevision = &snapshot, snapshotRevision
	writeJSON(response, http.StatusOK, summary)
}

// RequirementsRequest asks which requirements of the request's prepared
// source the scope's configuration does not meet. ManifestDigest names the
// AppManifest the caller prepared the source with.
type RequirementsRequest struct {
	ManifestDigest string `json:"manifestDigest"`
	// Snapshot reports the gaps of an exact frozen version instead of the
	// configuration head, such as the version a Preview applied.
	Snapshot *connectorconfig.SnapshotRef `json:"snapshot,omitempty"`
}

// RequirementsReport is every requirement gap of the configuration, names
// only; Ready when there is none.
type RequirementsReport struct {
	Ready bool                         `json:"ready"`
	Gaps  []appmanifest.RequirementGap `json:"gaps"`
}

// handleRequirements reports every requirement of the request's prepared
// source that the configuration it edits does not meet, at once. It is
// read-only: it neither initializes nor freezes anything. A connector release
// that cannot be resolved is no gap but a failure of the report.
func (host *ConfigurationHost) handleRequirements(response http.ResponseWriter, request *http.Request) {
	admission, internal := requireInternal(response, request)
	if !internal {
		return
	}
	ctx := request.Context()
	var check RequirementsRequest
	if decodeBody(response, request, 1<<16, &check) != nil || admission.SourceCommit == "" || admission.nextPublish != nil {
		writeCodedError(response, http.StatusBadRequest, "PROJECT_CONFIGURATION_INVALID", "Configuration requirements request is invalid")
		return
	}
	manifest, err := host.project.readManifest(ctx, admission)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if manifest.ManifestDigest != check.ManifestDigest {
		writeStoreError(response, connectorconfig.ErrConflict)
		return
	}
	draft, err := host.project.loadConnectionDraft(ctx, admission)
	if err == nil && check.Snapshot != nil {
		draft.document, err = host.project.canonical.ReadSnapshot(ctx, *check.Snapshot)
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	bundle, err := host.project.preparedBundle(ctx, admission)
	if err != nil {
		writeDefinitionError(response, err)
		return
	}
	gaps, err := host.requirementGaps(ctx, draft, manifest, bundle)
	if failure, notReady := errors.AsType[*notReadyError](err); notReady {
		writeCodedError(response, failure.status, failure.code, failure.message)
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, RequirementsReport{Ready: len(gaps) == 0, Gaps: gaps})
}

// readiness checks every declared connection and the application environment
// of the draft against the pinned Connector releases. It returns a
// *notReadyError for the first gap when the configuration is not ready, and
// any other error when stored configuration cannot be read.
func (host *ConfigurationHost) readiness(ctx context.Context, draft *connectionDraft, manifest projectManifestRecord, bundle *fdg.Bundle) error {
	gaps, err := host.requirementGaps(ctx, draft, manifest, bundle)
	if err != nil || len(gaps) == 0 {
		return err
	}
	return gapError(gaps[0])
}

// gapError is the validation answer of gap, with the code and message each
// kind of gap answered with before the gaps were listed.
func gapError(gap appmanifest.RequirementGap) *notReadyError {
	notReady := func(message string) *notReadyError {
		return &notReadyError{http.StatusConflict, "PROJECT_CONFIGURATION_NOT_READY", message}
	}
	switch gap.Kind {
	case appmanifest.GapConnectionRequired:
		return notReady("A declared connection requires configuration or authorization")
	case appmanifest.GapAuthorizationRequired:
		return notReady("A declared connection requires authorization")
	case appmanifest.GapConnectionIncomplete:
		return notReady("A declared connection configuration is incomplete")
	case appmanifest.GapTriggerBindingInvalid:
		return &notReadyError{http.StatusConflict, "CONNECTOR_TRIGGER_CONFIGURATION_INVALID", "Trigger configuration is invalid"}
	case appmanifest.GapTriggerBindingRequired:
		return &notReadyError{http.StatusConflict, "CONNECTOR_TRIGGER_CONFIGURATION_REQUIRED", "Configure every declared trigger binding before validation"}
	case appmanifest.GapOperationConfigurationRequired:
		return &notReadyError{http.StatusConflict, "CONNECTOR_USE_CONFIGURATION_REQUIRED", "Configure every required unit of each connector operation use before validation"}
	case appmanifest.GapEnvironmentRequired:
		return &notReadyError{http.StatusConflict, "APPLICATION_ENVIRONMENT_NOT_READY", "Declared application environment is incomplete or invalid"}
	}
	panic("unknown requirement gap kind " + gap.Kind.String())
}

// requirementGaps lists every gap of the draft against manifest: per declared
// connection its first connection gap, each trigger binding gap and each
// operation use gap, then each gap of the application environment. A connector release that cannot be
// resolved or that does not publish a declared trigger, and a declared trigger
// binding that no Flow of bundle declares, is a *notReadyError, and storage
// that cannot be read any other error.
func (host *ConfigurationHost) requirementGaps(
	ctx context.Context, draft *connectionDraft, manifest projectManifestRecord, bundle *fdg.Bundle,
) ([]appmanifest.RequirementGap, error) {
	var gaps []appmanifest.RequirementGap
	stepUses := connectorStepUses(bundle)
	for _, declaration := range manifest.Manifest.Connectors {
		connectionGaps, err := host.connectionGaps(ctx, draft, declaration, bundle)
		if err != nil {
			return nil, err
		}
		// Source/release/trigger schemas were checked above even for absence.
		// Only no selection is optional; selected stale or revoked entries
		// still pass all ordinary validation below.
		entry, found := draft.entry(declaration.module().Key())
		if declaration.Optional && (!found || entry.SharedConnectionID == "") {
			continue
		}
		useGaps, err := operationUseGaps(draft, declaration, stepUses[declaration.module()])
		if err != nil {
			return nil, err
		}
		gaps = append(append(gaps, connectionGaps...), useGaps...)
	}
	environmentGaps, err := environmentGaps(ctx, draft, manifest.Manifest.Application.Environment)
	if err != nil {
		return nil, err
	}
	return append(gaps, environmentGaps...), nil
}

// connectionGaps checks that a declared connection name selects an ACTIVE
// TeamSpace connection of its connector, major version and method, whose
// credential is READY, with settings and trigger bindings that the declared
// release validates. A TeamSpace connection serves every release of its
// major version.
func (host *ConfigurationHost) connectionGaps(
	ctx context.Context, draft *connectionDraft, declaration projectConnectorDeclaration, bundle *fdg.Bundle,
) ([]appmanifest.RequirementGap, error) {
	module := declaration.module()
	release, err := host.connectorReleases.Resolve(ctx, module)
	if err != nil {
		return nil, &notReadyError{http.StatusBadGateway, "CONNECTOR_RELEASE_UNAVAILABLE", "Verified connector release is unavailable: " + err.Error()}
	}
	entry, found := draft.entry(module.Key())
	selected, err := host.sharedSelection(ctx, entry)
	if err != nil {
		return nil, err
	}
	gap := func(kind appmanifest.RequirementGapKind) appmanifest.RequirementGap {
		return appmanifest.RequirementGap{Kind: kind, ConnectionName: declaration.ConnectionName, ConnectorID: declaration.ConnectorID}
	}
	var gaps []appmanifest.RequirementGap
	method, declared := release.Manifest.Spec.Auth.Method(declaration.AuthMethodID)
	var settings map[string]json.RawMessage
	switch {
	case !found || !declared || selected == nil || entry.ModulePath != module.ModulePath || entry.Provider != release.Manifest.Spec.Provider ||
		entry.AuthMethodID != declaration.AuthMethodID || selected.ConnectionStatus != connectiondao.ConnectionActive || selected.ConnectorID != module.ConnectorID ||
		selected.VersionLine != connectormanifest.VersionLine(module.ModuleVersion) || selected.AuthMethodID != method.ID:
		gaps = append(gaps, gap(appmanifest.GapConnectionRequired))
	case selected.CredentialStatus != connectiondao.CredentialReady:
		gaps = append(gaps, gap(appmanifest.GapAuthorizationRequired))
	case json.Unmarshal(entry.Configuration, &settings) != nil ||
		connectormanifest.NormalizeConfigurationFields(connectormanifest.ConfigurationFields(release.Manifest, method), settings, host.shared.PlainHTTP) != nil:
		gaps = append(gaps, gap(appmanifest.GapConnectionIncomplete))
	}
	bindings, err := draft.triggerBindings(module.ConnectorID, module.ConnectionName)
	if err != nil {
		return nil, err
	}
	for _, declared := range declaration.TriggerBindings {
		if _, published := release.Manifest.Trigger(declared.TriggerName); !published {
			return nil, &notReadyError{http.StatusConflict, "CONNECTOR_TRIGGER_SCHEMA_UNAVAILABLE", "The pinned connector release does not publish this trigger"}
		}
		configurationUI, found := connectorTriggerUI(bundle, module, declared)
		if !found {
			return nil, &notReadyError{http.StatusConflict, "CONNECTOR_TRIGGER_SCHEMA_UNAVAILABLE", "No Flow of the prepared source declares a configurable binding of this trigger for the pinned connector release"}
		}
		bindingGap := func(kind appmanifest.RequirementGapKind, unitLabel string) appmanifest.RequirementGap {
			bindingGap := gap(kind)
			bindingGap.TriggerName, bindingGap.BindingName, bindingGap.UnitLabel = declared.TriggerName, declared.BindingName, unitLabel
			return bindingGap
		}
		index := slices.IndexFunc(bindings, func(binding connectorTriggerBinding) bool {
			return binding.triggerName == declared.TriggerName && binding.bindingName == declared.BindingName
		})
		switch {
		case index < 0:
			gaps = append(gaps, bindingGap(appmanifest.GapTriggerBindingRequired, ""))
		case connectormanifest.ValidateUseConfiguration(bindings[index].configuration, configurationUI) != nil:
			gaps = append(gaps, bindingGap(appmanifest.GapTriggerBindingInvalid, ""))
		default:
			// A saved binding still needs a value for every required unit.
			for _, unit := range connectormanifest.MissingRequiredUnits(bindings[index].configuration, configurationUI) {
				gaps = append(gaps, bindingGap(appmanifest.GapTriggerBindingRequired, unit.Label))
			}
		}
	}
	return gaps, nil
}

// operationUseGaps lists, for each connector factory Step that uses the
// declared connection, each required unit that its saved use configuration
// holds no value for; a use never saved lacks every required unit.
func operationUseGaps(draft *connectionDraft, declaration projectConnectorDeclaration, uses []connectionStepUse) ([]appmanifest.RequirementGap, error) {
	configurations, err := draft.useConfigurations(declaration.ConnectorID, declaration.ConnectionName)
	if err != nil {
		return nil, err
	}
	var gaps []appmanifest.RequirementGap
	for _, use := range uses {
		configuration, _ := use.savedConfiguration(configurations)
		for _, unit := range connectormanifest.MissingRequiredUnits(configuration, use.ConfigurationUI) {
			gaps = append(gaps, appmanifest.RequirementGap{
				Kind: appmanifest.GapOperationConfigurationRequired, ConnectionName: declaration.ConnectionName, ConnectorID: declaration.ConnectorID,
				FlowName: use.FlowName, StepName: use.StepName, OperationID: use.OperationID, UnitLabel: unit.Label,
			})
		}
	}
	return gaps, nil
}

// environmentGaps lists each declared environment variable that is missing
// while required, stored with another classification or holds an invalid
// value. A stored environment the checks still refuse, such as one with a
// variable the source no longer declares or a secret that cannot be resolved,
// is one gap without a name: the environment's check answers only whether it
// is ready, and a save of the environment repairs it.
func environmentGaps(ctx context.Context, draft *connectionDraft, declarations []connectorconfig.EnvironmentDeclaration) ([]appmanifest.RequirementGap, error) {
	if draft.store.ValidateApplicationEnvironment(ctx, declarations, draft.document) == nil {
		return nil, nil
	}
	unnamed := []appmanifest.RequirementGap{{Kind: appmanifest.GapEnvironmentRequired}}
	resolved, err := draft.store.ResolveApplicationEnvironment(ctx, draft.document)
	if err != nil {
		return unnamed, nil //nolint:nilerr // the check above already refused this environment
	}
	var gaps []appmanifest.RequirementGap
	for _, field := range declarations {
		entry, exists := draft.document.Environment[field.Name]
		value, _ := resolved.Lookup(field.Name)
		if exists && (entry.SecretRef != nil) == field.Secret && connectorconfig.ValidateEnvironmentValue(field, value) == nil || !exists && !field.Required {
			continue
		}
		gaps = append(gaps, appmanifest.RequirementGap{Kind: appmanifest.GapEnvironmentRequired, EnvironmentName: field.Name})
	}
	if len(gaps) == 0 {
		return unnamed, nil
	}
	return gaps, nil
}

// PreviewRevisionChangedCode refuses a Preview start whose configuration head
// moved past the revision the start applies.
const PreviewRevisionChangedCode = "PREVIEW_CONFIGURATION_REVISION_CHANGED"

// refreshIntent records, before a Preview freeze writes its settings refresh
// of revision Applied as revision Written, the digest of what it writes.
type refreshIntent struct {
	Applied uint64 `json:"applied"`
	Written uint64 `json:"written"`
	Digest  string `json:"digest"`
}

// refreshKey is the intent of the settings refresh of revision applied.
func (project *projectConfiguration) refreshKey(applied uint64) string {
	return project.prefix + "/preview-refreshes/" + strconv.FormatUint(applied, 10)
}

// refreshDigest is the digest of a document's contents without its revision.
func refreshDigest(document connectorconfig.Configuration) string {
	document.Revision = 0
	return objectstore.Digest(comm.Must(json.Marshal(document)))
}

// recordRefresh records the intent to write refreshed over revision current
// for a Preview start that applies revision applied.
func (project *projectConfiguration) recordRefresh(ctx context.Context, applied, current uint64, refreshed connectorconfig.Configuration) error {
	key := project.refreshKey(applied)
	etag := ""
	if existing, err := project.objects.ReadObject(ctx, key, ""); err == nil {
		etag = existing.ETag
	} else if !errors.Is(err, connectorconfig.ErrObjectNotFound) {
		return err
	}
	intent := refreshIntent{Applied: applied, Written: current + 1, Digest: refreshDigest(refreshed)}
	_, err := project.writeObject(ctx, key, etag, comm.Must(json.Marshal(intent)))
	return err
}

// wroteRefresh reports whether head is exactly the settings refresh that a
// Preview start applying revision applied recorded and wrote.
func (project *projectConfiguration) wroteRefresh(ctx context.Context, applied uint64, head connectorconfig.Configuration) (bool, error) {
	object, err := project.objects.ReadObject(ctx, project.refreshKey(applied), "")
	if errors.Is(err, connectorconfig.ErrObjectNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var intent refreshIntent
	if err = comm.DecodeStrictJSON(object.Contents, &intent); err != nil {
		return false, err
	}
	return intent.Applied == applied && intent.Written == head.Revision && intent.Digest == refreshDigest(head), nil
}
