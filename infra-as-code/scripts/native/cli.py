# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Native lane: the merged Go server, Studio and the release builder on the host; everything else on the local Docker Engine.

Platform Temporal, MinIO and Dex, the gateway and the registry run as one Docker Compose project (services.py); the
server creates every Sandbox, Preview and Live container itself. Native does not use Kind and runs beside it: it serves
its own fixed loopback ports, which Topology keeps apart from Kind's, and takes no lock of Kind's. One owner per named
instance runs at a time: it holds that instance's owner lock while it runs and records the run before anything starts, and only a proven stop
or an explicit recovery releases the run (native/owner.py).
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import socket
import subprocess
import time
import urllib.request
import uuid

from acceptance.browser import acceptance_admission, acceptance_signals, browser_node_identity, run_browser_cases
from acceptance.processes import group_members, process_identity, stop_group
import build_inputs
from commands import CommandInterrupted, run, wait_until
import config_files
from git_host import repository_host
from local_prune import after_run
from local_platform import POSTGRES_OWNER, POSTGRES_SERVER
from native import coding, services
from native.discard import delete
from native.callback import callback_readiness
from native.lane import (CONFIG, Topology, build_token, cookie_signing_key, native_instance, platform_credentials, postgres_credentials, run_layer,
                         slack_signing_secret)
from native.owner import idle, native_admission, owner_lock, owner_state, release, retain, retained
from native.studio import MODES as STUDIO_MODES, prepare_studio, require_studio_unchanged
from operator_errors import require
from paths import ROOT, project_env_value
from private_files import write_private, write_private_json
from release_builder import builder_command

RUN_HEADER = "X-Superverse-Native-Run-ID"
# Only host executable basics reach the host processes; their settings come from the configuration and the lane's secrets.
HOST_BASICS = ("PATH", "HOME", "TMPDIR", "LANG", "TZ")
STOP = False
# The server restarts that `native restart-server` asked for, counted by the owner's SIGUSR1 handler.
RESTARTS = 0


def run_directory(instance, record):
    return instance.runs / record["runId"]


# Match the platform Pod grace; config validation bounds the Worker drain to at most 60s.
SERVER_SHUTDOWN_GRACE = 120
SERVER_REGISTRATION_WAIT = 120

def host_environment(*keys):
    return {key: os.environ[key] for key in keys if key in os.environ}


def save_run(instance, record):
    """The run's crash fence, and its own copy in the run directory that outlives it."""
    retain(instance, record)
    write_private_json(run_directory(instance, record) / "run.json", record)


def require_run(instance, args, *, running=False):
    value = retained(instance)
    require(args.run_id and value.get("runId") == args.run_id, "An exact current Native --run-id is required")
    if running:
        require(value.get("phase") == "ready" and process_identity(value["ownerProcess"]["pid"]) == value["ownerProcess"],
                "Native owner is not running and ready; explicit recovery is required")
        write_private(run_directory(instance, value) / "activity", str(time.time()))
    return value


def stop_requested():
    return STOP


def inputs_unchanged(instance, record):
    return (build_inputs.native_behavior_fingerprint() == record["sourceFingerprint"] and
            config_files.file_digest(CONFIG) == record["nativeConfigSHA256"] and
            all(config_files.file_digest(layer["path"]) == layer["sha256"] for layer in record["configuration"].get("layers", [])) and
            build_inputs.fingerprint(build_inputs.native_server_inputs()) == record["nativeSourceFingerprint"] and
            hashlib.sha256(Path(record["binary"]).read_bytes()).hexdigest() == record["binaryDigest"])


def ensure_ports_free(topology):
    for name, port in topology.ports.items():
        with socket.socket() as probe:
            # A listener refuses the bind; connections a stopped run left in TIME_WAIT do not, as they do not refuse the server.
            probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            try:
                probe.bind(("127.0.0.1", port))
            except OSError:
                raise RuntimeError(f"Native requires its unoccupied fixed port {port} ({name}); stop the process that holds it explicitly") from None


def docker_socket():
    """The unix socket of the engine the Docker CLI of this shell uses, which the server then uses too."""
    host = os.environ.get("DOCKER_HOST") or run(["docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}"], capture=True, timeout=30)
    require(host.startswith("unix:///"), "Local Native needs a Docker Engine on a unix socket")
    return host.removeprefix("unix://")


