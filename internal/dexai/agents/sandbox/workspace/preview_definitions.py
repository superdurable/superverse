# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import hashlib
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile


# Dex reports a Step group that is neither one run nor one choice as this warning; it never blocks.
ADVISORY_DIAGNOSTICS = frozenset({"v2_step_group_shape"})


def blocking_diagnostics(graph):
    return [d for d in graph.get("diagnostics") or []
            if not (d.get("severity") == "warning" and d.get("code") in ADVISORY_DIAGNOSTICS)]



class PlatformError(Exception):
    """A failure of the platform's own preparation rather than of the source;
    asking again may repair it."""


def command(arguments, **options):
    return subprocess.run(arguments, check=True, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, timeout=180, **options).stdout


def visualize(source, source_path, output):
    # A source with blocking diagnostics makes dexcli exit nonzero after it
    # wrote the partial graph, whose diagnostics are the reason to report.
    arguments = ["dexcli", "visualize", source_path, "--language", "go",
                 "--schema-version", "2.0", "--json", "--out", str(output)]
    process = subprocess.run(arguments, cwd=source, stdout=subprocess.PIPE,
                             stderr=subprocess.PIPE, timeout=180)
    graph = output.with_suffix(".json")
    if process.returncode != 0 and not graph.exists():
        raise subprocess.CalledProcessError(process.returncode, arguments, process.stdout, process.stderr)
    return json.loads(graph.read_text())


def render(tree):
    """Render every Flow that tree declares from an isolated archive of it, and
    reject the tree when any definition has a diagnostic."""
    with tempfile.TemporaryDirectory(prefix="superverse-preview-source-") as directory:
        archive = command(["git", "archive", tree])
        command(["tar", "-xf", "-", "-C", directory], input=archive)
        source = pathlib.Path(directory)
        if any(path.is_symlink() for path in source.rglob("*")):
            raise ValueError("SYMLINK_SOURCE")
        manifest = json.loads((source / "dex-app.yaml").read_text())
        definitions, invalid = [], []
        for index, entry in enumerate(manifest["flowDefinitions"]):
            source_path = entry["sourcePath"]
            if (pathlib.PurePosixPath(source_path).is_absolute() or
                    ".." in pathlib.PurePosixPath(source_path).parts or
                    not source_path.endswith(".go")):
                raise ValueError("INVALID_SOURCE_PATH")
            graph = visualize(source, source_path, source / (".preview-definition-%d" % index))
            if graph["schemaVersion"] != "2.0" or graph["valid"] is not True or blocking_diagnostics(graph):
                invalid.append("INVALID_DEFINITION %s: %s" % (source_path, json.dumps(graph["diagnostics"])))
            definitions.append({"sourcePath": source_path, "graph": graph})
    if invalid:
        raise ValueError("\n".join(invalid))
    return definitions


def rendered_flow(definition):
    """The commit gate's view of one rendered Flow: its type and its connector
    Steps with the branches the Flow wires, which the test stage covers."""
    graph = definition["graph"]
    branches = {}
    for edge in graph.get("edges") or []:
        if (edge.get("metadata") or {}).get("connectorBranch"):
            branches.setdefault(edge["from"], []).append(edge["label"])
    steps = []
    for node in graph.get("nodes") or []:
        connector = (node.get("metadata") or {}).get("connector")
        if node.get("kind") == "step" and connector:
            steps.append({"step": node["name"], "connection": connector.get("connectionName", ""),
                          "connectorId": connector.get("connectorId", ""), "modulePath": connector.get("modulePath", ""),
                          "moduleVersion": connector.get("moduleVersion", ""), "operation": connector.get("operationId", ""),
                          "branches": sorted(set(branches.get(node["id"], [])))})
    return {"sourcePath": definition["sourcePath"], "flowType": graph["flow"]["name"], "connectorSteps": steps}


def require_clean_head(commit):
    head = command(["git", "rev-parse", "HEAD"]).decode().strip()
    if head != commit or command(["git", "status", "--porcelain"]):
        raise ValueError("SOURCE_CHANGED")


def head_definitions(commit):
    """The Preview mode: the definitions of commit, which stays the clean HEAD."""
    require_clean_head(commit)
    definitions = render(commit)
    require_clean_head(commit)
    return {"definitions": definitions}


def worktree_tree():
    """The commit gate's mode: the tree that committing every change of the
    working tree would create, written through a copy of the index so that
    neither the index nor the working tree changes. The gate reads the rendered
    Flows (rendered_flow) from stdout."""
    index = command(["git", "rev-parse", "--git-path", "index"]).decode().strip()
    with tempfile.TemporaryDirectory(prefix="superverse-worktree-index-") as directory:
        environment = dict(os.environ, GIT_INDEX_FILE=os.path.join(directory, "index"))
        shutil.copyfile(index, environment["GIT_INDEX_FILE"])
        command(["git", "add", "--all"], env=environment)
        return command(["git", "write-tree"], env=environment).decode().strip()


def pinned_definitions(tree, commit):
    """The automatic deployment's mode: the definitions of commit, which need
    not be HEAD, rendered from an isolated archive of the commit, and the
    digest of the dex-app.yaml of the commit's exported tree, which
    superverse-previewctl source ensure wrote under the Preview state directory
    and the deployment builds. It checks no HEAD, status or working tree."""
    try:
        sources = pathlib.Path(os.environ["SUPERVERSE_PREVIEW_STATE_DIR"], "source").resolve()
        path = pathlib.Path(tree).resolve()
        marker = (path / ".superverse-source").read_text()
        manifest = (path / "dex-app.yaml").read_bytes()
    except (KeyError, OSError) as error:
        raise PlatformError("the exported source tree of %s is unreadable: %s" % (commit, error)) from error
    if path.parent != sources or path.name != commit or marker != commit:
        raise PlatformError("%s is not the exported source tree of %s" % (tree, commit))
    return {"definitions": render(commit), "manifestDigest": "sha256:" + hashlib.sha256(manifest).hexdigest()}


# The platform keeps the end of stderr as the reason, so the cause comes last.
# A failure of the platform's preparation exits with 2, any rejection of the
# source with 1.
try:
    if sys.argv[1] == "--worktree":
        flows = [rendered_flow(definition) for definition in render(worktree_tree())]
        print(json.dumps({"flows": flows}, separators=(",", ":")))
    elif sys.argv[1] == "--pinned":
        print(json.dumps(pinned_definitions(sys.argv[2], sys.argv[3]), separators=(",", ":")))
    else:
        print(json.dumps(head_definitions(sys.argv[1]), separators=(",", ":")))
except PlatformError as error:
    print("PlatformError: %s" % error, file=sys.stderr)
    sys.exit(2)
except subprocess.CalledProcessError as error:
    output = (error.stderr or b"").decode("utf-8", "replace").strip()
    print("%s exited with code %d: %s" % (error.cmd[0], error.returncode, output), file=sys.stderr)
    sys.exit(1)
except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
    print("%s: %s" % (type(error).__name__, error), file=sys.stderr)
    sys.exit(1)
