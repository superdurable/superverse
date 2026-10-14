// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/comm"
)

// Fixed addresses of coding profile v4 inside a Sandbox.
const (
	// PreviewApplicationPort is where the supervisor runs the Preview
	// application; the instance's Preview Service and Ingress route to it.
	PreviewApplicationPort = 3000
	// PreviewWorkerAddress is where the Preview application's Worker listens
	// inside the Pod; the Sandbox-local Dex dials it there.
	PreviewWorkerAddress = "127.0.0.1:18811"
)

// States the Preview supervisor answers; superverse-previewctl's README is
// the contract.
const (
	PreviewStateReady         = "ready"
	PreviewStateFull          = "full"
	PreviewStateInconsistent  = "inconsistent"
	PreviewStateFailed        = "failed"
	PreviewStateRunning       = "running"
	PreviewStateSourceMissing = "source_missing"
	PreviewStateStopped       = "stopped"
)

const (
	// previewCommandTimeout bounds one supervisor command. The daemon answers
	// over its socket within eight seconds, and the start then builds detached,
	// so a Sandbox Step asks again in its next polling round.
	previewCommandTimeout = 10 * time.Second
	// previewSourceTimeout bounds the export of one commit's source tree,
	// which runs in the command itself rather than in the daemon.
	previewSourceTimeout = 2 * time.Minute
	// maximumDiagnosticTailBytes bounds the supervisor output an error keeps.
	// It leaves room for the error's prefix inside the 2 KiB cause of a
	// Failure, which keeps the head, so the tail that explains the failure is
	// not cut.
	maximumDiagnosticTailBytes = 1536
	// minimumRedactedValueBytes is the shortest configured value redacted from
	// supervisor output; shorter values cannot be credentials.
	minimumRedactedValueBytes = 8
)

// ErrPreviewSupervisor is the supervisor's definite answer that the Preview
// application failed: its build failed, or it exited or missed its health
// probe within the startup budget. The error carries the redacted tail of the
// build or application log; asking again gives the same answer.
var ErrPreviewSupervisor = errors.New("the Preview application failed")

// previewStorageProfilePath holds the Preview storage profile under .git,
// which Git never adds to a source commit.
const previewStorageProfilePath = ".git/sv2-preview-aws"

// PreviewIdentity identifies one start of the Preview application: the
// prepared commit it was built from, and the configuration and grant
// generation it runs with.
type PreviewIdentity struct {
	Commit              string `json:"commit"`
	ConfigurationDigest string `json:"configurationDigest"`
	GrantGeneration     uint64 `json:"grantGeneration"`
}

// PreviewAnswer is one definite answer of the supervisor: its state and,
// depending on the command, the identity of the engine or of the application.
type PreviewAnswer struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
	// The engine of a ready environment; the retention is read back.
	TemporalClusterID string `json:"temporalClusterId"`
	RetentionDays     int    `json:"retentionDays"`
	TemporalVersion   string `json:"temporalVersion"`
	DexVersion        string `json:"dexVersion"`
	// The application of a start or of status.
	PreviewIdentity
	WorkerListening bool `json:"workerListening"`
	// Path is the source tree a source ensure answered.
	Path string `json:"path"`
}

// PreviewConfiguration is the material of one Preview start. It is transient:
// it carries credentials and is never persisted in a Flow. The platform builds
// all of it: the start's identity, and an Environment of the Project
// configuration grant plus the route address.
type PreviewConfiguration struct {
	PreviewIdentity
	Environment map[string]string
}

// EnsurePreviewEnvironment starts the instance's Preview engine, or finds it
// running: the supervisor answers starting, ready with the engine's identity,
// or a refusal (full, inconsistent, failed) with its reason.
func (c *Capabilities) EnsurePreviewEnvironment(ctx context.Context, binding Binding, commandID string) (PreviewAnswer, error) {
	return withSandbox(ctx, c.resolver, binding, func(client *sandboxd.SandboxClient) (PreviewAnswer, error) {
		return runPreviewctl(ctx, client, commandExecution(binding, commandID), nil, nil, "environment", "ensure")
	})
}

