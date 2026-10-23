// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package git

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd/sandboxdtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// TestAgentWorktreeDiskBudgetActualSandbox exercises the admission threshold
// against actual bounded filesystems, without replacing the disk-space API.
func TestAgentWorktreeDiskBudgetActualSandbox(t *testing.T) {
	for _, size := range []int{1024, 128} {
		t.Run(fmt.Sprintf("workspace-%dMiB", size), func(t *testing.T) {
			client := sandboxdtest.StartCodingContainerWithWorkspaceForTest(t, size).Client()
			shell(t, client, `set -e; cd /workspace; git init -q -b main .
printf 'base\n' > note.txt
git add note.txt; git -c user.name=t -c user.email=t@example.com commit -q -m init
printf 'dirty\n' >> note.txt`)
			operations := &GitOperations{}
			worktree, err := operations.CreateAgentWorktree(testContext(t), client, "k3n8q5r1", nil, nil)
			if size == 1024 {
				if err != nil {
					t.Fatalf("a 1 GiB workspace must admit a small snapshot: %v", err)
				}
				if got := shell(t, client, "cat "+worktree.Root+"/note.txt"); got != "base\ndirty\n" {
					t.Fatalf("the snapshot lost dirty source: %q", got)
				}
				return
			}
			var typed *sandboxd.ToolFailure
			if !errors.As(err, &typed) || typed.Code() != toolfailure.WorkspaceDiskLow {
				t.Fatalf("a workspace below 256 MiB must retain the typed refusal: %v", err)
			}
			if got := shell(t, client, "cd /workspace; git for-each-ref refs/superverse/agents; git worktree list --porcelain | grep -c '^worktree '"); got != "1\n" {
				t.Fatalf("the refused creation left a ref or worktree: %q", got)
			}
		})
	}
}

