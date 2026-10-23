# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Subagent worktrees of a Session's workspace.

One process runs one operation for one subagent and answers one JSON object on
stdout. Exit code 0 is the answer; exit code 1 with {"error": ...} is a failure
of the helper that asking again may repair. Every operation queries before it
writes, so a repeated operation repeats nothing it finished:

  create   snapshot the workspace with a temporary index into the base ref and
           check the snapshot out as a detached worktree; hard-link the files
           of each input directory, ignored by Git, at the same path in it
  finish   save the worktree, without its input directories, into the result
           ref, count its changes and remove the checkout; the refs keep the
           result
  merge    merge the result into the workspace file by file (plan, writes of
           the files that still hold their planned pre-image, receipt), or
           discard it; a worktree that its finish could not save is saved
           first. A merge of selected paths has its own plan and receipt and
           leaves the other files mergeable; a discard or a merge of every
           file closes the changes, so every later call answers its receipt
  cleanup  remove refs and receipts of a closed attempt after Session state
           retained its final receipt or superseded the closed record
  read     read named files of the result ref, each within its byte limit,
           without a checkout
"""
import hashlib
import json
import os
import shutil
import stat
import subprocess
import sys
import tempfile

GIT_TIMEOUT = 300
COMMITTER = {
    "GIT_AUTHOR_NAME": "SuperVerse Agent", "GIT_AUTHOR_EMAIL": "agent@superdurable.dev",
    "GIT_COMMITTER_NAME": "SuperVerse Agent", "GIT_COMMITTER_EMAIL": "agent@superdurable.dev",
}
REGULAR, EXECUTABLE, SYMLINK = "100644", "100755", "120000"
BINARY_PROBE_BYTES = 8000


class HelperError(Exception):
    """A failure of the helper itself; the caller may ask again."""


def run(arguments, cwd, env=None, data=None, check=True):
    """Runs one Git command in the trusted environment of the caller. No command
    takes an optional lock, such as the index refresh of git status, so the
    helper never rewrites the workspace's index or holds its lock against a Git
    command of the main agent."""
    environment = dict(os.environ)
    environment["GIT_OPTIONAL_LOCKS"] = "0"
    environment.update(env or {})
    process = subprocess.run(["git", "-c", "safe.directory=*", "-c", "core.quotePath=false", *arguments], cwd=cwd,
                             env=environment, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                             timeout=GIT_TIMEOUT, check=False)
    if check and process.returncode != 0:
        message = process.stderr.decode(errors="replace").strip()[:2000]
        raise HelperError("git %s exited with %d: %s" % (arguments[0], process.returncode, message))
    return process


def output(arguments, cwd, env=None, data=None):
    return run(arguments, cwd, env, data).stdout.decode().strip()


def remove(path):
    if os.path.lexists(path) and not os.path.isdir(path):
        os.remove(path)
    elif os.path.isdir(path):
        shutil.rmtree(path)


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


class Agent:
    """The worktree, refs and records of one subagent."""

    def __init__(self, workspace, agent_id):
        self.workspace = workspace
        self.id = agent_id
        self.git_dir = output(["rev-parse", "--absolute-git-dir"], workspace)
        self.worktree = os.path.join(self.git_dir, "superverse", "worktrees", agent_id)
        self.directory = os.path.join(self.git_dir, "superverse", "agents", agent_id)
        self.base_ref = "refs/superverse/agents/%s/base" % agent_id
        self.result_ref = "refs/superverse/agents/%s/result" % agent_id

    def commit(self, ref):
        process = run(["rev-parse", "--verify", "--quiet", ref + "^{commit}"], self.workspace, check=False)
        return process.stdout.decode().strip() if process.returncode == 0 else None

    def create_ref(self, ref, commit):
        """Creates ref at commit unless it exists; a ref another attempt created wins."""
        if run(["update-ref", ref, commit, ""], self.workspace, check=False).returncode == 0:
            return commit
        existing = self.commit(ref)
        if existing is None:
            raise HelperError("could not create %s" % ref)
        return existing

    def registered(self):
        return os.path.realpath(self.worktree) in registered_worktrees(self.workspace)


def registered_worktrees(workspace):
    listed = output(["worktree", "list", "--porcelain"], workspace)
    return {os.path.realpath(line[len("worktree "):]) for line in listed.splitlines() if line.startswith("worktree ")}


def disk_free(path):
    status = os.statvfs(path)
    return status.f_bavail * status.f_frsize


def snapshot_bytes(workspace):
    """The size of the changed and untracked files that are not ignored."""
    listed = run(["status", "--porcelain=v1", "-z", "--untracked-files=all"], workspace).stdout.split(b"\0")
    total, index = 0, 0
    while index < len(listed):
        entry = listed[index]
        index += 1
        if len(entry) < 4:
            continue
        if entry[:1] in (b"R", b"C"):
            index += 1  # the source path of a rename or copy follows
        path = os.path.join(workspace, os.fsdecode(entry[3:]))
        if os.path.isfile(path) and not os.path.islink(path):
            total += os.path.getsize(path)
    return total


def save(agent, base):
    """Saves the worktree on top of base into the result ref, without the input
    directories, whatever the worktree's .gitignore says."""
    for name in read_json(os.path.join(agent.directory, "inputs.json")) or []:
        remove(os.path.join(agent.worktree, name))
    run(["add", "-A"], agent.worktree)
    tree = output(["write-tree"], agent.worktree)
    result = output(["commit-tree", tree, "-p", base, "-m", "Superverse subagent %s result" % agent.id], agent.worktree, COMMITTER)
    return agent.create_ref(agent.result_ref, result)


