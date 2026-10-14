# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""GATEWAY load test: many HTTPRoutes on one Envoy Gateway, in a disposable Kind cluster of its own.

It never touches the lane's cluster. It installs the pinned Envoy Gateway and the chart's EnvoyProxy and GatewayClass of
the project routes, a Gateway "load" with the project Gateway's listener, one small backend per namespace and the routes,
and measures (docs/architecture/linear-scaling/ingress.md#tests):

- the time until the proxy serves every route after they are created in bulk;
- a full route configuration push: the time from a new proxy's start until it is ready with every route, and their size;
- the time from one new HTTPRoute until the proxy serves it;
- the memory and CPU of the controller and the proxy.

PYTHONPATH=infra-as-code/scripts python3 -m kind.gateway_load --routes 20000 [--keep]
"""
import argparse
from datetime import datetime
import json
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

import yaml

import chart_validation
from commands import run, stop_children
from kind import gateway
from kind.cluster import GATEWAY_NAMESPACE, KIND_NODE_IMAGE
from operator_errors import require
from paths import STATE
from pins import local_image
from private_files import write_private_json

CLUSTER = "superverse-v2-gateway-load"
KUBECONFIG = STATE / "gateway-load-kubeconfig"
NAMESPACES = 20
BATCH = 500
# The backend: the lanes' pinned registry image answers GET / with 200.
BACKEND_IMAGE, BACKEND_PORT = local_image("registry"), 5000
PROXY = "gateway.envoyproxy.io/owning-gateway-name=load"


def kubectl(*args, **kwargs):
    return run(["kubectl", "--kubeconfig", KUBECONFIG, "--context", "kind-" + CLUSTER, *args], **kwargs)


def helm(*args, **kwargs):
    return run(["helm", *args, "--kubeconfig", KUBECONFIG, "--kube-context", "kind-" + CLUSTER], **kwargs)


def apply(items):
    kubectl("apply", "--server-side", "--field-manager", "gateway-load", "-f", "-", input=json.dumps({"apiVersion": "v1", "kind": "List", "items": items}), timeout=600)


def free_port():
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        return reservation.getsockname()[1]


def forward(target, remote):
    """A port-forward to target in the gateway namespace; returns its process and local port."""
    port = free_port()
    process = subprocess.Popen(["kubectl", "--kubeconfig", str(KUBECONFIG), "--context", "kind-" + CLUSTER, "-n", GATEWAY_NAMESPACE, "port-forward",
                                "--address", "127.0.0.1", target, f"{port}:{remote}"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    deadline = time.monotonic() + 30
    while True:
        try:
            with socket.create_connection(("127.0.0.1", port), timeout=1):
                return process, port
        except OSError:
            require(time.monotonic() < deadline and process.poll() is None, "port-forward to " + target + " did not start")
            time.sleep(0.2)


def status(port, host):
    """The proxy's answer for host, 0 when it does not answer."""
    request = urllib.request.Request(f"http://127.0.0.1:{port}/", headers={"Host": host})
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code
    except OSError:
        return 0


def await_served(port, host, seconds):
    """Seconds until the proxy answers host with 200."""
    started = time.monotonic()
    while status(port, host) != 200:
        require(time.monotonic() - started < seconds, f"{host} was not served within {seconds} seconds")
        time.sleep(0.5)
    return round(time.monotonic() - started, 2)


def admin(port, path):
    with urllib.request.urlopen(f"http://127.0.0.1:{port}{path}", timeout=120) as response:
        return response.read()


def container_stats():
    """Memory working set and cumulative CPU of the controller and proxy containers, from the Kind node's runtime."""
    stats = json.loads(run(["docker", "exec", CLUSTER + "-control-plane", "crictl", "stats", "--output", "json"], capture=True))
    names = {item["id"]: item["metadata"]["name"] for item in json.loads(run(["docker", "exec", CLUSTER + "-control-plane", "crictl", "ps", "--output", "json"],
                                                                             capture=True))["containers"]}
    result = {}
    for item in stats["stats"]:
        name = names.get(item["attributes"]["id"], "")
        if name in ("envoy-gateway", "envoy"):
            result[name] = {"memoryWorkingSetMiB": round(int(item["memory"]["workingSetBytes"]["value"]) / 2**20, 1),
                            "cpuCoreSeconds": round(int(item["cpu"]["usageCoreNanoSeconds"]["value"]) / 1e9, 1)}
    return result


def proxy_report(admin_port, routes):
    """The proxy's served route table: its virtual hosts, its size and the proxy's memory."""
    dump = admin(admin_port, "/config_dump?resource=dynamic_route_configs")
    hosts = sum(len(entry["route_config"].get("virtual_hosts", [])) for entry in json.loads(dump)["configs"])
    require(hosts >= routes, f"the proxy serves {hosts} virtual hosts, fewer than {routes} routes")
    memory = json.loads(admin(admin_port, "/memory"))
    return {"virtualHosts": hosts, "routeConfigBytes": len(dump), "allocatedMiB": round(int(memory["allocated"]) / 2**20, 1),
            "heapMiB": round(int(memory["heap_size"]) / 2**20, 1)}


def push_timing(pod):
    """Seconds from the proxy container's start, and from the Pod's creation, until the proxy is ready with every route."""
    def at(stamp):
        return datetime.fromisoformat(stamp.replace("Z", "+00:00"))
    ready = next(at(condition["lastTransitionTime"]) for condition in pod["status"]["conditions"] if condition["type"] == "Ready")
    envoy = next(status for status in pod["status"]["containerStatuses"] if status["name"] == "envoy")
    return {"containerStartToReadySeconds": (ready - at(envoy["state"]["running"]["startedAt"])).total_seconds(),
            "podCreatedToReadySeconds": (ready - at(pod["metadata"]["creationTimestamp"])).total_seconds()}


def gateway_objects():
    """The chart's EnvoyProxy and GatewayClass of the project routes, and a Gateway "load" with the project listener."""
    with tempfile.TemporaryDirectory(prefix="sv2-gateway-load-") as directory:
        rendered = chart_validation.helm_template(Path(directory), "local-kind", chart_validation.environment_values("local-kind"),
                                                  "--show-only", "templates/project-gateway.yaml")
    objects = [document for document in yaml.safe_load_all(rendered) if document]
    for document in objects:
        if document["kind"] == "Gateway":
            document["metadata"]["name"] = "load"
            document["spec"]["listeners"][0]["allowedRoutes"]["namespaces"]["selector"] = {"matchLabels": {"superdurable.io/gateway-load": "true"}}
    return objects


def backend(namespace):
    labels = {"app": "backend"}
    return [{"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": namespace, "labels": {"superdurable.io/gateway-load": "true"}}},
            {"apiVersion": "apps/v1", "kind": "Deployment", "metadata": {"name": "backend", "namespace": namespace}, "spec": {
                "replicas": 1, "selector": {"matchLabels": labels}, "template": {"metadata": {"labels": labels}, "spec": {"containers": [{
                    "name": "backend", "image": BACKEND_IMAGE, "ports": [{"containerPort": BACKEND_PORT}],
                    "resources": {"requests": {"cpu": "10m", "memory": "32Mi"}}}]}}}},
            {"apiVersion": "v1", "kind": "Service", "metadata": {"name": "backend", "namespace": namespace},
             "spec": {"selector": labels, "ports": [{"port": 80, "targetPort": BACKEND_PORT}]}}]


