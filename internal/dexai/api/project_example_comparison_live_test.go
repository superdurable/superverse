// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"text/tabwriter"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// The real-model comparison of the closest-example start. It calls Claude
// Sonnet 5.5 through the lane's Worker, so it runs only when the developer
// asks for it: `make test-example-comparison-live` sets the switch, which
// test-integration never does: it skips the Makefile's DEVELOPER_ONLY_TESTS.
const (
	exampleComparisonSwitch   = "SUPERVERSE_TEST_EXAMPLE_COMPARISON"
	exampleComparisonSwitchOn = "real-model"
	// exampleComparisonCodingImage names the coding image the lane's
	// Sandboxes run, by digest, which the table's header records: one whose
	// module proxy and build caches seed every example, or the matched runs
	// pay a cold first code check.
	exampleComparisonCodingImage = "SUPERVERSE_TEST_CODING_IMAGE"
	// exampleComparisonRequest is the labelled set's request that the
	// comparison types, the one expected to start from
	// exampleComparisonExample that reads most like a person's description.
	exampleComparisonRequest = "Approvals in Slack threads: someone posts a request, a teammate approves by replying in the thread, and the bot confirms in the same thread."
	exampleComparisonExample = "slack-thread-approval"
	// exampleComparisonDeadline bounds one run from CreateProject, through its
	// Setup, to its first build or the end of its first turn.
	exampleComparisonDeadline = 30 * time.Minute
	// exampleComparisonDeletion is how long projecttest waits for one
	// Project's deletion, which follows each run, and for the TeamSpace's at
	// the end. The target's -timeout of 190m covers the test's context
	// (exampleComparisonSetup and the four runs with their deletions, 165m)
	// and, after a failure, the deletions of a run's Project and the
	// TeamSpace that its cleanup then waits for.
	exampleComparisonDeletion = 10 * time.Minute
	// exampleComparisonSetup bounds the lane's setup: the creator, its
	// TeamSpace and the two API servers.
	exampleComparisonSetup = 5 * time.Minute
	exampleComparisonPoll  = 2 * time.Second
	// exampleComparisonSettle is how long a turn's end must hold, at the same
	// last sequence, before it counts: a background command's end records the
	// command and its notification in one commit, and the Session opens the
	// notification's turn in a later one.
	exampleComparisonSettle = 10 * time.Second
)

// exampleComparisonArms is the order of the runs, the pick on and off twice
// each, the second pair reversed, so that neither arm always runs first. No
// run warms another's caches: each Session gets a new Sandbox, which starts
// from the coding image's seeds.
var exampleComparisonArms = []bool{true, false, false, true}

// exampleComparisonColumns are the columns of the comparison table, which
// exampleComparisonRun.cells fills.
var exampleComparisonColumns = []string{"run", "arm", "project", "template revision", "first push", "design built", "commit gate", "version 1",
	"first build (example/built/kept Steps, commit)", "ended at"}

// exampleComparisonRun is what one run observed of its Session, polled every
// exampleComparisonPoll: the times are the creation times of the tool results
// that the Session recorded, measured from the CreateProject call.
type exampleComparisonRun struct {
	number    int
	pick      bool
	projectID string
	revision  string
	created   time.Time
	// pushedAt is the first pushed, unblocked commit_and_push, the first
	// build, and designBuiltAt the push after which the confirmed design
	// was first built; zero when the run ended without one.
	pushedAt, designBuiltAt time.Time
	// commitCalls counts the commit_and_push results up to the first pushed
	// one, blocked those the commit gate blocked; gatePassed is whether one
	// pushed.
	commitCalls, blocked int
	gatePassed           bool
	// cardRevision and cardFeatures are the first recorded requirements
	// version the run saw, normally version 1, and its feature count.
	cardRevision int64
	cardFeatures int
	// firstBuild is the outcome record of a matched start, through the
	// business API's mapping.
	firstBuild *pb.AgentStartingExampleBuild
	ended      string
	seen       map[conversation.Sequence]bool
	lastPushAt time.Time
	// idleSince and idleSequence are when the run last began to look idle
	// with nothing that would open another turn, and its last sequence then.
	idleSince    time.Time
	idleSequence conversation.Sequence
}

