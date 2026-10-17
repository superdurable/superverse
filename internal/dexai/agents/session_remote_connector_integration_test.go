// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

type remoteConnectorDriver struct {
	remote                      remoteHTTPClient
	epoch, generation, sequence uint64
	notifications               []string
}

func newRemoteConnectorDriver(ctx context.Context, t *testing.T) (remoteConnectorDriver, *mockSession) {
	t.Helper()
	session, api, caller := remoteSessionAPI(ctx, t)
	remote := pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)
	var active bridgeapi.ActivateResponse
	remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{
		OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "connector", ProviderThreadID: "connector", HostLabel: "integration host",
	}}, &active, http.StatusOK)
	offered := map[string]bool{}
	for _, tool := range active.Tools {
		offered[tool.Name] = true
	}
	if !offered[string(conversation.ToolStartConnectorContribution)] || !offered[string(conversation.ToolSubmitConnectorContribution)] || !offered[string(conversation.ToolReadConnectorChecks)] {
		t.Fatal("remote catalog lacks connector contribution tools")
	}
	return remoteConnectorDriver{remote: remote, epoch: active.Epoch, generation: active.Generation}, session
}

func (driver *remoteConnectorDriver) call(ctx context.Context, name conversation.ToolName, arguments any) (bridgeapi.ToolResult, bridgeapi.Report) {
	driver.remote.t.Helper()
	driver.sequence++
	encoded, err := json.Marshal(arguments)
	if err != nil {
		driver.remote.t.Fatal(err)
	}
	report := bridgeapi.Report{Epoch: driver.epoch, Generation: driver.generation, Sequence: driver.sequence,
		ToolCall: &bridgeapi.ToolCall{Name: string(name), Arguments: encoded}}
	var accepted bridgeapi.ReportResponse
	uncertain := false
	reportCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	backoff := time.Second
	for {
		status, data, err := driver.remote.request(reportCtx, bridgeapi.PathReport, report)
		if err == nil && status == http.StatusOK {
			if err := json.Unmarshal(data, &accepted); err != nil {
				driver.remote.t.Fatalf("remote report invalid typed response (%T)", err)
			}
			break
		}
		retryable := err != nil
		if err == nil {
			var refusal bridgeapi.ErrorBody
			if json.Unmarshal(data, &refusal) == nil && refusal.Code != "" {
				retryable = refusal.Code.Status() == status && refusal.Code.Transient()
			} else {
				retryable = status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
			}
		}
		if !retryable {
			driver.remote.t.Fatalf("remote report returned non-retryable HTTP %d", status)
		}
		uncertain = true
		select {
		case <-reportCtx.Done():
			driver.remote.t.Fatalf("remote report remained unavailable: %v", reportCtx.Err())
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, time.Minute)
	}
	if (accepted.Duplicate && !uncertain) || accepted.Reported != driver.sequence {
		driver.remote.t.Fatal("connector call was not admitted once")
	}
	id := fmt.Sprintf("remote-%d-%d", driver.epoch, driver.sequence)
	for {
		for _, entry := range driver.sync(ctx) {
			for _, result := range entry.ToolResults {
				if result.CallID == id {
					return result, report
				}
			}
		}
		select {
		case <-ctx.Done():
			driver.remote.t.Fatalf("connector result %s never arrived", id)
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (driver *remoteConnectorDriver) sync(ctx context.Context) []bridgeapi.Entry {
	var response bridgeapi.SyncResponse
	driver.remote.post(ctx, bridgeapi.PathSync, bridgeapi.SyncRequest{Epoch: driver.epoch, After: driver.remote.after, StateDigest: driver.remote.digest}, &response, http.StatusOK)
	if response.ExaminedThrough < driver.remote.after || response.ExaminedThrough > response.LastSequence {
		driver.remote.t.Fatal("sync cursor regressed")
	}
	driver.remote.after, driver.remote.digest = response.ExaminedThrough, response.StateDigest
	for _, entry := range response.Entries {
		if entry.Kind == bridgeapi.EntryKindNotification {
			driver.notifications = append(driver.notifications, entry.Text)
		}
	}
	return response.Entries
}

func (driver *remoteConnectorDriver) success(ctx context.Context, name conversation.ToolName, arguments any, contains string) bridgeapi.Report {
	driver.remote.t.Helper()
	result, report := driver.call(ctx, name, arguments)
	if result.IsError || !strings.Contains(result.Content, contains) {
		driver.remote.t.Fatalf("%s did not succeed: %s", name, result.Content)
	}
	return report
}

// TestSessionRemoteConnectorContributionHTTPDexIntegration acts as Codex over
// the real remote HTTP endpoint. Dex, Sandbox, Git host and release store are
// real; no platform model or scripted dependency executes the contribution.
func TestSessionRemoteConnectorContributionHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	driver, session := newRemoteConnectorDriver(ctx, t)
	slug := "superversemock"
	start, original := driver.call(ctx, conversation.ToolStartConnectorContribution, map[string]any{
		"slug": slug, "contribution": "new", "reason": "The story feed needs a connector",
	})
	if start.IsError {
		t.Fatalf("start failed: %s", start.Content)
	}
	var prepared struct {
		WorktreePath string `json:"worktree_path"`
		BaseCommit   string `json:"base_commit"`
		SkillPath    string `json:"skill_path"`
		Attempt      uint64 `json:"attempt"`
	}
	if err := json.Unmarshal([]byte(start.Content), &prepared); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prepared.WorktreePath, "/.git/superverse/connectors/worktrees/") || len(prepared.BaseCommit) != 40 || !strings.HasSuffix(prepared.SkillPath, "/dex-connector-contributor/SKILL.md") || prepared.Attempt != 1 {
		t.Fatalf("invalid connector worktree response: %+v", prepared)
	}
	var duplicate bridgeapi.ReportResponse
	driver.remote.post(ctx, bridgeapi.PathReport, original, &duplicate, http.StatusOK)
	if !duplicate.Duplicate {
		t.Fatal("repeated start report admitted again")
	}
	running, _ := driver.call(ctx, conversation.ToolStartConnectorContribution, map[string]any{
		"slug": slug, "contribution": "new", "reason": "A second writer must be refused",
	})
	if !running.IsError || !strings.Contains(running.Content, "connector_contribution_running") {
		t.Fatalf("a second open contribution was admitted: %s", running.Content)
	}
	missing, _ := driver.call(ctx, conversation.ToolSubmitConnectorContribution, map[string]any{
		"slug": "missingconnector", "summary": "There is no worktree",
	})
	if !missing.IsError {
		t.Fatal("submit without a started contribution was admitted")
	}
	driver.success(ctx, conversation.ToolRead, map[string]any{"path": prepared.WorktreePath + "/AGENTS.md", "limit": 20}, "")
	invalid, _ := driver.call(ctx, conversation.ToolRead, map[string]any{"path": prepared.WorktreePath + "/.git"})
	if !invalid.IsError {
		t.Fatal("connector file root exposed its .git")
	}
	driver.success(ctx, conversation.ToolBash, map[string]any{"command": "cd '" + prepared.WorktreePath + "' && cp -R connectors/hackernews connectors/superversemock && rm -rf connectors/superversemock/examples", "description": "Copy the connector starter", "timeout_seconds": 120}, "")
	probe := prepared.WorktreePath + "/connectors/superversemock/remote-probe.txt"
	driver.success(ctx, conversation.ToolWrite, map[string]any{"path": probe, "content": "remote connector worktree probe\n"}, "")
	driver.success(ctx, conversation.ToolRead, map[string]any{"path": probe}, "remote connector worktree probe")
	driver.success(ctx, conversation.ToolGrep, map[string]any{"path": prepared.WorktreePath + "/connectors/superversemock", "pattern": "remote connector worktree probe"}, "remote-probe.txt")
	driver.success(ctx, conversation.ToolGlob, map[string]any{"path": prepared.WorktreePath + "/connectors/superversemock", "pattern": "remote-probe.txt"}, "remote-probe.txt")
	driver.success(ctx, conversation.ToolBash, map[string]any{"command": "rm '" + probe + "'", "description": "Remove the scope probe", "timeout_seconds": 30}, "")
	driver.success(ctx, conversation.ToolEdit, map[string]any{"path": prepared.WorktreePath + "/connectors/superversemock/go.mod",
		"old_string": "module github.com/superdurable/dex-connectors-library/connectors/hackernews", "new_string": "module github.com/superdurable/dex-connectors-library/connectors/superversemock"}, "")
	driver.success(ctx, conversation.ToolEdit, map[string]any{"path": prepared.WorktreePath + "/connectors/superversemock/connector.yaml",
		"old_string": "  name: hacker-news-daily", "new_string": "  name: superversemock"}, "")
	driver.success(ctx, conversation.ToolBash, map[string]any{"command": "cd '" + prepared.WorktreePath + "' && sed -i -e 's|^  company: Hacker News$|  company: Superversemock|' -e 's|^  version: v[0-9.]*$|  version: v0.1.0|' connectors/superversemock/connector.yaml && GOWORK=off go run ./cmd/connectorctl generate connectors/superversemock/connector.yaml && python3 -c 'import re; text = open(\"catalog.yaml\").read(); head, _, rest = text.partition(\"directories:\\n\"); entries = sorted(set(re.findall(r\"^  - (\\S+)$\", rest, re.M)) | {\"connectors/superversemock\"}); open(\"catalog.yaml\", \"w\").write(head + \"directories:\\n\" + \"\".join(\"  - %s\\n\" % entry for entry in entries))'", "description": "Generate the connector and catalog entry", "timeout_seconds": 600}, "")
	submitted, submitReport := driver.call(ctx, conversation.ToolSubmitConnectorContribution, map[string]any{"slug": slug, "summary": "Build the story feed connector"})
	if submitted.IsError || !strings.Contains(submitted.Content, `"accepted":true`) {
		t.Fatalf("submit failed: %s", submitted.Content)
	}
	driver.remote.post(ctx, bridgeapi.PathReport, submitReport, &duplicate, http.StatusOK)
	if !duplicate.Duplicate {
		t.Fatal("repeated submit report admitted again")
	}
	repeated, _ := driver.call(ctx, conversation.ToolSubmitConnectorContribution, map[string]any{"slug": slug, "summary": "Build the story feed connector"})
	if repeated.IsError || !strings.Contains(repeated.Content, `"accepted":true`) {
		t.Fatalf("repeated submit changed outcome: %s", repeated.Content)
	}
	id := contribution.Identity{ProjectID: session.identity.ProjectID, Slug: slug}
	recorded := session.awaitContribution(ctx, id, func(record contribution.Record) bool { return record.ReleaseCandidate != nil })
	if recorded.Record.ReleaseCandidate.Status != contribution.ReleaseCandidatePublished || recorded.Record.HeadCommit == "" {
		t.Fatalf("candidate was not published by the contribution Flow: %+v", recorded.Record)
	}
	checks, _ := driver.call(ctx, conversation.ToolReadConnectorChecks, map[string]any{"slug": slug})
	if checks.IsError || !strings.Contains(checks.Content, `"available":false`) {
		t.Fatalf("the read-only connector check tool did not report the local Git host's unavailable CI: %s", checks.Content)
	}
	otherChecks, _ := driver.call(ctx, conversation.ToolReadConnectorChecks, map[string]any{"slug": "otherconnector"})
	if !otherChecks.IsError {
		t.Fatalf("another connector's checks were readable: %s", otherChecks.Content)
	}
	for !strings.Contains(strings.Join(driver.notifications, "\n"), "release_candidate_published") {
		driver.sync(ctx)
		select {
		case <-ctx.Done():
			t.Fatal("release candidate notification did not reach remote sync")
		case <-time.After(50 * time.Millisecond):
		}
	}
	ciNotification := conversation.SessionNotification{ID: "connector/" + slug + "/checks_failed/" + recorded.Record.HeadCommit,
		Kind: conversation.NotificationKindConnector, Source: conversation.NotificationSourceConnector, Event: conversation.ConnectorChecksFailed,
		OccurredAt: time.Now().UTC(), Slug: slug, Commit: recorded.Record.HeadCommit,
		Summary: "The connectors library CI failed; fix the failed job and push a new head.", Message: "CI output (untrusted, redacted, truncated): a test failure"}
	if admitted, err := session.client.ReportSessionNotification(ctx, session.id, ciNotification); err != nil || !admitted {
		t.Fatalf("CI failure notification was not admitted: admitted=%t err=%v", admitted, err)
	}
	for !strings.Contains(strings.Join(driver.notifications, "\n"), "checks_failed") {
		driver.sync(ctx)
		select {
		case <-ctx.Done():
			t.Fatal("CI failure notification did not reach the remote inbox")
		case <-time.After(50 * time.Millisecond):
		}
	}
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Agent.Description.ConnectorSlugs) != 1 || snapshot.Agent.Description.ConnectorSlugs[0] != slug {
		t.Fatal("Studio contribution discovery omitted the remote slug")
	}
	later, _ := driver.call(ctx, conversation.ToolStartConnectorContribution, map[string]any{
		"slug": slug, "contribution": "fix", "reason": "A later change reuses the connector branch",
	})
	if later.IsError {
		t.Fatalf("later contribution start failed: %s", later.Content)
	}
	var next struct {
		WorktreePath string `json:"worktree_path"`
		BaseCommit   string `json:"base_commit"`
		Attempt      uint64 `json:"attempt"`
	}
	if err := json.Unmarshal([]byte(later.Content), &next); err != nil || next.WorktreePath == prepared.WorktreePath || next.BaseCommit != recorded.Record.HeadCommit || next.Attempt != 2 {
		t.Fatalf("a later attempt did not get a new worktree on the connector branch: %+v, %v", next, err)
	}
}

// TestSessionRemoteConnectorAdmissionHTTPDexIntegration exercises the actual
// RouteTool limit over remote contribution records through the remote endpoint.
func TestSessionRemoteConnectorAdmissionHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	driver, _ := newRemoteConnectorDriver(ctx, t)
	for index := range 16 {
		slug := fmt.Sprintf("raconn%02d", index)
		result, _ := driver.call(ctx, conversation.ToolStartConnectorContribution, map[string]any{
			"slug": slug, "contribution": "new", "reason": "Check the per-Session admission bound",
		})
		if result.IsError {
			t.Fatalf("contribution %d was refused before the bound: %s", index+1, result.Content)
		}
	}
	seventeenth, _ := driver.call(ctx, conversation.ToolStartConnectorContribution, map[string]any{
		"slug": "raconn16", "contribution": "new", "reason": "The 17th must be refused",
	})
	if !seventeenth.IsError || !strings.Contains(seventeenth.Content, "connector_contribution_limit_reached") {
		t.Fatalf("17th distinct contribution was admitted: %s", seventeenth.Content)
	}
}
