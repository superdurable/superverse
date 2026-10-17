// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package contribution

import (
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Identity names the contribution of one connector by one Project's Session.
type Identity struct {
	ProjectID string `json:"project_id"`
	Slug      string `json:"slug"`
}

// flowID is the FlowID of the contribution's ConnectorContributionFlow.
func (id Identity) flowID() string {
	return flowid.ConnectorContributionFlowID(id.ProjectID, id.Slug)
}

// sessionFlowID is the FlowID of the Session that contributes.
func (id Identity) sessionFlowID() string { return flowid.SessionFlowID(id.ProjectID) }

// Status is where a contribution stands. Before a pull request it is pending
// while the pull request is opened, unpublished on a server without an
// upstream library, or pull_request_failed; once the pull request is open as
// a draft it never returns to those, and only closed ends it. CONN2 adds
// ready, merged and released after draft. A struct that string literals cannot
// convert to; the indexed ContributionStatus Attribute holds its name.
type Status struct{ definition *status }

type status struct {
	name string
	// rank orders the statuses: a contribution moves only to a status of a
	// rank at least its own, except that a later head may retry a failed one.
	rank   int
	closed bool
}

var (
	StatusPullRequestPending = Status{&status{name: "pull_request_pending", rank: 1}}
	StatusUnpublished        = Status{&status{name: "unpublished", rank: 1}}
	StatusPullRequestFailed  = Status{&status{name: "pull_request_failed", rank: 1}}
	StatusDraft              = Status{&status{name: "draft", rank: 2}}
	StatusClosed             = Status{&status{name: "closed", rank: 9, closed: true}}
)

var statuses = []Status{StatusPullRequestPending, StatusUnpublished, StatusPullRequestFailed, StatusDraft, StatusClosed}

func (status Status) String() string { return status.definition.name }

// Closed reports whether the contribution ended.
func (status Status) Closed() bool { return status.definition.closed }

// HasPullRequest reports whether the contribution's pull request is open.
func (status Status) HasPullRequest() bool {
	return status.definition.rank >= StatusDraft.definition.rank && !status.Closed()
}

func (status Status) MarshalText() ([]byte, error) { return []byte(status.String()), nil }
func (status *Status) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, status, Status.String, statuses...)
}

// statusOf is the status the ContributionStatus Attribute holds; this Flow
// wrote it, so another spelling is a programming error.
func statusOf(name string) Status {
	var status Status
	comm.MustDo(status.UnmarshalText([]byte(name)))
	return status
}

// PullRequest is the contribution's pull request in the upstream library.
type PullRequest struct {
	Number int64  `json:"number"`
	URL    string `json:"url"`
	NodeID string `json:"node_id"`
	State  string `json:"state"`
}

// Record is the Contribution Attribute, the authority of the contribution:
// what was contributed, its branch with the head commit the last develop
// subagent pushed, its pull request, its status and, from CONN2 on, the
// release that contains it. Failure says why its pull request could not be
// opened. It is public data: no credential.
type Record struct {
	Module          string                        `json:"module"`
	Kind            conversation.ContributionKind `json:"kind"`
	Branch          string                        `json:"branch"`
	HeadCommit      string                        `json:"head_commit"`
	HeadRecordedAt  time.Time                     `json:"head_recorded_at"`
	Location        connectorlibrary.Location     `json:"location"`
	PullRequest     *PullRequest                  `json:"pull_request,omitempty"`
	Status          Status                        `json:"status"`
	ReleasedVersion string                        `json:"released_version,omitempty"`
	// ReleaseCandidate is the candidate of the latest head the Flow published
	// or tried to.
	ReleaseCandidate *ReleaseCandidate `json:"release_candidate,omitempty"`
	// Checks is the last terminal CI observation of this exact head. CI output
	// is bounded and redacted before this durable copy is written.
	Checks      *connectorlibrary.HeadChecks `json:"checks,omitempty"`
	Failure     string                       `json:"failure,omitempty"`
	Description string                       `json:"description"`
	Report      string                       `json:"report,omitempty"`
	UpdatedAt   time.Time                    `json:"updated_at"`
}

// maximumReportBytes bounds the subagent report a record keeps for the pull
// request's body.
const maximumReportBytes = 16 << 10

// Submission is what a completed develop subagent contributed, which the
// Session Service submits: the contribution, whose summary titles the pull
// request, and the subagent's report, its body.
type Submission struct {
	Identity     Identity                           `json:"identity"`
	Contribution conversation.ConnectorContribution `json:"contribution"`
	Report       string                             `json:"report"`
}

