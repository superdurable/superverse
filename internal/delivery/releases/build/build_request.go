// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package build

import (
	"errors"
	"regexp"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"

	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
)

type BuildState string

const (
	BuildMissing BuildState = "MISSING"
	BuildRunning BuildState = "RUNNING"
	BuildReady   BuildState = "READY"
	BuildFailed  BuildState = "FAILED"
	BuildUnknown BuildState = "UNKNOWN"
)

// ContractVersion is the only build provider wire version. It stays in every
// request because the hosted receipts decode BuildRequest strictly and the
// local build server requires the exact key set.
const ContractVersion uint32 = 2

// SourceRevisionID is the build revision of source preparation, which renders
// the manifest and Flow Definitions without building an image.
const SourceRevisionID = "source"

// RevisionIDPattern matches the revision of one Save and Build: build-<UUIDv4>.
var RevisionIDPattern = regexp.MustCompile(`^build-[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

// BuildRequest is the provider wire request. Its JSON is persisted in hosted
// receipts and read by both build executors, so the names are fixed.
type BuildRequest struct {
	ContractVersion       uint32                      `json:"contractVersion"`
	BuildRevisionID       string                      `json:"buildRevisionId,omitempty"`
	SourceOnly            bool                        `json:"sourceOnly,omitempty"`
	ConfigurationDigest   string                      `json:"configurationDigest,omitempty"`
	ConfigurationRevision uint64                      `json:"configurationRevision,omitempty"`
	RequestID             string                      `json:"requestId"`
	ProjectID             string                      `json:"projectId"`
	ReleaseID             string                      `json:"releaseId"`
	RepositoryFullName    string                      `json:"repositoryFullName"`
	SourceCommit          string                      `json:"sourceCommitSha"`
	ProfileVersion        string                      `json:"buildProfileVersion"`
	Profile               string                      `json:"buildProfile"`
	ProfileDigest         string                      `json:"buildProfileDigest"`
	Environment           deploymentnames.Environment `json:"environment"`
	ImageTag              string                      `json:"imageTag"`
	Attempt               uint64                      `json:"attempt"`
}

// Payloads are the four release artifacts a build renders from the source.
type Payloads struct {
	FlowDefinitions     string `json:"flowDefinitionBundle"`
	ConnectorContract   string `json:"connectorContract"`
	EnvironmentContract string `json:"environmentContract"`
	Manifest            string `json:"dexAppManifest"`
}

type BuildResult struct {
	State          BuildState `json:"state"`
	Attempt        uint64     `json:"attempt"`
	OperationID    string     `json:"operationId"`
	ImageReference string     `json:"artifactReference"`
	ImageDigest    string     `json:"imageId"`
	// Failure says why a FAILED build failed: a classification or a bounded
	// provider message, never build output, which can carry source or
	// credential material.
	Failure  string   `json:"failure,omitempty"`
	Payloads Payloads `json:"releaseArtifactPayloads"`
	// Timings are the phases of a finished build that the provider measured,
	// in the order it first entered them.
	Timings []BuildPhaseTiming `json:"phaseTimings,omitempty"`
}

// Phase codes a provider measures. Source preparation ends with ANALYSIS.
const (
	PhaseQueue                        = "QUEUE"
	PhaseHostPreparation              = "HOST_PREPARATION"
	PhaseDependencyPreparationForTest = "DEPENDENCY_PREPARATION"
	PhaseAnalysisForTest              = "ANALYSIS"
	PhaseCompilationForTest           = "COMPILATION"
	PhaseImagePublicationForTest      = "IMAGE_PUBLICATION"
)

// BuildPhaseTiming is one measured phase of a build: its first start and its
// work in milliseconds, at least one. Both executors write it as JSON, so the
// names are fixed.
type BuildPhaseTiming struct {
	Phase      string    `json:"phase"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMs int64     `json:"durationMs"`
}

// ImageTag names the Local Kind fixture's image of one build revision, which
// localbuilder/server.py recomputes and checks. The hosted builder carries it
// only inside the request identity and tags ECR with its own per-attempt tag.
func ImageTag(in BuildRequest) string {
	return deploymentnames.MustFor(in.Environment).WorkloadPrefix + "-" + in.Environment.String() + "-" + in.ProjectID + ":" + in.SourceCommit + "-" + in.ProfileDigest[:16] + "-" + in.BuildRevisionID
}

// MaxAttempt bounds the provider attempts of one build revision, as the Local
// Kind fixture does.
const MaxAttempt = 10000

var (
	projectIDPattern  = regexp.MustCompile(`^[a-z0-9]{1,32}$`)
	sha256HexPattern  = regexp.MustCompile(`^[a-f0-9]{64}$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

// ValidateIdentity accepts a request whose every identity is exact: request
// fingerprint, Project, main commit as ReleaseID, repository, profile digest,
// attempt and catalog environment, plus either source preparation or one
// build revision bound to a frozen configuration.
func ValidateIdentity(in BuildRequest) error {
	if in.ContractVersion != ContractVersion || !sha256HexPattern.MatchString(in.RequestID) || !projectIDPattern.MatchString(in.ProjectID) ||
		!flowid.IsCommitSHA(in.SourceCommit) || in.ReleaseID != in.SourceCommit || !repositoryPattern.MatchString(in.RepositoryFullName) ||
		!sha256HexPattern.MatchString(in.ProfileDigest) || in.Attempt < 1 || in.Attempt > MaxAttempt {
		return errors.New("invalid immutable build identity")
	}
	if _, err := deploymentnames.For(in.Environment); err != nil {
		return err
	}
	if in.SourceOnly {
		if in.BuildRevisionID != SourceRevisionID || in.ConfigurationDigest != "" || in.ConfigurationRevision != 0 {
			return errors.New("invalid source preparation identity")
		}
	} else if !RevisionIDPattern.MatchString(in.BuildRevisionID) || !flowid.IsSHA256Digest(in.ConfigurationDigest) || in.ConfigurationRevision == 0 {
		return errors.New("invalid immutable build revision identity")
	}
	return nil
}
