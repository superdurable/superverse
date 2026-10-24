#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Keep the generated summary block of every internal/ package README current.

The block follows the README title. Its Purpose sentence is hand-written; the Callers and
Dependencies sentences and the mermaid graph derive from production imports. Public summaries
exclude private Engine callers, and the public architecture table lists public Purposes only.
--write regenerates every block and the
table, keeps each Purpose and creates a README for a new package."""
import argparse
from collections import defaultdict
import difflib
import os
from pathlib import Path
import re
import subprocess
import sys

MODULE = "github.com/superdurable/superversev2/"
WORD_LIMIT = 30
BEGIN = "<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->"
END = "<!-- END GENERATED PACKAGE SUMMARY -->"
SPECIFICATION = Path("docs/architecture/superverse-v2.md")
TABLE_BEGIN = "<!-- BEGIN GENERATED PACKAGE TABLE (scripts/check-package-readmes.py; edit the README Purpose) -->"
TABLE_END = "<!-- END GENERATED PACKAGE TABLE -->"
GENERATED = re.compile(r"^// Code generated .* DO NOT EDIT\.$", re.MULTILINE)
repository = Path(__file__).resolve().parents[1]

# Every module that an internal/ package imports, reviewed by hand. An "api" module is a
# client of the remote API it names; a "library" runs in process and is not listed. A major
# version suffix (/vN) is ignored, so a new module fails until someone classifies it.
EXTERNAL = {
    "github.com/anthropics/anthropic-sdk-go": ("api", "Claude and compatible Messages APIs"),
    "github.com/aws/aws-sdk-go-v2": ("library", "AWS SDK core"),
    "github.com/aws/aws-sdk-go-v2/config": ("library", "AWS SDK configuration"),
    "github.com/aws/aws-sdk-go-v2/credentials": ("library", "AWS SDK credentials"),
    "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs": ("api", "Amazon CloudWatch Logs API"),
    "github.com/aws/aws-sdk-go-v2/service/codebuild": ("api", "AWS CodeBuild API"),
    "github.com/aws/aws-sdk-go-v2/service/ecr": ("api", "Amazon ECR API"),
    "github.com/aws/aws-sdk-go-v2/service/eks": ("api", "Amazon EKS API"),
    "github.com/aws/aws-sdk-go-v2/service/iam": ("api", "AWS IAM API"),
    "github.com/aws/aws-sdk-go-v2/service/s3": ("api", "AWS S3 API"),
    "github.com/aws/aws-sdk-go-v2/service/sts": ("api", "AWS STS API"),
    "github.com/aws/smithy-go": ("library", "AWS Smithy runtime"),
    "github.com/ghodss/yaml": ("library", "YAML decoding"),
    "github.com/go-faster/errors": ("library", "ogen runtime errors"),
    "github.com/go-faster/jx": ("library", "ogen JSON runtime"),
    "github.com/go-git/go-billy": ("library", "go-git filesystem"),
    "github.com/go-git/go-git": ("library", "pure-Go Git"),
    "github.com/google/uuid": ("library", "UUID"),
    "github.com/jackc/pgx": ("api", "PostgreSQL API"),
    "github.com/ledongthuc/pdf": ("library", "PDF object reader"),
    "github.com/minio/madmin-go": ("api", "MinIO admin API"),
    "github.com/minio/minio-go": ("library", "MinIO credentials"),
    "github.com/ogen-go/ogen": ("library", "ogen runtime validation"),
    "github.com/superdurable/dex-connectors-library/sdkgo": ("library", "Connector SDK project configuration"),
    "github.com/superdurable/dex/blob-cache-go": ("library", "Dex payload blob cache"),
    "github.com/superdurable/dex/sdk-go": ("api", "Dex Flow API"),
    "go.temporal.io/api": ("api", "Temporal WorkflowService API"),
    "golang.org/x/crypto": ("library", "Argon2id"),
    "golang.org/x/mod": ("library", "semantic versions"),
    "golang.org/x/net": ("library", "HTTP/2 and h2c transport"),
    "golang.org/x/sync": ("library", "errgroup"),
    "google.golang.org/genproto/googleapis/rpc": ("library", "gRPC error details"),
    "google.golang.org/grpc": ("library", "gRPC"),
    "google.golang.org/protobuf": ("library", "protobuf"),
    "gopkg.in/yaml.v3": ("library", "YAML decoding"),
    "sigs.k8s.io/agent-sandbox": ("library", "sandboxd protocol types"),
}
# Remote APIs a package reaches over net/http, a raw gRPC connection or a subprocess (also one run in
# the Sandbox), which no module import reveals.
REACHES = {
    "internal/comm/platform/connectorlibrary": ("Connector catalog API",),
    "internal/dexai/agents/sandbox/docker": ("sandboxd API",),
    "internal/dexai/agents/sandbox/kube": ("Preview application API",),
    "internal/dexai/agents/sandbox/workspace/git": ("GitHub Git API",),
    "internal/dexai/agents/sandbox/workspace/sandboxd": ("sandboxd API",),
    "internal/dexai/agents/session/sandboxtools": ("Connector catalog API",),
    "internal/dexai/delivery/deployments/docker": ("Live application API",),
    "internal/dexai/delivery/deployments/kube": ("Live application API",),
    "internal/dexai/delivery/releases/build/hosted": ("GitHub REST API",),
    "internal/dexai/delivery/releases/build/local": ("Local Kind build fixture API",),
    "internal/comm/platform/dockerapi": ("Docker Engine API",),
    "internal/comm/platform/githubapp": ("GitHub REST API",),
    "internal/comm/platform/kubeapi": ("Kubernetes API",),
    "internal/comm/platform/kubeapi/kubeapitest": ("Kubernetes API",),
    "internal/dexai/projects/infra/docker": ("project Dex API", "application Worker readiness HTTP", "gateway API"),
    "internal/dexai/projects/infra/docker/dockertest": ("Docker Engine API", "gateway API"),
    "internal/dexai/projects/infra/docker/dockertest/configprobe": ("project S3 API",),
    "internal/dexai/projects/infra/kube/connect": ("project Dex API",),
    "internal/dexai/projects/projecttest": ("business API",),
    "internal/dexai/studio/confighost": ("Connector provider API",),
    "internal/dexai/studio/confighost/connectormanifest": ("GitHub Releases API", "Connector library source API", "OAuth provider API"),
    "internal/dexai/tenancy/accountdao": ("PostgreSQL API",),
    "internal/dexai/tenancy/connections": ("OAuth provider API",),
}


def go_list(*arguments):
    return subprocess.check_output(["go", "list", *arguments], cwd=repository,
                                   env={**os.environ, "GOWORK": "off"}, text=True).splitlines()


def words(sentence):
    """Count words; a backticked path is one word."""
    return len(re.sub(r"`[^`]*`", "path", sentence).split())


def join(items):
    return items[0] if len(items) == 1 else ", ".join(items[:-1]) + " and " + items[-1]


def collapsed(paths):
    """Group paths by top-level directory (internal/<name>, cmd, ...); a group of one keeps its path."""
    groups = defaultdict(list)
    for path in paths:
        parts = path.split("/")
        groups["/".join(parts[:2] if parts[0] == "internal" else parts[:1])].append(path)
    return [(members[0], None) if len(members) == 1 else (key + "/**", len(members))
            for key, members in sorted(groups.items())]


# An item is (path, None) for one package or (directory/**, count) for a collapsed group.
def code(item):
    path, count = item
    return f"`{path}`" if count is None else f"`{path}` ({count})"


def label(item):
    path, count = item
    return path if count is None else f"{path} ({count})"


def fit(items, render):
    """Return items, collapsed by top-level directory when their rendered sentence exceeds the limit."""
    return items if words(render(items)) <= WORD_LIMIT else collapsed([path for path, _ in items])


def callers_sentence(callers):
    if not callers:
        return "no production package imports this package."
    single = len(callers) == 1 and callers[0][1] is None
    return f"{join([code(item) for item in callers])} {'imports' if single else 'import'} this package."


def private_engine(path):
    return path == "internal/dexengine" or path.startswith("internal/dexengine/")


def dependencies_sentence(dependencies, apis):
    clauses = []
    if dependencies:
        clauses.append("imports " + join([code(item) for item in dependencies]))
    if apis:
        stems = [name.removesuffix(" API") for name in apis]
        clauses.append("calls the " + join(stems) + (" API" if len(stems) == 1 else " APIs"))
    if not clauses:
        return "it imports no other package of this module and calls no remote API."
    return "it " + " and ".join(clauses) + "."


class Package:
    def __init__(self, line):
        path, directory, files, imports = line.split("|")
        self.path, self.directory = path.removeprefix(MODULE), Path(directory)
        self.files, self.imports = files.split(), imports.split()
        self.local = sorted(name.removeprefix(MODULE) for name in self.imports if name.startswith(MODULE))
        self.callers = set()

    def described(self):
        """Every internal/ package with hand-written Go code has a README; a generated-only package has none."""
        return self.path.startswith("internal/") and any(
            not GENERATED.search((self.directory / name).read_text()) for name in self.files)


def load_packages():
    packages = {}
    for line in go_list("-f", '{{.ImportPath}}|{{.Dir}}|{{join .GoFiles " "}}|{{join .Imports " "}}',
                        "./cmd/...", "./internal/...", "./sandbox/coding/cmd/...", "./sandbox/coding/internal/..."):
        package = Package(line)
        packages[package.path] = package
    for package in packages.values():
        for imported in package.local:
            if imported in packages:
                packages[imported].callers.add(package.path)
    return packages


def remote_apis(packages):
    """Return the remote APIs of every internal/ package (classified module imports plus REACHES) and the
    problems of the EXTERNAL and REACHES tables."""
    modules = {}
    for line in go_list("-deps", "-f", "{{.ImportPath}}|{{if .Module}}{{.Module.Path}}{{end}}|{{.Standard}}", "./internal/..."):
        path, module, standard = line.split("|")
        modules[path] = None if standard == "true" else module
    apis, used, problems = defaultdict(set), set(), []
    for package in packages.values():
        if not package.path.startswith("internal/"):
            continue
        apis[package.path].update(REACHES.get(package.path, ()))
        for imported in package.imports:
            if imported.startswith(MODULE) or modules[imported] is None:
                continue
            key = re.sub(r"/v[0-9]+$", "", modules[imported])
            if key not in EXTERNAL:
                problems.append(f"{package.path}: classify module {modules[imported]} in EXTERNAL of {Path(__file__).name}")
                continue
            used.add(key)
            kind, name = EXTERNAL[key]
            if kind == "api":
                apis[package.path].add(name)
    problems += [f"{Path(__file__).name}: EXTERNAL lists {module}, which no internal/ package imports"
                 for module in sorted(set(EXTERNAL) - used)]
    problems += [f"{Path(__file__).name}: REACHES lists {path}, which is not a described package"
                 for path in sorted(REACHES) if path not in packages or not packages[path].described()]
    return apis, problems


class Summary:
    """The generated block of one package; a caller or dependency list collapses only when its sentence
    would otherwise exceed the limit."""

    def __init__(self, package, apis):
        self.path, self.apis = package.path, sorted(apis, key=str.casefold)
        callers = package.callers if private_engine(package.path) else [
            path for path in package.callers if not private_engine(path)]
        self.callers = fit([(path, None) for path in sorted(callers)], callers_sentence)
        self.dependencies = fit([(path, None) for path in package.local],
                                lambda items: dependencies_sentence(items, self.apis))
        self.sentences = {"Callers": callers_sentence(self.callers),
                          "Dependencies": dependencies_sentence(self.dependencies, self.apis)}

    def block(self, purpose):
        graph = [f'  self["{self.path}"]']
        graph += [f'  c{index}["{label(item)}"] --> self' for index, item in enumerate(self.callers)]
        graph += [f'  self --> d{index}["{label(item)}"]' for index, item in enumerate(self.dependencies)]
        graph += [f'  self --> e{index}(["{name}"])' for index, name in enumerate(self.apis)]
        return "\n".join([
            BEGIN, f"Purpose: {purpose}", "",
            f"Callers: {self.sentences['Callers']}", "",
            f"Dependencies: {self.sentences['Dependencies']}", "",
            "```mermaid", "flowchart LR", *graph, "```", END])


def regions(text, begin, end):
    return re.findall(re.escape(begin) + r".*?" + re.escape(end), text, re.DOTALL)


def summary_blocks(text):
    return regions(text, BEGIN, END)


def difference(found, expected):
    diff = list(difflib.unified_diff(found.splitlines(), expected.splitlines(), lineterm="", n=0))[2:]
    return "\n    " + "\n    ".join(diff[:12])


def purpose_of(text):
    """Return the Purpose of the first summary block, or "" when there is none."""
    blocks = summary_blocks(text)
    match = re.search(r"^Purpose: (.*)$", blocks[0], re.MULTILINE) if blocks else None
    return match[1].strip() if match else ""


def purpose_problem(purpose):
    if not purpose or "TODO" in purpose:
        return "write the Purpose sentence"
    plain = re.sub(r"`[^`]*`", "path", purpose)
    if not plain.endswith(".") or re.search(r"[.!?]\s", plain):
        return "Purpose must be exactly one sentence ending with a period"
    if words(purpose) > WORD_LIMIT:
        return f"Purpose has {words(purpose)} words, limit {WORD_LIMIT}"
    return None


def regenerate(text, block):
    """Return text with block right after its title and every earlier summary block removed."""
    title, _, rest = text.partition("\n")
    for old in summary_blocks(text):
        rest = rest.replace(old, "")
    rest = rest.strip("\n")
    return f"{title}\n\n{block}\n" + (f"\n{rest}\n" if rest else "")


def readme_problems(name, text, expected):
    title, _, rest = text.partition("\n")
    blocks = summary_blocks(text)
    problems = []
    if not title.startswith("# "):
        problems.append(f"{name}: must start with a `# Title` line")
    if not blocks:
        problems.append(f"{name}: generated summary block missing")
    elif len(blocks) > 1:
        problems.append(f"{name}: generated summary block duplicated")
    else:
        if not rest.lstrip("\n").startswith(BEGIN):
            problems.append(f"{name}: generated summary block must follow the title")
        if blocks[0] != expected:
            problems.append(f"{name}: generated summary block differs:{difference(blocks[0], expected)}")
    problem = purpose_problem(purpose_of(text))
    if problem:
        problems.append(f"{name}: {problem}")
    return problems


def package_table(purposes):
    """Return the specification's table of every described package with its README Purpose."""
    rows = [f"| [`{path.removeprefix('internal/')}`]"
            f"({os.path.relpath(Path(path, 'README.md'), SPECIFICATION.parent)}) | {purpose} |"
            for path, purpose in purposes]
    return "\n".join([TABLE_BEGIN, "", "| Package | Purpose |", "| --- | --- |", *rows, "", TABLE_END])