// TestLiveExampleComparison compares, with the real model Claude Sonnet 5.5,
// a Project that starts from the closest example with one that starts from
// the default, both created from the same labelled request through the actual
// gRPC boundary of two in-process API servers of this build, one with
// agent.closestExamplePick on and one with it off, against the lane's Dex,
// Worker, PostgreSQL, Git host and MinIO:
//   - the request is the labelled set's, and this build's decision picks
//     slack-thread-approval for it, which the Project with the pick on starts
//     from and the one with the pick off does not;
//   - each of the four runs (on, off, off, on) waits for its Session's first
//     build, the first pushed commit_and_push and the first built design, or
//     the end of its first turn once no background command or subagent that
//     would open another still runs, for at most 30 minutes from
//     CreateProject, its Setup included, and records the template revision,
//     the time to the first push and to the first built design, the commit
//     gate's results, version 1's feature count and the matched start's
//     first-build record; it logs them and deletes its Project;
//   - the comparison table, headed by the lane's coding image, is logged when
//     the test ends, also when it fails; the test fails on an infrastructure
//     error or a run without a first build, never on which arm is faster.
func TestLiveExampleComparison(t *testing.T) {
	if os.Getenv(exampleComparisonSwitch) != exampleComparisonSwitchOn {
		t.Fatalf("TestLiveExampleComparison calls the real model: run it with make test-example-comparison-live, which sets %s=%s", exampleComparisonSwitch, exampleComparisonSwitchOn)
	}
	if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == "" {
		t.Fatal("ANTHROPIC_API_KEY is required: the lane's server reads the same key from the main checkout's .env, which the make target passes")
	}
	codingImage := os.Getenv(exampleComparisonCodingImage)
	if !strings.Contains(codingImage, "@sha256:") {
		t.Fatalf("%s must name, by digest, the coding image the lane's Sandboxes run, one that seeds every example's module graph and build cache", exampleComparisonCodingImage)
	}
	requireLabelledComparisonRequest(t)
	ctx, cancel := context.WithTimeout(t.Context(), exampleComparisonSetup+time.Duration(len(exampleComparisonArms))*(exampleComparisonDeadline+exampleComparisonDeletion))
	defer cancel()
	lane := newClosestExampleLane(ctx, t)
	servers := map[bool]pb.ProjectServiceClient{true: lane.server(true), false: lane.server(false)}
	revisions := map[bool]string{true: comm.Must(apptemplate.ExampleTemplate(exampleComparisonExample)).Revision, false: apptemplate.Current().Revision}
	var runs []*exampleComparisonRun
	// A failed run ends the test, which still logs every run measured so far.
	defer func() {
		t.Log("closest-example comparison on Claude Sonnet 5.5, coding image " + codingImage + ", request: " + exampleComparisonRequest + "\n" + exampleComparisonTable(runs))
	}()
	for number, pick := range exampleComparisonArms {
		run := &exampleComparisonRun{number: number + 1, pick: pick, created: time.Now(), seen: map[conversation.Sequence]bool{}}
		runs = append(runs, run)
		runComparison(ctx, t, lane, servers[pick], revisions[pick], run)
		t.Log(run.summary())
		projecttest.DeleteForTest(context.WithoutCancel(ctx), t, lane.projects, run.projectID)
	}
	for _, run := range runs {
		switch {
		case !run.gatePassed:
			t.Errorf("run %d (%s, Project %s) has no first build: it ended at %s after %d commit_and_push results", run.number, run.arm(), run.projectID, run.ended, run.commitCalls)
		case run.pick && run.firstBuild == nil:
			t.Errorf("run %d (Project %s) started from the example and pushed, but recorded no first build", run.number, run.projectID)
		}
	}
}

// runComparison creates the run's Project through server, waits for its
// Setup and observes its Session, all within the run's deadline, measured
// from its creation.
func runComparison(ctx context.Context, t *testing.T, lane closestExampleLane, server pb.ProjectServiceClient, revision string, run *exampleComparisonRun) {
	t.Helper()
	deadline, cancel := context.WithDeadline(ctx, run.created.Add(exampleComparisonDeadline))
	defer cancel()
	run.projectID = createWithFirstTurn(deadline, t, server, &pb.CreateProjectRequest{Caller: lane.creator, RequestId: flowid.RandomID(24),
		Name: fmt.Sprintf("%s %d", t.Name(), run.number), ManagedRepository: &pb.ManagedRepositorySource{}, TeamSpaceId: lane.teamSpaceID,
		InitialMessage: exampleComparisonRequest, MessageId: "first-" + flowid.RandomID(12), Model: pb.AgentModel_AGENT_MODEL_SONNET_5_5}, lane.projects)
	run.revision = projecttest.AwaitReadyForTest(deadline, t, lane.projects, run.projectID).TemplateRevision
	if run.revision != revision {
		t.Fatalf("run %d (%s) starts Project %s from %q, not %s", run.number, run.arm(), run.projectID, run.revision, revision)
	}
	observeComparisonRun(deadline, t, lane, run)
}

