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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	buildlocal "github.com/superdurable/superversev2/internal/dexai/delivery/releases/build/local"
)

// TestLiveSDKFloorLocalBuilderIntegration drives the actual Git service,
// source builder, Docker and isolated Go toolchain; no application dependency is replaced.
func TestLiveSDKFloorLocalBuilderIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	tool := os.Getenv("SUPERVERSE_TEST_BUILD_TOOL_IMAGE")
	if !strings.HasPrefix(tool, "sha256:") && !strings.Contains(tool, "@sha256:") {
		t.Fatal("SUPERVERSE_TEST_BUILD_TOOL_IMAGE must name an immutable local coding image")
	}
	command := func(directory, name string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = directory
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s failed: %v", name, err)
		}
		return strings.TrimSpace(string(output))
	}
	root := command("", "git", "rev-parse", "--show-toplevel")
	scratch := t.TempDir()
	manifestTool := filepath.Join(scratch, "app-manifest")
	command(root, "go", "build", "-o", manifestTool, "./cmd/tools/app-manifest")
	var lock struct {
		Dependencies struct {
			Registry struct{ Repository, Index string }
		} `json:"localDependencies"`
	}
	data, err := os.ReadFile(filepath.Join(root, "infra-as-code/source-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatal(err)
	}
	registryName := "sv2-sdk-floor-" + flowid.RandomID(8)
	command("", "docker", "run", "--detach", "--name", registryName, "--label", "superdurable.io/sdk-floor-test=true",
		"--publish", "127.0.0.1::5000", lock.Dependencies.Registry.Repository+"@"+lock.Dependencies.Registry.Index)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := exec.CommandContext(cleanup, "docker", "rm", "--force", registryName).Run(); err != nil {
			t.Errorf("remove owned registry: %v", err)
		}
	})
	var ports map[string][]struct{ HostPort string }
	if err := json.Unmarshal([]byte(command("", "docker", "inspect", "--format", "{{json .NetworkSettings.Ports}}", registryName)), &ports); err != nil {
		t.Fatal(err)
	}
	registry := "localhost:" + ports["5000/tcp"][0].HostPort
	wait := func(label string, done func() bool) {
		t.Helper()
		for !done() {
			select {
			case <-ctx.Done():
				t.Fatalf("%s: %v", label, ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	wait("registry readiness", func() bool {
		response, err := (&http.Client{Timeout: time.Second}).Get("http://" + registry + "/v2/")
		if err != nil {
			return false
		}
		defer func() { _ = response.Body.Close() }()
		return response.StatusCode == http.StatusOK
	})
	host, base := startHost(t, filepath.Join(scratch, "repositories"))
	project := flowid.RandomID(4)
	repository, _ := managedRepository(t, host, project, flowid.RandomID(8))
	repoToken := credential(t, host, repository)
	git := newGitClient(t, base, repoToken)
	clone := filepath.Join(scratch, "source")
	git.must(scratch, "clone", repository.CloneURL, clone)
	original, err := os.ReadFile(filepath.Join(clone, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("sdk-floor-test-", 4)
	tokenFile := filepath.Join(scratch, "builder-token")
	if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	process := exec.Command("python3", filepath.Join(root, "internal/dexai/delivery/releases/build/localbuilder/server.py"),
		"--port", fmt.Sprint(port), "--token-file", tokenFile, "--tool-image", tool, "--app-manifest-tool", manifestTool,
		"--git-base-url", base, "--target", "docker", "--registry", registry, "--state-dir", filepath.Join(scratch, "build-state"))
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "SUPERVERSE_LOCAL_NATIVE=") {
			process.Env = append(process.Env, variable)
		}
	}
	process.Stdout, process.Stderr = os.Stderr, os.Stderr
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = process.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- process.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("builder shutdown: %v", err)
			}
		case <-time.After(30 * time.Second):
			_ = process.Process.Kill()
			<-done
			t.Error("builder did not stop normally")
		}
	})
	builder, err := buildlocal.New(fmt.Sprintf("http://127.0.0.1:%d", port), token)
	if err != nil {
		t.Fatal(err)
	}
	profile := build.NewProfiles().Default()
	for _, scenario := range []struct {
		name, module string
		rejected     bool
	}{
		{"old", strings.Replace(string(original), "sdk-go v1.14.1", "sdk-go v1.12.0", 1), true},
		{"local", string(original) + "\nreplace github.com/superdurable/dex/sdk-go => ./local-dex\n", true},
		{"missing", strings.Replace(string(original), "\tgithub.com/superdurable/dex/sdk-go v1.14.1\n", "", 1), true},
		{"unparseable", strings.Replace(string(original), "sdk-go v1.14.1", "sdk-go not-a-version", 1), true},
		{"replaced-old", string(original) + "\nreplace github.com/superdurable/dex/sdk-go => github.com/superdurable/dex/sdk-go v1.12.0\n", true},
		{"replaced-supported", strings.Replace(string(original), "sdk-go v1.14.1", "sdk-go v1.12.0", 1) + "\nreplace github.com/superdurable/dex/sdk-go => github.com/superdurable/dex/sdk-go v1.14.1\n", false},
		{"valid", string(original), false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.name == "old" {
				if err := os.WriteFile(filepath.Join(clone, "go.mod"), []byte(scenario.module), 0644); err != nil {
					t.Fatal(err)
				}
				command(clone, "go", "mod", "download", "github.com/superdurable/dex/sdk-go@v1.12.0")
			}
			commit := git.commit(clone, "go.mod", scenario.module, "SDK floor "+scenario.name)
			git.must(clone, "push", "origin", "HEAD:main")
			request := build.BuildRequest{ContractVersion: build.ContractVersion, SourceOnly: true, BuildRevisionID: build.SourceRevisionID,
				RequestID: strings.Repeat(flowid.RandomID(32), 2), ProjectID: project, ReleaseID: commit, SourceCommit: commit,
				RepositoryFullName: repository.FullName, Profile: profile.ID, ProfileVersion: profile.Version, ProfileDigest: profile.Digest,
				Environment: deploymentnames.EnvironmentTest, Attempt: 1,
				ImageTag: fmt.Sprintf("sv2-test-%s:%s-%s-source", project, commit, strings.TrimPrefix(profile.Digest, "sha256:")[:16])}
			// RequestID is a SHA-256 hexadecimal fingerprint.
			request.RequestID = flowid.Fingerprint([]string{project, commit})
			wait("builder startup", func() bool { _, err := builder.Inspect(ctx, request); return err == nil })
			if _, err := builder.Start(ctx, request, repoToken); err != nil {
				t.Fatal(err)
			}
			var result build.BuildResult
			wait("source preparation", func() bool {
				var err error
				result, err = builder.Inspect(ctx, request)
				if err != nil {
					t.Fatal(err)
				}
				return result.State == build.BuildFailed || result.State == build.BuildReady
			})
			if scenario.rejected {
				if result.State != build.BuildFailed || result.FailureCategory != build.FailureUnsupportedDexSDK || !strings.Contains(result.Failure, "v1.13.0") {
					t.Fatalf("expected SDK-floor rejection, got state=%s category=%s hint=%q", result.State, result.FailureCategory.Name(), result.Failure)
				}
			} else if result.State != build.BuildReady {
				t.Fatalf("valid SDK source failed: category=%s hint=%q", result.FailureCategory.Name(), result.Failure)
			}
			wait("source cleanup", func() bool {
				err := builder.CleanupRelease(ctx, request)
				if errors.Is(err, build.ErrPending) {
					return false
				}
				if err != nil {
					t.Fatal(err)
				}
				return true
			})
			t.Logf("actual local builder result: %s, category=%s", result.State, result.FailureCategory.Name())
		})
	}
}
