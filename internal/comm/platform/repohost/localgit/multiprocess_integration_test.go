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
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
)

// TestLocalGitProcess is an OS-process fixture running the real Git host and filesystem.
func TestLocalGitProcess(t *testing.T) {
	dir := os.Getenv("SUPERVERSE_LOCALGIT_PROCESS_DIR")
	if dir == "" {
		t.Skip("helper process launched by TestLocalGitMultiProcessIntegration")
	}
	host, base := startHost(t, dir)
	defer func() { _ = host.Close() }()
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(map[string]string{"base": base}); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct{ Action, Project, Name string }
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			t.Fatal(err)
		}
		var response struct {
			Repository         repohost.Repository
			Token, Path, Error string
		}
		var err error
		switch request.Action {
		case "create":
			response.Repository, _ = managedRepository(t, host, request.Project, "setup-1")
			response.Token = credential(t, host, response.Repository)
		case "archive":
			err = host.ArchiveManaged(context.Background(), request.Project, "setup-1", request.Name)
		case "temporary":
			response.Path, err = os.MkdirTemp(dir, host.store.locks.TempPrefix(creatingPrefix))
		default:
			t.Fatal("unknown child operation")
		}
		if err != nil {
			response.Error = err.Error()
		}
		if err := encoder.Encode(response); err != nil {
			t.Fatal(err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

type gitProcess struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  *json.Decoder
	base    string
}

type gitProcessResponse struct {
	Repository         repohost.Repository
	Token, Path, Error string
}

func startGitProcess(t *testing.T, dir string) *gitProcess {
	t.Helper()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestLocalGitProcess$", "-test.timeout=2m")
	command.Env = append(os.Environ(), "SUPERVERSE_LOCALGIT_PROCESS_DIR="+dir)
	command.Stderr = os.Stderr
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	child := &gitProcess{command: command, input: input, output: json.NewDecoder(output)}
	var ready struct{ Base string }
	if err = child.output.Decode(&ready); err != nil {
		t.Fatal(err)
	}
	child.base = ready.Base
	t.Cleanup(func() { _ = input.Close(); _ = command.Process.Kill(); _ = command.Wait() })
	return child
}

func (p *gitProcess) call(t *testing.T, action, project, name string) gitProcessResponse {
	t.Helper()
	if err := json.NewEncoder(p.input).Encode(map[string]string{"Action": action, "Project": project, "Name": name}); err != nil {
		t.Fatal(err)
	}
	var result gitProcessResponse
	if err := p.output.Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	return result
}

func TestLocalGitMultiProcessIntegration(t *testing.T) {
	dir := t.TempDir()
	first := startGitProcess(t, dir)
	live := first.call(t, "temporary", "", "").Path
	second := startGitProcess(t, dir)
	if _, err := os.Stat(live); err != nil {
		t.Fatal("startup removed a live owner's temporary directory", err)
	}
	dead := second.call(t, "temporary", "", "").Path
	if err := second.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := second.command.Wait(); err == nil {
		t.Fatal("crashed child unexpectedly succeeded")
	}
	second = startGitProcess(t, dir)
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Fatal("startup did not remove dead owner's temporary directory", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatal("cleanup touched the live owner", err)
	}
	var a, b gitProcessResponse
	var creations sync.WaitGroup
	creations.Add(2)
	go func() { defer creations.Done(); a = first.call(t, "create", "ab12", "") }()
	go func() { defer creations.Done(); b = second.call(t, "create", "cd34", "") }()
	creations.Wait()
	if a.Repository.ID == b.Repository.ID {
		t.Fatal("concurrent processes reused a repository ID")
	}
	client := newGitClient(t, first.base, a.Token)
	clone := filepath.Join(t.TempDir(), "clone")
	client.must("", "clone", a.Repository.CloneURL, clone)
	old := client.must(clone, "rev-parse", "HEAD")
	one := client.commit(clone, "first.txt", "one", "First candidate")
	client.must(clone, "reset", "--hard", old)
	two := client.commit(clone, "second.txt", "two", "Second candidate")
	packs := [][]byte{packObjects(t, clone, client.must(clone, "rev-list", "--objects", one)), packObjects(t, clone, client.must(clone, "rev-list", "--objects", two))}
	urls := []string{a.Repository.CloneURL, second.base + "/" + a.Repository.FullName + repositorySuffix}
	successes := make(chan bool, 2)
	for i, commit := range []string{one, two} {
		go func(index int, hash string) {
			report := rawPush(t, urls[index], a.Token, []string{fmt.Sprintf("%s %s refs/heads/main", old, hash)}, packs[index])
			successes <- commandStatuses(report)["refs/heads/main"] == "ok"
		}(i, commit)
	}
	n := 0
	for range 2 {
		if <-successes {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("same-old-ref CAS accepted %d concurrent pushes", n)
	}
	client.must(clone, "fetch", "origin")
	winner := client.must(clone, "rev-parse", "origin/main")
	if winner != one && winner != two {
		t.Fatal("winning ref is neither candidate")
	}
	client.must(clone, "reset", "--hard", winner)
	next := client.commit(clone, "third.txt", "three", "Archive race")
	body := pushBody(t, []string{fmt.Sprintf("%s %s refs/heads/main", winner, next)}, packObjects(t, clone, client.must(clone, "rev-list", "--objects", next)))
	// Pause a real network upload after its headers. Archival may finish before
	// the upload, and the eventual push must re-read the record under the lock.
	reader, writer := io.Pipe()
	request, err := newArchiveRaceRequest(urls[0], a.Token, reader)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		response, err := archiveRaceHTTPClient.Do(request)
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		done <- err
	}()
	cut := len(body) / 2
	if _, err = writer.Write(body[:cut]); err != nil {
		t.Fatal(err)
	}
	second.call(t, "archive", "ab12", a.Repository.FullName)
	if _, err = writer.Write(body[cut:]); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("archived upload did not finish")
	}
	client.must(clone, "fetch", "origin")
	if got := client.must(clone, "rev-parse", "origin/main"); got != winner {
		t.Fatal("push changed ref after archival")
	}
	if output, err := client.run(clone, "push", "origin", "main"); err == nil || !strings.Contains(output, "403") {
		t.Fatalf("archived repository accepted push: %v %s", err, output)
	}
}

var archiveRaceHTTPClient = &http.Client{Timeout: 20 * time.Second}

func newArchiveRaceRequest(url, token string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url+"/git-receive-pack", body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-git-receive-pack-request")
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(gitUsername+":"+token)))
	return request, nil
}