// requireLabelledComparisonRequest fails unless the comparison's request is
// the labelled set's, expected to start from the example, and this build's
// decision picks that example for it.
func requireLabelledComparisonRequest(t *testing.T) {
	t.Helper()
	var labelled struct {
		Requests []struct {
			Expect string `json:"expect"`
			Text   string `json:"text"`
		} `json:"requests"`
	}
	comm.MustDo(json.Unmarshal(comm.Must(os.ReadFile("../../../app-template/examples/labelled-requests.json")), &labelled))
	found := false
	for _, request := range labelled.Requests {
		found = found || request.Text == exampleComparisonRequest && request.Expect == exampleComparisonExample
	}
	if !found {
		t.Fatalf("the labelled set holds no request %q expected to start from %s", exampleComparisonRequest, exampleComparisonExample)
	}
	if matched, isMatch := apptemplate.ClosestExample(exampleComparisonRequest); !isMatch || matched.ExampleID != exampleComparisonExample {
		t.Fatalf("this build's decision starts the comparison's request from %+v, not %s", matched, exampleComparisonExample)
	}
}

// observeComparisonRun polls the run's Session until its first build, the
// end of its first turn or deadline, the run's. A Session that stops, fails
// or never becomes readable is an infrastructure failure.
func observeComparisonRun(deadline context.Context, t *testing.T, lane closestExampleLane, run *exampleComparisonRun) {
	t.Helper()
	id, read := agentloop.SessionFlowID(run.projectID), false
	for {
		snapshot, err := lane.sessions.GetSnapshot(deadline, id)
		if err == nil && snapshot.Agent != nil {
			read = true
			if snapshot.Session.Status != agentloop.SessionStatusActive || snapshot.ExecutionFailure != agentloop.ExecutionRunning {
				t.Fatalf("the Session of Project %s ended as %s (%s) before its first build", run.projectID, snapshot.Session.Status, snapshot.ExecutionFailure)
			}
			if model := snapshot.Agent.Description.Model; model != conversation.ModelSonnet55 {
				t.Fatalf("the Session of Project %s runs %q, not Claude Sonnet 5.5", run.projectID, model)
			}
			if run.ended = run.observe(*snapshot.Agent, time.Now()); run.ended != "" {
				return
			}
		}
		select {
		case <-deadline.Done():
			if !read {
				t.Fatalf("the Session of Project %s was never readable (%v)", run.projectID, err)
			}
			run.ended = "the 30-minute deadline"
			return
		case <-time.After(exampleComparisonPoll):
		}
	}
}

