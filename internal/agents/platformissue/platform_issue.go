// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package platformissue

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/githubissues"
)

// Incident is one platform fault of a Session as its report names it:
// identifiers and classification only, never a message, a tool argument, file
// contents or conversation text. Its JSON keys are the persisted keys of the
// Incident Attribute.
type Incident struct {
	Environment   deploymentnames.Environment `json:"environment"`
	SessionFlowID string                      `json:"session_flow_id"`
	// CallID and Tool name the failed tool call; both are empty for a model call.
	CallID string `json:"call_id,omitempty"`
	Tool   string `json:"tool,omitempty"`
	// Code is the failure code of a tool result, or the class of a model call
	// the API rejected, such as invalid_request_error.
	Code string `json:"code"`
	// Owner and Effect are the spellings of the toolfailure.Owner and
	// toolfailure.Effect of the failure.
	Owner  string `json:"owner"`
	Effect string `json:"effect,omitempty"`
	// ErrorType is the type of the error behind the fault, such as
	// StartToClose or http_400; empty for a failure a tool classified.
	ErrorType  string    `json:"error_type,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// signature identifies the kind of the fault across Sessions
// (flowid.PlatformIssueSignature).
func (incident Incident) signature() string {
	return flowid.PlatformIssueSignature(incident.Owner, incident.Tool, incident.Code, incident.ErrorType)
}

// flowID addresses the PlatformIssueFlow of the incident's signature in its
// environment.
func (incident Incident) flowID() string {
	return issueFlowID(incident.Environment, incident.signature())
}

// requestIdentity is what the Start RequestID fingerprints: the Session, the
// call and the signature. A retry of the same report repeats it; another
// report of the signature, from another call or Session, differs.
func (incident Incident) requestIdentity() string {
	return flowid.Fingerprint(struct{ SessionFlowID, CallID, Signature string }{incident.SessionFlowID, incident.CallID, incident.signature()})
}

// logAttributes are the incident's identifiers and classification, the only
// values a log line of the report carries.
func (incident Incident) logAttributes() []any {
	return []any{"flow_id", incident.flowID(), "session_flow_id", incident.SessionFlowID, "call_id", incident.CallID, "tool", incident.Tool,
		"code", incident.Code, "owner", incident.Owner, "effect", incident.Effect, "error_type", incident.ErrorType, "signature", incident.signature()}
}

// outcomeStatus is what became of the report of a platform incident. It is a
// struct that string literals cannot convert to: the values below are its
// only ones, persisted and recorded by their names, and the zero outcomeStatus
// is no outcome.
type outcomeStatus struct{ name string }

var (
	// outcomeOpened: the report opened the incident's GitHub issue.
	outcomeOpened = outcomeStatus{"opened"}
	// outcomeFound: an open or closed issue already carried the incident's
	// signature, so the report opened none.
	outcomeFound = outcomeStatus{"found"}
	// outcomeLogged: the server names no issue repository, so the incident was
	// only logged.
	outcomeLogged = outcomeStatus{"logged"}
	// outcomeDropped: GitHub refused the report, or its transient failures
	// outlasted the retries; it was logged and dropped.
	outcomeDropped = outcomeStatus{"dropped"}
)

func (status outcomeStatus) String() string               { return status.name }
func (status outcomeStatus) MarshalText() ([]byte, error) { return []byte(status.name), nil }
func (status *outcomeStatus) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, status, outcomeStatus.String, outcomeOpened, outcomeFound, outcomeLogged, outcomeDropped)
}

// Outcome is what became of the report: the status, the issue of the
// signature when GitHub answered, and the bounded final error of a dropped
// report, or of an opened issue GitHub would not label, which holds no
// secret. Its JSON keys are the persisted keys of the Outcome Attribute.
type Outcome struct {
	Status      outcomeStatus `json:"status"`
	IssueNumber int64         `json:"issue_number,omitempty"`
	URL         string        `json:"url,omitempty"`
	Failure     string        `json:"failure,omitempty"`
}

// filedOutcome is the outcome of an issue GitHub returned.
func filedOutcome(filed githubissues.Filed) Outcome {
	status := outcomeFound
	if filed.Opened {
		status = outcomeOpened
	}
	return Outcome{Status: status, IssueNumber: filed.Number, URL: filed.URL}
}

// snapshot is one report, read from its Attributes: the incident that started
// the execution and its outcome, nil until the report ended.
type snapshot struct {
	Incident Incident
	Outcome  *Outcome
}

// outcomeEvent names the diagnostic event that RecordPlatformIssue records in
// the Flow's engine history: an outcomeEventRecord.
const outcomeEvent = "platform_issue"

// outcomeEventRecord is the event of one report: the signature, the outcome's
// spelling, the issue and the bounded final error of a dropped report. Its
// JSON keys are the event's.
type outcomeEventRecord struct {
	Signature   string `json:"signature"`
	Outcome     string `json:"outcome"`
	IssueNumber int64  `json:"issue_number,omitempty"`
	Failure     string `json:"failure,omitempty"`
}

// outcomeRecord is the event of the incident's outcome.
func outcomeRecord(incident Incident, outcome Outcome) outcomeEventRecord {
	return outcomeEventRecord{Signature: incident.signature(), Outcome: outcome.Status.String(), IssueNumber: outcome.IssueNumber, Failure: outcome.Failure}
}

// receipt is the result of a completed report.
type receipt struct {
	Signature string        `json:"signature"`
	Status    outcomeStatus `json:"status"`
}

// logOutcome logs the incident with what became of its report: a warning,
// or an error for a dropped report. The log is the only record of a logged
// incident outside the engine history.
func logOutcome(ctx context.Context, logger *slog.Logger, incident Incident, outcome Outcome) {
	attributes := append(incident.logAttributes(), "outcome", outcome.Status.String())
	if outcome.Status == outcomeDropped {
		logger.ErrorContext(ctx, "platform incident report dropped", append(attributes, "error", outcome.Failure)...)
		return
	}
	if outcome.IssueNumber != 0 {
		attributes = append(attributes, "issue", outcome.IssueNumber, "issue_url", outcome.URL)
	}
	if outcome.Failure != "" {
		attributes = append(attributes, "error", outcome.Failure)
	}
	logger.WarnContext(ctx, "platform incident", attributes...)
}

// logAttemptFailure logs a transient failure of one attempt, which the
// engine retries.
func logAttemptFailure(ctx dex.Context, logger *slog.Logger, incident Incident, err error) {
	logger.WarnContext(ctx, "platform incident report failed", append(incident.logAttributes(), "attempt", ctx.Attempt(), "error", dexflow.TruncateCause(err.Error()))...)
}

// issue is the GitHub issue of an incident: its label carries the environment
// and the signature, and its title and body hold identifiers and
// classification only.
func (incident Incident) issue() githubissues.Issue {
	environment, signature := incident.Environment, incident.signature()
	label := githubissues.IncidentLabel(environment, signature)
	subject := "a model call"
	if incident.Tool != "" {
		subject = issueText(incident.Tool)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "A Superverse server of the %s environment recorded this platform failure. It opens one issue per signature, "+
		"the SHA-256 of the owner, tool, code and error type, under the label %s; a later failure with the same signature finds this "+
		"issue and is only logged. The table holds identifiers and classification only.\n\n| Field | Value |\n| --- | --- |\n",
		issueValue(environment.String()), issueValue(label))
	for _, row := range [][2]string{
		{"Environment", environment.String()}, {"Signature", signature}, {"Session FlowID", incident.SessionFlowID}, {"Call ID", incident.CallID},
		{"Tool", incident.Tool}, {"Code", incident.Code}, {"Owner", incident.Owner}, {"Effect", incident.Effect},
		{"Error type", incident.ErrorType}, {"Time", incident.OccurredAt.UTC().Format(time.RFC3339)},
	} {
		fmt.Fprintf(&body, "| %s | %s |\n", row[0], issueValue(row[1]))
	}
	fmt.Fprintf(&body, "\nInspect the Session read-only with `dexcli flow summary %[1]s --no-hydrate` and `dexcli flow history %[1]s --no-hydrate`, "+
		"and the report with `dexcli flow history %[2]s --no-hydrate`, against the platform Dex of the environment.\n",
		issueText(incident.SessionFlowID), issueText(incident.flowID()))
	return githubissues.Issue{Label: label, Title: "Platform incident in " + environment.String() + ": " + issueText(incident.Code) + " in " + subject, Body: body.String()}
}

// maximumIssueValueBytes bounds one value of an incident in its issue.
const maximumIssueValueBytes = 128

// issueText keeps a value of an incident to the characters of identifiers,
// replacing any other, so that no value can format the issue's Markdown.
func issueText(value string) string {
	return strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || strings.ContainsRune("_-.:/*@+", r) {
			return r
		}
		return '?'
	}, comm.TruncateUTF8(value, maximumIssueValueBytes))
}

// issueValue is a value of an incident as inline code, or none when empty.
func issueValue(value string) string {
	if value == "" {
		return "none"
	}
	return "`" + issueText(value) + "`"
}

// The tests name these constants.
const (
	OutcomeEventForTest = outcomeEvent
)

// The tests name these values.
var (
	OutcomeLoggedForTest = outcomeLogged
)

// SnapshotForTest is snapshot for the tests.
type SnapshotForTest = snapshot

// OutcomeRecordForTest is outcomeEventRecord for the tests.
type OutcomeRecordForTest = outcomeEventRecord

// SignatureForTest is signature for the tests.
func (r Incident) SignatureForTest() string {
	return r.signature()
}
