# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Delete a stopped Native instance's data: every container, volume and network its label selects, its coding image tag
and its state directory.

The engine labels the platform services, Projects and Sessions an instance created with superdurable.io/native-instance,
so the selection never depends on a name pattern. A named instance also releases its topology index, which a later
instance may take; the default keeps index 0 and its next start begins with no account, Project or run.
"""
import json
import shutil
import subprocess

from commands import run
from native.lane import DEFAULT_INSTANCE, native_instance, release_instance_index
from native.owner import owner_lock, retained
from operator_errors import require
from pins import names_for

INSTANCE_LABEL = "superdurable.io/native-instance"


def selected(kind, *filters):
    listing = ["docker", kind, "ls", "--quiet", "--no-trunc"] if kind != "volume" else ["docker", kind, "ls", "--quiet"]
    if kind == "container":
        listing.append("--all")
    for value in filters:
        listing.extend(["--filter", value])
    return run(listing, capture=True, timeout=120).split()


def labeled(kind, name):
    """Select exact instance ownership, including the default's pre-instance V2 objects."""
    values = set(selected(kind, f"label={INSTANCE_LABEL}={name}"))
    if name == DEFAULT_INSTANCE:
        # Match Go's VerifyLabels: missing instance means default, but a different
        # instance (including an empty explicit label) never does. Docker ls has
        # no negative-label filter, so inspect only the V2-managed candidates.
        managed = "app.kubernetes.io/managed-by"
        product = names_for("test")["product"]
        template = "{{json .Config.Labels}}" if kind == "container" else "{{json .Labels}}"
        for identity in selected(kind, f"label={managed}={product}"):
            labels = json.loads(run(["docker", kind, "inspect", "--format", template, identity], capture=True, timeout=60)) or {}
            if labels.get(managed) == product and INSTANCE_LABEL not in labels:
                values.add(identity)
    return sorted(values)


def remove_network(identity):
    """An absent network is success, including removal racing the exact ID."""
    try:
        run(["docker", "network", "rm", identity], capture=True, timeout=120)
    except subprocess.CalledProcessError:
        # Reconcile through the API listing, rather than matching daemon error text.
        remaining = selected("network", "id=" + identity)
        if identity in remaining:
            raise


def delete(args):
    instance = native_instance(args.instance)
    require(args.confirm == instance.name, f"native delete removes every account, Project and run of the instance; pass --confirm {instance.name}")
    with owner_lock(instance):
        require(not retained(instance), f"Native instance {instance.name} retains a run; stop it (native stop) or recover it first")
        containers = labeled("container", instance.name)
        if containers:
            run(["docker", "rm", "--force", "--volumes", *containers], timeout=600)
        volumes, networks = labeled("volume", instance.name), labeled("network", instance.name)
        if volumes:
            run(["docker", "volume", "rm", *volumes], timeout=600)
        if networks:
            for network in networks:
                remove_network(network)
        if run(["docker", "image", "ls", "--quiet", instance.coding.tag], capture=True, timeout=60):
            run(["docker", "image", "rm", instance.coding.tag], timeout=120)
        if instance.root.exists():
            shutil.rmtree(instance.root)
        if instance.name != DEFAULT_INSTANCE:
            release_instance_index(instance.name)
        print(f"Deleted Native instance {instance.name}: {len(containers)} containers, {len(volumes)} volumes, {len(networks)} networks and its state")