def ready(port, run_id):
    with urllib.request.urlopen(f"http://127.0.0.1:{port}/readyz", timeout=3) as response:
        require(response.status == 200 and response.headers.get(RUN_HEADER) == run_id, "Readiness response does not identify this Native run")
        response.read(4096)


def await_child(process, label, check, *, seconds=120):
    """Wait until check passes while the child runs; a child that exits fails the wait at once instead of at its deadline."""
    try:
        wait_until(check, seconds=seconds, cancelled=lambda: STOP or process.poll() is not None)
    except CommandInterrupted:
        if process.poll() is not None:
            raise RuntimeError(f"The Native {label} exited while starting; see {label}.log in the run directory") from None
        raise


def spawn(instance, argv, record, label, *, env, cwd=ROOT):
    """Start a child in the owner's process group and record its exact identity before relying on it."""
    log = open(run_directory(instance, record) / (label + ".log"), "ab", buffering=0)
    os.chmod(log.name, 0o600)
    try:
        process = subprocess.Popen([str(value) for value in argv], cwd=cwd, env=env, stdout=log, stderr=log, close_fds=True)
    finally:
        log.close()
    identity = process_identity(process.pid)
    if not identity or identity["group"] != record["ownerProcess"]["group"]:
        process.terminate()
        process.wait(timeout=10)
        raise RuntimeError("Native child did not join its owner's process group")
    record.setdefault("children", []).append(dict(identity, role=label))
    save_run(instance, record)
    return process


def start(args):
    require(not args.config or Path(args.config).is_file(), "--config must name an existing server configuration profile")
    instance = native_instance(args.instance)
    topology = Topology(instance)
    with owner_lock(instance):
        with native_admission(instance) as owner:
            require(not retained(instance), "A Native run is retained for this instance; stop it (native stop) or recover it (native recover --run-id) first")
            missing = [tool for tool in ("docker", "helm", "go", "aws") if not shutil.which(tool)]
            require(not missing, "Native requires these tools on PATH: " + ", ".join(missing))
            ensure_ports_free(topology)
            if os.getpgrp() != os.getpid():
                os.setsid()
            require(os.getpgrp() == os.getpid(), "Native requires a dedicated process group")
            record = {"schema": 4, "phase": "preparing", "runId": str(uuid.uuid4()), "ownerProcess": process_identity(os.getpid()),
                      "children": [], "startedAt": time.time(), "serverRestarts": 0}
            record["laneOwner"] = owner
            run_directory(instance, record).mkdir(parents=True, mode=0o700)
            save_run(instance, record)  # The crash fence reserves capacity before any subprocess or container.

        def stop_signal(_signal, _frame):
            global STOP
            STOP = True

        def restart_signal(_signal, _frame):
            global RESTARTS
            RESTARTS += 1
        for sig in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
            signal.signal(sig, stop_signal)
        signal.signal(signal.SIGUSR1, restart_signal)
        processes = {}
        try:
            serve(instance, record, topology, prepare(instance, record, topology, args.studio, args.config), processes)
        finally:
            shut_down(instance, record, topology, processes)


