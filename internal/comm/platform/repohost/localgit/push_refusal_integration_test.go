// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package localgit

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha1" //nolint:gosec // the pack checksum is SHA-1
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
)

// These tests push what the host must refuse, with the real git command or, for
// what the real git command never sends, as model-written code in a Sandbox
// can: every request to the host is untrusted. The pushes go over HTTP to a
// host in the test process, and the real git command then checks that the
// repository is unharmed.

// pushBody frames a receive-pack request the way a client other than Git can:
// each command is "<old> <new> <reference>", and the pack follows them.
func pushBody(t *testing.T, commands []string, pack []byte) []byte {
	t.Helper()
	var body bytes.Buffer
	encoder := pktline.NewEncoder(&body)
	for i, command := range commands {
		if i == 0 {
			command += "\x00report-status"
		}
		if err := encoder.EncodeString(command + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := encoder.Flush(); err != nil {
		t.Fatal(err)
	}
	body.Write(pack)
	return body.Bytes()
}

// sendPush posts a receive-pack request body and returns the host's status and
// answer.
func sendPush(t *testing.T, url, token string, body []byte) (int, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url+"/git-receive-pack", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.SetBasicAuth(gitUsername, token)
	request.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	answer, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, answer
}

// rawPush sends commands and a pack and returns the host's report.
func rawPush(t *testing.T, url, token string, commands []string, pack []byte) *packp.ReportStatus {
	t.Helper()
	status, answer := sendPush(t, url, token, pushBody(t, commands, pack))
	if status != http.StatusOK {
		t.Fatalf("the push must be answered with a report, got status %d: %s", status, answer)
	}
	report := packp.NewReportStatus()
	if err := report.Decode(bytes.NewReader(answer)); err != nil {
		t.Fatal(err)
	}
	return report
}

// packOf frames entries, each an object header followed by its compressed
// content, as a version 2 pack that declares count objects, with the checksum
// Git verifies.
func packOf(count uint32, entries ...[]byte) []byte {
	framed := []byte("PACK")
	framed = binary.BigEndian.AppendUint32(framed, 2)
	framed = binary.BigEndian.AppendUint32(framed, count)
	for _, entry := range entries {
		framed = append(framed, entry...)
	}
	sum := sha1.Sum(framed) //nolint:gosec // the pack checksum is SHA-1
	return append(framed, sum[:]...)
}

// pack frames entries as a pack that declares exactly them.
func pack(entries ...[]byte) []byte {
	return packOf(uint32(len(entries)), entries...) //nolint:gosec // a test pack holds a few entries
}

// packEntry is one object of a pack: its type and inflated size in Git's
// variable-length header, then base (a REF_DELTA's base object, else nothing),
// then the compressed content.
func packEntry(t *testing.T, objectType plumbing.ObjectType, base []byte, content []byte) []byte {
	t.Helper()
	size := len(content)
	entry := []byte{byte(objectType)<<4 | byte(size&0x0f)}
	for size >>= 4; size > 0; size >>= 7 {
		entry[len(entry)-1] |= 0x80
		entry = append(entry, byte(size&0x7f))
	}
	entry = append(entry, base...)
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return append(entry, compressed.Bytes()...)
}

// copyDelta is the data of a valid delta that builds a target of targetSize
// bytes by copying a base of baseSize bytes again and again: a few hundred
// kilobytes that a parser without limits resolves into targetSize bytes.
func copyDelta(baseSize, targetSize int) []byte {
	data := binary.AppendUvarint(nil, uint64(baseSize))
	data = binary.AppendUvarint(data, uint64(targetSize))
	for remaining := targetSize; remaining > 0; {
		n := min(remaining, baseSize)
		// Copy n bytes from offset 0: no offset bytes, three size bytes.
		data = append(data, 0x80|0x10|0x20|0x40, byte(n), byte(n>>8), byte(n>>16))
		remaining -= n
	}
	return data
}

// packObjects makes, with the real git command, a pack of exactly the listed
// objects of the repository in dir: one object name per line, as
// `git rev-list --objects` prints them.
func packObjects(t *testing.T, dir string, objects string) []byte {
	t.Helper()
	command := exec.CommandContext(context.Background(), "git", "pack-objects", "--stdout")
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	command.Stdin = strings.NewReader(objects + "\n")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("git pack-objects: %v\n%s", err, stderr.String())
	}
	return stdout.Bytes()
}

// allocatedDuring is how many bytes the test process allocated while action
// ran. The host serves from this process, so it bounds the memory a request
// took, and so the peak of its heap.
func allocatedDuring(action func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	action()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// commandStatuses are the reported statuses by reference.
func commandStatuses(report *packp.ReportStatus) map[string]string {
	statuses := map[string]string{}
	for _, status := range report.CommandStatuses {
		statuses[status.ReferenceName.String()] = status.Status
	}
	return statuses
}

// TestLocalGitHostMalformedPackIntegration pushes a pack that cannot be read: the
// pusher hears a fixed reason, never the parser's error, and nothing changes.
func TestLocalGitHostMalformedPackIntegration(t *testing.T) {
	host, base := startHost(t, t.TempDir())
	repository, template := managedRepository(t, host, "ab12", "setup-1")
	token := credential(t, host, repository)

	update := template + " " + strings.Repeat("1", 40) + " refs/heads/main"
	report := rawPush(t, repository.CloneURL, token, []string{update}, pack([]byte("not a compressed object")))
	if report.UnpackStatus != string(statusUnpackFailed) {
		t.Fatalf("a malformed pack must be refused with the fixed reason, got %q", report.UnpackStatus)
	}
	if status := commandStatuses(report)["refs/heads/main"]; status != string(statusUnpackerError) {
		t.Fatalf("the update of a refused pack must report %q, got %q", statusUnpackerError, status)
	}
	if head, err := host.branchHead(repository.FullName, "main"); err != nil || head != template {
		t.Fatalf("a refused push must not move the branch: %v %s", err, head)
	}
	newGitClient(t, base, token).must("", "ls-remote", repository.CloneURL)
}

// TestLocalGitHostReferenceNamesIntegration pushes reference names that Git does
// not allow: each update is refused on its own, a valid one beside them is
// applied, and the repository keeps working.
func TestLocalGitHostReferenceNamesIntegration(t *testing.T) {
	host, base := startHost(t, t.TempDir())
	repository, template := managedRepository(t, host, "ab12", "setup-1")
	token := credential(t, host, repository)

	zero := strings.Repeat("0", 40)
	invalid := []string{"refs/heads/a..b", "refs/heads/x.lock", "refs/heads/-dash", "refs/tags/a~b", "refs/heads/.hidden", "refs/heads/../../config"}
	commands := []string{zero + " " + template + " refs/heads/valid"}
	for _, name := range invalid {
		commands = append(commands, zero+" "+template+" "+name)
	}
	report := rawPush(t, repository.CloneURL, token, commands, pack())
	statuses := commandStatuses(report)
	if report.UnpackStatus != string(statusOK) || statuses["refs/heads/valid"] != string(statusOK) {
		t.Fatalf("the valid reference must be created: %q %v", report.UnpackStatus, statuses)
	}
	for _, name := range invalid {
		if statuses[name] != string(statusInvalidName) {
			t.Fatalf("%s must be refused as %q, got %q", name, statusInvalidName, statuses[name])
		}
	}

	// Git itself sends a branch that starts with a dash; the host refuses it.
	client := newGitClient(t, base, token)
	cloneDir := filepath.Join(t.TempDir(), "clone")
	client.must("", "clone", repository.CloneURL, cloneDir)
	if output, err := client.run(cloneDir, "push", "origin", "HEAD:refs/heads/-dash"); err == nil || !strings.Contains(output, string(statusInvalidName)) {
		t.Fatalf("a branch that starts with a dash must be refused: %v\n%s", err, output)
	}
	listing := client.must("", "ls-remote", repository.CloneURL)
	if !strings.Contains(listing, "refs/heads/valid") || strings.Contains(listing, "..") || strings.Contains(listing, ".lock") ||
		strings.Contains(listing, "-dash") || strings.Contains(listing, "hidden") {
		t.Fatalf("only the valid reference may be listed:\n%s", listing)
	}
	if read, err := host.ReadRepository(context.Background(), repository.FullName); err != nil || read.ID != repository.ID {
		t.Fatalf("the repository record must be intact: %v", err)
	}
	client.must("", "clone", repository.CloneURL, filepath.Join(t.TempDir(), "fresh"))
}

// TestLocalGitHostReferenceWritesIntegration checks that the host writes whole
// reference files and leaves no staging file among them, and that a reference
// file which an in-place write left empty at a crash is corruption: it is never
// read as a missing reference, so neither a push nor the template
// initialization can create the branch again past the fast-forward check.
func TestLocalGitHostReferenceWritesIntegration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	host, base := startHost(t, dir)
	repository, template := managedRepository(t, host, "ab12", "setup-1")
	token := credential(t, host, repository)
	client := newGitClient(t, base, token)
	cloneDir := filepath.Join(t.TempDir(), "clone")
	client.must("", "clone", repository.CloneURL, cloneDir)
	pushed := client.commit(cloneDir, "first.txt", "first\n", "First commit")
	client.must(cloneDir, "push", "origin", "main", "HEAD:refs/heads/feature/nested")

	repositoryDir := filepath.Join(dir, repositoryOwner, "localkind-my-app-ab12"+repositorySuffix)
	var references []string
	err := filepath.WalkDir(filepath.Join(repositoryDir, "refs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		relative, _ := filepath.Rel(repositoryDir, path)
		references = append(references, filepath.ToSlash(relative)+" "+strings.TrimSpace(string(data)))
		return readErr
	})
	want := []string{"refs/heads/feature/nested " + pushed, "refs/heads/main " + pushed}
	if err != nil || !slices.Equal(references, want) {
		t.Fatalf("the references must be whole files and nothing else: %v %q", err, references)
	}
	if staged, _ := filepath.Glob(filepath.Join(repositoryDir, ".write-*")); len(staged) != 0 {
		t.Fatalf("no staging file may stay: %v", staged)
	}

	// An in-place write that a crash cut off leaves the branch's file empty.
	mainFile := filepath.Join(repositoryDir, "refs", "heads", "main")
	if err = os.WriteFile(mainFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	create := strings.Repeat("0", 40) + " " + template + " refs/heads/main"
	if status, answer := sendPush(t, repository.CloneURL, token, pushBody(t, []string{create}, pack())); status != http.StatusInternalServerError {
		t.Fatalf("a push to a repository with a corrupt reference must fail, got %d: %s", status, answer)
	}
	if output, pushErr := client.run(cloneDir, "push", "origin", "HEAD:refs/heads/main"); pushErr == nil {
		t.Fatalf("git must not push to a repository with a corrupt reference:\n%s", output)
	}
	if data, readErr := os.ReadFile(mainFile); readErr != nil || len(data) != 0 {
		t.Fatalf("a refused push must not write the reference: %v %q", readErr, data)
	}
	if _, err = host.branchHead(repository.FullName, "main"); err == nil || errors.Is(err, repohost.ErrNotFound) {
		t.Fatalf("a corrupt reference must be an error, not a missing branch: %v", err)
	}
	if _, err = host.InitializeTemplate(ctx, repository, apptemplate.Current().Revision, apptemplate.Current()); err == nil {
		t.Fatal("the template initialization must not write over a corrupt reference")
	}
}

// TestLocalGitHostIncompleteHistoryIntegration pushes histories that reach
// objects the repository does not have, in packs the real git command makes
// but would never send: a commit whose tree is left out, a commit whose parent
// is left out and an annotated tag whose target is left out. Each update is
// refused, the repository keeps working, and the complete histories are
// accepted later.
func TestLocalGitHostIncompleteHistoryIntegration(t *testing.T) {
	host, base := startHost(t, t.TempDir())
	repository, template := managedRepository(t, host, "ab12", "setup-1")
	token := credential(t, host, repository)
	client := newGitClient(t, base, token)
	cloneDir := filepath.Join(t.TempDir(), "clone")
	client.must("", "clone", repository.CloneURL, cloneDir)
	zero := strings.Repeat("0", 40)

	parent := client.commit(cloneDir, "parent.txt", "a file only the parent adds\n", "Commit whose tree is left out")
	report := rawPush(t, repository.CloneURL, token, []string{template + " " + parent + " refs/heads/main"}, packObjects(t, cloneDir, parent))
	if status := commandStatuses(report)["refs/heads/main"]; report.UnpackStatus != string(statusOK) || status != string(statusMissingObjects) {
		t.Fatalf("a commit without its tree must be refused: %q %q", report.UnpackStatus, status)
	}

	child := client.commit(cloneDir, "child.txt", "a file the child adds\n", "Commit whose parent is left out")
	childObjects := client.must(cloneDir, "rev-list", "--objects", parent+".."+child)
	report = rawPush(t, repository.CloneURL, token, []string{zero + " " + child + " refs/heads/side"}, packObjects(t, cloneDir, childObjects))
	if status := commandStatuses(report)["refs/heads/side"]; status != string(statusMissingObjects) {
		t.Fatalf("a new branch at a commit without its parent must be refused: %q", status)
	}

	client.must(cloneDir, "tag", "-a", "-m", "Tag whose target is left out", "v-incomplete", child)
	tag := client.must(cloneDir, "rev-parse", "v-incomplete")
	report = rawPush(t, repository.CloneURL, token, []string{zero + " " + tag + " refs/tags/v-incomplete"}, packObjects(t, cloneDir, tag))
	if status := commandStatuses(report)["refs/tags/v-incomplete"]; status != string(statusMissingObjects) {
		t.Fatalf("a tag without its target must be refused: %q", status)
	}

	// Nothing points to the incomplete histories, and a fresh clone is intact.
	if head, err := host.branchHead(repository.FullName, "main"); err != nil || head != template {
		t.Fatalf("a refused update must not move the branch: %v %s", err, head)
	}
	if listing := client.must("", "ls-remote", repository.CloneURL); strings.Contains(listing, "side") || strings.Contains(listing, "v-incomplete") {
		t.Fatalf("a refused update must not create a reference:\n%s", listing)
	}
	freshDir := filepath.Join(t.TempDir(), "fresh")
	client.must("", "clone", repository.CloneURL, freshDir)
	client.must(freshDir, "fsck", "--strict", "--full")

	// Git sends the complete histories, which the objects of the refused pushes do
	// not disturb.
	client.must(cloneDir, "push", "origin", "main", "v-incomplete")
	verifyDir := filepath.Join(t.TempDir(), "verify")
	client.must("", "clone", repository.CloneURL, verifyDir)
	client.must(verifyDir, "fsck", "--strict", "--full")
	if head := client.must(verifyDir, "rev-parse", "HEAD"); head != child {
		t.Fatalf("the complete history must be accepted: HEAD is %s, not %s", head, child)
	}
	if tagged := client.must(verifyDir, "rev-parse", "v-incomplete^{commit}"); tagged != child {
		t.Fatalf("the complete tag must be accepted: it points to %s, not %s", tagged, child)
	}
}

// TestLocalGitHostPushLimitsIntegration pushes objects that inflate far beyond
// their compressed size: the host refuses them from their headers, before it
// allocates their content, and the repository keeps working. Reads that have a
// limit check an object's size before they load it.
func TestLocalGitHostPushLimitsIntegration(t *testing.T) {
	ctx := context.Background()
	host, base := startHost(t, t.TempDir())
	repository, template := managedRepository(t, host, "ab12", "setup-1")
	token := credential(t, host, repository)
	client := newGitClient(t, base, token)
	cloneDir := filepath.Join(t.TempDir(), "clone")
	client.must("", "clone", repository.CloneURL, cloneDir)

	// 256 MiB of zeros compress to a few hundred kilobytes.
	zeros := filepath.Join(cloneDir, "zeros.bin")
	if err := os.WriteFile(zeros, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(zeros, 256<<20); err != nil {
		t.Fatal(err)
	}
	client.must(cloneDir, "add", "zeros.bin")
	client.must(cloneDir, "commit", "-m", "Add 256 MiB of zeros")
	var output string
	var pushErr error
	allocated := allocatedDuring(func() { output, pushErr = client.run(cloneDir, "push", "origin", "main") })
	t.Logf("the push of a 256 MiB object allocated %d KiB", allocated>>10)
	if pushErr == nil || !strings.Contains(output, string(statusObjectTooLarge)) {
		t.Fatalf("an object over the limit must be refused: %v\n%s", pushErr, output)
	}
	if allocated > 64<<20 {
		t.Fatalf("refusing the push must not allocate its object, the host allocated %d MiB", allocated>>20)
	}

	// A delta of a few hundred kilobytes against a blob the repository has, whose
	// target would be 200 MiB.
	baseFile := apptemplate.Current().Files[0]
	for _, file := range apptemplate.Current().Files {
		if len(file.Contents) > len(baseFile.Contents) {
			baseFile = file
		}
	}
	baseHash := plumbing.NewHash(baseFile.BlobID)
	tag := strings.Repeat("0", 40) + " " + strings.Repeat("2", 40) + " refs/tags/large"
	var report *packp.ReportStatus
	allocated = allocatedDuring(func() {
		delta := packEntry(t, plumbing.REFDeltaObject, baseHash[:], copyDelta(len(baseFile.Contents), 200<<20))
		report = rawPush(t, repository.CloneURL, token, []string{tag}, pack(delta))
	})
	if report.UnpackStatus != string(statusObjectTooLarge) || commandStatuses(report)["refs/tags/large"] != string(statusUnpackerError) {
		t.Fatalf("a delta whose target is over the limit must be refused: %q %v", report.UnpackStatus, commandStatuses(report))
	}
	t.Logf("refusing a delta with a 200 MiB target allocated %d KiB, building the pack included", allocated>>10)
	if allocated > 64<<20 {
		t.Fatalf("refusing the delta must not resolve it, the host allocated %d MiB", allocated>>20)
	}

	// Eleven objects of 100 MiB each are within the limit of an object but not
	// within the limit of a push.
	entries := make([][]byte, 11)
	for i := range entries {
		entries[i] = packEntry(t, plumbing.REFDeltaObject, baseHash[:], copyDelta(len(baseFile.Contents), maximumObjectBytes))
	}
	if report = rawPush(t, repository.CloneURL, token, []string{tag}, pack(entries...)); report.UnpackStatus != string(statusPushTooLarge) {
		t.Fatalf("objects over the limit of a push together must be refused: %q", report.UnpackStatus)
	}
	if report = rawPush(t, repository.CloneURL, token, []string{tag}, packOf(maximumPushObjects+1)); report.UnpackStatus != string(statusTooManyObjects) {
		t.Fatalf("a pack of too many objects must be refused: %q", report.UnpackStatus)
	}
	commands := make([]string, 0, 12_000)
	for i := range 12_000 {
		commands = append(commands, strings.Repeat("0", 40)+" "+template+" refs/heads/many-"+strconv.Itoa(i))
	}
	if status, answer := sendPush(t, repository.CloneURL, token, pushBody(t, commands, pack())); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("a push of too many reference updates must be refused, got %d: %s", status, answer)
	}

	// Nothing of the refused pushes is visible, and the repository keeps working.
	if listing := client.must("", "ls-remote", repository.CloneURL); strings.Contains(listing, "large") || strings.Contains(listing, "many-") {
		t.Fatalf("a refused push must not create a reference:\n%s", listing)
	}
	if head, err := host.branchHead(repository.FullName, "main"); err != nil || head != template {
		t.Fatalf("a refused push must not move the branch: %v %s", err, head)
	}
	freshDir := filepath.Join(t.TempDir(), "fresh")
	client.must("", "clone", repository.CloneURL, freshDir)
	client.must(freshDir, "fsck", "--strict", "--full")

	// A file within the limit of a push but over the limit of a read is refused
	// from its size, without being loaded.
	client.must(cloneDir, "reset", "--hard", "origin/main")
	large := filepath.Join(cloneDir, "large.bin")
	if err := os.WriteFile(large, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(large, 3*maximumFileBytes); err != nil {
		t.Fatal(err)
	}
	client.must(cloneDir, "add", "large.bin")
	client.must(cloneDir, "commit", "-m", "Add a file over the read limit")
	client.must(cloneDir, "push", "origin", "main")
	commit := client.must(cloneDir, "rev-parse", "HEAD")
	var readErr error
	allocated = allocatedDuring(func() { _, readErr = host.ReadFile(ctx, repository.FullName, commit, "large.bin") })
	if readErr == nil || !strings.Contains(readErr.Error(), "larger than") {
		t.Fatalf("a file over the read limit must be refused: %v", readErr)
	}
	t.Logf("refusing to read a %d MiB file allocated %d KiB", 3*maximumFileBytes>>20, allocated>>10)
	if allocated > maximumFileBytes {
		t.Fatalf("refusing the read must not load the file, the host allocated %d MiB", allocated>>20)
	}
}