def remove_worktree(agent):
    if agent.registered():
        run(["worktree", "remove", "--force", agent.worktree], agent.workspace)
    remove(agent.worktree)
    run(["worktree", "prune"], agent.workspace)


def clean_orphans(agent, keep):
    """Saves and removes the worktrees of subagents that no longer run."""
    parent = os.path.dirname(agent.worktree)
    if not os.path.isdir(parent):
        return
    for name in sorted(os.listdir(parent)):
        if name in keep or name == agent.id:
            continue
        orphan = Agent(agent.workspace, name)
        base = orphan.commit(orphan.base_ref)
        if base is not None and orphan.commit(orphan.result_ref) is None and orphan.registered():
            save(orphan, base)
        remove_worktree(orphan)


def create(agent, request):
    inputs = request.get("inputs", [])
    for name in inputs:
        source = os.path.join(agent.workspace, name)
        if os.path.islink(source) or not os.path.isdir(source):
            return {"refusal": "input_missing", "detail": "the workspace holds no directory %s" % name}
        # An input Git does not ignore would enter the snapshot as ordinary files, and the result would delete it.
        ignored = run(["check-ignore", "-q", name.rstrip("/") + "/"], agent.workspace, check=False).returncode == 0
        if not ignored or output(["ls-files", "-z", "--", name], agent.workspace):
            return {"refusal": "input_missing", "detail": "Git does not ignore every file of the directory %s" % name}
    base = agent.commit(agent.base_ref)
    if base is None:
        clean_orphans(agent, set(request["keep"]))
        free = disk_free(agent.workspace)
        if free < request["minimumFreeBytes"]:
            return {"refusal": "workspace_disk_low", "detail": "%d bytes are free on the workspace volume" % free}
        size = snapshot_bytes(agent.workspace)
        if size > request["maximumSnapshotBytes"]:
            return {"refusal": "snapshot_too_large", "detail": "the uncommitted and untracked files hold %d bytes" % size}
        os.makedirs(agent.directory, exist_ok=True)
        index = os.path.join(agent.directory, "snapshot.index")
        remove(index)
        environment = {"GIT_INDEX_FILE": index}
        run(["read-tree", "HEAD"], agent.workspace, environment)
        run(["add", "-A"], agent.workspace, environment)
        tree = output(["write-tree"], agent.workspace, environment)
        commit = output(["commit-tree", tree, "-p", "HEAD", "-m", "Superverse subagent %s snapshot" % agent.id], agent.workspace, COMMITTER)
        base = agent.create_ref(agent.base_ref, commit)
        remove(index)
    # A subagent that already finished keeps its result and gets no new checkout.
    if not agent.registered() and agent.commit(agent.result_ref) is None:
        remove(agent.worktree)
        run(["worktree", "prune"], agent.workspace)
        os.makedirs(os.path.dirname(agent.worktree), exist_ok=True)
        run(["worktree", "add", "--detach", agent.worktree, base], agent.workspace)
    if agent.registered():
        link_inputs(agent, inputs)
    return {"base": base, "worktree": agent.worktree}


