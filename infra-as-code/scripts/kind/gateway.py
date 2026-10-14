# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The pinned Envoy Gateway of a Kind cluster, which serves every project route (docs/architecture/linear-scaling/ingress.md).

Terraform installs the same pins of source-lock.json in hosted environments (terraform/kubernetes/controllers.tf).
"""
import json

from kind.cluster import GATEWAY_NAMESPACE
from paths import STATE
from pins import LOCK
from private_files import write_private

PIN = LOCK["envoyGateway"]
# The controller settings Terraform's helm_release.envoy_gateway sets too: one replica with bounded resources, and no
# topology injector, whose admission webhook the project routes do not need.
CONTROLLER_VALUES = {"deployment": {"replicas": 1, "envoyGateway": {"resources": {"requests": {"cpu": "100m", "memory": "256Mi"},
                                                                                   "limits": {"cpu": "1", "memory": "1Gi"}}}},
                     "topologyInjector": {"enabled": False}}


def install(helm, kubectl, *, values_name="envoy-gateway-values.json"):
    """Install Envoy Gateway and its CRDs with the cluster's own `helm` and `kubectl` commands.

    The Gateway API and Envoy Gateway CRDs are applied server-side on every install, so a new pin upgrades them: Helm never
    upgrades the CRDs of a chart's crds directory. The platform chart then creates the GatewayClass and the Gateway.
    """
    crds = helm("template", "envoy-gateway-crds", chart("gateway-crds-helm", PIN["crdsChartDigest"]), "--set", "crds.gatewayAPI.enabled=true",
                "--set", "crds.gatewayAPI.channel=experimental", "--set", "crds.envoyGateway.enabled=true", capture=True, timeout=300)
    # Helm prints the pulled chart's reference and digest on stdout before the manifests.
    lines = crds.splitlines()
    crds = "\n".join(lines[lines.index("---"):])
    kubectl("apply", "--server-side", "--force-conflicts", "--field-manager", "superverse-v2-kind", "-f", "-", input=crds, timeout=300)
    values = STATE / values_name
    write_private(values, json.dumps({"crds": {"enabled": False}, **CONTROLLER_VALUES,
                                      "global": {"images": {"envoyGateway": {"image": PIN["controllerImage"]}, "envoyProxy": {"image": PIN["proxyImage"]}}}}))
    helm("upgrade", "--install", "envoy-gateway", chart("gateway-helm", PIN["chartDigest"]), "--namespace", GATEWAY_NAMESPACE, "--create-namespace",
         "-f", values, "--wait", "--timeout", "5m", timeout=330)


def chart(name, digest):
    """The pinned Envoy Gateway chart by version and digest; Helm refuses a chart whose digest differs."""
    return f"{PIN['charts']}/{name}:{PIN['version']}@{digest}"
