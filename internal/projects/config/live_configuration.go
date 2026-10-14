// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost"
	"github.com/superdurable/superversev2/internal/studio/confighost/configitems"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
)

// Live's next publish is resolved from the Live scope's next-publish layer,
// Preview's applied version and current Live, for the prepared source a
// request names. This package reads what lies outside the Live host's prefix
// (Preview's applied version, and Preview's prepared source when it is the
// named one) and hands it to the host, which never reads outside its prefix.

// The internal routes of Live's next publish.
const (
	nextPublishRoute       = "/api/v2/project-configuration/live-items"
	nextPublishCopiesRoute = "/api/v2/project-configuration/live-derivations"
)

// slackChannelField is the field of a Slack trigger binding's configuration
// that names the channel it listens to.
const slackChannelField = "channelId"

// nextPublishInputs reads what the layer resolves with for target: current
// Live, Preview's applied version without its secrets and, when the target
// names Preview's prepared source, that source, read through the Preview's
// host.
func (c *Configuration) nextPublishInputs(ctx context.Context, target configurationTarget) (*confighost.NextPublish, error) {
	live, err := c.currentLive(ctx, target.project)
	if err != nil {
		return nil, err
	}
	preview, err := c.appliedPreview(ctx, target.project)
	if err != nil {
		return nil, err
	}
	inputs := &confighost.NextPublish{Live: live, Preview: preview}
	if target.previewSource {
		host, release, hostErr := c.acquireHost(ctx, previewScope(target.project))
		if hostErr != nil {
			return nil, hostErr
		}
		defer release()
		if inputs.Source, err = host.PreparedSource(ctx, target.manifest.SourceCommit); err != nil {
			return nil, fmt.Errorf("read the Preview's prepared source %s: %w", target.manifest.SourceCommit, err)
		}
	}
	return inputs, nil
}

// nextPublishAdmission is the internal admission of a request of target's
// next publish, checked against the target's source.
func (c *Configuration) nextPublishAdmission(ctx context.Context, target configurationTarget) (confighost.ConfigurationAdmission, error) {
	inputs, err := c.nextPublishInputs(ctx, target)
	if err != nil {
		return confighost.ConfigurationAdmission{}, err
	}
	return confighost.ConfigurationAdmission{Internal: true, TeamSpaceID: target.teamSpaceID, SourceCommit: target.manifest.SourceCommit, NextPublish: inputs}, nil
}

// nextPublish reports how Live's next publish resolves for the target's
// source, with the CHANNEL_SHARED warnings of its trigger bindings.
func (c *Configuration) nextPublish(ctx context.Context, actor string, target configurationTarget) (confighost.NextPublishReport, error) {
	admission, err := c.nextPublishAdmission(ctx, target)
	if err != nil {
		return confighost.NextPublishReport{}, err
	}
	var report confighost.NextPublishReport
	request := confighost.NextPublishRequest{ManifestDigest: target.manifest.ManifestDigest}
	if err = c.invokeHostAs(ctx, target.scope, admission, http.MethodPost, nextPublishRoute, request, &report); err != nil {
		return confighost.NextPublishReport{}, err
	}
	if err = c.markSharedChannels(ctx, actor, target.project, report.Items, report.Bindings); err != nil {
		return confighost.NextPublishReport{}, err
	}
	report.Bindings = nil
	return report, nil
}

// liveSnapshot is the snapshot of a Live target: the layer at its revision
// for a next-publish target, a version's frozen copy at its revision for a
// Release's, both READY when the next publish resolves without a gap for the
// target's source.
func (c *Configuration) liveSnapshot(ctx context.Context, actor string, target configurationTarget) (*pb.ProjectConfigurationSnapshot, confighost.NextPublishReport, error) {
	report, err := c.nextPublish(ctx, actor, target)
	if err != nil {
		return nil, report, err
	}
	revision := report.LayerRevision
	if !target.nextPublish && report.Copy != nil {
		revision = report.Copy.Revision
	}
	manifest := target.manifest
	snapshot := configurationSnapshot(target, Metadata{
		AppManifestRevision: confighost.PreparedAppManifestRevision, ConfigurationRevision: revision, SourceCommit: manifest.SourceCommit,
		ManifestDigest: manifest.ManifestDigest, ConnectorContractDigest: manifest.ConnectorContractDigest, EnvironmentContractDigest: manifest.EnvironmentContractDigest,
	})
	snapshot.Status = pb.ConfigurationStatus_CONFIGURATION_STATUS_READY
	if len(report.Gaps) > 0 {
		snapshot.Status = pb.ConfigurationStatus_CONFIGURATION_STATUS_NEEDS_REVIEW
		snapshot.RequirementGaps = appmanifest.RequirementGapSnapshots(report.Gaps)
	}
	snapshot.NextPublishRevision, snapshot.PreviewConfigurationRevision = report.LayerRevision, report.PreviewRevision
	snapshot.IsStale = !target.nextPublish && report.Copy != nil && report.Copy.Stale
	return snapshot, report, nil
}

