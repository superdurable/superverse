# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Native Studio: the checkout's Studio on the host, served on the lane's Studio origin with its own signing key.

`dev` serves `next dev` from the checkout, so Studio edits reload in place. `build` runs the production build of the
current source and serves its standalone output from a private copy in the run directory; only it admits acceptance.
"""
import shutil
import json

import build_inputs
from commands import run
from native.lane import browser_signing_key, login_auth_secrets
from operator_errors import require
from paths import ROOT

STUDIO_SOURCE = ROOT / "web/studio"
PROTO = "api/superverse/v2/superverse.proto"
MODES = ("dev", "build")
HOST = "127.0.0.1"


def source_fingerprint():
    return build_inputs.fingerprint(build_inputs.component_inputs("studio"))


def prepare_studio(record, directory, mode, host_environment, topology, document):
    """Record Studio's source identity and build it in `build` mode, with the browser source maps that let an E2E run
    record the Studio files each spec executes; return its (argv, cwd, environment)."""
    require(mode in MODES, "Native Studio mode must be dev or build")
    next_binary = STUDIO_SOURCE / "node_modules/.bin/next"
    require(next_binary.exists(), "Native Studio requires npm --prefix web/studio ci first")
    port = topology.ports["studio"]
    record["studio"] = {"mode": mode, "sourceFingerprint": source_fingerprint(), "address": topology.studio_origin}
    environment = dict(host_environment, BROWSER_SIGNING_KEY=browser_signing_key(topology.instance), SUPERVERSE_STUDIO_PUBLIC_ORIGIN=topology.studio_origin,
                       NEXT_TELEMETRY_DISABLED="1", SUPERVERSE_GRPC_ADDRESS=f"{HOST}:{topology.ports['grpc']}")
    login = document.get("accounts", {}).get("loginHost", {})
    environment["SUPERVERSE_STUDIO_AUTH_CONFIG"] = json.dumps({
        "productOrigin": topology.studio_origin, "loginOrigin": login.get("publicOrigin", ""),
        "handoffEnabled": bool(login.get("handoffEnabled")), "exchangeEnabled": bool(login.get("exchangeEnabled")),
        "exchangeURL": f"http://{HOST}:{port}", "studioClientID": "studio", "clients": login.get("clients", {}),
    })
    if login.get("handoffEnabled") or login.get("exchangeEnabled"):
        auth = login_auth_secrets(topology.instance, [id for id, client in login.get("clients", {}).items() if client.get("enabled")])
        environment["SUPERVERSE_LOGIN_HOST_ISSUER_SECRET_RING"] = json.dumps(auth["issuer"])
        environment["SUPERVERSE_LOGIN_HOST_CLIENT_SECRET_RINGS"] = json.dumps(auth["clients"])
        environment["SUPERVERSE_LOGIN_BROWSER_KEYS"] = json.dumps(auth["browser"])
    if mode == "dev":
        return [next_binary, "dev", "--hostname", HOST, "--port", str(port)], STUDIO_SOURCE, environment
    run(["npm", "--prefix", STUDIO_SOURCE, "run", "build"], env=dict(host_environment, NODE_ENV="production", NEXT_TELEMETRY_DISABLED="1",
                                                                    SUPERVERSE_STUDIO_SOURCE_MAPS="true"), timeout=900)
    require(source_fingerprint() == record["studio"]["sourceFingerprint"], "Studio source changed while building")
    release = directory / "studio"
    output = STUDIO_SOURCE / ".next"
    # The image's layout (web/studio/Dockerfile), copied so a later build in the checkout cannot change this run.
    shutil.copytree(output / "standalone", release, symlinks=True)
    shutil.copytree(output / "static", release / "web/studio/.next/static", symlinks=True)
    if (STUDIO_SOURCE / "public").is_dir():
        shutil.copytree(STUDIO_SOURCE / "public", release / "web/studio/public", symlinks=True, dirs_exist_ok=True)
    shutil.copytree(STUDIO_SOURCE / "proto", release / "web/studio/proto", symlinks=True, dirs_exist_ok=True)
    (release / PROTO).parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(ROOT / PROTO, release / PROTO)
    record["studio"]["buildId"] = (output / "BUILD_ID").read_text().strip()
    environment.update(NODE_ENV="production", HOSTNAME=HOST, PORT=str(port), SUPERVERSE_PROTO_PATH=str(release / PROTO))
    return ["node", "server.js"], release / "web/studio", environment


def require_studio_unchanged(record, directory):
    """Acceptance binds the production build of the recorded Studio source."""
    require(record["studio"]["mode"] == "build", "Native acceptance requires native start --studio build; next dev is not acceptance")
    require(source_fingerprint() == record["studio"]["sourceFingerprint"], "Studio source changed; restart the Native run")
    require((directory / "studio/web/studio/.next/BUILD_ID").read_text().strip() == record["studio"]["buildId"],
            "Native Studio build output changed")
