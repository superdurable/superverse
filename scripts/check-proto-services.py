#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Business protocol rules that Buf STANDARD lint does not cover.

The rules are decision 0011 and M1-M13 of docs/api/resource-grpc.md; each check names the rule it holds.

The script builds the module with the pinned buf itself (`BUF` holds the command, as the Makefile
sets it) and checks the descriptors, not the source text. `--write` also regenerates the service
table of docs/api/resource-grpc.md from the same descriptors.
"""
import json
import os
import re
import shlex
import subprocess
import sys
from pathlib import Path

repository = Path(__file__).resolve().parents[1]
PROTO = "superverse/v2/superverse.proto"
PACKAGE = ".superverse.v2."
DOCUMENT = repository / "docs/api/resource-grpc.md"
TABLE_BEGIN = "<!-- BEGIN GENERATED SERVICE TABLE (make generate-proto) -->"
TABLE_END = "<!-- END GENERATED SERVICE TABLE -->"

# How the browser reaches each service. A new service needs an explicit decision here; the BFF
# allowlist (BROWSER_SERVICES) is the runtime counterpart of the /api/rpc rows.
RPC_ROUTE, AUTH_ROUTE, FLOW_API_ROUTE = "/api/rpc/<Method>", "/api/auth", "/dex/..."
ROUTES = {
    "AccountService": AUTH_ROUTE,
    "ProjectService": RPC_ROUTE,
    "SessionService": RPC_ROUTE,
    "AgentService": RPC_ROUTE,
    "ModelCredentialService": RPC_ROUTE,
    "RemoteAgentService": RPC_ROUTE,
    "PreviewService": RPC_ROUTE,
    "ReleaseService": RPC_ROUTE,
    "DeploymentService": RPC_ROUTE,
    "FlowRunService": FLOW_API_ROUTE,
    "FlowDiagnosticsService": FLOW_API_ROUTE,
    "ConnectorService": FLOW_API_ROUTE,
    "ApplicationEnvironmentService": FLOW_API_ROUTE,
    "TeamSpaceService": RPC_ROUTE,
    "TeamSpaceMemberService": RPC_ROUTE,
    "ProjectMemberService": RPC_ROUTE,
    "TeamSpaceConnectionService": RPC_ROUTE,
    "AttachmentService": RPC_ROUTE,
    "EvolveService": RPC_ROUTE,
}
# The BFF accepts none of these in a request it forwards to an /api/rpc service, which carries JSON in
# string *_json fields instead; bytes fields and maps are refused as well.
BROWSER_REQUEST_FORBIDDEN_TYPES = {"google.protobuf.Struct", "google.protobuf.Value", "google.protobuf.ListValue", "google.protobuf.Timestamp"}
UNAUTHENTICATED_REQUESTS = {"RegisterAccountRequest", "LoginRequest"}
# A browser never selects an upstream, a transport or an authority.
FORBIDDEN_FIELDS = {
    "worker_address", "worker_url", "dex_address", "storage_url", "permissions", "http_method", "http_path",
}
# Persisted in Dex Attributes and Step payloads as JSON keyed by the protoc-gen-go field names.
FROZEN = {
    "AppManifestSnapshot": [
        "singular string source_commit = 1 json=sourceCommit",
        "singular string manifest_digest = 2 json=manifestDigest",
        "singular string connector_contract_digest = 3 json=connectorContractDigest",
        "singular string environment_contract_digest = 4 json=environmentContractDigest",
        "singular uint64 revision = 5 json=revision",
        "repeated ConnectorRequirement connectors = 6 json=connectors",
        "singular google.protobuf.Struct environment = 7 json=environment",
        "singular ObjectReference manifest_object = 8 json=manifestObject",
        "singular ObjectReference connector_contract_object = 9 json=connectorContractObject",
        "singular ObjectReference environment_contract_object = 10 json=environmentContractObject",
    ],
    "ObjectReference": [
        "singular string key = 1 json=key",
        "singular string version = 2 json=version",
        "singular string digest = 3 json=digest",
        "singular string media_type = 4 json=mediaType",
    ],
    "ConnectorRequirement": [
        "singular string connection_name = 1 json=connectionName",
        "singular string connector_id = 2 json=connectorId",
        "singular string version = 3 json=version",
        "singular string auth_method_id = 4 json=authMethodId",
        "repeated string operations = 5 json=operations",
        "singular string module_path = 7 json=modulePath",
        "repeated ConnectorTriggerBinding trigger_bindings = 8 json=triggerBindings",
    ],
    "ConnectorTriggerBinding": [
        "singular string trigger_name = 1 json=triggerName",
        "singular string binding_name = 2 json=bindingName",
    ],
}


def build_image():
    command = os.environ.get("BUF")
    if not command:
        sys.exit("BUF is unset; run this script through make check-proto or make generate-proto")
    build = [*shlex.split(command), "build", "--exclude-imports", "-o", "-#format=json"]
    result = subprocess.run(build, cwd=repository, capture_output=True, text=True)
    if result.returncode != 0:
        sys.exit(f"buf build failed:\n{result.stderr}")
    return json.loads(result.stdout)


class Protocol:
    """The services and messages of the one business file, messages by package-relative name."""

    def __init__(self, file):
        self.services = file.get("service", [])
        self.messages = dict(nested_messages(file.get("messageType", [])))
        self.enums = file.get("enumType", []) + [
            enum for message in self.messages.values() for enum in message.get("enumType", [])
        ]

    def request(self, method):
        return self.messages[method["inputType"].removeprefix(PACKAGE)]

    def response(self, method):
        return self.messages[method["outputType"].removeprefix(PACKAGE)]

    def reachable(self, name, seen=None):
        """The names of the message and of every message its fields reach."""
        seen = set() if seen is None else seen
        if name in self.messages and name not in seen:
            seen.add(name)
            for field in self.messages[name].get("field", []):
                if field["type"] == "TYPE_MESSAGE":
                    self.reachable(type_name(field), seen)
        return seen


def nested_messages(messages, prefix=""):
    for message in messages:
        name = prefix + message["name"]
        yield name, message
        yield from nested_messages(message.get("nestedType", []), name + ".")


def type_name(field):
    if field.get("typeName"):
        return field["typeName"].removeprefix(PACKAGE).removeprefix(".")
    return field["type"].removeprefix("TYPE_").lower()


def in_oneof(field):
    """A member of a real oneof; a proto3 `optional` field only has a synthetic one."""
    return "oneofIndex" in field and not field.get("proto3Optional")


def is_caller(field):
    return field["name"] == "caller" and field["number"] == 100 and type_name(field) == "CallerContext" \
        and field["label"] == "LABEL_OPTIONAL"


def check_services(protocol):
    """Service size, RPC names and streaming (decision 0011) and the Flow API shapes."""
    failures, declared = [], {}
    names = {service["name"] for service in protocol.services}
    if set(ROUTES) != names:
        failures.append(f"services differ from the route table: {sorted(set(ROUTES) ^ names)}")
    for service in protocol.services:
        methods = service.get("method", [])
        if len(methods) > 10:
            failures.append(f"{service['name']} has {len(methods)} RPCs; a service holds at most 10")
        for method in methods:
            name = method["name"]
            if name in declared:
                failures.append(f"RPC {name} is declared by {declared[name]} and {service['name']}")
            declared[name] = service["name"]
            if name.startswith("Watch") != bool(method.get("serverStreaming")):
                failures.append(f"{name}: an RPC starts with Watch exactly when it streams from the server")
            if method.get("clientStreaming"):
                failures.append(f"{name}: client streaming is not allowed")
            if ROUTES.get(service["name"]) == FLOW_API_ROUTE:
                failures += check_flow_api_method(protocol, method)
            else:
                reached = protocol.reachable(method["inputType"].removeprefix(PACKAGE))
                protocol.reachable(method["outputType"].removeprefix(PACKAGE), reached)
                if reached & {"FlowApiContext", "FlowApiDocument"}:
                    failures.append(f"{name}: only the Flow API services use FlowApiContext and FlowApiDocument")
    return failures


def field_shape(field):
    return field["name"], field["number"], type_name(field), field["label"]


def check_flow_api_method(protocol, method):
    failures = []
    context = [field_shape(field) for field in protocol.request(method).get("field", []) if field["number"] == 1]
    if context != [("context", 1, "FlowApiContext", "LABEL_OPTIONAL")]:
        failures.append(f"{method['name']}: field 1 of a Flow API request is FlowApiContext context")
    if [field_shape(field) for field in protocol.response(method).get("field", [])] != [
        ("document", 1, "FlowApiDocument", "LABEL_OPTIONAL")
    ]:
        failures.append(f"{method['name']}: a Flow API response is exactly {{FlowApiDocument document = 1}}")
    return failures


def check_fields(protocol):
    """M3 caller, forbidden authority fields and M11 declaration order."""
    failures = []
    authenticated = {
        protocol.request(method)["name"] for service in protocol.services for method in service.get("method", [])
    } - UNAUTHENTICATED_REQUESTS
    for name, message in protocol.messages.items():
        fields = message.get("field", [])
        for field in fields:
            if field["number"] >= 100 and not (is_caller(field) and name in authenticated):
                failures.append(f"{name}.{field['name']} = {field['number']}: only the caller field of an "
                                "authenticated request is numbered 100 or higher")
            if field["name"] in FORBIDDEN_FIELDS:
                failures.append(f"{name}.{field['name']}: a browser-selected authority or transport is forbidden")
        if name in authenticated and not any(is_caller(field) for field in fields):
            failures.append(f"{name}: an authenticated request carries CallerContext caller = 100")
        # A oneof block sorts by its lowest member.
        lowest = {}
        for field in filter(in_oneof, fields):
            lowest[field["oneofIndex"]] = min(lowest.get(field["oneofIndex"], field["number"]), field["number"])
        order = [(lowest[field["oneofIndex"]] if in_oneof(field) else field["number"], field["number"])
                 for field in fields]
        if order != sorted(order):
            failures.append(f"{name}: fields are declared in ascending number order")
    return failures


def check_browser_requests(protocol):
    """The requests of the /api/rpc services reach no bytes, map, Struct, Value, ListValue or Timestamp field."""
    failures = []
    for service in protocol.services:
        if ROUTES.get(service["name"]) != RPC_ROUTE:
            continue
        for method in service.get("method", []):
            for name in sorted(protocol.reachable(method["inputType"].removeprefix(PACKAGE))):
                message = protocol.messages[name]
                if message.get("options", {}).get("mapEntry"):
                    failures.append(f"{method['name']}: its request reaches the map {name}; carry JSON in a string *_json field")
                for field in message.get("field", []):
                    if field["type"] == "TYPE_BYTES" or type_name(field) in BROWSER_REQUEST_FORBIDDEN_TYPES:
                        failures.append(f"{method['name']}: its request reaches {name}.{field['name']} of type {type_name(field)}; "
                                        "carry JSON in a string *_json field")
    return failures


def frozen_signature(field):
    label = ("repeated" if field["label"] == "LABEL_REPEATED" else "oneof" if in_oneof(field)
             else "optional" if field.get("proto3Optional") else "singular")
    return f"{label} {type_name(field)} {field['name']} = {field['number']} json={field['jsonName']}"


def check_frozen(protocol):
    """M12: the persisted payloads match the pinned table."""
    failures = []
    for name, expected in FROZEN.items():
        actual = [frozen_signature(field) for field in protocol.messages.get(name, {}).get("field", [])]
        if actual != expected:
            failures.append(f"{name} is a frozen persisted payload; expected {expected}, found {actual}")
    return failures


def check_names(protocol):
    """M13: acronyms in type, service and RPC names are written as words; Buf checks PascalCase."""
    names = [service["name"] for service in protocol.services]
    names += [method["name"] for service in protocol.services for method in service.get("method", [])]
    names += [message["name"] for message in protocol.messages.values()
              if not message.get("options", {}).get("mapEntry")]
    names += [enum["name"] for enum in protocol.enums]
    failures = []
    for name in names:
        if re.search(r"[A-Z]{2}", name.replace("OAuth", "Oauth")):
            failures.append(f"{name}: acronyms are written as words (OAuth is the one exception)")
    return failures


def describe_field(protocol, field):
    """A field in the design notation: `name number`, prefixed by its type unless it is a string."""
    entry = protocol.messages.get(type_name(field), {}) if field["type"] == "TYPE_MESSAGE" else {}
    if entry.get("options", {}).get("mapEntry"):
        key, value = (type_name(part).removeprefix("google.protobuf.") for part in entry["field"])
        return f"map<{key}, {value}> {field['name']} {field['number']}"
    kind = "" if field["type"] == "TYPE_STRING" else type_name(field).removeprefix("google.protobuf.") + " "
    label = "repeated " if field["label"] == "LABEL_REPEATED" else "optional " if field.get("proto3Optional") else ""
    return f"{label}{kind}{field['name']} {field['number']}"


def describe_message(protocol, message):
    """The fields of a request or response, without the caller."""
    parts, described_oneofs = [], set()
    fields = [field for field in message.get("field", []) if not is_caller(field)]
    for field in fields:
        if not in_oneof(field):
            parts.append(describe_field(protocol, field))
        elif field["oneofIndex"] not in described_oneofs:
            described_oneofs.add(field["oneofIndex"])
            members = "; ".join(describe_field(protocol, member) for member in fields
                                if member.get("oneofIndex") == field["oneofIndex"])
            parts.append(f"oneof {message['oneofDecl'][field['oneofIndex']]['name']} {{{members}}}")
    return ", ".join(parts) or "{}"


def rpc_count(protocol):
    return sum(len(service.get("method", [])) for service in protocol.services)


def service_table(protocol):
    lines = [
        TABLE_BEGIN,
        "",
        f"{len(protocol.services)} services, {rpc_count(protocol)} RPCs. A field is a string unless a type is given. "
        "Every request",
        "except RegisterAccount and Login also carries `CallerContext caller 100`, which the table omits.",
        "",
        "| Service | RPCs | Reached through |",
        "| --- | --- | --- |",
    ]
    lines += [f"| {service['name']} | {len(service.get('method', []))} | `{ROUTES.get(service['name'])}` |"
              for service in protocol.services]
    for service in protocol.services:
        lines += ["", f"**{service['name']}**", "", "| RPC | Request | Response |", "| --- | --- | --- |"]
        for method in service.get("method", []):
            request = describe_message(protocol, protocol.request(method))
            response = describe_message(protocol, protocol.response(method))
            stream = "stream " if method.get("serverStreaming") else ""
            lines.append(f"| {method['name']} | `{request}` | {stream}`{response}` |")
    return "\n".join(lines + ["", TABLE_END])


def table_span(document):
    start, end = document.find(TABLE_BEGIN), document.find(TABLE_END)
    if start < 0 or end < start:
        sys.exit(f"{DOCUMENT.relative_to(repository)} lacks the generated service table markers")
    return start, end + len(TABLE_END)


def main():
    image = build_image()
    files = [file["name"] for file in image["file"]]
    if files != [PROTO]:
        sys.exit(f"the business module must hold exactly {PROTO}, found {files}")
    protocol = Protocol(image["file"][0])
    failures = check_services(protocol) + check_fields(protocol) + check_browser_requests(protocol) + check_frozen(protocol) + check_names(protocol)
    document = DOCUMENT.read_text()
    start, end = table_span(document)
    table = service_table(protocol)
    if "--write" in sys.argv[1:]:
        DOCUMENT.write_text(document[:start] + table + document[end:])
    elif document[start:end] != table:
        failures.append(f"{DOCUMENT.relative_to(repository)}: the service table is stale; run make generate-proto")
    if failures:
        sys.exit("\n".join(failures))
    print(f"Business proto: {len(protocol.services)} services, {rpc_count(protocol)} RPCs; repository rules hold")


main()