// LiveDraftState reports what build admission checks of a Release's frozen
// copy: its revision and whether Live, Preview's applied version or the
// layer changed since it was built. It reads no other Project.
func (c *Configuration) LiveDraftState(ctx context.Context, actor, projectID, releaseID string) (LiveDraftState, error) {
	target, err := c.resolveLiveTarget(ctx, actor, projectID, releaseID, WriteAccess)
	if err != nil {
		return LiveDraftState{}, err
	}
	admission, err := c.nextPublishAdmission(ctx, target)
	if err != nil {
		return LiveDraftState{}, err
	}
	var report confighost.NextPublishReport
	request := confighost.NextPublishRequest{ManifestDigest: target.manifest.ManifestDigest}
	if err = c.invokeHostAs(ctx, target.scope, admission, http.MethodPost, nextPublishRoute, request, &report); err != nil {
		return LiveDraftState{}, err
	}
	return LiveDraftState{ConfigurationRevision: report.Copy.Revision, Stale: report.Copy.Stale}, nil
}

// LiveDraftState is the admission view of a Release's frozen copy.
type LiveDraftState struct {
	ConfigurationRevision uint64
	Stale                 bool
}

// DeriveLive builds the frozen copy of a Release's version from the layer at
// expectedRevision (zero while it is absent) and Preview's applied version at
// expectedPreviewRevision, which the caller showed; it is ErrConflict when
// either moved.
func (c *Configuration) DeriveLive(ctx context.Context, actor, projectID, releaseID string, expectedRevision, expectedPreviewRevision uint64) (confighost.CopyReport, error) {
	target, err := c.resolveLiveTarget(ctx, actor, projectID, releaseID, WriteAccess)
	if err != nil {
		return confighost.CopyReport{}, err
	}
	admission, err := c.nextPublishAdmission(ctx, target)
	if err != nil {
		return confighost.CopyReport{}, err
	}
	request := confighost.CopyRequest{ManifestDigest: target.manifest.ManifestDigest, ExpectedLayerRevision: expectedRevision, ExpectedPreviewRevision: expectedPreviewRevision}
	var report confighost.CopyReport
	err = c.invokeHostAs(ctx, target.scope, admission, http.MethodPost, nextPublishCopiesRoute, request, &report)
	return report, err
}

// resolveLiveTarget resolves the frozen copy of a Release's version for actor.
func (c *Configuration) resolveLiveTarget(ctx context.Context, actor, projectID, releaseID string, access Access) (configurationTarget, error) {
	return c.resolveTarget(ctx, actor, &pb.ProjectConfigurationTarget{ProjectId: projectID, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, ReleaseId: releaseID}, access)
}

// currentLive names the deployment serving the Project's Live and the frozen
// configuration of the build it serves; nothing when Live serves nothing.
func (c *Configuration) currentLive(ctx context.Context, project projects.Project) (confighost.LiveConfiguration, error) {
	object, deploymentID, err := c.liveObject(ctx, project)
	if err != nil || deploymentID == "" {
		return confighost.LiveConfiguration{}, err
	}
	snapshot, err := infra.SnapshotRefFor(liveScope(project), object.Object)
	if err != nil {
		return confighost.LiveConfiguration{}, err
	}
	return confighost.LiveConfiguration{DeploymentID: deploymentID, Snapshot: &snapshot}, nil
}

// liveObject returns the frozen configuration of the build a Project's READY
// Live serves, with its deployment, or no deployment when there is none.
func (c *Configuration) liveObject(ctx context.Context, project projects.Project) (releases.ReadyConfiguration, string, error) {
	live := project.CurrentLive
	if live == nil || !live.Ready {
		return releases.ReadyConfiguration{}, "", nil
	}
	build, err := c.Releases.ReadBuildRevision(ctx, project.ProjectID, live.ReleaseID, live.BuildRevisionID)
	if err != nil {
		return releases.ReadyConfiguration{}, "", fmt.Errorf("read the build %s that Live serves: %w", live.BuildRevisionID, err)
	}
	if build.Configuration == nil {
		return releases.ReadyConfiguration{}, "", fmt.Errorf("the build %s that Live serves has no frozen configuration", live.BuildRevisionID)
	}
	return *build.Configuration, live.DeploymentID, nil
}

