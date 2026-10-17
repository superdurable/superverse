//go:build apptest

package apptest_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/llmmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/connectormock"
	"github.com/superdurable/dex-template-basic-process/internal/api/generated"
	"github.com/superdurable/dex-template-basic-process/internal/apptest"
	"github.com/superdurable/dex-template-basic-process/internal/process"
	"github.com/superdurable/dex-template-basic-process/internal/worker"
)

// The reference tests of TextSummaryFlow, run with make test-app. The llm
// connector's mock package scripts each branch the Flow wires; a connection
// without credentials, as before Studio Configuration, selects the defect
// branch without any provider request.

// unconfiguredSummaryModel is the summary-model connection of a Project whose
// Studio Configuration holds no API key.
func unconfiguredSummaryModel(t *testing.T) worker.Connections {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderOpenai}, sdkgo.StaticCredentialProvider[llm.Credentials]{})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := llm.NewConnection(client, sdkgo.ConnectionRef{Provider: llm.ConnectorID, Name: process.TextSummaryConnectionName})
	if err != nil {
		t.Fatal(err)
	}
	return worker.Connections{SummaryModel: connection}
}

// requireStatus waits until the summary reaches status and returns it.
func requireStatus(t *testing.T, env *apptest.Env, summaryID uuid.UUID, status generated.TextSummaryStatus) *generated.TextSummary {
	t.Helper()
	var summary *generated.TextSummary
	env.Eventually(t, 20*time.Second, func() bool {
		read, err := env.API.GetTextSummary(env.Context(t), generated.GetTextSummaryParams{SummaryId: summaryID})
		if err != nil {
			t.Fatalf("read summary %s: %v", summaryID, err)
		}
		summary = read
		return read.Status == status
	})
	return summary
}

// requireAPIError requires an error response with status and code.
func requireAPIError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var response *generated.ErrorStatusCode
	if !errors.As(err, &response) || response.StatusCode != status || response.Response.Error != code {
		t.Fatalf("want %d %s, got %v", status, code, err)
	}
}

func TestTextSummaryRejectsBlankText(t *testing.T) {
	env := apptest.Start(t, unconfiguredSummaryModel(t))
	summaryID := uuid.New()
	_, err := env.API.StartTextSummary(env.Context(t), &generated.StartTextSummaryRequest{RequestId: summaryID, Text: "   "})
	requireAPIError(t, err, http.StatusBadRequest, "invalid_text")
	// The rejected request started no Flow.
	_, err = env.API.GetTextSummary(env.Context(t), generated.GetTextSummaryParams{SummaryId: summaryID})
	requireAPIError(t, err, http.StatusNotFound, "summary_not_found")
}

func TestTextSummaryWithoutCredentialsReportsDefect(t *testing.T) {
	env := apptest.Start(t, unconfiguredSummaryModel(t))
	summaryID := uuid.New()
	accepted, err := env.API.StartTextSummary(env.Context(t), &generated.StartTextSummaryRequest{RequestId: summaryID, Text: "Dex keeps every Flow's state durable."})
	if err != nil || accepted.SummaryId != summaryID {
		t.Fatalf("start the summary: %+v, %v", accepted, err)
	}
	summary := requireStatus(t, env, summaryID, generated.TextSummaryStatusDefect)
	if summary.Summary != "" || !strings.Contains(summary.Notice, "Studio Configuration") {
		t.Fatalf("a failed request must show no summary and name the configuration: %+v", summary)
	}
}

func TestTextSummaryRepeatedRequestStartsOneSummary(t *testing.T) {
	env := apptest.Start(t, unconfiguredSummaryModel(t))
	summaryID := uuid.New()
	request := &generated.StartTextSummaryRequest{RequestId: summaryID, Text: "A repeated submission is one summary."}
	for range 2 {
		accepted, err := env.API.StartTextSummary(env.Context(t), request)
		if err != nil || accepted.SummaryId != summaryID {
			t.Fatalf("submit the summary: %+v, %v", accepted, err)
		}
	}
	requireStatus(t, env, summaryID, generated.TextSummaryStatusDefect)
}

// TestTextSummaryBranches runs the summary once per branch the Flow wires,
// with the model's answer scripted by the connector's mock package: a summary
// is shown only for the generated and truncated branches, and every failure
// branch records its status with a notice and no text.
func TestTextSummaryBranches(t *testing.T) {
	cases := []struct {
		name    string
		answer  func() connectormock.Case[llm.GenerateTextResponse]
		status  generated.TextSummaryStatus
		summary bool
	}{
		{"generated", llmmock.GenerateTextShortSummary, generated.TextSummaryStatusCompleted, true},
		{"truncated", llmmock.GenerateTextOutputTokenLimit, generated.TextSummaryStatusTruncated, true},
		{"blocked", llmmock.GenerateTextContentPolicy, generated.TextSummaryStatusBlocked, false},
		{"providerRejected", llmmock.GenerateTextInvalidKey, generated.TextSummaryStatusProviderRejected, false},
		{"invalidResponse", llmmock.GenerateTextSchemaMismatch, generated.TextSummaryStatusInvalidResponse, false},
		{"defect", llmmock.GenerateTextUnsupportedField, generated.TextSummaryStatusDefect, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			model := llmmock.New(t, process.TextSummaryConnectionName)
			model.GenerateText().Default(c.answer())
			env := apptest.Start(t, worker.Connections{SummaryModel: model.Connection()})
			summaryID := uuid.New()
			if _, err := env.API.StartTextSummary(env.Context(t), &generated.StartTextSummaryRequest{RequestId: summaryID, Text: "The connector shipped on time."}); err != nil {
				t.Fatalf("start the summary: %v", err)
			}
			summary := requireStatus(t, env, summaryID, c.status)
			if (summary.Summary != "") != c.summary || !c.summary && summary.Notice == "" {
				t.Fatalf("the %s branch shows %+v", c.name, summary)
			}
		})
	}
}
