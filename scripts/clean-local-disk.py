#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Reclaim local disk that nothing uses, by fixed rules: the same state always gives the same result.

Every rule names what it removes, the condition and the protection; there is no judgement per run. Rules, in order:

1. Git worktrees of this repository that are clean, whose work reached origin/main (or whose PR is merged or closed)
   and that no process uses: `git worktree remove`.
2. Orca worktrees of this repository that are clean, whose branch's PR is merged, that Orca has not used and nothing
   changed for 24 hours, that are not pinned and that no process uses: `orca worktree rm`, so Orca forgets them too.
   Nothing else in an Orca worktree is touched.
3. In every other idle worktree, the regenerable `node_modules` and `.next` of Studio.
4. Dormant scratchpads (nothing in them changed within the age limit, no process uses them) and old `sv2-*` temporary
   directories.
5. Kind E2E artifacts older than the age limit.
6. The Go build caches (the default one and the Makefile's `/tmp/superverse-v2-go-cache`) above their size limit: files
   unused for 24 hours, then the whole cache when no `go` command runs; the npm cache above its size limit.
7. Docker: `sv2 local prune` (images no lane records, old build cache) and dangling images.
8. Native instances, always through `sv2 native delete`, which refuses an instance whose owner lock is held or that
   retains a run: a registered instance whose state directory and Docker resources are gone, when the registry has
   not changed for an hour (it only frees the topology slot); and a stopped instance other than `default` with no
   running container whose state has not changed for 3 days (this deletes its demo data). The other instances are
   listed with their age. The low-disk threshold never shortens these limits.
9. Browsers that Playwright launched and lost: every Chrome or Chromium process whose command line names a Playwright
   temporary profile (`playwright_chromiumdev_profile-*`, which a user's own Chrome never uses), when no ancestor is a
   Playwright process
   and it has run for an hour: SIGTERM, then SIGKILL after 5 seconds. Then the Playwright temporary profiles and
   artifact directories that no process uses and that have not changed for an hour.
Below the low-disk threshold the age limits shrink to one hour and the Go caches and module cache are cleared whole.

Usage: scripts/clean-local-disk.py [--dry-run]
"""
import argparse
import fcntl
import json
import os
import re
import shutil
import subprocess
import sys
import time
import tempfile
from pathlib import Path

SCRIPT_DIRECTORY = Path(__file__).resolve().parent
HOME = Path.home()
DATA_VOLUME = "/System/Volumes/Data"
LOCK_FILE = Path(tempfile.gettempdir()) / "superverse-clean-local-disk.lock"

GIB = 1024 ** 3
HOUR = 3600
LOW_DISK_BYTES = 40 * GIB
REPORT_BYTES = 100 * 1024 ** 2  # smaller removals are counted in the summary only
GO_CACHE_LIMIT_BYTES = 30 * GIB
GO_CACHE_UNUSED_HOURS = 24  # Go touches a cache file when it uses it, so the modification time is the last use
# The repository's Makefile builds and tests with this cache (GOCACHE ?= /tmp/superverse-v2-go-cache).
LANE_GO_CACHES = (Path(tempfile.gettempdir()) / "superverse-v2-go-cache", Path(tempfile.gettempdir()) / "superversev2-go-cache")
NPM_CACHE_LIMIT_BYTES = 5 * GIB

# Age limits in hours; `LOW_DISK_AGE_HOURS` replaces all of them below the low-disk threshold.
WORKTREE_IDLE_HOURS = 6
ORCA_WORKTREE_IDLE_HOURS = 24
REGENERABLE_IDLE_HOURS = 48
SCRATCH_HOURS = 72
KIND_E2E_HOURS = 72
LOW_DISK_AGE_HOURS = 1
# Native instances hold users' demo data, so their limits are fixed and the low-disk threshold never shortens them.
NATIVE_IDLE_DAYS = 3
NATIVE_REGISTRY_SETTLED_HOURS = 1
NATIVE_LABEL = "superdurable.io/native-instance"
# Playwright launches every browser with a temporary profile of this name and removes it when the browser closes.
PLAYWRIGHT_PROFILE = "playwright_chromiumdev_profile-"
PLAYWRIGHT_TEMPORARY = re.compile(r"^(playwright_chromiumdev_profile|playwright-artifacts)-\w+$")
PLAYWRIGHT_ORPHAN_HOURS = 1
# The executable of a Playwright browser process: Chrome, its helpers, Chromium or the headless shell.
BROWSER_EXECUTABLE = re.compile(r"(Chrom(e|ium)[^/]*|chrome-headless-shell)$")

REGENERABLE_DIRECTORIES = ("web/studio/node_modules", "web/studio/.next")
SCRATCH_ROOT = Path(tempfile.gettempdir()) / f"claude-{os.getuid()}"
REMOVABLE_WORKTREE_ROOTS = (SCRATCH_ROOT, HOME / "Documents" / "superdurable" / "superversev2" / ".claude" / "worktrees")
TEMPORARY_ROOT = Path(tempfile.gettempdir())
TEMPORARY_PATTERN = re.compile(r"^sv2-[\w.-]+$")
# Orca runs its workers here and a worktree can look idle between steps: only a merged, long-idle one is removed, by Orca.
ORCA_WORKSPACES = HOME / "orca" / "workspaces"
# Commands that read the Go cache or the worktrees' Studio directories while they run.
BUILD_PATTERN = r"^\S*(go (build|test|run|vet|install)|make (ci|test|check|generate)|npm (ci|install|run)|playwright test)\b|sv2 (native|kind) (e2e|build|deploy)"
# Work in progress that must never be cleaned, whatever its state: the remote agent (BYOA) worktrees and runs.
PROTECTED_NAMES = ("remote-agent",)
# Only worktrees under these roots are removed whole; the tools that manage other worktrees (Orca, Codex) own them, so
# only their regenerable directories are removed.
def main_checkout():
    """The repository's main checkout, wherever this script runs from: the first entry of its worktree list."""
    listing = subprocess.run(["git", "worktree", "list", "--porcelain"], cwd=SCRIPT_DIRECTORY, capture_output=True, text=True, check=True).stdout
    return Path(listing.splitlines()[0].removeprefix("worktree "))


REPOSITORY = main_checkout()
NATIVE_INSTANCES = REPOSITORY / ".local" / "superverse-v2" / "native-instances.json"
KIND_E2E_ROOT = REPOSITORY / ".local" / "superverse-v2" / "kind-e2e"
SV2_PYTHON_CANDIDATES = (
    REPOSITORY / ".local" / "superverse-v2" / "python-venv" / "bin" / "python3",
    REPOSITORY / ".venv" / "bin" / "python3",
)


def run(command, cwd=None, timeout=900):
    """The completed process; a missing tool or a timeout is a failed process, never an exception."""
    try:
        return subprocess.run(command, cwd=cwd, capture_output=True, text=True, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired) as error:
        return subprocess.CompletedProcess(command, 127, "", str(error))


def sv2_python():
    """A Python with the infra-as-code requirements that runs `infra-as-code/sv2`, or None."""
    return next((str(candidate) for candidate in SV2_PYTHON_CANDIDATES if candidate.exists() and run([str(candidate), "-c", "import yaml"]).returncode == 0), None)


def free_bytes():
    return shutil.disk_usage(DATA_VOLUME).free


def size_bytes(path):
    result = run(["du", "-sk", str(path)], timeout=600)
    return int(result.stdout.split()[0]) * 1024 if result.returncode == 0 and result.stdout.split() else 0


def modified_seconds_ago(path):
    """Seconds since the newest change among the path and its direct entries; a missing path counts as brand new."""
    try:
        newest = os.lstat(path).st_mtime
        if path.is_dir() and not path.is_symlink():
            with os.scandir(path) as entries:
                newest = max([newest] + [entry.stat(follow_symlinks=False).st_mtime for entry in entries])
        return time.time() - newest
    except OSError:
        return 0.0


def gib(value):
    return f"{value / GIB:.1f} GiB"


class Process:
    """The paths that running processes use: working directories and command lines."""

    def __init__(self):
        lsof = run(["lsof", "-d", "cwd", "-Fn"], timeout=120).stdout
        self.directories = [line[1:] for line in lsof.splitlines() if line.startswith("n/")]
        self.commands = run(["ps", "-axo", "args="], timeout=60).stdout

    def uses(self, path):
        text = str(path)
        return any(directory == text or directory.startswith(text + "/") for directory in self.directories) or text in self.commands

    def running(self, pattern):
        return re.search(pattern, self.commands, re.MULTILINE) is not None


class Cleaner:
    def __init__(self, dry_run):
        self.dry_run = dry_run
        self.processes = Process()
        self.low_disk = free_bytes() < LOW_DISK_BYTES
        self.freed = []
        self.pull_requests = {}
        self.building = self.processes.running(BUILD_PATTERN)

    def hours(self, normal):
        return LOW_DISK_AGE_HOURS if self.low_disk else normal

    def remove(self, path, reason):
        size = size_bytes(path)
        self.freed.append((reason, str(path), size))
        if size >= REPORT_BYTES:
            print(f"{'would remove' if self.dry_run else 'remove'} {path} ({gib(size)}): {reason}")
        if self.dry_run:
            return
        if path.is_symlink() or path.is_file():
            path.unlink(missing_ok=True)
        else:
            shutil.rmtree(path, ignore_errors=True)

    def command(self, description, command, **options):
        print(f"{'would run' if self.dry_run else 'run'} {description}")
        if self.dry_run:
            return ""
        result = run(command, **options)
        if result.returncode != 0:
            print(f"  failed ({result.returncode}): {(result.stderr or result.stdout).strip()[-200:]}")
        return result.stdout

    # -- worktrees -----------------------------------------------------------------------------------------------

    def worktrees(self):
        listing = run(["git", "worktree", "list", "--porcelain"], cwd=REPOSITORY).stdout
        found = []
        for block in listing.split("\n\n"):
            fields = dict(line.split(" ", 1) for line in block.splitlines() if " " in line)
            if "worktree" in fields:
                found.append((Path(fields["worktree"]), fields.get("branch", "").removeprefix("refs/heads/"), fields.get("HEAD", "")))
        return [entry for entry in found if entry[0] != REPOSITORY]

    def pull_request_state(self, branch):
        """The state of the branch's newest PR (OPEN, MERGED, CLOSED), or "" when it has none or gh fails."""
        if branch not in self.pull_requests:
            result = run(["gh", "pr", "list", "--head", branch, "--state", "all", "--json", "state", "--jq", ".[0].state"], cwd=REPOSITORY, timeout=60)
            self.pull_requests[branch] = result.stdout.strip() if result.returncode == 0 else ""
        return self.pull_requests[branch]

    def work_is_on_main(self, branch, head):
        merged = run(["git", "merge-base", "--is-ancestor", head, "origin/main"], cwd=REPOSITORY).returncode == 0
        return merged or (bool(branch) and self.pull_request_state(branch) in ("MERGED", "CLOSED"))

    def orca_worktrees(self):
        """Orca's records of this repository's worktrees by path; empty when Orca cannot be asked, so nothing is removed."""
        result = run(["orca", "worktree", "list", "--json", "--repo", f"path:{REPOSITORY}"], timeout=60)
        try:
            return {entry["path"]: entry for entry in json.loads(result.stdout)["result"]["worktrees"]}
        except (ValueError, KeyError, TypeError):
            return {}

    def sweep_orca_worktree(self, path, branch, record):
        if not record or record.get("isPinned") or not branch:
            return
        hours = ORCA_WORKTREE_IDLE_HOURS
        if time.time() - record.get("lastActivityAt", time.time() * 1000) / 1000 <= hours * HOUR or not self.is_idle(path, hours):
            return
        if run(["git", "status", "--porcelain"], cwd=path).stdout.strip() != "" or self.pull_request_state(branch) != "MERGED":
            return
        size = size_bytes(path)
        self.freed.append(("merged idle Orca worktree", str(path), size))
        print(f"{'would remove' if self.dry_run else 'remove'} Orca worktree {path} ({gib(size)}): clean, PR merged, idle {hours} h")
        if not self.dry_run:
            result = run(["orca", "worktree", "rm", "--worktree", f"path:{path}", "--json"], timeout=300)
            if result.returncode != 0:
                print(f"  failed ({result.returncode}): {(result.stderr or result.stdout).strip()[-200:]}")

    def is_idle(self, path, hours):
        return not self.processes.uses(path) and modified_seconds_ago(path) > hours * HOUR

    def sweep_worktrees(self):
        run(["git", "fetch", "-q", "origin", "main"], cwd=REPOSITORY, timeout=120)
        orca = None
        for path, branch, head in self.worktrees():
            if not path.exists() or any(name in str(path) or name in branch for name in PROTECTED_NAMES):
                continue
            if ORCA_WORKSPACES in path.parents:
                orca = self.orca_worktrees() if orca is None else orca
                self.sweep_orca_worktree(path, branch, orca.get(str(path)))
                continue
            if not any(root in path.parents for root in REMOVABLE_WORKTREE_ROOTS):
                self.sweep_regenerable(path)
                continue
            clean = run(["git", "status", "--porcelain"], cwd=path).stdout.strip() == ""
            if clean and self.is_idle(path, self.hours(WORKTREE_IDLE_HOURS)) and self.work_is_on_main(branch, head):
                size = size_bytes(path)
                self.freed.append(("merged idle worktree", str(path), size))
                print(f"{'would remove' if self.dry_run else 'remove'} worktree {path} ({gib(size)}): clean, on main, idle")
                if not self.dry_run:
                    run(["git", "worktree", "remove", "--force", str(path)], cwd=REPOSITORY)
            else:
                self.sweep_regenerable(path)
        if not self.dry_run:
            run(["git", "worktree", "prune"], cwd=REPOSITORY)

    def sweep_regenerable(self, worktree):
        if not self.is_idle(worktree, self.hours(REGENERABLE_IDLE_HOURS)):
            return
        for relative in REGENERABLE_DIRECTORIES:
            directory = worktree / relative
            if directory.is_dir() and not directory.is_symlink():
                self.remove(directory, "regenerable directory of an idle worktree")

    # -- temporary files -------------------------------------------------------------------------------------------

    def sweep_scratch(self):
        """A scratchpad no process uses and whose newest direct entry is older than the age limit is dormant: its session
        is over, so every entry goes except a registered worktree."""
        registered = {str(path) for path, _, _ in self.worktrees()}
        for project in SCRATCH_ROOT.glob("*/") if SCRATCH_ROOT.is_dir() else []:
            for scratchpad in (session / "scratchpad" for session in project.glob("*/")):
                if not scratchpad.is_dir() or self.processes.uses(scratchpad):
                    continue
                if modified_seconds_ago(scratchpad) <= self.hours(SCRATCH_HOURS) * HOUR:
                    continue
                for entry in scratchpad.iterdir():
                    if str(entry) not in registered and not self.processes.uses(entry):
                        self.remove(entry, "entry of a dormant scratchpad")
        for entry in TEMPORARY_ROOT.iterdir():
            if TEMPORARY_PATTERN.match(entry.name) and not self.processes.uses(entry):
                if modified_seconds_ago(entry) > self.hours(SCRATCH_HOURS) * HOUR:
                    self.remove(entry, "old temporary directory")

    def sweep_kind_e2e(self):
        for entry in KIND_E2E_ROOT.glob("*/") if KIND_E2E_ROOT.is_dir() else []:
            if not self.processes.uses(entry) and modified_seconds_ago(entry) > self.hours(KIND_E2E_HOURS) * HOUR:
                self.remove(entry, "old Kind E2E artifacts")

    # -- caches and Docker -----------------------------------------------------------------------------------------

    def remove_unused_files(self, directory, hours):
        """Remove the cache files that nothing used for the given hours; Go treats a missing entry as a cache miss."""
        cutoff = time.time() - hours * HOUR
        removed = 0
        for parent, _, names in os.walk(directory):
            for name in names:
                path = os.path.join(parent, name)
                try:
                    if os.lstat(path).st_mtime < cutoff:
                        removed += os.lstat(path).st_size
                        if not self.dry_run:
                            os.unlink(path)
                except OSError:
                    continue
        self.freed.append(("unused Go cache files", str(directory), removed))
        print(f"{'would remove' if self.dry_run else 'remove'} {gib(removed)} of Go cache files unused for {hours} h in {directory}")

    def sweep_go_caches(self):
        default = Path(run(["go", "env", "GOCACHE"]).stdout.strip() or HOME / "Library/Caches/go-build")
        for cache in (default, *LANE_GO_CACHES):
            if not cache.is_dir() or size_bytes(cache) <= GO_CACHE_LIMIT_BYTES:
                continue
            self.remove_unused_files(cache, self.hours(GO_CACHE_UNUSED_HOURS))
            if self.low_disk and size_bytes(cache) > GO_CACHE_LIMIT_BYTES and not self.building and not self.dry_run:
                self.freed.append(("Go build cache", str(cache), size_bytes(cache)))
                self.command(f"go clean -cache in {cache}", ["env", f"GOCACHE={cache}", "go", "clean", "-cache"], timeout=3600)

    def sweep_caches(self):
        self.sweep_go_caches()
        npm = HOME / ".npm" / "_cacache"
        if npm.is_dir() and size_bytes(npm) > NPM_CACHE_LIMIT_BYTES and not self.processes.running(r"\bnpm (ci|install)\b"):
            self.freed.append(("npm cache", str(npm), size_bytes(npm)))
            self.command("npm cache clean --force", ["npm", "cache", "clean", "--force"], timeout=900)
        if self.low_disk and not self.building:
            self.command("go clean -modcache (low disk)", ["go", "clean", "-modcache"], timeout=1800)

    def sweep_docker(self):
        if run(["docker", "info"], timeout=30).returncode != 0:
            print("docker is not reachable; skipped")
            return
        python = sv2_python()
        if python:
            flags = ["--dry-run"] if self.dry_run else []
            result = run([python, str(REPOSITORY / "infra-as-code" / "sv2"), "local", "prune", *flags], cwd=REPOSITORY, timeout=1800)
            print("sv2 local prune: " + (result.stdout or result.stderr).strip().splitlines()[-1] if (result.stdout or result.stderr).strip() else "sv2 local prune: no output")
        else:
            print("sv2 local prune skipped: no Python environment with the infra-as-code requirements")
        self.command("docker image prune -f (dangling images)", ["docker", "image", "prune", "-f"], timeout=600)

    # -- Native instances ------------------------------------------------------------------------------------------

    def sweep_native_instances(self):
        try:
            instances = json.loads(NATIVE_INSTANCES.read_text())
        except (OSError, ValueError):
            return
        python = sv2_python()
        docker = run(["docker", "ps", "--all", "--format", "{{.State}} {{.Label \"" + NATIVE_LABEL + "\"}}"], timeout=30)
        if docker.returncode != 0 or not python:
            print("native instances skipped: docker or the infra-as-code Python environment is unavailable")
            return
        states = [line.split(" ", 1) for line in docker.stdout.splitlines() if " " in line]
        running = {name for state, name in states if state == "running"}
        registry_settled = modified_seconds_ago(NATIVE_INSTANCES) > NATIVE_REGISTRY_SETTLED_HOURS * HOUR
        for name in sorted(instances):
            root = REPOSITORY / ".local" / "superverse-v2" / ("native" if name == "default" else f"native-{name}")
            if name != "default" and not root.exists() and registry_settled and not self.has_native_resources(name):
                self.delete_native_instance(python, name, "its state and Docker resources are gone; frees its topology slot")
            elif name != "default" and root.exists() and name not in running and modified_seconds_ago(root) > NATIVE_IDLE_DAYS * 86400:
                self.delete_native_instance(python, name, f"stopped and unchanged for {modified_seconds_ago(root) / 86400:.1f} days")
            else:
                age = f"{modified_seconds_ago(root) / 86400:.1f} days since the last change" if root.exists() else "no state directory"
                print(f"native instance {name}: {'running' if name in running else 'stopped'}, {age} (kept)")

    def has_native_resources(self, name):
        listings = (["docker", "ps", "--all"], ["docker", "volume", "ls"], ["docker", "network", "ls"])
        return any(run([*listing, "--quiet", "--filter", f"label={NATIVE_LABEL}={name}"], timeout=60).stdout.strip() for listing in listings)

    def delete_native_instance(self, python, name, reason):
        self.freed.append(("Native instance", name, 0))
        print(f"{'would delete' if self.dry_run else 'delete'} native instance {name}: {reason}")
        if self.dry_run:
            return
        result = run([python, str(REPOSITORY / "infra-as-code" / "sv2"), "native", "delete", "--instance", name, "--confirm", name], cwd=REPOSITORY, timeout=1800)
        print("  " + ((result.stdout or result.stderr).strip().splitlines() or ["no output"])[-1])


    # -- Playwright browsers ---------------------------------------------------------------------------------------

    def sweep_playwright_browsers(self):
        table = {}
        for line in run(["ps", "-axo", "pid=,ppid=,etime=,args="], timeout=60).stdout.splitlines():
            fields = line.split(None, 3)
            if len(fields) == 4:
                table[int(fields[0])] = (int(fields[1]), elapsed_seconds(fields[2]), fields[3])
        executables = dict(line.strip().split(" ", 1) for line in run(["ps", "-axo", "pid=,comm="], timeout=60).stdout.splitlines() if " " in line.strip())

        def launched_by_playwright(pid):
            parent, seen = table[pid][0], set()
            while parent in table and parent not in seen and parent > 1:
                seen.add(parent)
                if "playwright" in table[parent][2] and PLAYWRIGHT_PROFILE not in table[parent][2]:
                    return True
                parent = table[parent][0]
            return False
        orphans = [pid for pid, (_, seconds, args) in table.items()
                   if PLAYWRIGHT_PROFILE in args and BROWSER_EXECUTABLE.search(executables.get(str(pid), "")) and seconds > PLAYWRIGHT_ORPHAN_HOURS * HOUR and not launched_by_playwright(pid)]
        if orphans:
            self.freed.append(("orphaned Playwright browser processes", str(len(orphans)), 0))
            print(f"{'would stop' if self.dry_run else 'stop'} {len(orphans)} Playwright browser processes whose test runner is gone")
            if not self.dry_run:
                run(["kill", "-TERM", *map(str, orphans)], timeout=30)
                time.sleep(5)
                run(["kill", "-KILL", *map(str, orphans)], timeout=30)
        for path in TEMPORARY_ROOT.iterdir() if TEMPORARY_ROOT.is_dir() else ():
            if PLAYWRIGHT_TEMPORARY.match(path.name) and modified_seconds_ago(path) > PLAYWRIGHT_ORPHAN_HOURS * HOUR and not self.processes.uses(path):
                self.remove(path, "Playwright temporary directory no process uses")


def elapsed_seconds(text):
    """Seconds of a ps elapsed time, [[dd-]hh:]mm:ss."""
    days, _, clock = text.rpartition("-")
    seconds = 0
    for part in clock.split(":"):
        seconds = seconds * 60 + int(part)
    return seconds + int(days or 0) * 86400


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--dry-run", action="store_true", help="list what would be removed and remove nothing")
    args = parser.parse_args()
    with open(LOCK_FILE, "w") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            print("another clean-local-disk run holds the lock; skipped")
            return 0
        before = free_bytes()
        cleaner = Cleaner(args.dry_run)
        if cleaner.low_disk:
            print(f"free space {gib(before)} is under {gib(LOW_DISK_BYTES)}: the age limits shrink to {LOW_DISK_AGE_HOURS} h")
        cleaner.sweep_worktrees()
        cleaner.sweep_scratch()
        cleaner.sweep_kind_e2e()
        cleaner.sweep_caches()
        cleaner.sweep_docker()
        cleaner.sweep_native_instances()
        cleaner.sweep_playwright_browsers()
        after = free_bytes()
        print(f"free {gib(before)} -> {gib(after)}; {len(cleaner.freed)} removals{' (dry run, nothing removed)' if args.dry_run else ''}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
