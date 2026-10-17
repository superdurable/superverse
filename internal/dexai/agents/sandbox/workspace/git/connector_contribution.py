# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Connector contributions of a Session's connector_develop subagents.

The Sandbox keeps one clone of the connectors library inside the workspace's
Git directory and one worktree per develop subagent, on the branch of its
connector. One process runs one operation for one subagent and answers one JSON
object on stdout. Exit code 0 is the answer; exit code 1 with {"error": ...} is
a failure in reaching the remote that asking again may repair, and exit code 2
with {"error": ...} a failure that asking again repeats. Every operation queries
before it writes, so a repeated operation repeats nothing it finished:

  create   fetch main (and the branch, when the remote has it) into the clone,
           check the branch out as the subagent's worktree and record its base
  finish   save the change into the result ref; for a contribution also
           verify that the change touches exactly the connector's module (and
           its catalog.yaml entry for a new connector), commit it once as the
           given identity, move the branch, push it without force when asked,
           checking the remote branch before and after, and remove the worktree
  candidate  choose the release candidate vX.Y.Z-rc.N of a pushed head: the
           manifest's version and the next candidate number on the remote, or
           the candidate whose tag already names the head; for the local Git
           host also validate it and build its release assets with main's
           connectorctl, then push its tag without force

A remote token, when the caller has one, is only in this process's Git
environment (an authorization header scoped to the remote's origin) and never
in an answer.
"""
import json
import os
import re
import shutil
import subprocess
import sys
import time

GIT_TIMEOUT = 300
BUILD_TIMEOUT = 900
CONNECTOR_CHECK_TIMEOUT = 1800
CONNECTOR_CHECK_OUTPUT_BYTES = 4096
STABLE_VERSION = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")
CATALOG = "catalog.yaml"
MODULE_ROOT = "connectors/"
SLUG = re.compile(r"^[a-z][a-z0-9-]{1,62}$")


class HelperError(Exception):
    """A failure of the helper that asking again repeats, such as a missing
    path or a local Git command that failed."""


class RemoteError(HelperError):
    """A failure in reaching the library's remote; the caller may ask again."""


# The Git commands that talk to the remote, whose failure may pass.
REMOTE_COMMANDS = {"fetch", "ls-remote", "push"}


class Refusal(Exception):
    """A refusal of the request that asking again cannot change."""

    def __init__(self, code, detail):
        super().__init__(detail)
        self.code, self.detail = code, detail


def run(arguments, cwd, env=None, check=True, redirected=True):
    environment = dict(os.environ)
    if not redirected:
        # The Sandbox's Git configuration redirects the library to this lane's
        # copy; a read of the public library itself goes without it.
        environment = {key: value for key, value in environment.items()
                       if not key.startswith(("GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_"))}
    environment["GIT_OPTIONAL_LOCKS"] = "0"
    environment.update(env or {})
    process = subprocess.run(["git", "-c", "safe.directory=*", "-c", "core.quotePath=false", *arguments], cwd=cwd,
                             env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=GIT_TIMEOUT, check=False)
    if check and process.returncode != 0:
        message = process.stderr.decode(errors="replace").strip()[:2000]
        error = RemoteError if arguments[0] in REMOTE_COMMANDS else HelperError
        raise error("git %s exited with %d: %s" % (arguments[0], process.returncode, message))
    return process


def output(arguments, cwd, env=None):
    return run(arguments, cwd, env).stdout.decode().strip()


def write_atomically(path, payload):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    temporary = path + ".tmp"
    with open(temporary, "w", encoding="utf-8") as file:
        json.dump(payload, file, sort_keys=True)
    os.replace(temporary, path)


def read_json(path):
    if not os.path.exists(path):
        return None
    with open(path, encoding="utf-8") as file:
        return json.load(file)


class Contribution:
    """The clone, the worktree, the refs and the receipt of one subagent."""

    def __init__(self, workspace, agent_id):
        self.git_dir = output(["rev-parse", "--absolute-git-dir"], workspace)
        self.root = os.path.join(self.git_dir, "superverse", "connectors")
        self.clone = os.path.join(self.root, "library")
        self.worktrees = os.path.join(self.root, "worktrees")
        self.worktree = os.path.join(self.worktrees, agent_id)
        self.receipt = os.path.join(self.root, "contributions", agent_id + ".json")
        self.id = agent_id
        self.base_ref = "refs/superverse/contributions/%s/base" % agent_id
        self.result_ref = "refs/superverse/contributions/%s/result" % agent_id

    def git(self, arguments, check=True, redirected=True):
        return run(arguments, self.clone, check=check, redirected=redirected)

    def commit(self, ref):
        process = self.git(["rev-parse", "--verify", "--quiet", ref + "^{commit}"], check=False)
        return process.stdout.decode().strip() if process.returncode == 0 else None

    def remote_head(self, branch):
        process = self.git(["ls-remote", "--exit-code", "origin", "refs/heads/" + branch], check=False)
        if process.returncode == 2:
            return None
        if process.returncode != 0:
            raise RemoteError("git ls-remote exited with %d: %s" % (process.returncode, process.stderr.decode(errors="replace").strip()[:2000]))
        fields = process.stdout.decode().split()
        if len(fields) != 2:
            raise RemoteError("git ls-remote answered %r" % process.stdout.decode()[:200])
        return fields[0]

    def ensure_clone(self, url):
        if not os.path.isdir(self.clone):
            os.makedirs(self.root, exist_ok=True)
            temporary = self.clone + ".creating"
            shutil.rmtree(temporary, ignore_errors=True)
            run(["init", "--quiet", "--bare", temporary], self.root)
            os.replace(temporary, self.clone)
        if self.git(["config", "--get", "remote.origin.url"], check=False).stdout.decode().strip() != url:
            self.git(["config", "remote.origin.url", url])

    def remove_worktree(self, path):
        if os.path.isdir(path):
            self.git(["worktree", "remove", "--force", path], check=False)
            shutil.rmtree(path, ignore_errors=True)
        self.git(["worktree", "prune"])


def disk_free(path):
    status = os.statvfs(path)
    return status.f_bavail * status.f_frsize


def create(contribution, request):
    """Fetches the library and checks the subagent's branch out as its
    worktree; a recorded base means an earlier attempt created it."""
    base = contribution.commit(contribution.base_ref) if os.path.isdir(contribution.clone) else None
    branch = request["branch"]
    if base is not None:
        return {"base": base, "worktree": contribution.worktree, "branch": branch}
    # The first contribution of a workspace creates the helper's directories.
    if disk_free(contribution.git_dir) < request["minimumFreeBytes"]:
        raise Refusal("workspace_disk_low", "the workspace volume has less than %d bytes free" % request["minimumFreeBytes"])
    os.makedirs(contribution.worktrees, exist_ok=True)
    contribution.ensure_clone(request["url"])
    contribution.git(["fetch", "--quiet", "--prune", "--no-tags", "origin", "+refs/heads/main:refs/remotes/origin/main"])
    remote = contribution.remote_head(branch)
    if remote is not None:
        contribution.git(["fetch", "--quiet", "--no-tags", "origin", "+refs/heads/%s:refs/remotes/origin/%s" % (branch, branch)])
    local = contribution.commit("refs/heads/" + branch)
    start = local
    if local is None:
        start = remote or contribution.commit("refs/remotes/origin/main")
    elif remote is not None and run_is_ancestor(contribution, local, remote):
        start = remote
    if start is None:
        raise HelperError("the library has no main branch")
    # A worktree that still holds the branch belongs to a subagent that never
    # finished; the next one takes the branch over.
    for listed in registered_worktrees(contribution):
        if listed["branch"] == "refs/heads/" + branch and os.path.realpath(listed["path"]) != os.path.realpath(contribution.worktree):
            contribution.remove_worktree(listed["path"])
    contribution.git(["update-ref", "refs/heads/" + branch, start])
    contribution.git(["update-ref", contribution.base_ref, start, ""])
    contribution.remove_worktree(contribution.worktree)
    contribution.git(["worktree", "add", "--quiet", contribution.worktree, branch])
    return {"base": start, "worktree": contribution.worktree, "branch": branch}


def run_is_ancestor(contribution, ancestor, descendant):
    return contribution.git(["merge-base", "--is-ancestor", ancestor, descendant], check=False).returncode == 0


def registered_worktrees(contribution):
    listed, current = [], {}
    for line in output(["worktree", "list", "--porcelain"], contribution.clone).splitlines():
        if line.startswith("worktree "):
            current = {"path": line[len("worktree "):], "branch": ""}
            listed.append(current)
        elif line.startswith("branch "):
            current["branch"] = line[len("branch "):]
    return listed


def manifest_name(text):
    """metadata.name of a connector.yaml: the first key below the top-level
    metadata mapping."""
    inside = False
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if not line[0].isspace():
            inside = line.rstrip() == "metadata:"
            continue
        if inside:
            key, _, value = line.strip().partition(":")
            if key == "name":
                return value.strip().strip("'\"")
    return None


def catalog_directories(text):
    return {line.strip()[2:].strip().strip("'\"") for line in text.splitlines() if line.strip().startswith("- ")}


def show(contribution, revision, path):
    process = contribution.git(["show", "%s:%s" % (revision, path)], check=False)
    return process.stdout.decode(errors="replace") if process.returncode == 0 else None


def verify(contribution, base, commit, request):
    """The module the change contributes, or a Refusal that says why the
    change is not one contribution of the connector."""
    slug, kind = request["slug"], request["contribution"]
    changed = [line for line in output(["diff", "--name-only", "--no-renames", base, commit], contribution.clone).splitlines() if line]
    others = [path for path in changed if path != CATALOG]
    if not others:
        raise Refusal("contribution_rejected", "the change touches no connector module")
    modules = set()
    for path in others:
        module = module_of(contribution, commit, path) or module_of(contribution, base, path)
        if module is None:
            raise Refusal("contribution_rejected", "%s is outside every connector module" % path)
        modules.add(module)
    if len(modules) != 1:
        raise Refusal("contribution_rejected", "the change touches %d connector modules: %s" % (len(modules), ", ".join(sorted(modules))))
    module = modules.pop()
    if module.rsplit("/", 1)[-1] != slug:
        raise Refusal("contribution_rejected", "the module %s is not the connector %s" % (module, slug))
    name = manifest_name(show(contribution, commit, module + "/connector.yaml") or "")
    if name != slug:
        raise Refusal("contribution_rejected", "%s/connector.yaml declares metadata.name %r, not %r" % (module, name, slug))
    existed = show(contribution, base, module + "/go.mod") is not None
    catalog_changed = CATALOG in changed
    if kind == "new":
        listed_before = module in catalog_directories(show(contribution, base, CATALOG) or "")
        listed_after = module in catalog_directories(show(contribution, commit, CATALOG) or "")
        if existed or listed_before:
            raise Refusal("contribution_rejected", "the new connector %s already exists in the library" % module)
        if not catalog_changed or not listed_after:
            raise Refusal("contribution_rejected", "a new connector adds its directory %s to catalog.yaml" % module)
    else:
        if not existed:
            raise Refusal("contribution_rejected", "a %s contribution changes the existing connector %s, which the library does not have" % (kind, module))
        if catalog_changed:
            raise Refusal("contribution_rejected", "a %s contribution leaves catalog.yaml unchanged" % kind)
    return module


def module_of(contribution, revision, path):
    """The connector module that holds path at revision: the nearest directory
    below connectors/ with a go.mod."""
    if not path.startswith(MODULE_ROOT):
        return None
    directory = os.path.dirname(path)
    while directory.startswith(MODULE_ROOT):
        if contribution.git(["cat-file", "-e", "%s:%s/go.mod" % (revision, directory)], check=False).returncode == 0:
            return directory
        directory = os.path.dirname(directory)
    return None


def stats(contribution, base, commit):
    files = insertions = deletions = 0
    for line in output(["diff", "--numstat", "--no-renames", base, commit], contribution.clone).splitlines():
        added, removed, _ = line.split("\t", 2)
        files += 1
        insertions += int(added) if added != "-" else 0
        deletions += int(removed) if removed != "-" else 0
    return files, insertions, deletions


def finish(contribution, request):
    """Commits the subagent's change once, pushes it when asked and removes
    the worktree. The receipt records the commit or why the change was
    refused, so a retry repeats neither."""
    receipt = read_json(contribution.receipt)
    base = contribution.commit(contribution.base_ref) if os.path.isdir(contribution.clone) else None
    if receipt is None:
        if base is None or not os.path.isdir(contribution.worktree):
            return {"missing": True}
        receipt = commit_worktree(contribution, base, request)
        if "checkFailed" in receipt:
            return receipt
        write_atomically(contribution.receipt, receipt)
    answer = dict(receipt)
    if "commit" in receipt and request["push"]:
        try:
            answer["pushed"] = push(contribution, receipt, request)
        except Refusal as refusal:
            answer["pushRefused"] = refusal.detail
    if os.path.isdir(contribution.worktree):
        contribution.remove_worktree(contribution.worktree)
    return answer


def commit_worktree(contribution, base, request):
    worktree = contribution.worktree
    run(["add", "--all"], worktree)
    tree = output(["write-tree"], worktree)
    if tree == output(["rev-parse", base + "^{tree}"], worktree):
        return {"base": base, "unchanged": True}
    identity = {"GIT_AUTHOR_NAME": request["name"], "GIT_AUTHOR_EMAIL": request["email"],
                "GIT_COMMITTER_NAME": request["name"], "GIT_COMMITTER_EMAIL": request["email"]}
    commit = output(["commit-tree", tree, "-p", base, "-m", request["subject"]], worktree, env=identity)
    contribution.git(["update-ref", contribution.result_ref, commit])
    if not request["contribute"]:
        # A subagent that did not complete contributes nothing; its work stays
        # in the result ref, off the branch.
        return {"base": base, "saved": commit}
    try:
        module = verify(contribution, base, commit, request)
    except Refusal as refusal:
        return {"base": base, "saved": commit, "rejected": refusal.detail}
    check_failure = check_connector(contribution, base, commit, module, request["publicURL"])
    if check_failure:
        return {"base": base, "checkFailed": check_failure}
    branch = "refs/heads/" + request["branch"]
    current = contribution.commit(branch)
    if current != commit:
        contribution.git(["update-ref", branch, commit, base])
    files, insertions, deletions = stats(contribution, base, commit)
    return {"base": base, "commit": commit, "module": module, "files": files, "insertions": insertions, "deletions": deletions}


def check_connector(contribution, base, commit, module, public_url):
    """Run the library's CI targets on the one staged connector before publishing it."""
    root = contribution.worktree
    # The local Git host mirrors main without tags. The library's dependency
    # validator requires official release tags, including the Connector SDK.
    contribution.git(["fetch", "--quiet", "--no-tags", public_url, "+refs/tags/*:refs/tags/*"], redirected=False)
    cache = os.path.join(contribution.root, "check-cache")
    for directory in ("go", "go-build", "npm"):
        os.makedirs(os.path.join(cache, directory), exist_ok=True)
    environment = {key: value for key, value in os.environ.items()
                   if not key.startswith(("GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_"))}
    environment.update({"GOWORK": "off", "GOPATH": os.path.join(cache, "go"),
                        "GOMODCACHE": os.path.join(cache, "go", "pkg", "mod"),
                        "GOCACHE": os.path.join(cache, "go-build"),
                        "npm_config_cache": os.path.join(cache, "npm"),
                        "TMPDIR": "/tmp",
                        "CONNECTOR_SCOPE": "changed", "CONNECTOR_BASE": base, "CONNECTOR_HEAD": commit})
    selected, selection_output, _ = run_check(
        ["go", "run", "./cmd/connectorctl", "test-matrix", "--catalog", "catalog.yaml", "--scope", "changed",
         "--base", base, "--head", commit, "--shard-index", "0", "--shard-count", "1"], root, environment, 180)
    selected_modules = [line.strip() for line in selection_output.splitlines() if line.strip().startswith(MODULE_ROOT)]
    if selected != 0 or selected_modules != [module]:
        return "connector selection failed: expected %s, got %s" % (module, selection_output or "no module")
    code, output, elapsed = run_check(["make", "catalog-check"], root, environment, 120)
    if code != 0:
        return "make catalog-check failed after %ds (exit %d):\n%s" % (elapsed, code, output)
    code, output, elapsed = test_connector_without_race(root, environment, module, 600)
    if code != 0:
        return "test-connectors without race failed after %ds (exit %d):\n%s" % (elapsed, code, output)
    targets = (("test-dex-compat-current", CONNECTOR_CHECK_TIMEOUT),
               ("studio-bundle-theme", 120), ("react-connectors", 600),
               ("test-dex-compat-latest-go-sdk", 600))
    for target, limit in targets:
        code, output, elapsed = run_check(["make", target], root, environment, limit)
        if code != 0:
            lines = [line.strip() for line in output.splitlines() if line.strip()]
            last_line = next((line for line in reversed(lines) if "gate failed:" in line), lines[-1] if lines else "no output")
            return "make %s failed after %ds (exit %d): %s\n%s" % (target, elapsed, code, last_line, output)
    return ""


def test_connector_without_race(root, environment, module, limit):
    """Mirror the library's test-connectors target without -race: the coding image has no C compiler and CGO is disabled."""
    commands = (
        (["python3", "script/release/component_release.py", "validate-connector", "--component-path", module,
          "--sdk-module", "github.com/superdurable/dex-connectors-library/sdkgo"], root),
        (["go", "test", "./..."], os.path.join(root, module)),
        (["go", "vet", "./..."], os.path.join(root, module)),
        (["go", "run", "./cmd/connectorctl", "validate", module + "/connector.yaml"], root),
        (["go", "run", "./cmd/connectorctl", "generate", "--check", module + "/connector.yaml"], root),
    )
    started, combined = time.monotonic(), ""
    for arguments, cwd in commands:
        remaining = round(limit - (time.monotonic() - started))
        if remaining <= 0:
            return 124, combined.strip(), round(time.monotonic() - started)
        code, output, _ = run_check(arguments, cwd, environment, remaining)
        combined = (combined + "\n$ " + " ".join(arguments) + "\n" + output)[-CONNECTOR_CHECK_OUTPUT_BYTES:]
        if code != 0:
            return code, combined.strip(), round(time.monotonic() - started)
    return 0, combined.strip(), round(time.monotonic() - started)


def run_check(arguments, root, environment, limit):
    """Keep only the output tail while the library command runs under a hard limit."""
    started = time.monotonic()
    command = ["timeout", "--kill-after=10s", "%ds" % limit, *arguments]
    with subprocess.Popen(command, cwd=root, env=environment, stdout=subprocess.PIPE,
                          stderr=subprocess.STDOUT) as process:
        tail = b""
        for chunk in iter(lambda: process.stdout.read(4096), b""):
            tail = (tail + chunk)[-CONNECTOR_CHECK_OUTPUT_BYTES:]
        code = process.wait()
    return code, tail.decode(errors="replace").strip(), round(time.monotonic() - started)


def push(contribution, receipt, request):
    """Publishes the commit on the branch without force: a remote branch that
    already holds it is published, one at the base or missing is pushed, and
    one that moved elsewhere is refused. The remote is read again afterwards."""
    branch, commit = request["branch"], receipt["commit"]
    contribution.ensure_clone(request["url"])
    remote = contribution.remote_head(branch)
    if remote == commit:
        return True
    if remote is not None and remote != receipt["base"] and not run_is_ancestor(contribution, remote, commit):
        raise Refusal("contribution_push_refused", "the remote branch %s moved to %s, which this contribution does not build on" % (branch, remote))
    contribution.git(["push", "--quiet", "origin", "%s:refs/heads/%s" % (commit, branch)])
    if contribution.remote_head(branch) != commit:
        raise RemoteError("the remote branch %s does not hold %s after the push" % (branch, commit))
    return True


def manifest_version(text):
    """metadata.version of a connector.yaml, read as manifest_name reads the name."""
    inside = False
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if not line[0].isspace():
            inside = line.rstrip() == "metadata:"
            continue
        if inside:
            key, _, value = line.strip().partition(":")
            if key == "version":
                return value.strip().strip("'\"")
    return None


def remote_tags(contribution, pattern):
    """The remote's tags that match pattern, by name, with the commit each names."""
    tags = {}
    for line in output(["ls-remote", "--tags", "origin", pattern], contribution.clone).splitlines():
        sha, _, ref = line.partition("\t")
        name = ref[len("refs/tags/"):]
        if name.endswith("^{}"):
            tags[name[:-3]] = sha
        else:
            tags.setdefault(name, sha)
    return tags


def candidate(contribution, request):
    """The release candidate of the pushed head: an existing tag that names it,
    or the next candidate number of the manifest's version. For the local Git
    host it is also validated and built with the library's own tooling at main,
    which the subagent cannot change, and its tag is pushed."""
    commit, module = request["commit"], request["module"]
    contribution.ensure_clone(request["url"])
    contribution.git(["fetch", "--quiet", "--no-tags", "origin", "+refs/heads/main:refs/remotes/origin/main"])
    contribution.git(["cat-file", "-e", commit + "^{commit}"])
    version = manifest_version(show(contribution, commit, module + "/connector.yaml") or "")
    if version is None or not STABLE_VERSION.match(version):
        raise Refusal("release_candidate_refused", "%s/connector.yaml declares no version vX.Y.Z" % module)
    existing = remote_tags(contribution, "refs/tags/%s/%s-rc.*" % (module, version))
    numbers = [int(name.rsplit("-rc.", 1)[1]) for name in existing if name.rsplit("-rc.", 1)[1].isdigit()]
    reused = [name for name, sha in existing.items() if sha == commit]
    tag = reused[0] if reused else "%s/%s-rc.%d" % (module, version, max(numbers, default=0) + 1)
    answer = {"directory": module, "version": tag.rsplit("/", 1)[1], "tag": tag, "commit": commit}
    if not request["local"]:
        return answer
    answer["assets"] = build_assets(contribution, request, answer)
    if not reused:
        contribution.git(["push", "--quiet", "origin", "%s:refs/tags/%s" % (commit, tag)])
        if remote_tags(contribution, "refs/tags/" + tag).get(tag) != commit:
            raise RemoteError("the tag %s does not name %s after the push" % (tag, commit))
    return answer


def build_assets(contribution, request, answer):
    """Builds the candidate's release assets as the library's Release Connector
    Candidate workflow does: main's connectorctl validates the candidate and
    writes its release metadata, and a Studio UI is built from the candidate.
    The official release tags of the connector come from the public library, so
    the validation sees its latest release."""
    module, commit = answer["directory"], answer["commit"]
    stable = [name for name in remote_tags_of(contribution, request["publicURL"], "refs/tags/%s/v*" % module)
              if STABLE_VERSION.match(name.rsplit("/", 1)[1])]
    if stable:
        contribution.git(["fetch", "--quiet", "--no-tags", request["publicURL"], *["refs/tags/%s:refs/tags/%s" % (name, name) for name in stable]], redirected=False)
    out = os.path.join(contribution.root, "candidates", contribution.id)
    tooling = os.path.join(contribution.root, "tooling", contribution.id)
    source = os.path.join(contribution.root, "candidate", contribution.id)
    shutil.rmtree(out, ignore_errors=True)
    os.makedirs(out)
    for path, revision in ((tooling, "refs/remotes/origin/main"), (source, commit)):
        contribution.remove_worktree(path)
        contribution.git(["worktree", "add", "--quiet", "--detach", path, revision])
    try:
        connectorctl = ["go", "run", "./cmd/connectorctl"]
        build(connectorctl + ["release-candidate", "--catalog", "catalog.yaml", "--candidate-root", source, "--directory", module,
                              "--commit", commit, "--version", answer["version"]], tooling, refusal=True)
        ui = []
        if os.path.exists(os.path.join(source, module, "ui", "package-lock.json")):
            for directory in (os.path.join(source, "sdk", "react"), os.path.join(source, module, "ui")):
                build(["npm", "ci"], directory)
                build(["npm", "run", "build"], directory)
            build(connectorctl + ["ui-artifact", "--manifest", os.path.join(source, module, "connector.yaml"),
                                  "--ui-root", os.path.join(source, module, "ui", "dist"),
                                  "--output", os.path.join(out, "connector-ui.tgz"),
                                  "--digest-output", os.path.join(out, "connector-ui.tgz.sha256")], tooling)
            ui = ["--ui-artifact", os.path.join(out, "connector-ui.tgz"), "--ui-digest", os.path.join(out, "connector-ui.tgz.sha256")]
        build(connectorctl + ["release-artifact", "--manifest", os.path.join(source, module, "connector.yaml"),
                              "--module-path", module_path(os.path.join(source, module, "go.mod")), "--version", answer["version"],
                              "--tag", answer["tag"], "--source-sha", commit,
                              "--output", os.path.join(out, "connector-release.json"),
                              "--digest-output", os.path.join(out, "connector-release.json.sha256"), *ui], tooling)
        with open(os.path.join(out, "connector-release.complete"), "w", encoding="utf-8") as marker:
            marker.write(answer["tag"] + "\n")
    finally:
        contribution.remove_worktree(tooling)
        contribution.remove_worktree(source)
    return {name: os.path.relpath(os.path.join(out, name), os.path.dirname(contribution.git_dir)) for name in sorted(os.listdir(out))}


def remote_tags_of(contribution, url, pattern):
    process = contribution.git(["ls-remote", "--tags", url, pattern], redirected=False)
    return [line.partition("\t")[2][len("refs/tags/"):] for line in process.stdout.decode().splitlines()
            if line and not line.endswith("^{}")]


def module_path(go_mod):
    with open(go_mod, encoding="utf-8") as file:
        for line in file:
            if line.startswith("module "):
                return line[len("module "):].strip()
    raise HelperError("%s declares no module" % go_mod)


def build(arguments, cwd, refusal=False):
    """Runs one build command of the library; a refused validation is a Refusal
    that names its reason, any other failure repeats when asked again."""
    environment = dict(os.environ)
    environment["GOWORK"] = "off"
    process = subprocess.run(arguments, cwd=cwd, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                             timeout=BUILD_TIMEOUT, check=False)
    if process.returncode != 0:
        message = (process.stderr or process.stdout).decode(errors="replace").strip()[-2000:]
        if refusal:
            raise Refusal("release_candidate_refused", message)
        raise HelperError("%s exited with %d: %s" % (" ".join(arguments[:3]), process.returncode, message))


def main():
    operation, workspace, agent_id = sys.argv[1], sys.argv[2], sys.argv[3]
    request = json.load(sys.stdin)
    if not SLUG.match(request.get("slug", "")):
        raise HelperError("the slug %r is not a connector ID" % request.get("slug"))
    contribution = Contribution(workspace, agent_id)
    try:
        answer = {"create": create, "finish": finish, "candidate": candidate}[operation](contribution, request)
    except Refusal as refusal:
        answer = {"refusal": refusal.code, "detail": refusal.detail}
    json.dump(answer, sys.stdout)


if __name__ == "__main__":
    try:
        main()
    except (RemoteError, subprocess.TimeoutExpired) as error:
        json.dump({"error": str(error)}, sys.stdout)
        sys.exit(1)
    except (HelperError, OSError, ValueError, KeyError, IndexError) as error:
        json.dump({"error": "%s: %s" % (type(error).__name__, error)}, sys.stdout)
        sys.exit(2)