def route(index):
    """The route of load-<index>.localhost, built as the platform builds a project route."""
    return {"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute", "metadata": {"name": f"r{index}", "namespace": f"load-{index % NAMESPACES}"},
            "spec": {"parentRefs": [{"group": "gateway.networking.k8s.io", "kind": "Gateway", "namespace": GATEWAY_NAMESPACE, "name": "load"}],
                     "hostnames": [f"load-{index}.localhost"], "rules": [{"backendRefs": [{"name": "backend", "port": 80}], "timeouts": {"request": "0s"}}]}}


def measure(routes, result):
    """Fill result phase by phase, so a run that fails, such as an out-of-memory proxy, keeps what it measured."""
    gateway.install(helm, kubectl, values_name="gateway-load-values.json")
    apply(gateway_objects())
    apply([item for index in range(NAMESPACES) for item in backend(f"load-{index}")])
    kubectl("wait", "--for=condition=Programmed", "gateway/load", "-n", GATEWAY_NAMESPACE, "--timeout=300s", timeout=330)
    for index in range(NAMESPACES):
        kubectl("rollout", "status", "deployment/backend", "-n", f"load-{index}", "--timeout=300s", timeout=330)
    service = kubectl("get", "service", "-n", GATEWAY_NAMESPACE, "-l", PROXY, "-o", "jsonpath={.items[0].metadata.name}", capture=True)
    processes = []
    try:
        traffic, traffic_port = forward("service/" + service, 80)
        processes.append(traffic)
        started = time.monotonic()
        for first in range(0, routes, BATCH):
            apply([route(index) for index in range(first, min(first + BATCH, routes))])
        result["bulkApplySeconds"] = round(time.monotonic() - started, 2)
        result["bulkServedSeconds"] = round(result["bulkApplySeconds"] + await_served(traffic_port, f"load-{routes - 1}.localhost", 3600), 2)
        result["afterBulk"] = container_stats()
        pod = kubectl("get", "pod", "-n", GATEWAY_NAMESPACE, "-l", PROXY, "-o", "jsonpath={.items[0].metadata.name}", capture=True)
        proxy_admin, admin_port = forward("pod/" + pod, 19000)
        processes.append(proxy_admin)
        result["proxyAfterBulk"] = proxy_report(admin_port, routes)
        save(result)
        stop_children(processes[1:])
        del processes[1:]
        # A full push: a new proxy Pod receives the whole route configuration from the controller. Envoy reports ready only
        # once its initial listener and route configuration arrived, so the Pod's own timestamps time the push.
        kubectl("delete", "pod", pod, "-n", GATEWAY_NAMESPACE, "--wait=false")
        kubectl("wait", "--for=delete", "pod/" + pod, "-n", GATEWAY_NAMESPACE, "--timeout=300s", timeout=330)
        kubectl("wait", "--for=condition=Ready", "pod", "-l", PROXY, "-n", GATEWAY_NAMESPACE, "--timeout=900s", timeout=930)
        pod = kubectl("get", "pod", "-n", GATEWAY_NAMESPACE, "-l", PROXY, "-o", "jsonpath={.items[0].metadata.name}", capture=True)
        result["fullPush"] = push_timing(json.loads(kubectl("get", "pod", pod, "-n", GATEWAY_NAMESPACE, "-o", "json", capture=True)))
        stop_children(processes)
        processes.clear()
        traffic, traffic_port = forward("service/" + service, 80)
        processes.append(traffic)
        await_served(traffic_port, f"load-{routes - 1}.localhost", 60)
        proxy_admin, admin_port = forward("pod/" + pod, 19000)
        processes.append(proxy_admin)
        result["proxyAfterFullPush"] = proxy_report(admin_port, routes)
        save(result)
        # One more route at a time, as a Live activation or a Preview start writes it.
        result["singleRouteServedSeconds"] = []
        for index in range(routes, routes + 5):
            started = time.monotonic()
            apply([route(index)])
            result["singleRouteServedSeconds"].append(round(time.monotonic() - started + await_served(traffic_port, f"load-{index}.localhost", 900), 2))
        result["final"] = container_stats()
        result["restarts"] = kubectl("get", "pods", "-n", GATEWAY_NAMESPACE, "-o", "jsonpath={range .items[*]}{.metadata.name}={.status.containerStatuses[*].restartCount} {end}",
                                     capture=True)
    finally:
        stop_children(processes)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--routes", type=int, default=20000)
    parser.add_argument("--keep", action="store_true", help="keep the load cluster for inspection; delete it with `kind delete cluster --name " + CLUSTER + "`")
    args = parser.parse_args()
    require(CLUSTER not in run(["kind", "get", "clusters"], capture=True).splitlines(), "The load cluster exists; delete it first: kind delete cluster --name " + CLUSTER)
    run(["kind", "create", "cluster", "--name", CLUSTER, "--image", KIND_NODE_IMAGE, "--kubeconfig", KUBECONFIG, "--wait", "180s"], timeout=900)
    result = {"routes": args.routes, "namespaces": NAMESPACES, "envoyGateway": gateway.PIN["version"], "proxyImage": gateway.PIN["proxyImage"],
              "kindNode": KIND_NODE_IMAGE}
    try:
        measure(args.routes, result)
    except Exception as error:
        result["error"] = f"{type(error).__name__}: {error}"[-2000:]
        result["containers"] = containers()
        raise
    finally:
        save(result)
        print(json.dumps(result, indent=2))
        if not args.keep:
            run(["kind", "delete", "cluster", "--name", CLUSTER], timeout=600)
            KUBECONFIG.unlink(missing_ok=True)


def save(result):
    write_private_json(STATE / f"gateway-load-result-{result['routes']}.json", result)


def containers():
    """Restarts and last terminations of the gateway's containers, such as OOMKilled."""
    pods = json.loads(kubectl("get", "pods", "-n", GATEWAY_NAMESPACE, "-o", "json", capture=True))["items"]
    return {pod["metadata"]["name"]: [{"name": status["name"], "restarts": status["restartCount"], "lastState": status.get("lastState", {})}
                                      for status in pod["status"].get("containerStatuses", [])] for pod in pods}

if __name__ == "__main__":
    main()