def link_inputs(agent, inputs):
    """Hard-links every file of each input directory of the workspace at the
    same path in the worktree, sharing the files' read-only inodes, and records
    the inputs, which save leaves out."""
    os.makedirs(agent.directory, exist_ok=True)
    write_atomically(os.path.join(agent.directory, "inputs.json"), inputs)
    for name in inputs:
        source = os.path.join(agent.workspace, name)
        target = os.path.join(agent.worktree, name)
        for directory, _, files in os.walk(source):
            relative = os.path.relpath(directory, source)
            os.makedirs(os.path.normpath(os.path.join(target, relative)), exist_ok=True)
            for file_name in files:
                original = os.path.join(directory, file_name)
                linked = os.path.normpath(os.path.join(target, relative, file_name))
                if os.path.lexists(linked) and os.path.samestat(os.lstat(linked), os.lstat(original)):
                    continue
                # Anything else at the path, such as a file the checkout wrote, gives way to the link.
                remove(linked)
                os.link(original, linked, follow_symlinks=False)


def numstat(agent, base, result):
    listed = run(["diff", "--numstat", "-z", "--no-renames", base, result], agent.workspace).stdout.decode()
    files = insertions = deletions = 0
    for record in listed.split("\0"):
        fields = record.split("\t")
        if len(fields) < 3:
            continue
        files += 1
        insertions += int(fields[0]) if fields[0].isdigit() else 0
        deletions += int(fields[1]) if fields[1].isdigit() else 0
    return {"files": files, "insertions": insertions, "deletions": deletions}


def finish(agent, _request):
    base = agent.commit(agent.base_ref)
    if base is None:
        return {"missing": True}
    result = agent.commit(agent.result_ref)
    if result is None:
        if not agent.registered():
            remove_worktree(agent)
            return {"base": base, "missing": True}
        result = save(agent, base)
    remove_worktree(agent)
    answer = {"base": base, "result": result}
    answer.update(numstat(agent, base, result))
    return answer


def cleanup(agent, request):
    """Forget a closed attempt; only explicit expiry may remove its checkout."""
    if agent.registered() and not request.get("removeCheckout", False):
        raise HelperError("cannot clean up a registered worktree")
    if request.get("removeCheckout", False):
        remove_worktree(agent)
    for ref in (agent.base_ref, agent.result_ref):
        if agent.commit(ref) is not None:
            run(["update-ref", "-d", ref], agent.workspace)
    if not request.get("removeCheckout", False):
        remove(agent.worktree)
        run(["worktree", "prune"], agent.workspace)
    remove(agent.directory)
    return {"cleaned": True}


def tree_entries(agent, old, new):
    """The files that differ between two trees: path -> ((old mode, old oid), (new mode, new oid))."""
    listed = run(["diff-tree", "-r", "-z", "--no-renames", "--no-commit-id", old, new], agent.workspace).stdout.split(b"\0")
    entries, index = {}, 0
    while index + 1 < len(listed):
        header, path = listed[index].decode(), os.fsdecode(listed[index + 1])
        index += 2
        fields = header.lstrip(":").split(" ")
        if len(fields) < 5:
            continue
        before = None if fields[0] == "000000" else (fields[0], fields[2])
        after = None if fields[1] == "000000" else (fields[1], fields[3])
        entries[path] = (before, after)
    return entries


def workspace_tree(agent):
    """The tree the workspace holds now, uncommitted and untracked files included."""
    os.makedirs(agent.directory, exist_ok=True)
    index = os.path.join(agent.directory, "merge.index")
    remove(index)
    environment = {"GIT_INDEX_FILE": index}
    run(["read-tree", "HEAD"], agent.workspace, environment)
    run(["add", "-A"], agent.workspace, environment)
    tree = output(["write-tree"], agent.workspace, environment)
    remove(index)
    return tree


def blob(agent, oid):
    return run(["cat-file", "blob", oid], agent.workspace).stdout