def prepare(instance, record, topology, studio_mode, profile):
    """Prepare the host Studio, the coding image and its profile, the host server binary and its configuration layers.

    profile is an optional server configuration profile layered over native.yaml, for a test that needs a setting other
    than the lane's default, such as sandbox.hold; the run binds its digest as it binds native.yaml's.
    """
    directory = run_directory(instance, record)
    studio = prepare_studio(record, directory, studio_mode, host_environment(*HOST_BASICS), topology)
    save_run(instance, record)
    record["coding"], record["profile"] = coding.prepare(instance)
    record["profileSHA256"] = coding.profile_digest(instance, record["profile"])
    record["sourceFingerprint"] = build_inputs.native_behavior_fingerprint()
    record["commonSourceFingerprint"] = build_inputs.common_behavior_fingerprint()
    record["nativeSourceFingerprint"] = build_inputs.fingerprint(build_inputs.native_server_inputs())
    record["nativeConfigSHA256"] = config_files.file_digest(CONFIG)
    binary = directory / "superverse-server"
    run(["go", "build", "-trimpath", "-o", binary, "./cmd/superverse-server"],
        env=dict(os.environ, GOWORK="off", GOFLAGS="", CGO_ENABLED="0", GOOS=platform.system().lower(), GOARCH=build_inputs.architecture()), private=True, timeout=300)
    require(build_inputs.fingerprint(build_inputs.native_server_inputs()) == record["nativeSourceFingerprint"], "Native source changed while compiling")
    record.update(binary=str(binary), binaryDigest=hashlib.sha256(binary.read_bytes()).hexdigest(), platform=platform.system().lower() + "/" + build_inputs.architecture())
    # The last layer is what only this run knows; every address is native.yaml's, which the server validates as Local Native's topology.
    layer = run_layer(directory, {**topology.layer(), "native": {**topology.layer()["native"], "runId": record["runId"], "stateDir": str(instance.root), "dockerSocket": docker_socket()},
                                  "localgit": {**topology.layer()["localgit"], "dir": str(instance.git.repositories)}, "server": {**topology.layer()["server"], "blobCacheDir": str(directory / "blob-cache"), "rpcCoverageFile": str(directory / "rpc-coverage.json")},
                                  "sandbox": {"profile": record["profile"]}, "live": services.project_images()})
    operator = [Path(profile).resolve(strict=True)] if profile else []
    layers = [CONFIG, *operator, layer]
    config_files.preflight_files(*layers)
    document = config_files.read_layer(CONFIG)
    for path in operator:
        document = config_files.merge(document, config_files.read_layer(path))
    record["configuration"] = {"profile": CONFIG.stem, "sha256": record["nativeConfigSHA256"], "document": document,
                               "layers": [{"profile": path.stem, "path": str(path), "sha256": config_files.file_digest(path)} for path in operator]}
    save_run(instance, record)
    return {"studio": studio, "layers": layers}


def server_environment(instance, layers):
    """The host server's secrets, generated and retained by the lane, and its configuration files."""
    credentials = platform_credentials(instance)
    environment = dict(host_environment(*HOST_BASICS), SUPERVERSE_CONFIG_FILE=",".join(map(str, layers)), AWS_ACCESS_KEY_ID=credentials["access"],
                       AWS_SECRET_ACCESS_KEY=credentials["secret"], SUPERVERSE_COOKIE_SIGNING_KEY=cookie_signing_key(instance),
                       SUPERVERSE_LOCAL_BUILD_TOKEN_FILE=str(instance.build_token), SUPERVERSE_LOCAL_GIT_SIGNING_KEY=instance.git.signing_key(),
                       SUPERVERSE_POSTGRES_PASSWORD=postgres_credentials(instance)[POSTGRES_SERVER])
    # A developer's manual test with a real Slack app passes its Signing Secret and the platform client's secret in the
    # launcher's environment or the main checkout's .env (docs/runbooks/slack-relay-manual-test.md); otherwise the lane's
    # retained test secret signs the relay tests' events. Neither is ever printed.
    environment["SUPERVERSE_SLACK_SIGNING_SECRET"] = (os.environ.get("SUPERVERSE_SLACK_SIGNING_SECRET") or project_env_value("SUPERVERSE_SLACK_SIGNING_SECRET")
                                                      or slack_signing_secret(instance))
    client_secrets = os.environ.get("SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS") or project_env_value("SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS")
    if client_secrets:
        environment["SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS"] = client_secrets
    return environment