// EnsurePreviewSource exports commit from the workspace repository into its
// isolated source tree under the Preview state directory, or finds it
// exported; it keeps at most that tree and the one in use. The supervisor
// answers ready with the tree's Path, or source_missing for a commit the
// repository does not hold. An automatic deployment renders and builds only
// from that tree, never from the working tree.
func (c *Capabilities) EnsurePreviewSource(ctx context.Context, binding Binding, commandID, commit string) (PreviewAnswer, error) {
	return withSandbox(ctx, c.resolver, binding, func(client *sandboxd.SandboxClient) (PreviewAnswer, error) {
		return runPreviewctlWithin(ctx, client, commandExecution(binding, "source/"+commandID), previewSourceTimeout, nil, nil, "source", "ensure", "--commit", commit)
	})
}

// StopPreviewEnvironment stops the instance's Preview engine: the supervisor
// answers stopping, then stopped once every engine process exited and released
// the engine state.
func (c *Capabilities) StopPreviewEnvironment(ctx context.Context, binding Binding, commandID string) (PreviewAnswer, error) {
	return withSandbox(ctx, c.resolver, binding, func(client *sandboxd.SandboxClient) (PreviewAnswer, error) {
		return runPreviewctl(ctx, client, commandExecution(binding, commandID), nil, nil, "environment", "stop")
	})
}

// BeginPreview starts the Preview application of config's commit, which the
// supervisor builds detached in an isolated source tree while the caller polls
// through BeginPreview again; a running application of the same identity is
// not rebuilt. Storage credentials reach the workspace only as a mode-0600
// file; only paths and the environment the platform built enter the process.
// An answered application failure is ErrPreviewSupervisor with its reason.
func (c *Capabilities) BeginPreview(ctx context.Context, binding Binding, commandID string, config PreviewConfiguration) (PreviewAnswer, error) {
	profile := previewStorageProfile(config)
	environment := previewEnvironment(config, profile != nil)
	answer, err := withSandbox(ctx, c.resolver, binding, func(client *sandboxd.SandboxClient) (PreviewAnswer, error) {
		if profile != nil {
			if err := client.WriteFile(ctx, previewStorageProfilePath, profile, "0600"); err != nil {
				return PreviewAnswer{}, fmt.Errorf("deliver the Preview storage profile: %w", err)
			}
		}
		return runPreviewctl(ctx, client, commandExecution(binding, commandID), environment, configuredValues(config), "begin", "--commit", config.Commit)
	})
	if err == nil && answer.State == PreviewStateFailed {
		return answer, fmt.Errorf("%w: %s", ErrPreviewSupervisor, answer.Reason)
	}
	return answer, err
}

// previewStorageProfile is the AWS shared-credentials file of the storage keys
// the platform issued for the start, or nil when it issued none.
func previewStorageProfile(config PreviewConfiguration) []byte {
	access, secret := config.Environment["AWS_ACCESS_KEY_ID"], config.Environment["AWS_SECRET_ACCESS_KEY"]
	if access == "" {
		return nil
	}
	return []byte("[default]\naws_access_key_id=" + access + "\naws_secret_access_key=" + secret + "\n")
}

// StopPreview stops the Preview application of the instance; the engine
// keeps running. The supervisor answers stopping, then stopped.
func (c *Capabilities) StopPreview(ctx context.Context, binding Binding, commandID string) (PreviewAnswer, error) {
	return withSandbox(ctx, c.resolver, binding, func(client *sandboxd.SandboxClient) (PreviewAnswer, error) {
		return runPreviewctl(ctx, client, commandExecution(binding, commandID), nil, nil, "stop")
	})
}

// CheckPreviewWorker proves that the Preview application of identity serves
// in the instance and its Worker accepts connections, so a Flow started on the
// Preview engine runs that application's code.
func (c *Capabilities) CheckPreviewWorker(ctx context.Context, binding Binding, identity PreviewIdentity) error {
	answer, err := withSandbox(ctx, c.resolver, binding, func(client *sandboxd.SandboxClient) (PreviewAnswer, error) {
		return runPreviewctl(ctx, client, sandboxd.PlatformExecutionID("preview-status"), nil, nil, "status")
	})
	if err != nil {
		return err
	}
	if answer.State != PreviewStateRunning || answer.PreviewIdentity != identity || !answer.WorkerListening {
		return fmt.Errorf("the Preview application Worker does not serve: the supervisor answers %s for commit %s", answer.State, answer.Commit)
	}
	return nil
}

// commandExecution is the sandboxd execution of one supervisor command of an
// instance. sandboxd keeps no durable request identity, so the identity only
// fences a second run of the same command while one is in flight.
func commandExecution(binding Binding, commandID string) sandboxd.ExecutionID {
	return sandboxd.DeriveExecutionID(sandboxd.InvocationIdentity{FlowID: binding.FlowID, CallID: "preview/" + commandID})
}

