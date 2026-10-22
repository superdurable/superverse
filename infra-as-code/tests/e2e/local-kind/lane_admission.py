#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Exercise lane admission through real Git, files, OS locks, processes and the CLI; start no lane."""
import json
import os
from pathlib import Path
import select
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[4]


def check(source=ROOT):
    with tempfile.TemporaryDirectory(prefix="sv2-lane-admission-") as temporary:
        checkout = Path(temporary)
        infra = checkout / "infra-as-code"
        shutil.copytree(source / "infra-as-code/scripts", infra / "scripts", ignore=shutil.ignore_patterns("__pycache__"))
        shutil.copy(source / "infra-as-code/source-lock.json", infra / "source-lock.json")
        shutil.copy(source / "infra-as-code/sv2", infra / "sv2")
        subprocess.run(["git", "init", "-q", checkout], check=True)
        state = checkout / ".local/superverse-v2"
        environment = dict(os.environ, PYTHONDONTWRITEBYTECODE="1", PYTHONPATH=str(infra / "scripts"),
                           SUPERVERSE_LANE_OWNER="admission-a", SUPERVERSE_LANE_WORKER="worker")

        def command(*arguments, owner="admission-a", success=True):
            result = subprocess.run([sys.executable, infra / "sv2", *arguments], env=dict(environment, SUPERVERSE_LANE_OWNER=owner),
                                    text=True, capture_output=True, timeout=15)
            assert (result.returncode == 0) == success, (arguments, result.stdout, result.stderr)
            return result.stdout if success else result.stderr

        def helper(code, *, owner="admission-a"):
            return subprocess.run([sys.executable, "-c", code], env=dict(environment, SUPERVERSE_LANE_OWNER=owner),
                                  text=True, capture_output=True, check=True, timeout=15).stdout

        first = json.loads(command("kind", "queue", "join"))
        assert first["position"] == 1
        assert json.loads(command("kind", "queue", "join"))["ticket"] == first["ticket"]
        second = json.loads(command("kind", "queue", "join", owner="admission-b"))
        assert second["position"] == 2
        assert "owned" in command("kind", "queue", "leave", "--ticket", first["ticket"], owner="admission-b", success=False)
        code = 'from kind.locks import kind_action_lock\nwith kind_action_lock("up", ' + repr(second["ticket"]) + '): pass'
        result = subprocess.run([sys.executable, "-c", code], env=dict(environment, SUPERVERSE_LANE_OWNER="admission-b"),
                                text=True, capture_output=True, timeout=15)
        assert result.returncode and "head ticket" in result.stderr
        assert not (state / "state.json").exists()  # No cluster ownership was created.
        (state / "kind-e2e").mkdir()
        fence = state / "kind-e2e/e2e-admission.json"
        fence.write_text('{}')
        code = 'from kind.locks import kind_action_lock\nwith kind_action_lock("up", ' + repr(first["ticket"]) + '): pass'
        result = subprocess.run([sys.executable, "-c", code], env=environment, text=True, capture_output=True, timeout=15)
        assert result.returncode and "reconciliation" in result.stderr and fence.exists()
        fence.unlink()
        hold = code.replace(': pass', ':\n import time\n print("ready", flush=True)\n time.sleep(30)')
        child = subprocess.Popen([sys.executable, "-c", hold], env=environment, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            assert select.select([child.stdout], [], [], 10)[0] and child.stdout.readline().strip() == "ready"
            assert "active command" in command("kind", "queue", "leave", "--ticket", first["ticket"], success=False)
            result = subprocess.run([sys.executable, "-c", code], env=environment, text=True, capture_output=True, timeout=15)
            assert result.returncode and "action lock" in result.stderr
            queue_path = state / "kind-queue.json"
            records = json.loads(queue_path.read_text())
            records[0]["updatedAt"] = time.time() - 7 * 3600
            queue_path.write_text(json.dumps(records))
            assert json.loads(command("kind", "queue", "status"))["entries"][0]["active"]
        finally:
            child.terminate()
            child.wait(timeout=10)
            child.stdout.close()
            child.stderr.close()
        # Dead exact process no longer pins an expired head; FIFO advances.
        status = json.loads(command("kind", "queue", "status"))
        assert [entry["ticket"] for entry in status["entries"]] == [second["ticket"]]
        assert queue_path.stat().st_mode & 0o777 == 0o600
        command("kind", "queue", "leave", "--ticket", second["ticket"], owner="admission-b")
        assert not json.loads(command("kind", "queue", "status"))["entries"]
        # An abandoned head expires before its six-hour waiting lease; promotion
        # grants a fresh head window even when the next ticket waited longer.
        head = json.loads(command("kind", "queue", "join"))
        waiter = json.loads(command("kind", "queue", "join", owner="admission-b"))
        records = json.loads(queue_path.read_text())
        records[0].update(headSince=time.time()-1900, updatedAt=time.time()-1900)
        records[1]["updatedAt"] = time.time()-3600
        queue_path.write_text(json.dumps(records))
        status = json.loads(command("kind", "queue", "status"))
        assert [entry["ticket"] for entry in status["entries"]] == [waiter["ticket"]]
        promoted = status["entries"][0]
        assert promoted["headSince"] > time.time()-10 and promoted["headIdleDeadline"] > time.time()+1700
        # Join renews the head's idle window without resetting FIFO headSince.
        records = json.loads(queue_path.read_text())
        records[0].update(headSince=time.time()-1700, updatedAt=time.time()-1700)
        queue_path.write_text(json.dumps(records))
        renewed = json.loads(command("kind", "queue", "join", owner="admission-b"))
        assert renewed["ticket"] == waiter["ticket"] and renewed["headIdleDeadline"] > time.time()+1700
        assert renewed["headSince"] < time.time()-1600
        command("kind", "queue", "leave", "--ticket", waiter["ticket"], owner="admission-b")
        code = '''from types import SimpleNamespace
from paths import STATE
from native.owner import native_admission, retain
from acceptance.processes import process_identity
import os, time
instance = SimpleNamespace(name="test", root=STATE / "native-test")
with native_admission(instance) as owner:
 retain(instance, {"runId":"run-test", "laneOwner":owner, "startedAt":time.time()-10800, "ownerProcess":process_identity(os.getpid())})
'''
        helper(code)
        code = '''from types import SimpleNamespace
from native.owner import native_admission
with native_admission(SimpleNamespace(name="another")): pass
'''
        result = subprocess.run([sys.executable, "-c", code], env=dict(environment, SUPERVERSE_LANE_WORKER="another-worker"), text=True, capture_output=True, timeout=15)
        assert result.returncode and "one instance: test" in result.stderr
        helper(code, owner="admission-b")
        idle = json.loads(command("native", "idle", "--hours", "2"))["instances"]
        assert len(idle) == 1 and idle[0]["instance"] == "test" and not idle[0]["ownerRunning"]
        assert (state / "native-test/owner.json").exists()  # Report never stops or releases it.
        (state / "native-test").rename(state / "native")
        assert json.loads(command("native", "idle", "--hours", "2"))["instances"][0]["instance"] == "default"
        hold = '''from paths import STATE
from private_files import private_lock
import time
with private_lock(STATE / "native-cap.lock", message="busy"):
 print("ready", flush=True)
 time.sleep(30)
'''
        child = subprocess.Popen([sys.executable, "-c", hold], env=environment, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            assert select.select([child.stdout], [], [], 10)[0] and child.stdout.readline().strip() == "ready"
            result = subprocess.run([sys.executable, "-c", code], env=dict(environment, SUPERVERSE_LANE_OWNER="admission-b"),
                                    text=True, capture_output=True, timeout=15)
            assert result.returncode and "reserving owner capacity" in result.stderr
        finally:
            child.terminate()
            child.wait(timeout=10)
            child.stdout.close()
            child.stderr.close()
        print("Lane admission: FIFO, duplicate join, owner checks, real action lock, fence, expiry, process pin, head idle expiry/renewal/promotion, cap and idle report passed")


if __name__ == "__main__":
    check()