// requestID is the Start RequestID of the contribution's first submission:
// the Session, the slug, the branch and the head commit, so the same
// submission repeated joins the execution and a later head is an update.
func (submission Submission) requestID() string {
	return flowid.Fingerprint(struct{ SessionFlowID, Slug, Branch, HeadCommit string }{
		submission.Identity.sessionFlowID(), submission.Identity.Slug, submission.Contribution.Branch, submission.Contribution.HeadCommit})
}

// record is the record of the submission at its first status.
func (submission Submission) record(status Status, now time.Time) Record {
	contribution := submission.Contribution
	return Record{Module: contribution.Module, Kind: contribution.Kind, Branch: contribution.Branch, HeadCommit: contribution.HeadCommit, Location: contribution.Location,
		Status: status, Description: contribution.Summary, Report: comm.TruncateUTF8(submission.Report, maximumReportBytes),
		HeadRecordedAt: now, UpdatedAt: now}
}

// update applies a later submission of the same connector: its head, kind,
// location, description and report. A contribution whose pull request could
// not be opened becomes pending again, so the new head retries it; one with a
// pull request keeps it, which the pushed branch already updated. It reports
// whether the pull request must be opened.
func (record Record) update(submission Submission, publishes bool, now time.Time) (Record, bool) {
	updated := submission.record(record.Status, now)
	updated.PullRequest, updated.ReleasedVersion = record.PullRequest, record.ReleasedVersion
	reopens := publishes && record.Status == StatusPullRequestFailed && submission.Contribution.Location != connectorlibrary.LocationSandbox
	if reopens {
		updated.Status = StatusPullRequestPending
	}
	return updated, reopens
}

// draft is the draft pull request of the record: titled as the library
// requires, with the subagent's report and what the platform states about
// verification as its body.
func (record Record) draft(slug string) connectorlibrary.PullRequestDraft {
	body := fmt.Sprintf("Contributed by a Superverse connector_develop subagent: a %s contribution to `%s`.\n\n"+
		"A maintainer reviews this pull request; checks that need real provider credentials were not run and are listed as unverified in the report.\n\n## Report\n\n%s\n",
		record.Kind, record.Module, record.Report)
	return connectorlibrary.PullRequestDraft{Branch: record.Branch, Title: git.ContributionSubject(slug, record.Description), Body: body}
}

// ReleaseCandidateStatus is where a head's release candidate stands. A struct
// that string literals cannot convert to; the values below are persisted by
// their names.
type ReleaseCandidateStatus struct{ name string }

var (
	// ReleaseCandidatePublished: the candidate's release holds its assets.
	ReleaseCandidatePublished = ReleaseCandidateStatus{"published"}
	// ReleaseCandidateFailed: it was not published within the wait limit, or
	// the library refused it.
	ReleaseCandidateFailed = ReleaseCandidateStatus{"failed"}
)

func (status ReleaseCandidateStatus) String() string               { return status.name }
func (status ReleaseCandidateStatus) MarshalText() ([]byte, error) { return []byte(status.name), nil }
func (status *ReleaseCandidateStatus) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, status, ReleaseCandidateStatus.String, ReleaseCandidatePublished, ReleaseCandidateFailed)
}