def serve(instance, record, topology, prepared, processes):
    """Start the platform services and every owned child, prove readiness and supervise until stopped."""
    ports = topology.ports
    environment = server_environment(instance, prepared["layers"])

    def migrate():
        # The server never migrates: the binary applies the pending migrations as the schema owner, as Kind's init container does.
        run([record["binary"], "migrate"], env=dict(host_environment(*HOST_BASICS), SUPERVERSE_CONFIG_FILE=environment["SUPERVERSE_CONFIG_FILE"],
                                                    SUPERVERSE_POSTGRES_OWNER_PASSWORD=postgres_credentials(instance)[POSTGRES_OWNER]), timeout=120)
    services.up(topology, stop_requested, migrate)
    record["platform"] = services.require_running(topology)
    save_run(instance, record)
    instance.git.repositories.mkdir(parents=True, exist_ok=True, mode=0o700)
    build_token(instance)
    arguments = ["--port", str(ports["builder"]), "--target", "docker", "--registry", topology.registry, "--state-dir", instance.release_builds]
    builder_environment = dict(host_environment(*HOST_BASICS, *services.DOCKER_SETTINGS), SUPERVERSE_LOCAL_NATIVE="true",
                               SUPERVERSE_NATIVE_RUN_ID=record["runId"], SUPERVERSE_NATIVE_PROCESS_GROUP=str(record["ownerProcess"]["group"]))
    builder = builder_command(token_file=instance.build_token, tool_image=record["coding"]["id"], repository_host=repository_host(record["configuration"]["document"]),
                              local_git_url=topology.local_git_url, arguments=arguments)
    processes["builder"] = spawn(instance, builder, record, "builder", env=builder_environment)
    await_child(processes["builder"], "builder", lambda: services.connect(ports["builder"]), seconds=60)
    processes["server"] = spawn(instance, [record["binary"]], record, "server", env=environment)
    await_child(processes["server"], "server", lambda: ready(ports["health"], record["runId"]), seconds=SERVER_REGISTRATION_WAIT)
    studio_argv, studio_cwd, studio_environment = prepared["studio"]
    processes["studio"] = spawn(instance, studio_argv, record, "studio", env=studio_environment, cwd=studio_cwd)
    await_child(processes["studio"], "studio", lambda: services.connect(ports["studio"]))
    record["runtimeIdentity"] = environment_identity(instance, topology, record)
    record["callbackReadiness"] = callback_readiness(f"127.0.0.1:{ports['dex']}", stop_requested)
    if record["studio"]["mode"] == "build":
        # Only an acceptance-capable run binds its startup source; a dev run is edited while it starts.
        require(inputs_unchanged(instance, record), "Native behavioral/host inputs changed during startup")
    record.update(phase="ready", readyAt=time.time())
    save_run(instance, record)
    print(f"Native ready in {round(record['readyAt'] - record['startedAt'])} s; run ID: {record['runId']}; Studio ({record['studio']['mode']}) at "
          f"{record['studio']['address']}, logs in {run_directory(instance, record)}", flush=True)
    checked = time.monotonic()
    try:
        while not STOP:
            while record["serverRestarts"] < RESTARTS and not STOP:
                restart(instance, record, processes, environment, ports)
            require_children_running(instance, record, processes)
            if time.monotonic() - checked >= 10:
                services.require_running(topology, record["platform"])
                checked = time.monotonic()
            time.sleep(1)
    except CommandInterrupted:
        pass  # A stop signal that lands during a supervision command interrupts it instead of setting STOP.


def require_children_running(instance, record, processes):
    """Fail on the first owned child found exited, naming its role and exit status, which the run record keeps."""
    for role, process in processes.items():
        code = process.poll()
        if code is not None:
            record["exitedFirst"] = {"role": role, "returncode": code, "at": time.time()}
            save_run(instance, record)
            how = f"signal {signal.Signals(-code).name}" if code < 0 else f"status {code}"
            raise RuntimeError(f"The Native {role} exited with {how}; all others will stop")


def restart(instance, record, processes, environment, ports):
    """Replace the host server, and with it the Dex Worker, as a Pod replacement does in Kind; then wait for the new one."""
    record["phase"] = "restarting"
    save_run(instance, record)
    old = processes.pop("server")
    old.terminate()
    try:
        old.wait(timeout=SERVER_SHUTDOWN_GRACE)
    except subprocess.TimeoutExpired:
        old.kill()
        old.wait(timeout=5)
    record["children"] = [child for child in record["children"] if child["pid"] != old.pid]
    processes["server"] = spawn(instance, [record["binary"]], record, "server", env=environment)
    await_child(processes["server"], "server", lambda: ready(ports["health"], record["runId"]), seconds=SERVER_REGISTRATION_WAIT)
    record.update(phase="ready", serverRestarts=record["serverRestarts"] + 1)
    save_run(instance, record)
    print(f"Native server restarted ({record['serverRestarts']})", flush=True)