// markSharedChannels adds CHANNEL_SHARED to each Slack trigger binding item
// whose channel a binding of another Project's Live, on the same TeamSpace
// connection, listens to. Another Project is named only to its members, and
// one whose Live cannot be read is logged and skipped.
func (c *Configuration) markSharedChannels(ctx context.Context, actor string, project projects.Project, items []configitems.Item, uses []confighost.BindingUse) error {
	channels := map[configitems.Key]string{}
	var connectionIDs []string
	for _, use := range uses {
		if channel := bindingChannel(use.Key.ConnectorID, use.Configuration); channel != "" {
			channels[use.Key] = channel
			connectionIDs = append(connectionIDs, use.SharedConnectionID)
		}
	}
	if len(channels) == 0 {
		return nil
	}
	others, err := c.Shared.Usages.ProjectsUsing(ctx, project.TeamSpaceID, connectionIDs, infra.ScopeLive)
	if err != nil {
		return err
	}
	for _, otherID := range others {
		if otherID == project.ProjectID {
			continue
		}
		listening, name, err := c.liveChannels(ctx, actor, otherID, project.TeamSpaceID)
		if err != nil {
			// The warning is advisory: a Project that cannot be read is skipped, never this read.
			c.Logger.WarnContext(ctx, "CHANNEL_SHARED skipped a Project whose Live configuration could not be read",
				"project_id", project.ProjectID, "other_project_id", otherID, "error", err)
			continue
		}
		for index := range items {
			use := slices.IndexFunc(uses, func(use confighost.BindingUse) bool { return use.Key == items[index].Key })
			channel := channels[items[index].Key]
			if channel == "" || !slices.Contains(listening[uses[use].SharedConnectionID], channel) {
				continue
			}
			if !slices.Contains(items[index].Warnings, configitems.ChannelShared) {
				items[index].Warnings = append(items[index].Warnings, configitems.ChannelShared)
			}
			// A Project the caller may view is named rather than "another Project".
			if items[index].WarningProjectName == "" {
				items[index].WarningProjectName = name
			}
		}
	}
	return nil
}

// liveChannels returns the Slack channels the bindings of a Project's Live
// listen to, by the TeamSpace connection they select, with the Project's name
// when actor is its member. It reads the exact frozen version Live pins and
// keeps nothing but channel IDs.
func (c *Configuration) liveChannels(ctx context.Context, actor, projectID, teamSpaceID string) (map[string][]string, string, error) {
	project, err := c.Projects.Read(ctx, projectID)
	if err != nil || project.TeamSpaceID != teamSpaceID || project.ProjectStatus != projects.StatusReady {
		return nil, "", err
	}
	ready, deploymentID, err := c.liveObject(ctx, project)
	if err != nil || deploymentID == "" {
		return nil, "", err
	}
	data, err := c.Storage.ReadConfiguration(ctx, liveScope(project), ready.Object)
	if err != nil {
		return nil, "", fmt.Errorf("read the Live configuration of Project %s: %w", projectID, err)
	}
	var document connectorconfig.Configuration
	if err = json.Unmarshal(data, &document); err != nil {
		return nil, "", fmt.Errorf("decode the Live configuration of Project %s: %w", projectID, err)
	}
	selected := confighost.SharedSelections(document)
	listening := map[string][]string{}
	for _, binding := range document.TriggerBindings {
		if channel := bindingChannel(binding.ConnectorID, binding.Configuration); channel != "" && selected[binding.ConnectionName] != "" {
			listening[selected[binding.ConnectionName]] = append(listening[selected[binding.ConnectionName]], channel)
		}
	}
	name := ""
	if _, authorizeErr := c.Projects.Authorize(ctx, actor, projectID, projects.CapabilityView); authorizeErr == nil {
		name = project.Name
	} else if !errors.Is(authorizeErr, projects.ErrForbidden) {
		return nil, "", authorizeErr
	}
	return listening, name, nil
}

// bindingChannel is the channel a Slack trigger binding listens to, or "".
func bindingChannel(connectorID string, configuration json.RawMessage) string {
	if connectorID != connections.SlackConnectorID {
		return ""
	}
	var fields map[string]json.RawMessage
	var channel string
	if json.Unmarshal(configuration, &fields) != nil || json.Unmarshal(fields[slackChannelField], &channel) != nil {
		return ""
	}
	return channel
}
