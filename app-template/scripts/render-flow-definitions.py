#!/usr/bin/env python3
"""Render every Flow listed in dex-app.yaml as strict FDG 2.0.

`make check-fdg-v2` runs this inside the commit gate. It also checks that each
connector entry names the version go.mod selects. `--output` writes the rendered
definitions as JSON.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

# Dex reports a Step group that is neither one run nor one choice as this warning; it never blocks.
ADVISORY_DIAGNOSTICS = frozenset({"v2_step_group_shape"})


def blocking_diagnostics(graph):
    return [d for d in graph.get("diagnostics") or []
            if not (d.get("severity") == "warning" and d.get("code") in ADVISORY_DIAGNOSTICS)]


ROOT = Path(__file__).resolve().parents[1]


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, help="write the rendered definitions as JSON")
    parser.add_argument("--dexcli", default=os.environ.get("DEXCLI", "dexcli"))
    arguments = parser.parse_args()
    try:
        manifest = json.loads((ROOT / "dex-app.yaml").read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as failure:
        raise SystemExit("dex-app.yaml must contain valid JSON-compatible YAML") from failure
    check_connector_versions(manifest.get("connectors") or [])
    entries = manifest.get("flowDefinitions") or []
    if not entries or any(not isinstance(entry, dict) for entry in entries):
        raise SystemExit("dex-app.yaml must list each Flow source as {\"sourcePath\": ...} in flowDefinitions")
    definitions = sorted((render(entry.get("sourcePath"), arguments.dexcli) for entry in entries),
                         key=lambda definition: definition["flowType"])
    flow_types = [definition["flowType"] for definition in definitions]
    if len(set(flow_types)) != len(flow_types):
        raise SystemExit("Flow types must be unique: " + ", ".join(flow_types))
    if arguments.output:
        arguments.output.write_bytes(canonical_json(definitions) + b"\n")
    else:
        print(f"Validated {len(definitions)} strict FDG 2.0 definitions")


def check_connector_versions(connectors: list[dict[str, object]]) -> None:
    module = subprocess.run(["go", "mod", "edit", "-json"], cwd=ROOT, check=True, capture_output=True, text=True)
    selected = {requirement["Path"]: requirement["Version"] for requirement in json.loads(module.stdout).get("Require") or []}
    for entry in connectors:
        path, version = entry.get("modulePath"), entry.get("version")
        if selected.get(path) != version:
            raise SystemExit(
                f"connection {entry.get('connectionName')} declares {path} {version}, but go.mod selects "
                f"{selected.get(path) or 'nothing'}; run go get {path}@{version} or update dex-app.yaml so both versions match")


def render(source_path: object, dexcli: str) -> dict[str, object]:
    if not isinstance(source_path, str) or not source_path.endswith(".go") or not (ROOT / source_path).is_file():
        raise SystemExit(f"Flow source does not exist: {source_path}")
    with tempfile.TemporaryDirectory(prefix="fdg-") as temporary:
        output = Path(temporary) / "flow"
        result = subprocess.run(
            [dexcli, "visualize", source_path, "--schema-version", "2.0", "--json", "--out", str(output)],
            cwd=ROOT, check=False, capture_output=True, text=True)
        try:
            graph = json.loads(output.with_suffix(".json").read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            graph = None
    # The renderer can exit nonzero and still write actionable diagnostics; show them.
    if result.returncode != 0 or not isinstance(graph, dict) or graph.get("valid") is not True or blocking_diagnostics(graph):
        diagnostics = json.dumps(graph.get("diagnostics"), ensure_ascii=False)[:8192] if isinstance(graph, dict) else ""
        raise SystemExit(f"strict FDG 2.0 rendering failed for {source_path}: "
                         + (result.stderr or result.stdout).strip()[:2048] + "\n" + diagnostics)
    flow_type = (graph.get("flow") or {}).get("name")
    if graph.get("schemaVersion") != "2.0" or not flow_type:
        raise SystemExit(f"{source_path} did not render a named FDG 2.0 Flow")
    return {"flowType": flow_type, "sourcePath": source_path, "schemaVersion": "2.0",
            "digest": "sha256:" + hashlib.sha256(canonical_json(graph)).hexdigest(), "valid": True, "graph": graph}


def canonical_json(value: object) -> bytes:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode("utf-8")


if __name__ == "__main__":
    main()
