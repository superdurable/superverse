//go:build apptest

package apptest_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/connectors/github/githubmock"
	"github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/connectors/google/gmail/gmailmock"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/llmmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/connectormock"
	"github.com/superdurable/dex-template-basic-process/internal/api/generated"
	"github.com/superdurable/dex-template-basic-process/internal/apptest"
	"github.com/superdurable/dex-template-basic-process/internal/process"
	"github.com/superdurable/dex-template-basic-process/internal/worker"
	"github.com/superdurable/dex/sdk-go/dex"
)

type mocks struct {
	github *githubmock.Mock
	model  *llmmock.Mock
	gmail  *gmailmock.Mock
}

func newMocks(t *testing.T) (worker.Connections, mocks) {
	t.Helper()
	gh := githubmock.New(t, process.GitHubConnectionName)
	model := llmmock.New(t, process.LLMConnectionName)
	mail := gmailmock.New(t, process.GmailConnectionName)
	gh.ListReleases().Default(githubmock.ListReleasesNoReleases())
	model.GenerateText().Default(llmmock.GenerateTextShortSummary())
	mail.SendMessage().Default(gmailmock.SendMessageSentToCustomer())
	return worker.Connections{GitHub: gh.Connection(), OpenAI: model.Connection(), Gmail: mail.Connection()}, mocks{github: gh, model: model, gmail: mail}
}

func subscribe(t *testing.T, env *apptest.Env, email string, requestID uuid.UUID) {
	t.Helper()
	accepted, err := env.API.Subscribe(env.Context(t), &generated.SubscribeRequest{RequestId: requestID, Email: email})
	if err != nil || accepted.Status != generated.SubscribeAcceptedStatusAccepted {
		t.Fatalf("subscribe: %+v, %v", accepted, err)
	}
}

func subscriberCount(t *testing.T, env *apptest.Env) int64 {
	t.Helper()
	var display map[string]any
	err := env.Dex.InvokeRPC(env.Context(t), process.SubscriberRegistryFlowID, process.SubscriptionRegistryFlow{}.GetDexDisplay, nil, &display)
	if err != nil {
		return 0
	}
	number, ok := display["subscriber-count"].(float64)
	if !ok {
		t.Fatalf("subscriber count is missing: %+v", display)
	}
	return int64(number)
}

func awaitSubscriberCount(t *testing.T, env *apptest.Env, count int64) {
	t.Helper()
	env.Eventually(t, 20*time.Second, func() bool { return subscriberCount(t, env) == count })
}

func campaignInput() process.NewsletterCampaignInput {
	return process.NewsletterCampaignInput{EventID: uuid.NewString(), TeamID: "T123", ChannelID: "C123", Timestamp: uuid.NewString(), UserID: "UADMIN"}
}

func startCampaign(t *testing.T, env *apptest.Env, input process.NewsletterCampaignInput) string {
	t.Helper()
	id := process.NewsletterCampaignFlowID(input.TeamID, input.ChannelID, input.Timestamp)
	_, err := env.Dex.StartFlow(env.Context(t), process.NewsletterCampaignFlow{}, id, input, dex.StartFlowOptions{
		IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, RequestID: &input.EventID,
	})
	if err != nil {
		t.Fatalf("start campaign: %v", err)
	}
	return id
}

func awaitCampaign(t *testing.T, env *apptest.Env, id string, status generated.NewsletterStatus) *generated.Newsletter {
	t.Helper()
	var latest *generated.Newsletter
	env.Eventually(t, 30*time.Second, func() bool {
		read, err := env.API.GetNewsletter(env.Context(t), generated.GetNewsletterParams{CampaignId: id})
		if err != nil {
			return false
		}
		latest = read
		return read.Status == status
	})
	return latest
}

func recentRelease(id int64, name, repository string, at time.Time) github.Release {
	return github.Release{ID: id, Name: name, TagName: name, URL: "https://github.com/superdurable/" + repository + "/releases/tag/" + name, Body: "Release notes for " + name, PublishedAt: &at}
}