// runPreviewctl runs one supervisor command to its exit within
// previewCommandTimeout (runPreviewctlWithin).
func runPreviewctl(ctx context.Context, client *sandboxd.SandboxClient, execution sandboxd.ExecutionID, environment []sandboxd.EnvironmentVariable, secrets []string, arguments ...string) (PreviewAnswer, error) {
	return runPreviewctlWithin(ctx, client, execution, previewCommandTimeout, environment, secrets, arguments...)
}

// runPreviewctlWithin runs one supervisor command to its exit within timeout.
// Exit 0 with a readable answer is a definite answer. Any other exit is a
// supervisor, transport or usage error that may be asked again; it keeps the
// supervisor's reason or, without one, the tail of its output. Every
// configured value is redacted from both first, because the supervisor runs
// repository code with those values.
func runPreviewctlWithin(ctx context.Context, client *sandboxd.SandboxClient, execution sandboxd.ExecutionID, timeout time.Duration, environment []sandboxd.EnvironmentVariable, secrets []string, arguments ...string) (PreviewAnswer, error) {
	command := strings.Join(arguments, " ")
	output, err := client.RunToExit(ctx, sandboxd.StartRequest{
		ExecutionID:        execution,
		Arguments:          append([]string{"superverse-previewctl"}, arguments...),
		Environment:        environment,
		Timeout:            timeout,
		MaximumOutputBytes: 16 << 10,
	})
	if err != nil {
		return PreviewAnswer{}, fmt.Errorf("run the Preview supervisor's %s: %w", command, err)
	}
	var answer PreviewAnswer
	decodeErr := json.Unmarshal([]byte(output.Stdout), &answer)
	answer.Reason = comm.TailUTF8(redact(answer.Reason, secrets), maximumDiagnosticTailBytes)
	if output.ExitCode == 0 && decodeErr == nil {
		return answer, nil
	}
	diagnostic := answer.Reason
	if diagnostic == "" {
		diagnostic = comm.TailUTF8(redact(strings.TrimSpace(output.Stdout+"\n"+output.Stderr), secrets), maximumDiagnosticTailBytes)
	}
	return PreviewAnswer{}, fmt.Errorf("the Preview supervisor's %s exited with code %d: %s", command, output.ExitCode, diagnostic)
}

// redact replaces every secret in text.
func redact(text string, secrets []string) string {
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

// configuredValues are the environment values a supervisor answer must not
// repeat: the supervisor runs repository code with them, and code may print
// what it was given.
func configuredValues(config PreviewConfiguration) []string {
	values := make([]string, 0, len(config.Environment))
	for _, value := range config.Environment {
		if len(value) >= minimumRedactedValueBytes {
			values = append(values, value)
		}
	}
	return values
}

// previewEnvironment is the process environment of a Preview start: the tool
// environment, because the supervisor builds and runs repository code, the
// configuration digest and grant generation of the start's identity, the path
// of the storage profile when there is one, and the variables the platform
// built for the start (its Project configuration grant and the route address)
// except the storage keys, which only that profile carries. No application
// configuration value enters it.
func previewEnvironment(config PreviewConfiguration, storageProfile bool) []sandboxd.EnvironmentVariable {
	environment := append(sandboxd.ToolProcessEnvironment(sandboxd.WorkspaceRoot),
		sandboxd.EnvironmentVariable{Name: "DEX_PROJECT_CONFIG_DIGEST", Value: config.ConfigurationDigest},
		sandboxd.EnvironmentVariable{Name: "SUPERVERSE_PREVIEW_GRANT_GENERATION", Value: strconv.FormatUint(config.GrantGeneration, 10)},
	)
	if storageProfile {
		environment = append(environment, sandboxd.EnvironmentVariable{Name: "AWS_SHARED_CREDENTIALS_FILE", Value: sandboxd.WorkspaceRoot + "/" + previewStorageProfilePath})
	}
	for _, name := range slices.Sorted(maps.Keys(config.Environment)) {
		if name != "AWS_ACCESS_KEY_ID" && name != "AWS_SECRET_ACCESS_KEY" && name != "DEX_PROJECT_CONFIG_DIGEST" {
			environment = append(environment, sandboxd.EnvironmentVariable{Name: name, Value: config.Environment[name]})
		}
	}
	return environment
}