// ReleaseCandidate is the release candidate of one head of the contribution:
// its tag and version, where it stands and, when it failed, why.
type ReleaseCandidate struct {
	connectorlibrary.ReleaseCandidate
	Status    ReleaseCandidateStatus `json:"status"`
	Failure   string                 `json:"failure,omitempty"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// candidateAttempt is the input of the release candidate Steps: the candidate
// of a head, the location of its branch and, once decided, whether it was
// published or why not.
type candidateAttempt struct {
	Candidate connectorlibrary.ReleaseCandidate `json:"candidate"`
	Location  connectorlibrary.Location         `json:"location"`
	Published bool                              `json:"published,omitempty"`
	Failure   string                            `json:"failure,omitempty"`
	// Reported is the status the subagent's task notification already reported,
	// published or refused; a record of that status sends no second
	// notification.
	Reported ReleaseCandidateStatus `json:"reported,omitzero"`
}

// candidateAttemptOf is the release candidate attempt of a submission, nil when
// its branch stayed in the Sandbox; a candidate the library refused is decided.
func candidateAttemptOf(contribution conversation.ConnectorContribution) *candidateAttempt {
	switch {
	case contribution.Location == connectorlibrary.LocationSandbox:
		return nil
	case contribution.ReleaseCandidate != nil:
		attempt := &candidateAttempt{Candidate: *contribution.ReleaseCandidate, Location: contribution.Location}
		if contribution.CandidatePublished {
			attempt.Reported = ReleaseCandidatePublished
		}
		return attempt
	case contribution.CandidateRefused != "":
		return &candidateAttempt{Candidate: connectorlibrary.ReleaseCandidate{Commit: contribution.HeadCommit}, Location: contribution.Location,
			Failure: contribution.CandidateRefused, Reported: ReleaseCandidateFailed}
	}
	return nil
}

// candidateNotification is the connector notification of a head's release
// candidate: published, which tells the agent the version to use, or failed.
// A published one is named by its version, a failed one by its head.
func candidateNotification(id Identity, record Record, candidate ReleaseCandidate) conversation.SessionNotification {
	notification := conversation.SessionNotification{Kind: conversation.NotificationKindConnector, Source: conversation.NotificationSourceConnector,
		OccurredAt: candidate.UpdatedAt, Slug: id.Slug}
	if candidate.Status == ReleaseCandidatePublished {
		notification.Event = conversation.ConnectorReleaseCandidatePublished
		notification.ID = "connector/" + id.Slug + "/" + notification.Event.String() + "/" + candidate.Version
		notification.Summary = fmt.Sprintf("The connector %s is published as the release candidate %s of %s. Use it: run go get %s@%s, "+
			"set the version of its connection in dex-app.yaml to %s, then commit and push.", id.Slug, candidate.Version, record.Module,
			record.Module, candidate.Version, candidate.Version)
		notification.Message = candidate.Tag
		return notification
	}
	notification.Event = conversation.ConnectorReleaseCandidateFailed
	notification.ID = "connector/" + id.Slug + "/" + notification.Event.String() + "/" + candidate.Commit[:12]
	notification.Summary = fmt.Sprintf("No release candidate of the connector %s was published for its branch head %s.", id.Slug, candidate.Commit[:12])
	notification.Message = comm.TruncateUTF8(candidate.Failure, 2048)
	return notification
}

// pullRequestAttempt is the input of the pull request Steps: the head the
// attempt opens it for and, once it decided, the pull request or why GitHub
// refused it.
type pullRequestAttempt struct {
	HeadCommit  string       `json:"head_commit"`
	PullRequest *PullRequest `json:"pull_request,omitempty"`
	Failure     string       `json:"failure,omitempty"`
}

// contributionEnd is the close of a deleting Project's contribution.
type contributionEnd struct{}

// Snapshot is the retained read of a contribution.
type Snapshot struct {
	Identity Identity `json:"identity"`
	Record   Record   `json:"record"`
}

// receipt is the output of a completed ConnectorContributionFlow.
type receipt struct {
	Identity Identity `json:"identity"`
	Status   Status   `json:"status"`
}

// failureNotification is the connector notification of a pull request that
// could not be opened, whose ID names the head commit: each head reports it
// once, as connector/<slug>/pull_request_failed/<head12>.
func failureNotification(id Identity, record Record, at time.Time) conversation.SessionNotification {
	event := conversation.ConnectorPullRequestFailed
	return conversation.SessionNotification{ID: "connector/" + id.Slug + "/" + event.String() + "/" + record.HeadCommit[:12], Kind: conversation.NotificationKindConnector,
		Source: conversation.NotificationSourceConnector, OccurredAt: at, Slug: id.Slug, Event: event,
		Summary: fmt.Sprintf("The draft pull request of the connector %s could not be opened; its branch %s stays as it is.", id.Slug, record.Branch),
		Message: comm.TruncateUTF8(record.Failure, 2048)}
}

// startAttributes are what a contribution starts with, installed atomically
// with StartFlow: its identity, its Project, indexed for the cleanup of a
// deleting Project, and its status.
func startAttributes(submission Submission) []dex.InitialAttributeDef {
	attributes := &dexflow.InitialAttributes{}
	dexflow.Add(attributes, identityAttribute, submission.Identity)
	dexflow.Add(attributes, projectIDAttribute, submission.Identity.ProjectID)
	dexflow.Add(attributes, statusAttribute, StatusPullRequestPending.String())
	dexflow.Add(attributes, submissionAttribute, submission)
	return attributes.Values()
}

// FlowIDForTest is flowID for the tests.
func (r Identity) FlowIDForTest() string {
	return r.flowID()
}
