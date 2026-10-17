//go:build apptest

package apptest_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-template-basic-process/internal/api/generated"
	"github.com/superdurable/dex-template-basic-process/internal/apptest"
	"github.com/superdurable/dex-template-basic-process/internal/process"
	"github.com/superdurable/dex-template-basic-process/internal/worker"
	"github.com/superdurable/dex/sdk-go/dex"
)

// The tests of SlackThreadApprovalFlow, run with make test-app. The Slack
// connector has no mock package yet, so the slack-approvals connection is one
// without credentials, as before Studio Configuration: the completion reply
// selects the defect branch without any Slack request. The tests start the
// Flow as Studio Start Flow and the start Trigger do, and send the reply as
// the reply Trigger does.

// unconfiguredSlack is the slack-approvals connection of a Project whose
// Studio Configuration holds no Slack authorization.
func unconfiguredSlack(t *testing.T) worker.Connections {
	t.Helper()
	client, err := slack.New(slack.Config{}, sdkgo.StaticCredentialProvider[slack.Credentials]{})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := slack.NewConnection(client, sdkgo.ConnectionRef{Provider: slack.ConnectorID, Name: process.SlackApprovalsConnectionName})
	if err != nil {
		t.Fatal(err)
	}
	return worker.Connections{SlackApprovals: connection}
}

// startThread starts the approval of a new root message, as the start Trigger
// does, and returns its ID.
func startThread(t *testing.T, env *apptest.Env, thread process.ThreadInput) string {
	t.Helper()
	approvalID := process.SlackThreadApprovalFlowID(thread.TeamID, thread.ChannelID, thread.ThreadTimestamp)
	_, err := env.Dex.StartFlow(env.Context(t), process.SlackThreadApprovalFlow{}, approvalID, thread, dex.StartFlowOptions{
		IDReusePolicy:  dex.IDReuseDisallow,
		AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true},
		RequestID:      &thread.EventID,
	})
	if err != nil {
		t.Fatalf("start the thread approval: %v", err)
	}
	return approvalID
}

func newThread() process.ThreadInput {
	return process.ThreadInput{EventID: "Ev" + uuid.NewString(), TeamID: "T0123ABCD", ChannelID: "C0456EFGH", ThreadTimestamp: "1790400000." + uuid.NewString()[:6]}
}

// requireStatus waits until the approval reaches status and returns it.
func requireStatus(t *testing.T, env *apptest.Env, approvalID string, status generated.ThreadApprovalStatus) *generated.ThreadApproval {
	t.Helper()
	var approval *generated.ThreadApproval
	env.Eventually(t, 20*time.Second, func() bool {
		read, err := env.API.GetThreadApproval(env.Context(t), generated.GetThreadApprovalParams{ApprovalId: approvalID})
		if err != nil {
			t.Fatalf("read approval %s: %v", approvalID, err)
		}
		approval = read
		return read.Status == status
	})
	return approval
}

func TestThreadApprovalStartsOnRootMessage(t *testing.T) {
	env := apptest.Start(t, unconfiguredSlack(t))
	thread := newThread()
	approvalID := startThread(t, env, thread)
	// A redelivered root message reaches the same approval and starts no second one.
	if again := startThread(t, env, thread); again != approvalID {
		t.Fatalf("a redelivered root message must reach %s, got %s", approvalID, again)
	}
	approval := requireStatus(t, env, approvalID, generated.ThreadApprovalStatusWaitingForReply)
	if approval.ChannelId != thread.ChannelID || approval.ThreadTimestamp != thread.ThreadTimestamp || approval.ReplyUserId != "" {
		t.Fatalf("the approval must keep its thread and have no reply yet: %+v", approval)
	}
}

func TestThreadApprovalReplyPostsCompletion(t *testing.T) {
	env := apptest.Start(t, unconfiguredSlack(t))
	approvalID := startThread(t, env, newThread())
	requireStatus(t, env, approvalID, generated.ThreadApprovalStatusWaitingForReply)
	var result process.ThreadReplyResult
	err := env.Dex.InvokeRPC(env.Context(t), approvalID, process.SlackThreadApprovalFlow{}.ReceiveThreadReply,
		process.ThreadReply{EventID: "Ev" + uuid.NewString(), UserID: "U0789IJKL"}, &result)
	if err != nil || !result.Accepted {
		t.Fatalf("the first reply must be accepted: %+v, %v", result, err)
	}
	// Without Slack credentials the completion reply is not sent, and the
	// approval says so instead of looking completed.
	approval := requireStatus(t, env, approvalID, generated.ThreadApprovalStatusFailed)
	if approval.ReplyUserId != "U0789IJKL" || !strings.Contains(approval.Notice, "Studio Configuration") {
		t.Fatalf("a failed completion reply must name the replier and the configuration: %+v", approval)
	}
}

func TestThreadApprovalUnknownIDIsNotFound(t *testing.T) {
	env := apptest.Start(t, unconfiguredSlack(t))
	_, err := env.API.GetThreadApproval(env.Context(t), generated.GetThreadApprovalParams{ApprovalId: "slack-thread-approval-T0-C0-0." + uuid.NewString()[:6]})
	var response *generated.ErrorStatusCode
	if !errors.As(err, &response) || response.StatusCode != http.StatusNotFound || response.Response.Error != "approval_not_found" {
		t.Fatalf("want 404 approval_not_found, got %v", err)
	}
}