// observe records what a snapshot read at now shows of the run and returns
// why the run ends, or "" while it goes on: its first build, which the
// design's build completes when version 1 carries one, or the end of its
// first turn, once it held for exampleComparisonSettle with nothing running
// that would open another turn.
func (run *exampleComparisonRun) observe(agent agentloop.AgentSnapshot, now time.Time) string {
	for _, message := range agent.History.Messages {
		tool := message.Message
		if run.seen[message.Sequence] || tool.Role != conversation.MessageRoleTool || tool.ToolName == nil || *tool.ToolName != conversation.ToolCommitAndPush || tool.ToolExecution == nil {
			continue
		}
		run.seen[message.Sequence] = true
		pushed := tool.ToolExecution.ResultCode == agentloop.CommitPushedForTest
		if pushed {
			run.lastPushAt = tool.CreatedAt
		}
		if run.gatePassed {
			continue
		}
		run.commitCalls++
		switch {
		case pushed:
			run.gatePassed, run.pushedAt = true, tool.CreatedAt
		case tool.ToolExecution.ResultCode == agentloop.CommitBlockedForTest:
			run.blocked++
		}
	}
	description := agent.Description
	if confirmed := description.ConfirmedRequirements; confirmed != nil && run.cardRevision == 0 {
		run.cardRevision, run.cardFeatures = confirmed.Revision, len(confirmed.Spec.Features)
	}
	if description.ConfirmedDesignBuilt && run.designBuiltAt.IsZero() {
		run.designBuiltAt = run.lastPushAt
	}
	if run.firstBuild == nil {
		run.firstBuild = agentDescription(description).GetStartingExample().GetFirstBuild()
	}
	versionHasDesign := description.ConfirmedRequirements != nil && description.ConfirmedRequirements.Spec.Design != nil
	if run.gatePassed && (!run.designBuiltAt.IsZero() || !versionHasDesign) && (!run.pick || run.firstBuild != nil) {
		return "its first build"
	}
	// A background command or a subagent that still runs ends with a
	// notification, which opens another turn.
	idle := description.LastSequence > 0 && description.Turn > 0 && description.PendingQueuedMessageCount == 0 && description.PendingSteeredMessageCount == 0 &&
		(description.Status == agentloop.AgentStatusWaitingForMessage || description.Status == agentloop.AgentStatusWaitingForToolApproval) &&
		!slices.ContainsFunc(description.BackgroundCommands, agenttasks.BackgroundCommand.IsRunning) &&
		!slices.ContainsFunc(description.Subagents, func(record agenttasks.SubagentRecord) bool { return !record.IsEnded(now) })
	switch {
	case !idle:
		run.idleSince = time.Time{}
	case run.idleSince.IsZero() || run.idleSequence != description.LastSequence:
		run.idleSince, run.idleSequence = now, description.LastSequence
	case now.Sub(run.idleSince) >= exampleComparisonSettle:
		return fmt.Sprintf("the end of turn %d (%s)", description.Turn, description.Status)
	}
	return ""
}

func (run *exampleComparisonRun) arm() string {
	if run.pick {
		return "pick on"
	}
	return "pick off"
}

// cells are the run's row of the comparison table; a run that a failure cut
// short shows what it measured.
func (run *exampleComparisonRun) cells() []string {
	since := func(at time.Time) string {
		if at.IsZero() {
			return "-"
		}
		return at.Sub(run.created).Round(time.Second).String()
	}
	gate := fmt.Sprintf("no push in %d", run.commitCalls)
	if run.gatePassed {
		gate = fmt.Sprintf("passed at call %d (%d blocked)", run.commitCalls, run.blocked)
	}
	card := "-"
	if run.cardRevision > 0 {
		card = fmt.Sprintf("v%d: %d features", run.cardRevision, run.cardFeatures)
	}
	build := "-"
	if b := run.firstBuild; b != nil {
		build = fmt.Sprintf("%d/%d/%d %.12s", b.GetExampleSteps(), b.GetBuiltSteps(), b.GetKeptSteps(), b.GetCommit())
	}
	ended := run.ended
	if ended == "" {
		ended = "- (the test failed during the run)"
	}
	return []string{fmt.Sprint(run.number), run.arm(), run.projectID, run.revision, since(run.pushedAt), since(run.designBuiltAt), gate, card, build, ended}
}

// summary is the run's row with its column names, logged as the run ends.
func (run *exampleComparisonRun) summary() string {
	cells := run.cells()
	pairs := make([]string, len(cells))
	for index, cell := range cells {
		pairs[index] = exampleComparisonColumns[index] + ": " + cell
	}
	return strings.Join(pairs, "; ")
}

// exampleComparisonTable lays the runs out with each arm's mean time to the
// first push.
func exampleComparisonTable(runs []*exampleComparisonRun) string {
	var table strings.Builder
	writer := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, strings.Join(exampleComparisonColumns, "\t"))
	totals, pushed, counts := map[bool]time.Duration{}, map[bool]int{}, map[bool]int{}
	for _, run := range runs {
		fmt.Fprintln(writer, strings.Join(run.cells(), "\t"))
		counts[run.pick]++
		if run.gatePassed {
			totals[run.pick] += run.pushedAt.Sub(run.created)
			pushed[run.pick]++
		}
	}
	comm.MustDo(writer.Flush())
	for _, pick := range []bool{true, false} {
		mean := "no run pushed"
		if pushed[pick] > 0 {
			mean = (totals[pick] / time.Duration(pushed[pick])).Round(time.Second).String()
		}
		fmt.Fprintf(&table, "%s: mean time to the first push %s over %d of %d runs\n", (&exampleComparisonRun{pick: pick}).arm(), mean, pushed[pick], counts[pick])
	}
	return table.String()
}