def is_text(agent, version):
    return version is not None and version[0] in (REGULAR, EXECUTABLE) and b"\0" not in blob(agent, version[1])[:BINARY_PROBE_BYTES]


def merge_text(agent, base, ours, theirs):
    """Merges three text versions with diff3 markers into a new blob: (oid, conflicts)."""
    with tempfile.TemporaryDirectory(dir=agent.directory) as directory:
        paths = []
        for name, version in (("ours", ours), ("base", base), ("theirs", theirs)):
            path = os.path.join(directory, name)
            with open(path, "wb") as file:
                file.write(blob(agent, version[1]))
            paths.append(path)
        process = run(["merge-file", "-p", "--diff3", "-L", "workspace", "-L", "snapshot", "-L", "agent-" + agent.id] + paths,
                      agent.workspace, check=False)
        if process.returncode < 0 or process.returncode > 127:
            raise HelperError("git merge-file failed: %s" % process.stderr.decode(errors="replace")[:2000])
        merged = output(["hash-object", "-w", "--stdin"], agent.workspace, data=process.stdout)
    return merged, process.returncode


def selects(name, path):
    """Whether the requested path name, a file or a directory, selects path."""
    return name == "." or path == name or path.startswith(name + "/")


def plan_merge(agent, base, result, paths):
    """Plans the merge of the changed files paths selects, all when it is None,
    and lists the requested paths that select no changed file."""
    theirs_changes = tree_entries(agent, base, result)
    ours_changes = tree_entries(agent, base, workspace_tree(agent))
    entries = []
    for path in sorted(theirs_changes):
        if paths is not None and not any(selects(name, path) for name in paths):
            continue
        before, theirs = theirs_changes[path]
        ours = ours_changes[path][1] if path in ours_changes else before
        entry = {"path": path, "pre": ours, "post": ours, "write": False}
        if ours == before:
            entry.update(post=theirs, write=True, result="applied")
        elif ours == theirs:
            entry["result"] = "unchanged"
        elif before is None:
            entry.update(result="conflict", conflict="add_add")
        elif ours is None or theirs is None:
            entry.update(result="conflict", conflict="delete_modify")
        elif not (is_text(agent, before) and is_text(agent, ours) and is_text(agent, theirs)):
            entry.update(result="conflict", conflict="binary")
        else:
            mode = theirs[0] if ours[0] == before[0] else ours[0]
            merged, conflicts = merge_text(agent, before, ours, theirs)
            entry.update(post=(mode, merged), write=True, result="conflict" if conflicts else "applied")
            if conflicts:
                entry["conflict"] = "content"
        entries.append(entry)
    unknown = [name for name in paths or () if not any(selects(name, path) for path in theirs_changes)]
    return {"base": base, "result": result, "entries": entries, "unknown": unknown}


def current_state(agent, path):
    """The (mode, oid) a workspace path holds now, or None when it is absent."""
    absolute = os.path.join(agent.workspace, path)
    if not os.path.lexists(absolute):
        return None
    status = os.lstat(absolute)
    if stat.S_ISLNK(status.st_mode):
        return (SYMLINK, output(["hash-object", "--stdin"], agent.workspace, data=os.fsencode(os.readlink(absolute))))
    if not stat.S_ISREG(status.st_mode):
        return ("directory", "")
    mode = EXECUTABLE if status.st_mode & 0o111 else REGULAR
    return (mode, output(["hash-object", "--", path], agent.workspace))


def write_version(agent, path, version):
    absolute = os.path.join(agent.workspace, path)
    if version is None:
        if os.path.lexists(absolute):
            os.remove(absolute)
        return
    os.makedirs(os.path.dirname(absolute) or agent.workspace, exist_ok=True)
    temporary = absolute + ".superverse-merge"
    remove(temporary)
    content = blob(agent, version[1])
    if version[0] == SYMLINK:
        os.symlink(os.fsdecode(content), temporary)
    else:
        with open(temporary, "wb") as file:
            file.write(content)
        os.chmod(temporary, 0o755 if version[0] == EXECUTABLE else 0o644)
    os.replace(temporary, absolute)


def as_version(value):
    return None if value is None else tuple(value)


