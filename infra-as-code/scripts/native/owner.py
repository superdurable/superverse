# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""One owner per named Native instance, and the crash fence of its run.

The owner of a Native run holds its instance's owner lock for its whole life, as `recover` does while it cleans up, and
records the run in owner.json before it starts any process or container; the record stays until a proven stop or an
explicit recovery releases it. The lane serves its own ports and takes no lock of Kind's (kind/locks.py), so the two
lanes run at the same time.
"""
from contextlib import contextmanager
import json

from operator_errors import require
from paths import MAIN_CHECKOUT, STATE
from private_files import private_lock, write_private_json

# A checkout from before the lanes ran in parallel kept one mode for both: its launcher takes this OS lock shared for
# every Kind command and exclusively for a Native run, which it records in the mode file. An owner holds the lock as
# well and refuses a Native run that the mode file retains, so such a checkout never runs a second owner of this lane's
# Compose project and state; its Kind commands wait until the owner stops.
LEGACY_LOCK = STATE / "execution.lock"
LEGACY_MODE = STATE / "execution-mode.json"


def owner_state(instance):
    """The owner record as it is: the retained run, the last released one, or nothing."""
    record = instance.root / "owner.json"
    if not record.exists():
        return {}
    value = json.loads(record.read_text())
    require(value.get("checkout") == str(MAIN_CHECKOUT), "Native owner state belongs to another checkout")
    return value


def retained(instance):
    """The record of the retained run, or {} when no run is retained."""
    value = owner_state(instance)
    return value if value.get("runId") else {}


def retain(instance, record):
    """Record the run as retained; the fence exists before any process or container of the run."""
    write_private_json(instance.root / "owner.json", dict(record, checkout=str(MAIN_CHECKOUT)))


def release(instance, record):
    """A proven stop or recovery releases the run."""
    write_private_json(instance.root / "owner.json", {"checkout": str(MAIN_CHECKOUT), "previousRunId": record["runId"]})


@contextmanager
def owner_lock(instance):
    """The lane's exclusivity: `start` holds it for the owner's life and `recover` while it cleans up."""
    with private_lock(instance.root / "owner.lock", message="Another Native owner or recovery holds this Native instance's owner lock"):
        if instance.name != "default":
            # Older launchers take one global lock for their default topology.
            # A named instance has no shared state, ports, Compose project or
            # Docker range, so taking that historical lock would needlessly
            # prevent it from running beside an already-running old default.
            yield
            return
        with private_lock(LEGACY_LOCK, shared=True, message="A checkout from before parallel lanes holds its shared lock for a Kind command or "
                                                            "a Native owner; stop it or let it finish first"):
            require(not LEGACY_MODE.exists() or json.loads(LEGACY_MODE.read_text()).get("mode") != "native",
                    "A checkout from before parallel lanes retains a Native run in " + str(LEGACY_MODE) +
                    "; stop or recover it with that checkout's launcher first")
            yield
