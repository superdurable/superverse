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
import shutil

from commands import run
from native.lane import DEFAULT_INSTANCE, native_instance, release_instance_index
from native.owner import owner_lock, retained
from operator_errors import require

INSTANCE_LABEL = "superdurable.io/native-instance"


def labeled(kind, name):
    listing = ["docker", kind, "ls", "--quiet", "--filter", f"label={INSTANCE_LABEL}={name}"]
    return run(listing[:3] + ["--all"] + listing[3:] if kind == "container" else listing, capture=True, timeout=120).split()


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
            run(["docker", "network", "rm", *networks], timeout=120)
        if run(["docker", "image", "ls", "--quiet", instance.coding.tag], capture=True, timeout=60):
            run(["docker", "image", "rm", instance.coding.tag], timeout=120)
        if instance.root.exists():
            shutil.rmtree(instance.root)
        if instance.name != DEFAULT_INSTANCE:
            release_instance_index(instance.name)
        print(f"Deleted Native instance {instance.name}: {len(containers)} containers, {len(volumes)} volumes, {len(networks)} networks and its state")
