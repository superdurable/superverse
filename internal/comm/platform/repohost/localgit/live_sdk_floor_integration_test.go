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

	"github.com/google/uuid"

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
	var sdkVersion string
	for _, line := range strings.Split(string(original), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "github.com/superdurable/dex/sdk-go" {
			sdkVersion = fields[1]
		}
	}
	if sdkVersion == "" {
		t.Fatal("template has no Dex SDK requirement")
	}
	startBuilder := func(t *testing.T, preparationTimeout string, native, uncertainRemove bool) *buildlocal.Builder {
		t.Helper()
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
		// Inject a bounded delay and shorter deadline; Git, Docker and Go remain real.
		process := exec.Command("python3", "-c", `import importlib.util, sys
spec = importlib.util.spec_from_file_location("localbuilder", sys.argv.pop(1))
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)
builder.DEPENDENCY_PREPARATION_TIMEOUT = float(sys.argv.pop(1))
uncertain_remove = sys.argv.pop(1) == "true"
fault_receipt = builder.Path(sys.argv.pop(1))
if uncertain_remove:
    run = builder.subprocess.run
    injected = False
    def lost_remove(argv, **kwargs):
        global injected
        result = run(argv, **kwargs)
        if argv[:3] == ["docker", "rm", "--force"] and not injected:
            injected = True
            builder.private_json(fault_receipt, {"realRemovalCalled": True})
            raise builder.subprocess.TimeoutExpired(argv, kwargs.get("timeout", 10))
        return result
    builder.subprocess.run = lost_remove
if builder.DEPENDENCY_PREPARATION_TIMEOUT < 600:
    # Delay the real metadata container after it starts, keeping Git/Docker/Go real.
    original = builder.Job.command
    def delayed(self, argv, **kwargs):
        if argv[:2] == ["docker", "run"] and argv[-1] == builder.DEPENDENCY_PREPARATION_SCRIPT:
            argv = list(argv)
            argv[-1] = 'printf "metadata-timeout-started api_token=synthetic-timeout-secret\\n" >&2; sleep 600;\n' + argv[-1]
        return original(self, argv, **kwargs)
    builder.Job.command = delayed
builder.main()`, filepath.Join(root, "internal/dexai/delivery/releases/build/localbuilder/server.py"), preparationTimeout, fmt.Sprint(uncertainRemove), filepath.Join(scratch, "remove-response-lost"),
			"--port", fmt.Sprint(port), "--token-file", tokenFile, "--tool-image", tool, "--app-manifest-tool", manifestTool,
			"--git-base-url", base, "--target", "docker", "--registry", registry, "--state-dir", filepath.Join(scratch, "build-state-"+preparationTimeout))
		for _, variable := range os.Environ() {
			if !strings.HasPrefix(variable, "SUPERVERSE_LOCAL_NATIVE=") {
				process.Env = append(process.Env, variable)
			}
		}
		if native {
			process.Env = append(process.Env, "SUPERVERSE_LOCAL_NATIVE=true", "SUPERVERSE_NATIVE_RUN_ID=metadata-timeout-integration", fmt.Sprintf("SUPERVERSE_NATIVE_PROCESS_GROUP=%d", syscall.Getpgrp()))
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
		return builder
	}
	builder := startBuilder(t, "600", false, false)
	profile := build.NewProfiles().Default()
	for _, scenario := range []struct {
		name, module    string
		rejected        bool
		metadataFailure bool
		timeout         bool
	}{
		{"old", strings.Replace(string(original), "sdk-go "+sdkVersion, "sdk-go v1.12.0", 1), true, false, false},
		{"local", string(original) + "\nreplace github.com/superdurable/dex/sdk-go => ./local-dex\n", true, false, false},
		{"missing", strings.Replace(string(original), "\tgithub.com/superdurable/dex/sdk-go "+sdkVersion+"\n", "", 1), true, false, false},
		{"unparseable", strings.Replace(string(original), "sdk-go "+sdkVersion, "sdk-go not-a-version", 1), false, true, false},
		{"replaced-old", string(original) + "\nreplace github.com/superdurable/dex/sdk-go => github.com/superdurable/dex/sdk-go v1.12.0\n", true, false, false},
		{"replaced-supported", strings.Replace(string(original), "sdk-go "+sdkVersion, "sdk-go v1.12.0", 1) + "\nreplace github.com/superdurable/dex/sdk-go => github.com/superdurable/dex/sdk-go " + sdkVersion + "\n", false, false, false},
		{"metadata-stderr", string(original) + "\n" + strings.Repeat("无效指令\n", 300) + "api_token=synthetic-secret-value\n", false, true, false},
		{"metadata-timeout", string(original), false, true, true},
		{"metadata-late-create", string(original), false, true, true},
		{"metadata-remove-uncertain", string(original), false, true, true},
		{"metadata-native-timeout", string(original), false, true, true},
		{"cancelled", string(original), false, false, false},
		{"valid", string(original), false, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			builder := builder
			if scenario.timeout {
				timeout := "60"
				if scenario.name == "metadata-late-create" {
					timeout = "0.000001"
				}
				builder = startBuilder(t, timeout, scenario.name == "metadata-native-timeout", scenario.name == "metadata-remove-uncertain")
			}
			if scenario.name == "old" {
				if err := os.WriteFile(filepath.Join(clone, "go.mod"), []byte(scenario.module), 0644); err != nil {
					t.Fatal(err)
				}
				command(clone, "go", "mod", "download", "github.com/superdurable/dex/sdk-go@v1.12.0")
			}
			commit := git.commit(clone, "go.mod", scenario.module+"\n// scenario: "+scenario.name+"\n", "SDK floor "+scenario.name)
			git.must(clone, "push", "origin", "HEAD:main")
			request := build.BuildRequest{ContractVersion: build.ContractVersion, SourceOnly: true, BuildRevisionID: build.SourceRevisionID,
				RequestID: strings.Repeat(flowid.RandomID(32), 2), ProjectID: project, ReleaseID: commit, SourceCommit: commit,
				RepositoryFullName: repository.FullName, Profile: profile.ID, ProfileVersion: profile.Version, ProfileDigest: profile.Digest,
				Environment: deploymentnames.EnvironmentTest, Attempt: 1,
				ImageTag: fmt.Sprintf("sv2-test-%s:%s-%s-source", project, commit, strings.TrimPrefix(profile.Digest, "sha256:")[:16])}
			if scenario.timeout {
				request.SourceOnly = false
				request.BuildRevisionID = "build-" + uuid.NewString()
				request.ConfigurationDigest = "sha256:" + flowid.Fingerprint([]string{project, commit})
				request.ConfigurationRevision = 1
				request.ImageTag = strings.TrimSuffix(request.ImageTag, "-source") + "-" + request.BuildRevisionID
			}
			// RequestID is a SHA-256 hexadecimal fingerprint.
			request.RequestID = flowid.Fingerprint([]string{project, commit})
			wait("builder startup", func() bool { _, err := builder.Inspect(ctx, request); return err == nil })
			if _, err := builder.Start(ctx, request, repoToken); err != nil {
				t.Fatal(err)
			}
			if scenario.name == "cancelled" {
				wait("explicit cancellation", func() bool {
					err := builder.Cancel(ctx, request)
					if errors.Is(err, build.ErrPending) {
						return false
					}
					if err != nil {
						t.Fatal(err)
					}
					return true
				})
			}
			var result build.BuildResult
			observedUnknown := false
			wait("source preparation", func() bool {
				var err error
				result, err = builder.Inspect(ctx, request)
				if err != nil {
					t.Fatal(err)
				}
				if result.State == build.BuildUnknown && scenario.name == "metadata-late-create" && !observedUnknown {
					observedUnknown = true
					if !strings.HasPrefix(result.Failure, "METADATA_COMMAND_TIMEOUT:") || !strings.Contains(result.Failure, "cleanup outcome unknown") {
						t.Fatalf("unknown creation lost its timeout detail: %q", result.Failure)
					}
					// A real Docker create models the daemon finishing an admitted create after client exit.
					// No fixture substitutes for Docker: Inspect must observe and remove this exact container.
					name := "sv2-source-" + request.RequestID[:32] + "-1-1"
					command("", "docker", "create", "--name", name, "--entrypoint", "go", tool, "version")
				}
				return result.State == build.BuildFailed || result.State == build.BuildReady
			})
			if scenario.name == "cancelled" {
				if result.State != build.BuildFailed || result.Failure != "CANCELLED" {
					t.Fatalf("explicit cancellation: state=%s detail=%q", result.State, result.Failure)
				}
			} else if scenario.metadataFailure {
				code := "METADATA_COMMAND_FAILED"
				if scenario.timeout {
					code = "METADATA_COMMAND_TIMEOUT"
				}
				if result.State != build.BuildFailed || !strings.HasPrefix(result.Failure, code+":") || !strings.Contains(result.Failure, "go mod edit -json") || !strings.Contains(result.Failure, "stderr tail:") || result.FailureCategory == build.FailureUnsupportedDexSDK {
					t.Fatalf("expected retained metadata failure, got state=%s category=%s detail=%q", result.State, result.FailureCategory.Name(), result.Failure)
				}
				if scenario.timeout && scenario.name != "metadata-late-create" && (!strings.Contains(result.Failure, "deadline exceeded") || !strings.Contains(result.Failure, "metadata-timeout-started") || !strings.Contains(result.Failure, "[REDACTED]") || strings.Contains(result.Failure, "synthetic-timeout-secret")) {
					t.Fatalf("timeout diagnostic missing: %q", result.Failure)
				}
				if scenario.name == "metadata-stderr" && (!strings.Contains(result.Failure, "[truncated]") || !strings.Contains(result.Failure, "[REDACTED]") || strings.Contains(result.Failure, "synthetic-secret-value") || len(result.Failure) > 2600) {
					t.Fatalf("stderr is not bounded/redacted: %q", result.Failure)
				}
				if scenario.name == "metadata-remove-uncertain" {
					if _, err := os.Stat(filepath.Join(scratch, "remove-response-lost")); err != nil {
						t.Fatalf("real Docker removal fault was not injected: %v", err)
					}
				}
				if scenario.name == "metadata-late-create" && !observedUnknown {
					t.Fatal("an unobserved aborted create was incorrectly settled")
				}
				if scenario.timeout {
					filter := "name=^/sv2-source-" + request.RequestID[:32] + "-1-"
					if remaining := command("", "docker", "container", "ls", "--all", "--filter", filter, "--format", "{{.Names}}"); remaining != "" {
						t.Fatalf("owned metadata container survived timeout: %s", remaining)
					}
				}
				t.Logf("recorded failure detail: %s", result.Failure)
			} else if scenario.rejected {
				if result.State != build.BuildFailed || result.FailureCategory != build.FailureUnsupportedDexSDK || !strings.Contains(result.Failure, "v1.13.0") {
					t.Fatalf("expected SDK-floor rejection, got state=%s category=%s hint=%q", result.State, result.FailureCategory.Name(), result.Failure)
				}
			} else if result.State != build.BuildReady {
				t.Fatalf("valid SDK source failed: category=%s hint=%q", result.FailureCategory.Name(), result.Failure)
			}
			if scenario.name != "cancelled" {
				page, err := builder.ReadLog(ctx, request, build.LogRead{Tail: true, MaximumBytes: 64 << 10})
				if err != nil {
					t.Fatal(err)
				}
				outcome := "ok"
				if scenario.metadataFailure {
					outcome = "platform failure"
					if scenario.timeout {
						outcome = "timed out"
					}
				} else if scenario.rejected {
					outcome = "rejected"
				}
				if !strings.Contains(page.Text, "Dex SDK check: "+outcome) {
					t.Fatalf("SDK check outcome %q was not recorded", outcome)
				}
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
