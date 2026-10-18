# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Exercise connect prompts through a real CLI and the local Orca runtime."""

import os
import pty
import re
import select
import signal
import tempfile
import time
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
BINARY = Path(os.environ.get("DEXAI_CONNECT_BINARY", REPO / "bin/dexai-connect"))
ENDPOINT = "http://127.0.0.1:26089"


def run_case(name, arguments, prompt, response, expected_code):
    child, master = pty.fork()
    if child == 0:
        os.execv(str(BINARY), [str(BINARY), "connect", "--endpoint", ENDPOINT, *arguments])

    observed = b""
    deadline = time.monotonic() + 30
    while prompt not in observed and time.monotonic() < deadline:
        if select.select([master], [], [], 0.2)[0]:
            try:
                observed += os.read(master, 65536)
            except OSError:
                break
    if prompt not in observed:
        os.kill(child, signal.SIGKILL)
        os.close(master)
        os.waitpid(child, 0)
        raise AssertionError(f"{name}: prompt absent or CLI exited early")

    if response == "NEW":
        label = re.escape(str(REPO).encode())
        choice = re.search(rb"(?m)^[ \t]*(\d+)\) New conversation in " + label + rb"\r?$", observed)
        if choice is None:
            os.kill(child, signal.SIGKILL)
            os.close(master)
            os.waitpid(child, 0)
            raise AssertionError(f"{name}: no New conversation choice for this worktree")
        os.write(master, choice.group(1) + b"\n")
        deadline = time.monotonic() + 20
        while b"Pairing code from Studio" not in observed and time.monotonic() < deadline:
            if select.select([master], [], [], 0.2)[0]:
                try:
                    observed += os.read(master, 65536)
                except OSError:
                    break
        if b"Pairing code from Studio" not in observed:
            os.kill(child, signal.SIGKILL)
            os.close(master)
            os.waitpid(child, 0)
            raise AssertionError(f"{name}: selecting New did not reach the pairing prompt")
        os.kill(child, signal.SIGINT)
    elif response == "SIGINT":
        os.kill(child, signal.SIGINT)
    else:
        os.write(master, response.encode() + b"\n")
    deadline = time.monotonic() + 4
    while time.monotonic() < deadline:
        # A PTY child can wait for output to drain before closing its tty.
        if select.select([master], [], [], 0.05)[0]:
            try:
                os.read(master, 65536)
            except OSError:
                pass
        done, status = os.waitpid(child, os.WNOHANG)
        if done:
            code = os.waitstatus_to_exitcode(status)
            os.close(master)
            if code != expected_code:
                raise AssertionError(f"{name}: exit {code}, expected {expected_code}")
            print(f"{name}: exit {code} without another Enter")
            return
    os.kill(child, signal.SIGKILL)
    os.close(master)
    os.waitpid(child, 0)
    raise AssertionError(f"{name}: CLI did not exit promptly")


def main():
    if not BINARY.is_file():
        raise SystemExit("build the CLI first with make build-dexai-connect-local")
    with tempfile.TemporaryDirectory(prefix="bridge-prompt-") as directory:
        arguments = [
            "--state-dir", str(Path(directory) / "state"),
            "--codex-config", str(Path(directory) / "codex.toml"),
        ]
        run_case("choice-zero", arguments, b"Connect which conversation", "0", 0)
        run_case("choice-SIGINT", arguments, b"Connect which conversation", "SIGINT", 130)
        run_case("new-without-worktree", arguments, b"Connect which conversation", "NEW", 130)
        run_case(
            "pairing-SIGINT",
            [*arguments, "--new", "--worktree", f"path:{REPO}"],
            b"Pairing code from Studio (Ctrl+C to cancel)",
            "SIGINT",
            130,
        )


if __name__ == "__main__":
    main()