func scriptReleases(gh *githubmock.Mock) {
	now := time.Now().UTC()
	dexRecent := recentRelease(101, "v1.0.0", "dex", now.Add(-24*time.Hour))
	dexOld := recentRelease(102, "v0.9.0", "dex", now.Add(-9*24*time.Hour))
	dexDraft := recentRelease(103, "draft", "dex", now.Add(-time.Hour))
	dexDraft.IsDraft = true
	connectorsRecent := recentRelease(201, "v2.0.0", "dex-connectors-library", now.Add(-48*time.Hour))
	gh.ListReleases().When(func(input github.ListReleasesInput) bool { return input.Repository == "dex" && input.Page == 1 },
		githubmock.ListReleasesListed(github.ReleasePage{Releases: []github.Release{dexOld, dexRecent, dexDraft}, NextPage: 2}, nil))
	gh.ListReleases().When(func(input github.ListReleasesInput) bool { return input.Repository == "dex" && input.Page == 2 },
		githubmock.ListReleasesListed(github.ReleasePage{Releases: []github.Release{dexRecent}, NextPage: 0}, nil))
	gh.ListReleases().When(func(input github.ListReleasesInput) bool { return input.Repository == "dex-connectors-library" },
		githubmock.ListReleasesListed(github.ReleasePage{Releases: []github.Release{connectorsRecent}, NextPage: 0}, nil))
}

func TestSubscriptionValidationAndDeduplication(t *testing.T) {
	connections, mock := newMocks(t)
	env := apptest.Start(t, connections)
	_, err := env.API.Subscribe(env.Context(t), &generated.SubscribeRequest{RequestId: uuid.New(), Email: "bad-address"})
	var apiErr *generated.ErrorStatusCode
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid email should get 400: %v", err)
	}
	baseline := subscriberCount(t, env)
	address := "reader" + strings.ReplaceAll(uuid.NewString(), "-", "") + "@example.com"
	requestID := uuid.New()
	subscribe(t, env, strings.ToUpper(address), requestID)
	subscribe(t, env, address, requestID)
	subscribe(t, env, address, uuid.New())
	awaitSubscriberCount(t, env, baseline+1)
	id := startCampaign(t, env, campaignInput())
	report := awaitCampaign(t, env, id, generated.NewsletterStatusCompleted)
	if report.RecipientCount != baseline+1 || report.SentCount != baseline+1 || len(mock.gmail.SendMessage().Calls()) != int(baseline+1) {
		t.Fatalf("duplicate subscription delivered more than once: %+v", report)
	}
}

func TestNewsletterReleasePagesAndTriggerIdentity(t *testing.T) {
	connections, mock := newMocks(t)
	scriptReleases(mock.github)
	env := apptest.Start(t, connections)
	baseline := subscriberCount(t, env)
	subscribe(t, env, "one@example.com", uuid.New())
	subscribe(t, env, "two@example.com", uuid.New())
	awaitSubscriberCount(t, env, baseline+2)

	config := slack.ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123", ThreadTriggerMatcher: slack.MessageMatcher{PosterUserIDs: []string{"UADMIN"}}}
	filter, err := process.NewNewsletterTriggerFilter(config)
	if err != nil {
		t.Fatal(err)
	}
	ts := uuid.NewString()
	event := sdkgo.TriggerEvent[slack.MessageEvent]{ID: "evt-1", Payload: slack.MessageEvent{TeamID: "T123", ChannelID: "C123", Timestamp: ts, ThreadTimestamp: ts, UserID: "UADMIN", Text: "publish newsletter"}}
	if !filter(event) {
		t.Fatal("administrator command should be accepted")
	}
	otherUser := event
	otherUser.Payload.UserID = "UOTHER"
	if filter(otherUser) {
		t.Fatal("other Slack users must not publish")
	}
	otherChannel := event
	otherChannel.Payload.ChannelID = "COTHER"
	if filter(otherChannel) {
		t.Fatal("other channels must not publish")
	}
	otherText := event
	otherText.Payload.Text = "a normal discussion"
	if filter(otherText) {
		t.Fatal("ordinary messages must not publish")
	}
	reply := event
	reply.Payload.ThreadTimestamp = "1720000000.000000"
	if filter(reply) {
		t.Fatal("thread replies must not publish")
	}

	input := process.MapNewsletterTrigger(event)
	id := startCampaign(t, env, input)
	report := awaitCampaign(t, env, id, generated.NewsletterStatusCompleted)
	if report.ReleaseCount != 2 || report.RecipientCount != baseline+2 || report.SentCount != baseline+2 || !strings.Contains(report.Report, "connector shipped") {
		t.Fatalf("wrong campaign result: %+v", report)
	}
	if len(mock.github.ListReleases().Calls()) != 3 || len(mock.model.GenerateText().Calls()) != 1 || len(mock.gmail.SendMessage().Calls()) != int(baseline+2) {
		t.Fatalf("expected three GitHub pages, one model call, two emails")
	}
	if !strings.Contains(mock.model.GenerateText().Calls()[0].Input.Messages[0].Text, "superdurable/dex-connectors-library") {
		t.Fatal("model input omitted connector releases")
	}
	startCampaign(t, env, input)
	if len(mock.gmail.SendMessage().Calls()) != int(baseline+2) {
		t.Fatal("replayed Slack message sent again")
	}
	recipients := map[string]bool{}
	for _, call := range mock.gmail.SendMessage().Calls() {
		if len(call.Input.To) != 1 {
			t.Fatalf("email not individual: %+v", call.Input)
		}
		recipients[call.Input.To[0]] = true
	}
	if !recipients["one@example.com"] || !recipients["two@example.com"] {
		t.Fatalf("wrong recipients: %+v", recipients)
	}
}

