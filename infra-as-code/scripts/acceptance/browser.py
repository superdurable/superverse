# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The same real browser cases, private reports and owned cleanup for both local lanes.

CASE_TIMEOUTS is the one registry of acceptance cases; release evidence requires exactly these.
"""
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import time

from acceptance.processes import process_identity, remember_browser_children, stop_browser_children, stop_group
from commands import child_cancellation, signal_handlers
import git_host
from operator_errors import require
from paths import ROOT, STATE
from pins import BROWSER_NODE_VERSIONS
from private_files import private_lock, write_private, write_private_json

CASE_TIMEOUTS = {"registration.spec.ts": 1200, "create-project.spec.ts": 2400, "template-approval.spec.ts": 3600}
CASES = tuple(CASE_TIMEOUTS)


def successful_stats(stats):
    """Playwright statistics of a fully passing case: at least one test, nothing failed, skipped or flaky."""
    require(type(stats.get("expected")) is int and stats["expected"] >= 1, "Browser case has no successful tests")
    require(all(type(stats.get(k, 0)) is int and stats.get(k, 0) == 0 for k in ("unexpected", "skipped", "flaky")),
            "Acceptance contains failed, skipped or flaky tests")
    return {k: stats.get(k, 0) for k in ("expected", "unexpected", "skipped", "flaky")}


def browser_node_identity():
    """Identify a supported host Node before admitting real browser acceptance."""
    try:
        result = subprocess.run(["node", "-p", "JSON.stringify({version:process.version,executable:process.execPath})"],
                                capture_output=True, text=True, check=True, timeout=10)
        identity = json.loads(result.stdout)
        executable = Path(identity["executable"]).resolve(strict=True)
        identity["executable"] = str(executable)
        identity["sha256"] = hashlib.sha256(executable.read_bytes()).hexdigest()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        raise RuntimeError("Cannot identify the host Node runtime for browser acceptance") from None
    require(identity["version"] in BROWSER_NODE_VERSIONS, "Browser acceptance requires Node " + " or ".join(v.removeprefix("v") for v in BROWSER_NODE_VERSIONS) +
            " on PATH; Node 26 stalls pinned Playwright trace merging")
    return identity


@contextmanager
def acceptance_admission(directory):
    """Own a lane's acceptance directory; a retained fence from a failed run blocks the next one."""
    fence = directory / "e2e-admission.json"
    with private_lock(directory / "e2e.lock", message="Local browser acceptance still owns its lock: " + str(directory / "e2e.lock")):
        require(not fence.exists(), "Local browser acceptance needs explicit manual reconciliation: " + str(fence))
        yield fence


def acceptance_signals():
    """Raise KeyboardInterrupt on SIGTERM and SIGHUP too, so acceptance cleanup runs for every operator signal."""
    def interrupt(_signal, _frame):
        raise KeyboardInterrupt
    return signal_handlers(interrupt)


def run_browser_cases(cases_root, fence, browser_node, check_environment_identity, *, lane, studio, git, local_git_url, repository_host,
                      mock_llm, coverage_directory=None, **admission_extra):
    """Admit every real case under a new private directory of `cases_root` and run them in order.

    The specs read the Session's model from its Snapshot and drive a mock Session through the mock composer.
    studio is the lane's Studio origin. repository_host is the Git host the deployed server uses (github or localgit): the specs
    read pushed commits from it, the local Git host at local_url with an administrator token signed by the key of the lane's git files.
    A coverage_directory receives each passing case's spec coverage (tests/e2e/support/coverage), which needs a Studio
    build with browser source maps. Only normal completion of all cases yields results. The caller owns the final proof, its
    receipt and removing the fence; any failure leaves the fence for explicit reconciliation.
    """
    llm_mode = os.environ.get("SUPERVERSE_E2E_LLM", "mock")
    require(llm_mode in {"mock", "real"}, "SUPERVERSE_E2E_LLM must be mock or real")
    require(mock_llm == (llm_mode == "mock"), "Application LLM mode must match connections.mockLLM in the deployed local profile")
    if llm_mode == "real":
        require(bool(os.environ.get("SUPERVERSE_E2E_REAL_LLM_APPROVAL", "").strip()), "Real LLM verification requires the platform developer's recorded approval reason")
    require(all((ROOT / "web/studio/tests/e2e" / case).is_file() for case in CASES), "All three real browser specs must exist; no partial receipt")
    directory = cases_root / str(time.time_ns())
    directory.mkdir(parents=True, mode=0o700)
    admission = {"schema": 1, **admission_extra, "ownerProcess": process_identity(os.getpid()), "browserNode": browser_node,
                 "directory": str(directory), "applicationModel": llm_mode, "startedAt": time.time(), "phase": "ADMITTED"}
    write_private_json(fence, admission)
    results = []
    for case in CASES:
        require(browser_node_identity() == browser_node, "Browser Node identity changed during acceptance")
        admission.update(phase="RUNNING", case=case)
        write_private_json(fence, admission)
        check_environment_identity()
        report = run_browser_case(case, directory, check_environment_identity, lane=lane, studio=studio, git=git, local_git_url=local_git_url,
                                  repository_host=repository_host, coverage_directory=coverage_directory)
        stats = json.loads(report.read_text()).get("stats", {})
        successful_stats(stats)
        print(lane + " acceptance passed: " + case, flush=True)
        results.append({"case": case, "report": str(report.relative_to(STATE)), "sha256": hashlib.sha256(report.read_bytes()).hexdigest(), "stats": stats})
    require(browser_node_identity() == browser_node, "Browser Node identity changed during acceptance")
    return results