def shut_down(instance, record, topology, processes):
    """Stop every owned process, then the platform services, and release the run once both are proven stopped."""
    record["phase"] = "stopping"
    save_run(instance, record)
    stop_group(record, owner_pid=os.getpid(), processes=list(processes.values()), term_seconds=SERVER_SHUTDOWN_GRACE)
    for process in processes.values():
        process.wait(timeout=5)
    record.update(phase="stopped", stoppedAt=time.time())
    save_run(instance, record)
    services.stop(topology)
    write_private_json(run_directory(instance, record) / "run.json", dict(record, platformStopped=True))
    release(instance, record)


def environment_identity(instance, topology, record):
    """The platform containers, the coding image and the rendered profile that the run serves; acceptance requires them unchanged."""
    return {"platform": services.require_running(topology, record["platform"]), "codingImage": {"id": instance.coding.accepted()["id"], "reference": coding.engine_reference(instance)},
            "profile": record["profile"], "profileSHA256": coding.profile_digest(instance, record["profile"])}


def stop(args):
    instance = native_instance(args.instance)
    record = require_run(instance, args)
    expected = record["ownerProcess"]
    require(process_identity(expected["pid"]) == expected, "Native owner is absent or changed; use explicit recover")
    os.kill(expected["pid"], signal.SIGTERM)
    deadline = time.monotonic() + SERVER_SHUTDOWN_GRACE + 360
    while time.monotonic() < deadline:
        if not group_members(expected["group"]):
            require(retained(instance).get("runId") != record["runId"], "Native stopped without stopping its platform services; run native recover --instance " + instance.name + " --run-id " + record["runId"])
            after_run("Native " + instance.name)
            return
        time.sleep(0.25)
    raise RuntimeError("Native stop has not proven all owned processes exited")


def recover(args):
    instance = native_instance(args.instance)
    with owner_lock(instance):
        record = require_run(instance, args)
        expected = record["ownerProcess"]
        require(not process_identity(expected["pid"]), "Native owner PID still exists; stop the matching owner or resolve PID reuse without signaling it")
        # A process-group ID stays reserved while members survive their leader.
        members = group_members(expected["group"])
        known = [{key: child[key] for key in ("pid", "group", "started")} for child in record.get("children", [])]
        require(not members or any(child in members for child in known), "No surviving recorded child proves group ownership; manual identity reconciliation required")
        stop_group(record, term_seconds=SERVER_SHUTDOWN_GRACE)
        services.stop(Topology(instance))
        record.update(phase="stopped", stoppedAt=time.time(), recovered=True)
        if run_directory(instance, record).is_dir():
            write_private_json(run_directory(instance, record) / "run.json", record)
        release(instance, record)
        print("Native run recovered: its processes and platform services are stopped")


def restart_server(args):
    """Ask the running owner to replace its host server, for the Worker restart tests; return once the new server is ready."""
    instance = native_instance(args.instance)
    record = require_run(instance, args, running=True)
    before = record["serverRestarts"]
    os.kill(record["ownerProcess"]["pid"], signal.SIGUSR1)
    deadline = time.monotonic() + SERVER_SHUTDOWN_GRACE + SERVER_REGISTRATION_WAIT + 30
    while time.monotonic() < deadline:
        current = require_run(instance, args)
        require(process_identity(current["ownerProcess"]["pid"]) == current["ownerProcess"], "Native owner exited during the server restart")
        if current["serverRestarts"] > before and current["phase"] == "ready":
            print(f"Native server restarted; run ID: {current['runId']}")
            return
        time.sleep(0.25)
    raise RuntimeError("The Native server restart did not finish in time; inspect the run's server.log")


def status(args):
    instance = native_instance(args.instance)
    value = owner_state(instance)
    if value.get("runId"):
        value = dict(value, instance=instance.name, platformServices={name: entry["running"] for name, entry in services.identity(Topology(instance)).items()})
    print(json.dumps(value, indent=2))


