# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The one way every lane runs a bounded external command and owns its child process."""
from contextlib import contextmanager
from pathlib import Path
import signal
import subprocess
import time

from paths import ROOT


class CommandInterrupted(KeyboardInterrupt):
    """An operator signal stopped an owned command. Retry loops must not treat it as transient."""


@contextmanager
def signal_handlers(handler):
    """Handle SIGINT, SIGTERM and SIGHUP with `handler` for the block, then restore the previous handlers."""
    previous = {}
    try:
        for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            previous[signum] = signal.signal(signum, handler)
        yield
    finally:
        for signum, old in previous.items():
            signal.signal(signum, old)


@contextmanager
def child_cancellation():
    """Defer interruption until a returned Popen handle is under cleanup ownership."""
    received = []

    def cancel(signum, _frame):
        received[:] = [signum]
    with signal_handlers(cancel):
        yield received


def stop_children(processes):
    """Reap only owned direct children; attempt every child even after a failure."""
    for action, budget in (("terminate", 20), ("kill", 5)):
        for process in processes:
            if process.poll() is None:
                try:
                    getattr(process, action)()
                except OSError:
                    pass  # Still escalate and reap this and every other child.
        deadline = time.monotonic() + budget
        for process in processes:
            try:
                process.wait(timeout=max(0, deadline - time.monotonic()))
            except (OSError, subprocess.TimeoutExpired):
                pass
        if all(process.poll() is not None for process in processes):
            return
    raise RuntimeError("Owned command cleanup is incomplete; reconcile before retrying")


def wait_until(check, *, seconds, interval=0.25, cancelled=lambda: False):
    """Return check() once it passes, retrying its transient failures until `seconds` elapse.

    An operator interrupt (CommandInterrupted) is never retried, and cancelled() ends the wait as one.
    """
    deadline = time.monotonic() + seconds
    while True:
        if cancelled():
            raise CommandInterrupted("Waiting was interrupted; external effects may require reconciliation")
        try:
            return check()
        except (RuntimeError, OSError, ValueError):
            if time.monotonic() >= deadline:
                raise
            time.sleep(interval)


def run(command, *, capture=False, input=None, env=None, timeout=60, private=False):
    """Run one command from the repository root; return its stripped stdout when captured.

    timeout=None is reserved for an explicitly long-lived service. A non-zero exit raises
    CalledProcessError with the captured output. private=True captures all output and raises
    a RuntimeError naming only the tool, because its arguments and output may hold secrets.
    An operator signal raises CommandInterrupted; a timeout raises RuntimeError. Neither
    proves that the command's external effects were rolled back.
    """
    argv = [str(value) for value in command]
    tool = Path(argv[0]).name
    capture = capture or private
    with child_cancellation() as cancelled:
        process = None
        try:
            process = subprocess.Popen(argv, cwd=ROOT, stdin=subprocess.PIPE if input is not None else None,
                                       stdout=subprocess.PIPE if capture else None,
                                       stderr=subprocess.PIPE if capture else None, text=True, env=env)
            deadline = None if timeout is None else time.monotonic() + timeout
            pending_input = input
            while not cancelled:
                remaining = None if deadline is None else deadline - time.monotonic()
                if remaining is not None and remaining <= 0:
                    raise RuntimeError(tool + " timed out; external effects may require reconciliation")
                try:
                    stdout, stderr = process.communicate(pending_input, timeout=0.25 if remaining is None else min(0.25, remaining))
                    break
                except subprocess.TimeoutExpired:
                    pending_input = None
            if cancelled:
                raise CommandInterrupted(tool + " was interrupted; external effects may require reconciliation")
            if process.returncode and private:
                raise RuntimeError(tool + " failed; inspect it privately before retrying")
            if process.returncode:
                raise subprocess.CalledProcessError(process.returncode, argv, stdout, stderr)
            return stdout.strip() if capture else ""
        finally:
            if process is not None:
                try:
                    stop_children([process])
                finally:
                    for stream in (process.stdin, process.stdout, process.stderr):
                        if stream is not None:
                            stream.close()
