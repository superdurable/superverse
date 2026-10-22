# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""FIFO reservations for the shared Kind lane, in the main checkout's private state."""
from contextlib import contextmanager
import json
import os
import time
import uuid

from acceptance.processes import process_identity
from native.owner import lane_owner
from operator_errors import require
from paths import STATE
from private_files import private_lock, write_private_json

QUEUE = STATE / "kind-queue.json"
LOCK = STATE / "kind-queue.lock"
LEASE_SECONDS = 6 * 3600
HEAD_IDLE_SECONDS = 30 * 60


def active(entry):
    identity = entry.get("activeProcess")
    return bool(identity and process_identity(identity["pid"]) == identity)


def expire(values):
    """Waiting leases last six hours; every newly promoted head gets thirty idle minutes."""
    now = time.time()
    values[:] = [entry for entry in values if entry["updatedAt"] + LEASE_SECONDS > now or active(entry)]
    while values:
        head = values[0]
        head.setdefault("headSince", now)
        if active(head) or head_deadline(head) > now:
            break
        values.pop(0)


def head_deadline(entry):
    return max(entry["headSince"], entry["updatedAt"]) + HEAD_IDLE_SECONDS


@contextmanager
def entries():
    with private_lock(LOCK, message="Another Kind queue command is updating the queue; retry"):
        values = json.loads(QUEUE.read_text()) if QUEUE.exists() else []
        expire(values)
        yield values
        # Leave promotes the next waiter immediately, without charging its waiting time.
        expire(values)
        write_private_json(QUEUE, values)


def owned(entry):
    owner = lane_owner()
    return all(entry["owner"][key] == owner[key] for key in ("session", "worker"))


def queue(args):
    with entries() as values:
        if args.queue_action == "join":
            entry = next((entry for entry in values if owned(entry)), None)
            if entry is None:
                entry = {"ticket": str(uuid.uuid4()), "owner": lane_owner(), "joinedAt": time.time()}
                values.append(entry)
            entry["updatedAt"] = time.time()
            if values[0] is entry:
                entry.setdefault("headSince", time.time())
            print(json.dumps(dict(entry, position=values.index(entry) + 1,
                                  headIdleDeadline=head_deadline(entry) if values[0] is entry else None), indent=2))
        elif args.queue_action == "leave":
            ticket = args.ticket or os.environ.get("SUPERVERSE_KIND_TICKET")
            entry = next((entry for entry in values if entry["ticket"] == ticket), None)
            require(entry is not None and owned(entry), "An exact ticket owned by this session/worker is required")
            require(not active(entry), "Kind ticket has an active command; wait for it to exit before leaving")
            values.remove(entry)
            print("Left Kind queue: " + ticket)
        else:
            print(json.dumps({"leaseSeconds": LEASE_SECONDS, "headIdleSeconds": HEAD_IDLE_SECONDS,
                              "entries": [dict(entry, position=index + 1, active=active(entry),
                                               headIdleDeadline=head_deadline(entry) if index == 0 else None)
                                                                          for index, entry in enumerate(values)]}, indent=2))


@contextmanager
def admission(ticket):
    """Called inside kind-action.lock; queue admission supplements the lock and crash fence."""
    ticket = ticket or os.environ.get("SUPERVERSE_KIND_TICKET")
    with entries() as values:
        require(values and ticket and values[0]["ticket"] == ticket,
                "Kind mutation/E2E requires the head ticket: run sv2 kind queue join, inspect queue status, then pass --ticket or SUPERVERSE_KIND_TICKET")
        entry = values[0]
        require(owned(entry), "Kind head ticket belongs to another session/worker")
        entry.update(activeProcess=process_identity(os.getpid()), updatedAt=time.time())
        require(entry["activeProcess"] is not None, "Cannot establish Kind queue command process identity")
    try:
        yield
    finally:
        with entries() as values:
            entry = next((entry for entry in values if entry["ticket"] == ticket), None)
            if entry:
                entry.pop("activeProcess", None)
                entry["updatedAt"] = time.time()
