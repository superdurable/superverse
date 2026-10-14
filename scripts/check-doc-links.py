#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Check the relative links and anchors of the repository's Markdown documents.

Every Markdown link to a repository path must name an existing file or
directory, and every `#anchor` must name a heading of its target, as GitHub
derives heading anchors. External links, links inside code and links to
generated or vendored trees are not checked. The Cursor rule copies the text of
AGENTS.md, so its links resolve from the repository root.
"""

from __future__ import annotations

import re
import argparse
import subprocess
import sys
from functools import cache
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parent.parent

# Trees whose Markdown is generated, vendored or another project's own.
SKIPPED_PREFIXES = (
    "app-template/",
    "dex-skills/",
    "gen/",
    "third_party/",
    "web/studio/node_modules/",
)

LINK = re.compile(r"(?<!!)\[(?:[^\[\]]|\[[^\]]*\])*\]\(\s*<?([^)\s>]+)>?(?:\s+\"[^\"]*\")?\s*\)")
IMAGE = re.compile(r"!\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+\"[^\"]*\")?\s*\)")
REFERENCE = re.compile(r"^\s{0,3}\[[^\]]+\]:\s*<?(\S+?)>?(?:\s+\"[^\"]*\")?\s*$", re.MULTILINE)
FENCE = re.compile(r"^\s*(```|~~~)")
INLINE_CODE = re.compile(r"(`+)[\s\S]*?\1")
HEADING = re.compile(r"^\s{0,3}#{1,6}\s+(.*?)\s*#*\s*$")
HTML_ANCHOR = re.compile(r"<a\s+(?:name|id)=\"([^\"]+)\"")


def markdown_files() -> list[Path]:
    listed = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "*.md", "*.mdc"],
        cwd=ROOT, check=True, capture_output=True, text=True,
    ).stdout.split()
    return [ROOT / name for name in listed if not name.startswith(SKIPPED_PREFIXES) and (ROOT / name).is_file()]


def prose_lines(text: str, keep_code: bool = False) -> list[tuple[int, str]]:
    """The lines outside fenced code, with inline code blanked unless kept;
    a code span may continue over several lines."""
    lines, fenced = [], False
    for line in text.splitlines():
        if FENCE.match(line):
            fenced = not fenced
            lines.append("")
        else:
            lines.append("" if fenced else line)
    prose = "\n".join(lines)
    if not keep_code:
        prose = INLINE_CODE.sub(lambda span: re.sub(r"[^\n]", " ", span.group(0)), prose)
    return list(enumerate(prose.split("\n"), 1))


def github_slug(heading: str) -> str:
    """The anchor GitHub derives from a heading: its text without markup,
    lower-cased, with punctuation removed and spaces turned into hyphens."""
    parts = re.split(r"(`+[^`]*`+)", heading)
    text = "".join(part.strip("`") if part.startswith("`") else re.sub(r"<[^>]+>", "", part) for part in parts)
    text = re.sub(r"!?\[([^\]]*)\]\([^)]*\)", r"\1", text).strip().lower()
    text = re.sub(r"[^\w\- ]", "", text)
    return text.replace(" ", "-")


@cache
def anchors(path: Path) -> frozenset[str]:
    seen: dict[str, int] = {}
    found = set()
    for _, line in prose_lines(path.read_text(encoding="utf-8"), keep_code=True):
        found.update(HTML_ANCHOR.findall(line))
        match = HEADING.match(line)
        if not match:
            continue
        slug = github_slug(match.group(1))
        count = seen.get(slug, 0)
        seen[slug] = count + 1
        found.add(slug if count == 0 else f"{slug}-{count}")
    return frozenset(found)


def check(path: Path) -> list[str]:
    errors = []
    for number, line in prose_lines(path.read_text(encoding="utf-8")):
        targets = LINK.findall(line) + IMAGE.findall(line) + REFERENCE.findall(line)
        for target in targets:
            if re.match(r"^[a-z][a-z0-9+.-]*:", target, re.IGNORECASE) or target.startswith("//"):
                continue
            name, _, anchor = target.partition("#")
            name = unquote(name.split("?", 1)[0])
            base = ROOT if path.suffix == ".mdc" else path.parent
            resolved = path if not name else (ROOT / name.lstrip("/") if name.startswith("/") else base / name)
            location = f"{path.relative_to(ROOT)}:{number}"
            if not resolved.exists():
                errors.append(f"{location}: missing link target {target}")
                continue
            if anchor and resolved.is_file() and resolved.suffix in (".md", ".mdc"):
                if unquote(anchor).lower() not in anchors(resolved.resolve()):
                    errors.append(f"{location}: missing anchor {target}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--public-boundary", type=Path, metavar="PATHS_YAML",
                        help="check public Markdown against the export path classification")
    args = parser.parse_args()
    if args.public_boundary:
        return check_public_boundary(args.public_boundary)
    errors = [error for path in markdown_files() for error in check(path)]
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        print(f"{len(errors)} broken documentation links", file=sys.stderr)
        return 1
    return 0


def check_public_boundary(paths_yaml: Path) -> int:
    """Check links and process references in Markdown selected for export."""
    # The export manifest is the only path policy; this optional private-repo
    # check uses the same classification function as the exporter.
    sys.path.insert(0, str(ROOT / "scripts/public-export"))
    from export import classify, read_rules

    rules = read_rules(paths_yaml)
    def is_private(name: str) -> bool:
        return classify(name, rules) != "public"

    process = re.compile(r"superdurable/superversev2#\d+|\bPR\s*#\d+|\(#\d+\)|(?<![\w])#\d{2,}\b", re.I)
    errors = []
    listed = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "*.md", "*.mdc"],
        cwd=ROOT, check=True, capture_output=True, text=True,
    ).stdout.splitlines()
    for name in listed:
        document = ROOT / name
        if not document.is_file():
            continue
        relative = document.relative_to(ROOT).as_posix()
        if is_private(relative):
            continue
        for number, line in enumerate(document.read_text(encoding="utf-8").splitlines(), 1):
            if process.search(line):
                errors.append(f"{relative}:{number}: private pull request reference")
        for number, line in prose_lines(document.read_text(encoding="utf-8")):
            location = f"{relative}:{number}"
            for target in LINK.findall(line) + IMAGE.findall(line) + REFERENCE.findall(line):
                if re.match(r"^[a-z][a-z0-9+.-]*:", target, re.I) or target.startswith(("//", "#")):
                    continue
                name = unquote(target.split("#", 1)[0].split("?", 1)[0])
                if not name:
                    continue
                base = ROOT if document.suffix == ".mdc" else document.parent
                resolved = (ROOT / name.lstrip("/") if name.startswith("/") else base / name).resolve()
                if not resolved.is_relative_to(ROOT):
                    continue
                if is_private(resolved.relative_to(ROOT).as_posix()):
                    errors.append(f"{location}: link to private path {target}")
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        print(f"{len(errors)} public boundary violations", file=sys.stderr)
        return 1
    print("Public boundary: no private links or pull request references")
    return 0


if __name__ == "__main__":
    sys.exit(main())