def table_problems(text, expected):
    tables = regions(text, TABLE_BEGIN, TABLE_END)
    if len(tables) != 1:
        return [f"{SPECIFICATION}: needs exactly one generated package table, found {len(tables)}"]
    if tables[0] != expected:
        return [f"{SPECIFICATION}: generated package table differs:{difference(tables[0], expected)}"]
    return []


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true", help="regenerate every block, keeping each Purpose")
    args = parser.parse_args()
    packages = load_packages()
    apis, problems = remote_apis(packages)
    scope = [package for _, package in sorted(packages.items()) if package.described()]
    purposes = []
    for package in scope:
        summary = Summary(package, apis[package.path])
        problems += [f"{package.path}: the {name} sentence has {words(sentence)} words even when collapsed, "
                     f"limit {WORD_LIMIT}; the package does too much, split it"
                     for name, sentence in summary.sentences.items() if words(sentence) > WORD_LIMIT]
        readme = package.directory / "README.md"
        name = readme.relative_to(repository)
        if not readme.exists() and not args.write:
            problems.append(f"{name}: missing")
            continue
        text = readme.read_text() if readme.exists() else f"# {package.path}\n"
        expected = summary.block(purpose_of(text) or "TODO")
        if args.write and text.startswith("# "):
            text = regenerate(text, expected)
            readme.write_text(text)
        problems += readme_problems(name, text, expected)
        if not private_engine(package.path):
            purposes.append((package.path, purpose_of(text)))
    specification = (repository / SPECIFICATION).read_text()
    table = package_table(purposes)
    if args.write:
        for old in regions(specification, TABLE_BEGIN, TABLE_END):
            specification = specification.replace(old, table)
        (repository / SPECIFICATION).write_text(specification)
    problems += table_problems(specification, table)
    problems += [f"{path.relative_to(repository)}: delete it; package documentation belongs in the README"
                 for path in sorted((repository / "internal").rglob("doc.go"))]
    if problems:
        print("\n".join(problems + ["run `make generate-package-readmes`, then write the Purpose sentence"]), file=sys.stderr)
        return 1
    print(f"Package READMEs: {len(scope)} generated summaries and the specification's package table are current")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