// TestAgentWorktreeActualSandbox drives the subagent worktree helper through
// the real sandboxd of a coding image container that the test owns, with the
// image's own Git and Python: the snapshot of a dirty workspace through a
// temporary index, a worktree inside the Git directory, which Git accepts,
// finishing into the result ref, the per-file three-way merge with every
// conflict kind, discard, repeated calls, a merge resumed between its plan and
// its receipt, the cleanup of an orphan worktree and the refusals.
func TestAgentWorktreeActualSandbox(t *testing.T) {
	container := sandboxdtest.StartCodingContainerForTest(t)
	client := container.Client()
	operations := &GitOperations{}
	shell(t, client, `set -e; cd /workspace; git init -q -b main .
printf 'one\ntwo\nthree\nfour\nfive\nsix\nseven\n' > clean.txt
printf 'alpha\n' > both.txt; printf 'snapshot\n' > theirs.txt; printf 'keep\n' > deleted.txt
printf 'binary\0one' > image.bin; printf '*.log\n' > .gitignore
git add -A && git -c user.name=t -c user.email=t@example.com commit -q -m init
printf 'dirty change\n' >> theirs.txt; printf 'untracked\n' > untracked.txt; printf 'ignored\n' > build.log`)
	head, status := shell(t, client, "cd /workspace && git rev-parse HEAD"), shell(t, client, "cd /workspace && git status --porcelain")
	// A touch makes the index's stat data of clean.txt stale, which a status
	// read that takes Git's optional locks would refresh and write back.
	index := shell(t, client, "cd /workspace && touch -d @1000000000 clean.txt && sha256sum .git/index")

	const agent = "k3n8q5r1"
	worktree, err := operations.CreateAgentWorktree(testContext(t), client, agent, nil, nil)
	if err != nil {
		t.Fatalf("create the worktree: %v", err)
	}
	if shell(t, client, "cd /workspace && sha256sum .git/index") != index {
		t.Fatal("the snapshot rewrote the workspace's index")
	}
	if worktree.Root != AgentWorktreeRoot(agent) || worktree.Root != "/workspace/.git/superverse/worktrees/"+agent {
		t.Fatalf("the worktree is not inside the Git directory: %+v", worktree)
	}
	if again, err := operations.CreateAgentWorktree(testContext(t), client, agent, nil, nil); err != nil || again != worktree {
		t.Fatalf("a repeated creation did not answer the same worktree: %+v, %v", again, err)
	}
	if got := shell(t, client, "cd "+worktree.Root+" && cat theirs.txt untracked.txt && ls build.log 2>/dev/null; true"); got != "snapshot\ndirty change\nuntracked\n" {
		t.Fatalf("the snapshot does not hold exactly the uncommitted and untracked files: %q", got)
	}
	if shell(t, client, "cd /workspace && git rev-parse HEAD") != head || shell(t, client, "cd /workspace && git status --porcelain") != status {
		t.Fatal("the snapshot changed the workspace's HEAD, index or files")
	}

	// The subagent changes its worktree; the main agent changes the workspace.
	shell(t, client, `set -e; cd `+worktree.Root+`
sed -i 's/^one$/ONE/' clean.txt; printf 'agent\n' > both.txt; printf 'agent theirs\n' >> theirs.txt
rm deleted.txt; printf 'binary\0agent' > image.bin; printf 'agent new\n' > new.txt; printf 'agent only\n' > agent.txt`)
	finished, err := operations.FinishAgentWorktree(testContext(t), client, agent)
	if err != nil || finished.Missing || finished.Base != worktree.Base || finished.Files != 7 {
		t.Fatalf("finish: %+v, %v", finished, err)
	}
	if again, err := operations.FinishAgentWorktree(testContext(t), client, agent); err != nil || again != finished {
		t.Fatalf("a repeated finish did not answer the saved result: %+v, %v", again, err)
	}
	if got := shell(t, client, "test -e "+worktree.Root+" && echo present; cd /workspace && git worktree list --porcelain | grep -c '^worktree '"); got != "1\n" {
		t.Fatalf("the finished checkout was not removed: %q", got)
	}
	shell(t, client, `set -e; cd /workspace
sed -i 's/^seven$/SEVEN/' clean.txt; printf 'workspace\n' > both.txt; printf 'workspace change\n' >> deleted.txt
printf 'binary\0workspace' > image.bin; printf 'workspace new\n' > new.txt`)

	receipt, err := operations.MergeAgentChanges(testContext(t), client, agent, MergeRequest{})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	slices.Sort(receipt.Applied)
	if receipt.Outcome != MergeMergedWithConflicts || !slices.Equal(receipt.Applied, []string{"agent.txt", "clean.txt", "theirs.txt"}) || len(receipt.Unchanged) != 0 {
		t.Fatalf("merge receipt: %+v", receipt)
	}
	conflicts := map[string]mergeConflictKind{}
	for _, conflict := range receipt.Conflicts {
		conflicts[conflict.Path] = conflict.Kind
	}
	want := map[string]mergeConflictKind{"both.txt": conflictContent, "deleted.txt": conflictDeleteModify, "image.bin": conflictBinary, "new.txt": conflictAddAdd}
	if len(conflicts) != len(want) {
		t.Fatalf("conflicts: %+v", receipt.Conflicts)
	}
	for path, kind := range want {
		if conflicts[path] != kind {
			t.Fatalf("%s: want conflict %s, got %+v", path, kind, receipt.Conflicts)
		}
	}
	merged := shell(t, client, "cd /workspace && cat clean.txt theirs.txt agent.txt new.txt deleted.txt && od -c image.bin | head -1")
	if !strings.Contains(merged, "ONE\ntwo") || !strings.Contains(merged, "SEVEN\n") || !strings.Contains(merged, "dirty change\nagent theirs\n") ||
		!strings.Contains(merged, "agent only\nworkspace new\nkeep\nworkspace change\n") || !strings.Contains(merged, "w   o   r   k") {
		t.Fatalf("the merged workspace is wrong:\n%s", merged)
	}
	if markers := shell(t, client, "cd /workspace && cat both.txt"); !strings.Contains(markers, "<<<<<<< workspace\nworkspace\n||||||| snapshot\nalpha\n=======\nagent\n>>>>>>> agent-"+agent) {
		t.Fatalf("the content conflict holds no diff3 markers:\n%s", markers)
	}
	shell(t, client, "cd /workspace && printf 'resolved\n' > both.txt")
	if again, err := operations.MergeAgentChanges(testContext(t), client, agent, MergeRequest{Discard: true}); err != nil || again.Outcome != MergeMergedWithConflicts {
		t.Fatalf("a repeated call did not answer the recorded receipt: %+v, %v", again, err)
	}
	if shell(t, client, "cd /workspace && cat both.txt") != "resolved\n" {
		t.Fatal("a repeated merge wrote a file again")
	}

	// A merge resumed between its plan and its receipt writes each file once.
	const resumed = "m2n4p6q8"
	resumedTree, err := operations.CreateAgentWorktree(testContext(t), client, resumed, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	shell(t, client, "set -e; cd "+resumedTree.Root+"; printf 'first\n' > first.txt; printf 'second\n' > second.txt")
	if _, err = operations.FinishAgentWorktree(testContext(t), client, resumed); err != nil {
		t.Fatal(err)
	}
	first, err := operations.MergeAgentChanges(testContext(t), client, resumed, MergeRequest{})
	if err != nil || first.Outcome != MergeMerged || len(first.Applied) != 2 {
		t.Fatalf("merge: %+v, %v", first, err)
	}
	shell(t, client, "set -e; cd /workspace; rm .git/superverse/agents/"+resumed+"/receipt-apply.json second.txt")
	again, err := operations.MergeAgentChanges(testContext(t), client, resumed, MergeRequest{})
	if err != nil || again.Outcome != MergeMerged || len(again.Applied) != 2 || shell(t, client, "cd /workspace && cat first.txt second.txt") != "first\nsecond\n" {
		t.Fatalf("the resumed merge: %+v, %v", again, err)
	}

	// A merge of selected paths, files or directories, merges only the changed
	// files they name, lists a path that names none as unknown and leaves the
	// others mergeable; the same selection answers its receipt, and a merge of
	// every file then merges the rest and closes the changes.
	const subset = "s2t4v6x8"
	subsetTree, err := operations.CreateAgentWorktree(testContext(t), client, subset, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	shell(t, client, "set -e; cd "+subsetTree.Root+"; mkdir -p docs/deep; printf 'top\n' > top.txt; printf 'doc\n' > docs/a.md; printf 'deep\n' > docs/deep/b.md")
	if _, err = operations.FinishAgentWorktree(testContext(t), client, subset); err != nil {
		t.Fatal(err)
	}
	part, err := operations.MergeAgentChanges(testContext(t), client, subset, MergeRequest{Paths: []string{"docs", "typo.txt"}})
	if err != nil || part.Outcome != MergeMerged || !slices.Equal(part.Applied, []string{"docs/a.md", "docs/deep/b.md"}) || !slices.Equal(part.Unknown, []string{"typo.txt"}) ||
		len(part.Unchanged) != 0 || shell(t, client, "ls /workspace/top.txt 2>/dev/null; true") != "" {
		t.Fatalf("the merge of a directory and a path that names no change: %+v, %v", part, err)
	}
	if same, err := operations.MergeAgentChanges(testContext(t), client, subset, MergeRequest{Paths: []string{"typo.txt", "docs/"}}); err != nil ||
		same.Outcome != MergeMerged || !slices.Equal(same.Applied, part.Applied) || !slices.Equal(same.Unknown, part.Unknown) {
		t.Fatalf("the same selection did not answer its receipt: %+v, %v", same, err)
	}
	rest, err := operations.MergeAgentChanges(testContext(t), client, subset, MergeRequest{})
	if err != nil || rest.Outcome != MergeMerged || !slices.Equal(rest.Applied, []string{"top.txt"}) || !slices.Equal(rest.Unchanged, []string{"docs/a.md", "docs/deep/b.md"}) ||
		len(rest.Unknown) != 0 || shell(t, client, "cat /workspace/top.txt") != "top\n" {
		t.Fatalf("the merge of every file after a selection: %+v, %v", rest, err)
	}
	if closed, err := operations.MergeAgentChanges(testContext(t), client, subset, MergeRequest{Paths: []string{"top.txt"}}); err != nil ||
		closed.Outcome != MergeMerged || !slices.Equal(closed.Applied, rest.Applied) {
		t.Fatalf("a merge after the merge of every file did not answer its receipt: %+v, %v", closed, err)
	}

	// discard leaves the workspace as it is.
	const discarded, unsaved, lost, running, orphan = "d3f5h7j9", "u6w8y2a4", "w3x5z7b9", "r4t6v8x2", "o5p7q9s3"
	discardedTree, err := operations.CreateAgentWorktree(testContext(t), client, discarded, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	shell(t, client, "cd "+discardedTree.Root+" && printf 'dropped\n' > dropped.txt")
	if _, err = operations.FinishAgentWorktree(testContext(t), client, discarded); err != nil {
		t.Fatal(err)
	}
	if receipt, err := operations.MergeAgentChanges(testContext(t), client, discarded, MergeRequest{Discard: true}); err != nil || receipt.Outcome != MergeDiscarded ||
		shell(t, client, "ls /workspace/dropped.txt 2>/dev/null; true") != "" {
		t.Fatalf("discard: %+v, %v", receipt, err)
	}
	// A merge first saves the worktree of a subagent whose end could not save
	// it, and a subagent whose worktree was lost before it was saved has
	// nothing to merge.
	unsavedTree, err := operations.CreateAgentWorktree(testContext(t), client, unsaved, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	shell(t, client, "cd "+unsavedTree.Root+" && printf 'kept\n' > kept.txt")
	receipt, err = operations.MergeAgentChanges(testContext(t), client, unsaved, MergeRequest{})
	if err != nil || receipt.Outcome != MergeMerged || !slices.Equal(receipt.Applied, []string{"kept.txt"}) || shell(t, client, "cd /workspace && cat kept.txt && test ! -e "+
		unsavedTree.Root+" && git show refs/superverse/agents/"+unsaved+"/result:kept.txt") != "kept\nkept\n" {
		t.Fatalf("the merge did not save and merge an unsaved worktree: %+v, %v", receipt, err)
	}
	lostTree, err := operations.CreateAgentWorktree(testContext(t), client, lost, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	shell(t, client, "cd /workspace && git worktree remove --force "+lostTree.Root)
	if receipt, err = operations.MergeAgentChanges(testContext(t), client, lost, MergeRequest{}); err != nil || receipt.Outcome != MergeNothing {
		t.Fatalf("the merge of a lost worktree: %+v, %v", receipt, err)
	}
	orphanTree, err := operations.CreateAgentWorktree(testContext(t), client, orphan, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	shell(t, client, "cd "+orphanTree.Root+" && printf 'unsaved\n' > unsaved.txt")
	// The next creation saves and removes the worktree of a subagent that no longer runs.
	if _, err = operations.CreateAgentWorktree(testContext(t), client, running, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := shell(t, client, "cd /workspace && test ! -e "+orphanTree.Root+" && git show refs/superverse/agents/"+orphan+"/result:unsaved.txt"); got != "unsaved\n" {
		t.Fatalf("the orphan worktree was not saved and removed: %q", got)
	}

	shell(t, client, "head -c 110000000 /dev/zero > /workspace/large.bin")
	if _, err = operations.CreateAgentWorktree(testContext(t), client, "l8m2n4p6", []string{running}, nil); !hasCode(err, toolfailure.SnapshotTooLarge) {
		t.Fatalf("a snapshot over 100 MiB was not refused: %v", err)
	}
	if got := shell(t, client, "test -d "+AgentWorktreeRoot(running)+" && echo kept"); got != "kept\n" {
		t.Fatal("the worktree of a running subagent was not kept")
	}
	if err := operations.CleanupAgentWorktree(testContext(t), client, running, false); err == nil || !strings.Contains(err.Error(), "cannot clean up a registered worktree") {
		t.Fatalf("ordinary cleanup removed or accepted a registered checkout: %v", err)
	}
	if got := shell(t, client, "cd /workspace && test -d "+AgentWorktreeRoot(running)+" && git show-ref --quiet --verify refs/superverse/agents/"+running+"/base && echo kept"); got != "kept\n" {
		t.Fatalf("ordinary cleanup changed the running checkout or ref: %q", got)
	}
	if err := operations.CleanupAgentWorktree(testContext(t), client, running, true); err != nil {
		t.Fatalf("explicit expiry cleanup of a registered checkout: %v", err)
	}
	if got := shell(t, client, "cd /workspace && test ! -e "+AgentWorktreeRoot(running)+" && test ! -e .git/superverse/agents/"+running+" && ! git show-ref --quiet --verify refs/superverse/agents/"+running+"/base && echo cleaned"); got != "cleaned\n" {
		t.Fatalf("explicit expiry cleanup retained the checkout or ref: %q", got)
	}
	if err := operations.CleanupAgentWorktree(testContext(t), client, running, true); err != nil {
		t.Fatalf("repeated expiry cleanup with no checkout, refs or directory: %v", err)
	}
}

func hasCode(err error, code toolfailure.Code) bool {
	var failure *sandboxd.ToolFailure
	return errors.As(err, &failure) && failure.Code() == code
}

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// shell runs a script in the container's workspace and returns its standard
// output; a failure fails the test.
func shell(t *testing.T, client *sandboxd.SandboxClient, script string) string {
	t.Helper()
	output, err := client.RunToExit(testContext(t), sandboxd.StartRequest{ExecutionID: sandboxd.PlatformExecutionID("worktree-test"), Arguments: []string{"bash", "-c", script},
		Environment: sandboxd.GitProcessEnvironment(), Timeout: time.Minute})
	if err != nil || output.ExitCode != 0 {
		t.Fatalf("%q: %v, exit %d: %s", script, err, output.ExitCode, output.Stderr)
	}
	return output.Stdout
}

// TestAgentWorktreeInputsActualSandbox drives start_agent's inputs through the
// real sandboxd of a coding image container: an ignored input directory of the
// workspace appears in the subagent's worktree at the same path, its files
// hard links of the workspace's read-only ones; the subagent's result never
// holds it, even after the worktree stops ignoring it; and an input that does
// not exist refuses the creation before any snapshot.
func TestAgentWorktreeInputsActualSandbox(t *testing.T) {
	container := sandboxdtest.StartCodingContainerForTest(t)
	client := container.Client()
	operations := &GitOperations{}
	const input = ".local/superverse-evolve/r-1"
	shell(t, client, `set -e; cd /workspace; git init -q -b main .
printf '/.local/\n' > .gitignore; printf 'source\n' > app.txt
git add -A && git -c user.name=t -c user.email=t@example.com commit -q -m init
mkdir -p `+input+`/sub; printf '{"flow_id":"a"}\n' > `+input+`/runs.jsonl; printf 'goals\n' > `+input+`/sub/goals.md
chmod 0444 `+input+`/runs.jsonl `+input+`/sub/goals.md`)

	if _, err := operations.CreateAgentWorktree(testContext(t), client, "m2n4p6q8", nil, []string{".local/superverse-evolve/r-9"}); !hasCode(err, toolfailure.SubagentInputMissing) {
		t.Fatalf("a missing input did not refuse the creation: %v", err)
	}
	if refs := shell(t, client, "cd /workspace && git for-each-ref refs/superverse | wc -l"); strings.TrimSpace(refs) != "0" {
		t.Fatalf("the refused creation left a snapshot: %s refs", refs)
	}
	const agent = "p6q8r2s4"
	worktree, err := operations.CreateAgentWorktree(testContext(t), client, agent, nil, []string{input})
	if err != nil {
		t.Fatalf("create the worktree with its input: %v", err)
	}
	inodes := shell(t, client, "stat -c %i /workspace/"+input+"/runs.jsonl "+worktree.Root+"/"+input+"/runs.jsonl /workspace/"+input+"/sub/goals.md "+worktree.Root+"/"+input+"/sub/goals.md")
	if fields := strings.Fields(inodes); len(fields) != 4 || fields[0] != fields[1] || fields[2] != fields[3] {
		t.Fatalf("the input files are not hard links of the workspace's: %q", inodes)
	}
	if mode := shell(t, client, "stat -c %a "+worktree.Root+"/"+input+"/runs.jsonl"); mode != "444\n" {
		t.Fatalf("the linked input is not read-only: %q", mode)
	}
	shell(t, client, `set -e; cd `+worktree.Root+`; printf '' > .gitignore; printf 'change\n' >> app.txt`)
	finished, err := operations.FinishAgentWorktree(testContext(t), client, agent)
	if err != nil || finished.Missing {
		t.Fatalf("finish: %+v, %v", finished, err)
	}
	if tree := shell(t, client, "cd /workspace && git ls-tree -r --name-only "+finished.Result); strings.Contains(tree, ".local") || !strings.Contains(tree, "app.txt") {
		t.Fatalf("the result holds the input or lost the change:\n%s", tree)
	}
	if kept := shell(t, client, "cat /workspace/"+input+"/runs.jsonl"); kept != "{\"flow_id\":\"a\"}\n" {
		t.Fatalf("finishing the subagent changed the workspace's input: %q", kept)
	}
}