def run_browser_case(case, directory, check_environment_identity, *, lane, studio, git, local_git_url, repository_host, coverage_directory):
    """Run one Playwright case in its own process group, then stop everything it owns; return its report."""
    report = directory / (case + ".json")
    launch_record = directory / (case + "-launch.json")
    env = dict(os.environ, SUPERVERSE_E2E_BASE_URL=studio, PLAYWRIGHT_JSON_OUTPUT_FILE=str(report),
               SUPERVERSE_E2E_REPOSITORY_HOST=repository_host)
    if coverage_directory:
        env["SUPERVERSE_E2E_COVERAGE_DIR"] = str(coverage_directory)
    if repository_host == git_host.LOCAL_GIT:
        # The specs read the pushed commits through the host's JSON interface, which takes an administrator token.
        env.update(SUPERVERSE_E2E_LOCAL_GIT_URL=local_git_url, SUPERVERSE_E2E_LOCAL_GIT_TOKEN_FILE=str(git.write_administrator_token()))
    argv = ["npm", "exec", "--prefix", ROOT / "web/studio", "--", "playwright", "test", ROOT / "web/studio/tests/e2e" / case,
            "--config", ROOT / "web/studio/playwright.e2e.config.ts", "--project=chromium", "--reporter=json", "--workers=1", "--output", directory / (case + "-artifacts")]
    log = open(directory / (case + ".log"), "xb", buffering=0)
    os.chmod(log.name, 0o600)
    with log:
        process, case_owner, observed = None, None, {}
        try:
            # Defer signals until Popen has returned and its exact identity is recorded,
            # without an inherited blocked signal mask or an unsafe preexec hook.
            with child_cancellation() as interrupted:
                process = subprocess.Popen([str(value) for value in argv], cwd=ROOT, env=env, stdout=log, stderr=log, start_new_session=True)
                write_private(launch_record, json.dumps({"pid": process.pid, "identity": None, "status": "IDENTITY_PENDING"}))
                identity = process_identity(process.pid)
                if identity:
                    observed[identity["pid"]] = identity
                require(identity and identity["group"] == process.pid, "Browser acceptance process ownership is unknown; reconcile " + str(launch_record))
                case_owner = {"ownerProcess": identity}
                write_private(launch_record, json.dumps({"identity": identity, "status": "IDENTIFIED"}))
            if interrupted:
                raise KeyboardInterrupt
            supervise_case(process, case, directory, observed, check_environment_identity, lane=lane)
        finally:
            with signal_handlers(signal.SIG_IGN):
                stop_case(process, case_owner, observed, directory, case, launch_record)
    report.chmod(0o600)
    return report


def supervise_case(process, case, directory, observed, check_environment_identity, *, lane):
    """Wait for the case to pass, recording its browser children and proving the environment unchanged meanwhile."""
    deadline = time.monotonic() + CASE_TIMEOUTS[case]
    progress = 0.0
    while process.poll() is None:
        remember_browser_children(observed)
        write_private_json(directory / (case + "-processes.json"), list(observed.values()))
        check_environment_identity()
        now = time.monotonic()
        require(now < deadline, lane + " browser case exceeded its explicit deadline: " + case)
        if now >= progress:
            print(lane + " acceptance running: " + case, flush=True)
            progress = now + 60
        time.sleep(0.25)
    require(not process.returncode, lane + " browser case failed; private report retained: " + case)


def stop_case(process, case_owner, observed, directory, case, launch_record):
    """Record the observed processes, stop the case's process group, then its observed browser children.

    Each step runs even when an earlier one fails. Failure keeps the admission fence even if observed
    children exited; unobserved reparented children cannot be proven absent.
    """
    try:
        write_private_json(directory / (case + "-processes.json"), list(observed.values()))
    finally:
        try:
            if case_owner is not None:
                stop_group(case_owner, processes=(process,))
                process.wait(timeout=5)
            elif process is not None:
                # Never signal a numeric PID without its exact start identity; the admission fence remains.
                raise RuntimeError("Browser launch identity is uncertain; reconcile " + str(launch_record))
        finally:
            stop_browser_children(observed)
