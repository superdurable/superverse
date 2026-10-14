#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Reject Go imports that break the package layering of decision 0010.

Production imports follow the levels and the provider-package rules. The Kubernetes boundary also
covers test files, because the packages that may reach Kubernetes decide which pull requests need
the Local Kind lane. Integration tests may import the implementations they exercise, so the
provider-package rules apply to production imports only."""
import json
import os
import re
import subprocess
import sys

MODULE = "github.com/superdurable/superversev2/"
# Shared packages hold no business ownership; business packages build on them. A root covers the
# package at that path and every package below it, test helpers included (dexflowtest, kubeapitest).
SHARED_ROOTS = ("internal/comm", "internal/app", "internal/platform", "internal/studio/flowapi",
                "internal/agents/session/toolinput/generated", "internal/agents/remoteagent/bridgeapi")
# The server configuration schema is read only where the process starts, validates and composes from it.
CONFIG = "cmd/superverse-server/config"
CONFIG_IMPORTERS = ("cmd/superverse-server", "cmd/tools/check-config", "internal/bootstrap")
# A capability with several implementations has its interface in the parent package and each
# implementation in a sub-package of its own. Callers import the parent; only internal/bootstrap
# imports an implementation, and an implementation never imports another. The Kubernetes
# implementations of the Sandbox instances and the Live serving build on the Kubernetes machinery of
# the project infrastructure, and its test fixture composes the local backend.
IMPLEMENTATIONS = {
    "internal/platform/repohost": ("github", "localgit"),
    "internal/platform/eventqueue": ("memory",),
    "internal/delivery/releases/build": ("local", "hosted"),
    "internal/projects/infra": ("kube", "docker"),
    "internal/projects/infra/kube": ("local", "hosted", "connect"),
    "internal/projects/infra/kube/hosted": ("identity", "storage"),
    "internal/agents/sandbox": ("kube", "docker"),
    "internal/delivery/deployments": ("kube", "docker"),
}
IMPLEMENTATION_IMPORTS = {
    ("internal/agents/sandbox/kube", "internal/projects/infra/kube"),
    ("internal/agents/sandbox/docker", "internal/projects/infra/docker"),
    ("internal/delivery/deployments/kube", "internal/projects/infra/kube"),
    ("internal/delivery/deployments/docker", "internal/projects/infra/docker"),
    ("internal/projects/infra/kube/kubetest", "internal/projects/infra/kube/local"),
    ("internal/projects/infra/kube/hosted", "internal/projects/infra/kube/hosted/identity"),
    ("internal/projects/infra/kube/hosted", "internal/projects/infra/kube/hosted/storage"),
    ("internal/projects/infra/kube/hosted/storage", "internal/projects/infra/kube/hosted/identity"),
}
# Kubernetes code lives only in the Kubernetes packages, so that a change to it, and nothing else,
# needs the Local Kind lane. Only these packages import the Kubernetes API client and its test
# client (the root covers kubeapitest), in production and in test files; the composition root does
# it in one file.
KUBERNETES_API = "internal/platform/kubeapi"
KUBERNETES_PACKAGES = ("internal/platform/kubeapi", "internal/projects/infra/kube", "internal/agents/sandbox/kube",
                       "internal/delivery/deployments/kube")
KUBERNETES_FILES = {("internal/bootstrap", "kubernetes.go")}
# Likewise only the Docker packages import the Docker Engine API client, in test files as well.
DOCKER_API = "internal/platform/dockerapi"
DOCKER_PACKAGES = ("internal/platform/dockerapi", "internal/projects/infra/docker", "internal/agents/sandbox/docker",
                   "internal/delivery/deployments/docker")
# Each runtime API client with the packages, and the single files, that may import it.
BOUNDARIES = {KUBERNETES_API: ("Kubernetes", KUBERNETES_PACKAGES, KUBERNETES_FILES), DOCKER_API: ("Docker", DOCKER_PACKAGES, set())}
# Every business package has a level and imports only business packages of a strictly lower level.
# The levels are the longest import chain below each package, so a new edge that needs a higher
# level shows up in review. A business test helper (sandboxdtest) is levelled like its parent's code.
LEVELS = {
    "internal/agents/platformissue": 1,
    "internal/agents/remoteagentdao": 1,
    "internal/agents/toolfailure": 1,
    "internal/agents/session/flowdesign": 1,
    "internal/api/watch": 1,
    "internal/delivery/deliverydao": 1,
    "internal/delivery/releases/build": 1,
    "internal/projects/infra": 1,
    "internal/projects/projectdao": 1,
    "internal/projects/repobinding": 1,
    "internal/studio/confighost/configitems": 1,
    "internal/studio/confighost/connectormanifest": 1,
    "internal/tenancy/accountdao": 1,
    "internal/tenancy/memberdao": 1,
    "internal/tenancy/teamspacedao": 1,
    "internal/tenancy/teamspaces": 1,
    "internal/tenancy/connectiondao": 1,
    "internal/agents/sandbox/workspace/sandboxd": 2,
    "internal/agents/session/conversation": 2,
    "internal/projects": 2,
    "internal/projects/infra/docker": 2,
    "internal/projects/infra/kube": 2,
    "internal/delivery/releases/build/hosted": 2,
    "internal/delivery/releases/build/local": 2,
    "internal/studio/confighost": 3,
    "internal/tenancy/accounts": 2,
    "internal/tenancy/connections": 2,
    "internal/agents/sandbox/workspace/git": 3,
    "internal/projects/infra/kube/connect": 3,
    "internal/projects/infra/kube/hosted/identity": 3,
    "internal/projects/infra/kube/hosted/storage": 4,
    "internal/projects/infra/kube/hosted": 5,
    "internal/projects/infra/kube/local": 3,
    "internal/agents/sandbox/workspace/sandboxd/sandboxdtest": 3,
    "internal/agents/session/modelprovider": 3,
    "internal/agents/session/toolinput": 3,
    "internal/delivery/releases": 3,
    "internal/projects/setup": 3,
    "internal/tenancy/slackrelay": 3,
    "internal/projects/projecttest": 3,
    "internal/projects/infra/docker/dockertest": 3,
    "internal/projects/infra/docker/dockertest/configprobe": 1,
    "internal/delivery/deployments/generationapp": 1,
    "internal/projects/infra/kube/kubetest": 4,
    "internal/agents/sandbox/workspace": 4,
    "internal/projects/config": 4,
    "internal/agents/session/sandboxtools": 5,
    "internal/agents/session/agenttasks": 5,
    "internal/agents/remoteexec": 5,
    "internal/agents/sandbox": 5,
    "internal/delivery/deployments": 5,
    "internal/agents/sandbox/docker": 6,
    "internal/agents/sandbox/kube": 6,
    "internal/delivery/deployments/docker": 6,
    "internal/agents/sandbox/preview": 6,
    "internal/agents/sandbox/provision": 6,
    "internal/delivery/deployments/kube": 6,
    "internal/agents/session/agentloop": 6,
    "internal/agents/sandbox/sessionworkspace": 7,
    "internal/agents/session/backgroundcommand": 6,
    "internal/agents/session": 7,
    "internal/agents/session/subagent": 7,
    "internal/agents/session/contribution": 7,
    "internal/agents/session/modelprovider/mockmodel": 7,
    "internal/agents": 8,
    "internal/api/boundary": 9,
    "internal/agents/remoteagent": 9,
    "internal/studio/runs": 9,
    "internal/agents/diagnostics": 10,
    "internal/api/tenancy": 10,
    "internal/api/remoteagent": 10,
    "internal/api": 11,
    "internal/bootstrap": 12,
}


def under(path, *roots):
    return any(path == root or path.startswith(root + "/") for root in roots)


def violations(package, imported):
    if imported == CONFIG and package not in CONFIG_IMPORTERS:
        yield f"{package} imports the server configuration; only {', '.join(CONFIG_IMPORTERS)} may"
    if imported.startswith("cmd/"):
        return
    if under(imported, "internal/bootstrap") and not package.startswith("cmd/"):
        yield f"{package} imports the composition root; only cmd/* may"
    if under(imported, "internal/api") and not (package.startswith("cmd/") or under(package, "internal/api", "internal/bootstrap")):
        yield f"{package} imports the business gRPC API; only internal/api/**, internal/bootstrap and cmd/* may"
    if not package.startswith("internal/"):
        return
    for parent, names in IMPLEMENTATIONS.items():
        for name in names:
            implementation = f"{parent}/{name}"
            if (under(imported, implementation) and not under(package, implementation, "internal/bootstrap")
                    and (package, imported) not in IMPLEMENTATION_IMPORTS):
                yield f"{package} imports the implementation {implementation}; only internal/bootstrap composes an implementation of {parent}"
    if under(package, "internal/comm") and not under(imported, "internal/comm"):
        yield f"{package} imports {imported}; internal/comm/** imports only internal/comm/**"
    if under(package, *SHARED_ROOTS):
        if not under(imported, *SHARED_ROOTS):
            yield f"shared package {package} imports business package {imported}"
    elif not under(imported, *SHARED_ROOTS) and LEVELS[package] <= LEVELS[imported]:
        yield f"{package} (level {LEVELS[package]}) imports {imported} (level {LEVELS[imported]}); a business package imports only lower levels"


def boundary_violations(api, package, directory, files):
    """Yield the files of package that import the runtime API client api without being one of its packages."""
    runtime, packages, single_files = BOUNDARIES[api]
    if under(package, *packages):
        return
    pattern = re.compile(r'"' + re.escape(MODULE + api) + r'(/[^"]*)?"')
    for file in sorted(files):
        with open(os.path.join(directory, file), encoding="utf-8") as source:
            imports_client = any(pattern.search(line) for line in source)
        if imports_client and (package, file) not in single_files:
            allowed = ", ".join(packages) + "".join(f" and {path}/{name}" for path, name in sorted(single_files))
            yield f"{package} imports {api} in {file}; only the {runtime} packages ({allowed}) may, in test files as well"


def main():
    listing = subprocess.check_output(
        # The tags select the files of the live and hosted tests, so the Kubernetes boundary covers them.
        ["go", "list", "-tags=live,hosted",
         "-json=ImportPath,Dir,GoFiles,TestGoFiles,XTestGoFiles,Imports,TestImports,XTestImports",
         "./cmd/...", "./internal/...", "./sandbox/coding/cmd/...", "./sandbox/coding/internal/..."],
        env={**os.environ, "GOWORK": "off"}, text=True)
    decoder = json.JSONDecoder()
    imports, clients = {}, {}
    position = 0
    while True:
        while position < len(listing) and listing[position].isspace():
            position += 1
        if position >= len(listing):
            break
        entry, position = decoder.raw_decode(listing, position)
        package = entry["ImportPath"].removeprefix(MODULE)
        imports[package] = [name.removeprefix(MODULE) for name in entry.get("Imports", [])
                            if name.removeprefix(MODULE).startswith(("internal/", "cmd/"))]
        # Test files count for the runtime boundaries, so every import list is searched.
        everything = entry.get("Imports", []) + entry.get("TestImports", []) + entry.get("XTestImports", [])
        for api in BOUNDARIES:
            if any(under(name.removeprefix(MODULE), api) for name in everything):
                clients[(api, package)] = (entry["Dir"], entry.get("GoFiles", []) + entry.get("TestGoFiles", []) + entry.get("XTestGoFiles", []))
    business = sorted(package for package in imports if package.startswith("internal/") and not under(package, *SHARED_ROOTS))
    failures = [f"business package {package} has no level; add it to LEVELS in {os.path.basename(__file__)}"
                for package in business if package not in LEVELS]
    failures += [f"LEVELS names {package}, which is not a business package" for package in sorted(set(LEVELS) - set(business))]
    if not failures:
        for package, imported in sorted(imports.items()):
            for name in imported:
                failures.extend(violations(package, name))
        for (api, package), (directory, files) in sorted(clients.items()):
            failures.extend(boundary_violations(api, package, directory, files))
    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    print("Package layers: shared packages stay below business packages, each business package imports only lower levels "
          "and only the Kubernetes and Docker packages import their API clients")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
