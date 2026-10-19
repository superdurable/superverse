# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Kind lane: one owned local cluster, never the current kubectl context, never a hosted mutation."""
import argparse
import json
import subprocess
import sys
import time
from pathlib import Path

import build_inputs
from commands import child_cancellation, run, stop_children
from kind.acceptance import e2e
from kind.builder import builder
from kind.cluster import CLUSTER, FORWARDS, NAMESPACE, PORTS, STATE_FILE, kind_nodes, kubectl, port_forward_argv, require_kind_version, verify_owner
from kind.images import build
from kind.locks import kind_action_lock
from kind import server_config
from local_prune import after_run
from kind.platform import deploy, up
from operator_errors import require
from paths import MAIN_CHECKOUT, ROOT


def ports(args):
    """Forward the loopback port catalog until interrupted; stop every forward if one exits."""
    verify_owner()
    mappings = {}
    for forward in FORWARDS.values():
        if forward.dependency or not args.dependencies_only:
            mappings.setdefault((forward.namespace, forward.service), []).append(f"{forward.local_port}:{forward.remote_port}")
    with child_cancellation() as cancelled:
        processes = []
        try:
            for (namespace, service), service_mappings in mappings.items():
                if cancelled:
                    return
                processes.append(subprocess.Popen(port_forward_argv(service, *service_mappings, namespace=namespace)))
            while not cancelled and all(process.poll() is None for process in processes):
                time.sleep(0.25)
            require(cancelled, "A port-forward exited; all V2 forwards are being stopped")
        finally:
            stop_children(processes)


def status(_args):
    value = verify_owner()
    kubectl("get", "pods,services", "-n", NAMESPACE)
    print(json.dumps({"checkout": str(MAIN_CHECKOUT), "worktree": str(ROOT), "images": value["images"], "ports": PORTS}, indent=2))


def stop(_args):
    """Stop the Kind node to free its memory; its state stays and the next Kind command starts it again."""
    verify_owner()
    for node in kind_nodes():
        run(["docker", "stop", "--time", "60", node], timeout=180)


def delete(args):
    verify_owner()
    require(args.confirm == CLUSTER, "Explicit --confirm superverse-v2 is required to delete this owned cluster")
    run(["kind", "delete", "cluster", "--name", CLUSTER], timeout=600)
    # Receipts and source/image identities stay; a new up creates a fresh owner marker.
    STATE_FILE.unlink()


COMMANDS = {"up": up, "build": build, "deploy": deploy, "ports": ports, "status": status, "e2e": e2e, "stop": stop, "delete": delete, "builder": builder}


def monitoring(args):
    # The optional implementation and its deployment assets are private operations material.
    sys.path.insert(0, str(ROOT / "infra-as-code/monitoring"))
    import kind_monitoring
    {"up": kind_monitoring.up, "down": kind_monitoring.down, "check": kind_monitoring.check}[args.monitoring_action]()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=[*COMMANDS, "monitoring"])
    parser.add_argument("monitoring_action", nargs="?", choices=["up", "down", "check"])
    parser.add_argument("--component", choices=["all", *build_inputs.COMPONENTS], default="all")
    parser.add_argument("--secret-env-file", help="deploy: private env file with the server secrets; it may hold other keys, which are ignored "
                                                  "because the configuration profile is the only source of every setting")
    parser.add_argument("--studio-secret-env-file")
    parser.add_argument("--published-dependencies", help="Committed reviewed dependency approval (required by deploy)")
    parser.add_argument("--config", default=str(server_config.BASE),
                        help="deploy: the server configuration profile, layered over local-kind.yaml (default); "
                             "local-kind-acceptance.yaml is the final acceptance, whose model the platform developer decides at kind e2e")
    parser.add_argument("--confirm")
    parser.add_argument("--dependencies-only", action="store_true")
    args = parser.parse_args()
    if args.command == "monitoring":
        require(args.monitoring_action is not None, "Choose monitoring up, down or check")
        require_kind_version()
        with kind_action_lock("monitoring-" + args.monitoring_action):
            monitoring(args)
        return
    if args.command == "status":
        status(args)
        return
    require_kind_version()
    try:
        with kind_action_lock(args.command):
            COMMANDS[args.command](args)
    finally:
        # A finished acceptance run reclaims the lanes' unused images and build cache; outside the lock, so the Kind node is pruned too.
        if args.command == "e2e":
            after_run("Kind e2e")
