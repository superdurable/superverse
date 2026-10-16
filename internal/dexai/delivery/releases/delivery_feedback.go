// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases

import (
	"context"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
)

// Delivery feedback: ProjectReleaseFlow and ProjectDeployFlow tell the
// Project's Session when a Live release or deployment fails. The Step that
// commits the failure decides its owner and moves to a notify Step, which
// makes one call of the narrow port DeliveryFeedback and returns to that
// Step's original successor, so a lost notification only drops the event: the
// failure stays on its own resource.

// DeliveryFeedback tells the Project's Session that a Live release or
// deployment failed. The Session Service implements it; the notify Steps of
// both delivery Flows call it once per committed failure.
type DeliveryFeedback interface {
	// ReportDeliveryFailure delivers failure to the Session of its Project. A
	// Session that does not admit it, or that is missing or closed, drops it.
	// An error is a delivery that may be repeated: the failure's ID makes a
	// repeat a no-op once admitted.
	ReportDeliveryFailure(ctx context.Context, failure DeliveryFailure) error
}

// DeliverySource is the Flow whose failure a DeliveryFailure reports. It is a
// struct that string literals cannot convert to: the values below are its only
// ones, persisted by their names.
type DeliverySource struct{ name string }

var (
	SourceRelease    = DeliverySource{"release"}
	SourceDeployment = DeliverySource{"deployment"}
)

func (source DeliverySource) String() string               { return source.name }
func (source DeliverySource) MarshalText() ([]byte, error) { return []byte(source.name), nil }
func (source *DeliverySource) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, source, DeliverySource.String, SourceRelease, SourceDeployment)
}

// DeliveryFailure is a failure a delivery Flow committed, as its Session is
// told it, and the input of the notify Step. The Step that commits the
// failure builds it from the state it commits, so every retry of the notify
// Step, a Worker restart or a recovery admitted meanwhile repeats exactly the
// same ID, which names the Revision of that commit, never a later one:
// release/<releaseID>/source/r<revision> for the Release's source,
// release/<releaseID>/<buildRevisionID>/r<revision> for a build and
// deployment/<deploymentID>/g<generation>/r<revision> for a deployment.
type DeliveryFailure struct {
	ID        string
	ProjectID string
	Source    DeliverySource
	// Owner is who must act on the failure; only an application failure of
	// the Session's HEAD may open an automatic turn.
	Owner conversation.DeliveryOwner
	// Commit is the Release's main commit. ResourceID is the build revision
	// or the deployment the failure concerns, empty for the Release's source.
	Commit, ResourceID string
	Failure            dexflow.Failure
	// Gaps are what the commit's Live configuration lacks, by name only.
	Gaps       []appmanifest.RequirementGap `json:",omitempty"`
	OccurredAt time.Time
}

// releaseFailure is the failure the Release s committed at revision, of its
// build revision buildRevisionID or, when that is empty, of its source.
func releaseFailure(s Snapshot, buildRevisionID string, revision uint64, failure dexflow.Failure, owner conversation.DeliveryOwner, at time.Time) DeliveryFailure {
	resource := "source"
	if buildRevisionID != "" {
		resource = buildRevisionID
	}
	return DeliveryFailure{
		ID: fmt.Sprintf("release/%s/%s/r%d", s.ReleaseID, resource, revision), ProjectID: s.ProjectID, Source: SourceRelease, Owner: owner,
		Commit: s.SourceCommit, ResourceID: buildRevisionID, Failure: failure, OccurredAt: at,
	}
}
