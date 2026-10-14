// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package setup

import (
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/projects"
)

// Setup is the ProjectSetupFlow snapshot. Phase is the Setup phase in
// progress, which a failure records.
type Setup struct {
	Identity                                                  dexflow.StartIdentity
	CreatedAt, UpdatedAt                                      time.Time
	ProjectID, SetupID, SourceCommit, TemplateRevision, Phase string
	Status                                                    Status
	Revision                                                  uint64
	Repository                                                projects.RepositoryBindingRef
	AppManifest                                               *pb.AppManifestSnapshot
	ProjectDeletionRequested                                  bool
	Failure                                                   *dexflow.Failure
}

// Status is a value of the SetupStatus Attribute, which keyword3 indexes, so
// it is an alias of string; its constants declare the set. They are persisted,
// and the API maps each to the SetupStatus enum value of the same name.
type Status = string

const (
	statusPending     Status = "PENDING"
	statusRunning     Status = "RUNNING"
	StatusComplete    Status = "COMPLETE"
	StatusSetupFailed Status = "SETUP_FAILED"
	StatusCancelled   Status = "CANCELLED"
	statusCleaning    Status = "CLEANING"
)

// FlowID addresses this Setup: setup-<projectID>-<setupID>.
func (s Setup) FlowID() string {
	return flowid.SetupFlowID(s.ProjectID, s.SetupID)
}

// setupStart is the accepted Project fields a Setup starts from.
type setupStart struct {
	Identity                             dexflow.StartIdentity
	ProjectID, SetupID, TemplateRevision string
}

// setupCleanupResult answers a cleanup request; Read reports completion.
type setupCleanupResult struct {
	Rejection *dexflow.Rejection
}
