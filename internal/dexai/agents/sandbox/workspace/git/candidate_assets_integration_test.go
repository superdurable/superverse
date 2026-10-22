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
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd/sandboxdtest"
)

// The production helper inspects actual files in a disposable coding Sandbox.
func TestConnectorCandidateAssetsActualSandbox(t *testing.T) {
	client := sandboxdtest.StartCodingContainerForTest(t).Client()
	// Loading the embedded production module avoids a replacement filesystem or
	// build dependency: the same validation runs at the end of build_assets.
	script := connectorContributionProgram + `
workspace = "/workspace"
out = os.path.join(workspace, ".git/superverse/connectors/candidates/ab12cd34")
os.makedirs(out)
with open(os.path.join(out, "connector-release.json"), "w") as asset:
    asset.write("{}")
expected = {"connector-release.json": {"path": ".git/superverse/connectors/candidates/ab12cd34/connector-release.json", "size": 2, "sha256": hashlib.sha256(b"{}").hexdigest()}}
assert candidate_asset_paths(out, workspace) == expected
with open("/workspace/candidate-private", "w") as secret:
    secret.write("must never be published")
for name, kind in (("leak", "link"), ("directory", "directory"), ("pipe", "fifo")):
    target = os.path.join(out, name)
    if kind == "link":
        os.symlink("/workspace/candidate-private", target)
    elif kind == "directory":
        os.mkdir(target)
    else:
        os.mkfifo(target)
    published = {}
    try:
        published.update(candidate_asset_paths(out, workspace))
        raise AssertionError("unsafe candidate accepted")
    except Refusal as refusal:
        assert refusal.code == "release_candidate_refused", refusal.code
        assert name in refusal.detail, refusal.detail
        assert not published, published
    if kind == "directory":
        os.rmdir(target)
    else:
        os.unlink(target)
assert candidate_asset_paths(out, workspace) == expected
os.rename(out, out + "-real")
os.symlink(out + "-real", out)
try:
    candidate_asset_paths(out, workspace)
    raise AssertionError("symlinked output directory accepted")
except Refusal as refusal:
    assert refusal.code == "release_candidate_refused"
os.unlink(out)
os.rename(out + "-real", out)
print(json.dumps(candidate_asset_paths(out, workspace)))
`
	// The embedded program's CLI main must not run when imported by this probe.
	program := "exec(compile(" + string(comm.Must(json.Marshal(script))) + ", 'connector_contribution.py', 'exec'), {'__name__': 'candidate_asset_probe'})"
	result, err := client.RunToExit(testContext(t), sandboxd.StartRequest{
		ExecutionID: sandboxd.PlatformExecutionID("candidate-assets-test"),
		Arguments:   []string{"python3", "-c", program}, Timeout: time.Minute,
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("candidate asset confinement: %v, exit %d: %s %s", err, result.ExitCode, result.Stdout, result.Stderr)
	}
	var entries map[string]candidateAsset
	if err := json.Unmarshal([]byte(result.Stdout), &entries); err != nil {
		t.Fatalf("decode validated assets: %v", err)
	}
	assets, err := readCandidateAssets(testContext(t), client, "ab12cd34", entries)
	if err != nil || string(assets["connector-release.json"]) != "{}" {
		t.Fatalf("regular candidate assets: %v, %+v", err, assets)
	}
	shell(t, client, "printf '[]' > /workspace/.git/superverse/connectors/candidates/ab12cd34/connector-release.json")
	if assets, err := readCandidateAssets(testContext(t), client, "ab12cd34", entries); err == nil || assets != nil || !strings.Contains(err.Error(), "changed after validation") {
		t.Fatalf("a same-size replacement reached publication: %v, %+v", err, assets)
	}
	shell(t, client, "rm /workspace/.git/superverse/connectors/candidates/ab12cd34/connector-release.json; ln -s /workspace/candidate-private /workspace/.git/superverse/connectors/candidates/ab12cd34/connector-release.json")
	if assets, err := readCandidateAssets(testContext(t), client, "ab12cd34", entries); err == nil || assets != nil || !strings.Contains(err.Error(), "changed after validation") {
		t.Fatalf("a file swapped for a symlink reached publication: %v, %+v", err, assets)
	}
}