def request_key(request, paths):
    """The key of a merge request's plan and receipt: a discard, a merge of every
    file, or a merge of the selected paths, keyed by their sorted list."""
    if request["action"] == "discard":
        return "discard"
    if paths is None:
        return "apply"
    return "apply-" + hashlib.sha256("\0".join(paths).encode()).hexdigest()[:16]


def merge(agent, request):
    # A discard or a merge of every file closes the subagent's changes, and
    # every later call answers its receipt.
    for closing in ("discard", "apply"):
        receipt = read_json(os.path.join(agent.directory, "receipt-%s.json" % closing))
        if receipt is not None:
            return receipt
    paths = request.get("paths")
    if paths is not None:
        paths = sorted({os.path.normpath(path) for path in paths})
    key = request_key(request, paths)
    receipt_path = os.path.join(agent.directory, "receipt-%s.json" % key)
    receipt = read_json(receipt_path)
    if receipt is not None:
        return receipt
    base, result = agent.commit(agent.base_ref), agent.commit(agent.result_ref)
    if base is not None and result is None and agent.registered():
        # The subagent ended, but its finish could not save the worktree,
        # which still holds its changes: save them as its finish would.
        result = save(agent, base)
        remove_worktree(agent)
    if base is None or result is None or request["action"] == "discard":
        # A discard drops the result; a subagent whose worktree was lost before
        # it was saved left nothing to merge.
        discard = request["action"] == "discard"
        receipt = {"outcome": "discarded" if discard else "nothing_to_merge", "applied": [], "conflicts": [], "unchanged": [],
                   "unknown": [] if discard else list(paths or [])}
        write_atomically(receipt_path, receipt)
        return receipt
    plan_path = os.path.join(agent.directory, "plan-%s.json" % key)
    plan = read_json(plan_path)
    if plan is None:
        plan = plan_merge(agent, base, result, paths)
        write_atomically(plan_path, plan)
    applied, conflicts, unchanged = [], [], []
    for entry in plan["entries"]:
        path, pre, post = entry["path"], as_version(entry["pre"]), as_version(entry["post"])
        if entry["write"]:
            state = current_state(agent, path)
            if state == pre and state != post:
                write_version(agent, path, post)
            elif state != post:
                conflicts.append({"path": path, "kind": "changed_during_merge"})
                continue
        if entry["result"] == "conflict":
            conflicts.append({"path": path, "kind": entry["conflict"]})
        elif entry["result"] == "unchanged":
            unchanged.append(path)
        else:
            applied.append(path)
    outcome = "merged_with_conflicts" if conflicts else ("merged" if applied else "nothing_to_merge")
    receipt = {"outcome": outcome, "applied": applied, "conflicts": conflicts, "unchanged": unchanged, "unknown": plan["unknown"]}
    write_atomically(receipt_path, receipt)
    return receipt


def read(agent, request):
    """Reads each requested path of the result ref as UTF-8 text: missing, over
    its limit, or not text is answered instead of its content."""
    result = agent.commit(agent.result_ref)
    files = {}
    for name, limit in sorted(request["files"].items()):
        if result is None:
            files[name] = {"missing": True}
            continue
        process = run(["cat-file", "-s", "%s:%s" % (result, name)], agent.workspace, check=False)
        if process.returncode != 0:
            files[name] = {"missing": True}
            continue
        size = int(process.stdout.decode().strip())
        if size > limit:
            files[name] = {"size": size, "too_large": True}
            continue
        data = run(["cat-file", "blob", "%s:%s" % (result, name)], agent.workspace).stdout
        try:
            files[name] = {"size": size, "content": data.decode("utf-8")}
        except UnicodeDecodeError:
            files[name] = {"size": size, "binary": True}
    return {"result": result or "", "files": files}


OPERATIONS = {"create": create, "finish": finish, "merge": merge, "cleanup": cleanup, "read": read}


def main():
    try:
        operation, workspace, agent_id = sys.argv[1], sys.argv[2], sys.argv[3]
        request = json.loads(sys.stdin.read() or "{}")
        answer = OPERATIONS[operation](Agent(workspace, agent_id), request)
    except (HelperError, OSError, ValueError, KeyError, IndexError, subprocess.TimeoutExpired) as error:
        json.dump({"error": "%s: %s" % (type(error).__name__, error)}, sys.stdout)
        return 1
    json.dump(answer, sys.stdout)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