func TestNewsletterGitHubFailureBranches(t *testing.T) {
	cases := []struct {
		name   string
		answer func() connectormock.Case[github.ReleasePage]
	}{
		{"insufficientScope", githubmock.ListReleasesInsufficientScopeFailure},
		{"authorizationRevoked", githubmock.ListReleasesAuthorizationRevokedFailure},
		{"notFound", githubmock.ListReleasesNotFoundFailure},
		{"providerRejected", githubmock.ListReleasesProviderRejectedFailure},
		{"invalidResponse", githubmock.ListReleasesInvalidResponseFailure},
		{"defect", githubmock.ListReleasesDefectFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connections, mock := newMocks(t)
			mock.github.ListReleases().Default(tc.answer())
			env := apptest.Start(t, connections)
			report := awaitCampaign(t, env, startCampaign(t, env, campaignInput()), generated.NewsletterStatusReleaseFailed)
			if report.SentCount != 0 || len(mock.model.GenerateText().Calls()) != 0 || len(mock.gmail.SendMessage().Calls()) != 0 {
				t.Fatalf("GitHub failure should stop delivery: %+v", report)
			}
		})
	}
}

func TestNewsletterModelFailureBranches(t *testing.T) {
	cases := []struct {
		name   string
		answer func() connectormock.Case[llm.GenerateTextResponse]
	}{
		{"truncated", llmmock.GenerateTextOutputTokenLimit},
		{"blocked", llmmock.GenerateTextContentPolicy},
		{"providerRejected", llmmock.GenerateTextInvalidKey},
		{"invalidResponse", llmmock.GenerateTextSchemaMismatch},
		{"defect", llmmock.GenerateTextUnsupportedField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connections, mock := newMocks(t)
			scriptReleases(mock.github)
			mock.model.GenerateText().Default(tc.answer())
			env := apptest.Start(t, connections)
			report := awaitCampaign(t, env, startCampaign(t, env, campaignInput()), generated.NewsletterStatusGenerationFailed)
			if report.SentCount != 0 || len(mock.gmail.SendMessage().Calls()) != 0 {
				t.Fatalf("model failure should stop delivery: %+v", report)
			}
		})
	}
}

func TestNewsletterGmailBranches(t *testing.T) {
	cases := []struct {
		name                  string
		answer                func() connectormock.Case[gmail.SendMessageOutput]
		sent, failed, unknown int64
	}{
		{"sent", gmailmock.SendMessageSentToCustomer, 1, 0, 0},
		{"rejected", gmailmock.SendMessageInvalidRecipient, 0, 1, 0},
		{"uncertain", gmailmock.SendMessageConnectionLost, 0, 0, 1},
		{"defect", gmailmock.SendMessageNoRecipients, 0, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			connections, mock := newMocks(t)
			mock.gmail.SendMessage().Default(tc.answer())
			env := apptest.Start(t, connections)
			baseline := subscriberCount(t, env)
			subscribe(t, env, "reader"+strings.ReplaceAll(uuid.NewString(), "-", "")+"@example.com", uuid.New())
			awaitSubscriberCount(t, env, baseline+1)
			report := awaitCampaign(t, env, startCampaign(t, env, campaignInput()), generated.NewsletterStatusCompleted)
			if report.SentCount != tc.sent*(baseline+1) || report.FailedCount != tc.failed*(baseline+1) || report.UnknownCount != tc.unknown*(baseline+1) || len(mock.gmail.SendMessage().Calls()) != int(baseline+1) {
				t.Fatalf("wrong Gmail branch or duplicate send: %+v", report)
			}
		})
	}
}
