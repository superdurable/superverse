# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Atomic owner-only (0600) files, retained secrets and non-blocking locks for private local state and evidence."""
from contextlib import contextmanager
import fcntl
import json
import os
import tempfile

from operator_errors import require


def write_private(path, contents):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor, temporary = tempfile.mkstemp(prefix="." + path.name + "-", dir=path.parent)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "w") as output:
            output.write(contents)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def write_private_json(path, value):
    write_private(path, json.dumps(value, indent=2) + "\n")


def retained_secret(path, generate):
    """A secret kept in a private file: written once from generate(), then read back stripped."""
    if not path.exists():
        write_private(path, generate())
    require(not path.is_symlink() and path.is_file() and not path.stat().st_mode & 0o077, "Retained secret must be a private regular file: " + str(path))
    return path.read_text().strip()


@contextmanager
def private_lock(path, *, shared=False, message):
    """Hold an OS lock for the block; a concurrent owner fails fast with `message` instead of waiting."""
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor = os.open(path, os.O_RDWR | os.O_CREAT, 0o600)
    try:
        try:
            fcntl.flock(descriptor, (fcntl.LOCK_SH if shared else fcntl.LOCK_EX) | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError(message) from None
        yield
    finally:
        os.close(descriptor)
