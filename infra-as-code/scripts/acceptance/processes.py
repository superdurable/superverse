# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Exact owned process identities and bounded cleanup.

A process is identified by its PID, process group and start time, never by name, port or
parent alone, so a reused PID is never signalled.
"""
import os
import signal
import subprocess
import time


def process_identity(pid):
    result = subprocess.run(["ps", "-p", str(pid), "-o", "pid=,pgid=,lstart="], text=True, capture_output=True, timeout=5)
    values = result.stdout.strip().split(None, 2) if result.returncode == 0 else []
    return {"pid": int(values[0]), "group": int(values[1]), "started": values[2]} if len(values) == 3 else None


def group_members(group):
    observer = subprocess.Popen(["ps", "-axo", "pid=,pgid=,lstart="], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        output, _ = observer.communicate(timeout=5)
    except subprocess.TimeoutExpired:
        observer.kill()
        observer.wait()
        raise RuntimeError("Process-group inspection timed out") from None
    if observer.returncode:
        raise RuntimeError("Process-group inspection failed")
    return [{"pid": int(v[0]), "group": int(v[1]), "started": v[2].strip()} for row in output.splitlines()
            if len(v := row.split(None, 2)) == 3 and int(v[1]) == group and int(v[0]) != observer.pid]


def stop_group(record, *, owner_pid=None, processes=()):
    """TERM, then KILL, every member of the owner's group until none remain."""
    group = record["ownerProcess"]["group"]
    for sig, seconds in ((signal.SIGTERM, 20), (signal.SIGKILL, 5)):
        deadline = time.monotonic() + seconds
        while True:
            current_owner = process_identity(record["ownerProcess"]["pid"])
            if current_owner and current_owner != record["ownerProcess"]:
                raise RuntimeError("Owned group leader PID was reused; refusing to signal any process")
            for process in processes:
                process.poll()  # Reap direct children before proving they exited.
            members = [p for p in group_members(group) if p["pid"] != owner_pid]
            if not members:
                return
            for identity in members:
                if process_identity(identity["pid"]) == identity:
                    try:
                        os.kill(identity["pid"], sig)
                    except ProcessLookupError:
                        pass
            if time.monotonic() >= deadline:
                break
            time.sleep(0.2)
    raise RuntimeError("Owned subprocesses have not all exited; mode remains fenced")


def remember_browser_children(known):
    """Add live descendants of known processes to `known` and drop exited ones."""
    try:
        result = subprocess.run(["ps", "-axo", "pid=,ppid=,pgid=,lstart="], text=True, capture_output=True, check=True, timeout=5).stdout
    except subprocess.SubprocessError:
        raise RuntimeError("Browser process inspection failed; cleanup is unproven") from None
    table = {}
    for row in result.splitlines():
        parts = row.split(None, 3)
        if len(parts) == 4:
            table[int(parts[0])] = {"pid": int(parts[0]), "parent": int(parts[1]), "group": int(parts[2]), "started": parts[3].strip()}

    def identity(item):
        return {key: item[key] for key in ("pid", "group", "started")}
    # Pinned Playwright gives Chromium its own process group, so follow exact parents instead.
    while True:
        additions = {pid: identity(child) for pid, child in table.items()
                     if pid not in known and child["parent"] in known and child["parent"] in table and identity(table[child["parent"]]) == known[child["parent"]]}
        if not additions:
            break
        known.update(additions)
        if len(known) > 256:
            raise RuntimeError("Browser descendant identity budget exceeded")
    for pid in list(known):
        if pid not in table:
            del known[pid]


def stop_browser_children(known):
    deadline = time.monotonic() + 10
    while True:
        remaining = []
        for identity in known.values():
            if time.monotonic() >= deadline:
                raise RuntimeError("Browser identity inspection deadline exceeded; manual reconciliation required")
            if process_identity(identity["pid"]) == identity:
                remaining.append(identity)
        if not remaining:
            return
        if time.monotonic() >= deadline:
            raise RuntimeError("Observed detached browser children did not exit; manual reconciliation required")
        for identity in remaining:
            if process_identity(identity["pid"]) == identity:
                try:
                    os.kill(identity["pid"], signal.SIGKILL)
                except ProcessLookupError:
                    pass
        time.sleep(0.1)