def e2e(args):
    browser_node = browser_node_identity()
    instance = native_instance(args.instance)
    topology = Topology(instance)
    record = require_run(instance, args, running=True)
    directory = run_directory(instance, record)
    try:
        acceptance(instance, topology, record, directory, browser_node, args)
    finally:
        after_run("Native e2e")


def acceptance(instance, topology, record, directory, browser_node, args):
    """Run the three cases against the unchanged run and write the receipt."""
    def check_environment_identity():
        require_run(instance, args, running=True)
    with acceptance_signals(), acceptance_admission(directory) as fence:
        require_studio_unchanged(record, directory)
        require(inputs_unchanged(instance, record), "Native source/binary changed; restart the Native run")
        before = environment_identity(instance, topology, record)
        require(before == record["runtimeIdentity"], "Native environment changed since startup")
        started = time.time()
        coverage = directory / "e2e-coverage" / str(time.time_ns())
        coverage.mkdir(parents=True, mode=0o700)
        results = run_browser_cases(directory / "e2e", fence, browser_node, check_environment_identity, lane="Native", studio=topology.studio_origin, git=instance.git,
                                    local_git_url=topology.local_git_url, repository_host=repository_host(record["configuration"]["document"]),
                                    mock_llm=bool(record["configuration"]["document"].get("connections", {}).get("mockLLM")),
                                    coverage_directory=coverage, runId=record["runId"])
        current = require_run(instance, args, running=True)
        require_studio_unchanged(record, directory)
        require(inputs_unchanged(instance, record) and environment_identity(instance, topology, current) == before, "Native identity changed during acceptance")
        document = record["configuration"]["document"]
        receipt = {"schema": 3, "environment": "local-native", "runId": record["runId"], "sourceFingerprint": record["sourceFingerprint"],
                   "commonSourceFingerprint": record["commonSourceFingerprint"], "nativeSourceFingerprint": record["nativeSourceFingerprint"],
                   "binaryDigest": record["binaryDigest"], "platform": record["platform"], "ownerProcess": record["ownerProcess"],
                   "runtimeIdentity": before, "callbackReadiness": record["callbackReadiness"], "coding": record["coding"], "studio": record["studio"],
                   "serverConfiguration": {key: record["configuration"][key] for key in ("profile", "sha256", "layers")},
                   "mockModel": bool(document.get("agent", {}).get("mockModel")), "agentModel": document.get("agent", {}).get("model"),
                   "repositoryHost": repository_host(document),
                   "applicationModel": "mock" if document.get("connections", {}).get("mockLLM") else "real",
                   "timings": {"startupSeconds": round(record["readyAt"] - record["startedAt"]), "casesSeconds": round(time.time() - started)},
                   "cases": results, "passedAt": time.time(), "browserNode": browser_node}
        write_private_json(instance.receipt, receipt)
        # Only normal completion of every case and final proof removes the fence.
        fence.unlink()
        print("Three actual Native cases passed; receipt: " + str(instance.receipt) + ". It cannot authorize AWS publication")
        # The cases' spec coverage refreshes their entries of the committed map the evidence check reads.
        print(run(["python3", ROOT / "scripts/studio-coverage-map.py", "merge", coverage], capture=True))
        print(run(["python3", ROOT / "scripts/rpc-coverage-map.py", "merge", "native-e2e", directory / "rpc-coverage.json"], capture=True))


COMMANDS = {"start": start, "stop": stop, "recover": recover, "restart-server": restart_server, "status": status, "e2e": e2e, "delete": delete, "idle": idle}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=COMMANDS)
    parser.add_argument("--run-id")
    parser.add_argument("--hours", type=float, default=2, help="idle: report observed inactivity of at least this many hours")
    parser.add_argument("--studio", choices=STUDIO_MODES, default="dev",
                        help="start only: dev serves next dev with hot reload; build serves the production build and admits native e2e")
    parser.add_argument("--config", help="start only: a server configuration profile layered over native.yaml, for a test that needs a "
                                         "setting other than the lane's default, such as a short sandbox.hold")
    parser.add_argument("--confirm", help="delete only: the instance's name, confirming that its data goes")
    parser.add_argument("--instance", default="default", help="Native instance name: 1 to 12 lowercase letters or digits (default: default)")
    args = parser.parse_args()
    COMMANDS[args.command](args)
