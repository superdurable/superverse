# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Explicit, reviewed published dependencies for the hosted-eligible Kind lane."""
import hashlib
import json
from pathlib import Path
import re
import subprocess
import urllib.parse
import urllib.request

from operator_errors import require
from paths import ROOT

DIRECTORY = ROOT / "infra-as-code/dependencies/published"
RELEASE_PATH = "/superdurable/dex-connectors-library/releases/download/"
RELEASE_ASSET = "/connector-release.json"
MAX_RELEASE_METADATA = 16 * 1024 * 1024


def release_identity(url):
    # Match the official Dex module/tag grammar. Only tag separators may be
    # percent-encoded, once; do not normalize arbitrary URL paths or escapes.
    require(isinstance(url, str) and all(32 < ord(char) < 127 for char in url), "Artifact authority requires an unambiguous official release URL")
    parsed = urllib.parse.urlsplit(url)
    require(parsed.geturl() == url and parsed.scheme == "https" and parsed.netloc == "github.com" and not parsed.query and not parsed.fragment and
            parsed.path.startswith(RELEASE_PATH) and parsed.path.endswith(RELEASE_ASSET),
            "Artifact authority must be the official connector-release.json asset")
    encoded_tag = parsed.path[len(RELEASE_PATH):-len(RELEASE_ASSET)]
    require(re.fullmatch(r"(?:[a-z0-9./-]|%2[fF])+", encoded_tag), "Connector release tag contains an unsupported URL escape")
    tag = urllib.parse.unquote(encoded_tag)
    require(re.fullmatch(r"connectors/[a-z0-9][a-z0-9-]*(?:/[a-z0-9][a-z0-9-]*)*/v[0-9]+\.[0-9]+\.[0-9]+", tag),
            "Artifact authority requires an exact official connector module release tag")
    module, version = tag.rsplit("/", 1)
    return tag, "github.com/superdurable/dex-connectors-library/" + module, version


def profile_identity():
    directory = ROOT / "internal/delivery/releases/build/build-profiles/go-react-v1"
    parts = [(directory / name).read_bytes() for name in ("profile.json", "Dockerfile")]
    require(json.loads(parts[0]).get("id") == "go-react-v1", "Published dependencies require the go-react-v1 BuildProfile")
    return {"id": "go-react-v1", "version": "bp1", "sha256": hashlib.sha256(parts[0]).hexdigest(),
            "digest": hashlib.sha256(b"\0".join(parts)).hexdigest()}


def validate(value):
    require(set(value) == {"schema", "buildProfile", "projectDexImage", "artifactAuthority"} and value.get("schema") == 1,
            "Unsupported published dependency approval format")
    require(value.get("buildProfile") == profile_identity(), "Published BuildProfile differs from the reviewed immutable source")
    image = value.get("projectDexImage", "")
    require(re.fullmatch(r"[a-z0-9][a-z0-9./:_-]+@sha256:[a-f0-9]{64}", image) and not any(x in image for x in ("live-source-", "local-", "localhost", "127.0.0.1")),
            "A published digest-qualified project Dex image is required")
    authority = value.get("artifactAuthority", {})
    require(set(authority) == {"kind", "sourceCommit", "sha256", "url"} and authority.get("kind") == "published" and
            re.fullmatch(r"[a-f0-9]{40}", authority.get("sourceCommit", "")) and re.fullmatch(r"[a-f0-9]{64}", authority.get("sha256", "")),
            "Published connector artifact authority requires exact reviewed source and bytes")
    release_identity(authority.get("url", ""))
    return value


def verify_release(authority):
    tag, module, version = release_identity(authority["url"])
    try:
        with urllib.request.urlopen(authority["url"], timeout=30) as response:
            data = response.read(MAX_RELEASE_METADATA + 1)
    except OSError:
        raise RuntimeError("Published artifact authority could not be retrieved; no local substitute is allowed") from None
    require(len(data) <= MAX_RELEASE_METADATA and hashlib.sha256(data).hexdigest() == authority["sha256"],
            "Published artifact authority bytes differ from reviewed approval")
    try:
        release = json.loads(data)
    except (ValueError, UnicodeError):
        raise RuntimeError("Published connector release metadata is invalid") from None
    require(isinstance(release, dict) and release.get("tag") == tag and release.get("modulePath") == module and
            release.get("version") == version and release.get("sourceSha") == authority["sourceCommit"],
            "Published connector release identity differs from reviewed approval")


def load(path, *, verify_publication=False):
    path = Path(path).resolve()
    require(path.is_relative_to(DIRECTORY) and path.is_file() and not path.is_symlink(),
            "Published dependencies require a reviewed file under infra-as-code/dependencies/published")
    relative = path.relative_to(ROOT).as_posix()
    try:
        # Exact bytes, not commands.run's stripped text, are compared with the file.
        committed = subprocess.check_output(["git", "show", "HEAD:" + relative], cwd=ROOT, stderr=subprocess.DEVNULL, timeout=60)
    except subprocess.CalledProcessError:
        raise RuntimeError("Published dependency approval must already be committed") from None
    require(path.read_bytes() == committed, "Published dependency approval differs from committed bytes")
    value = validate(json.loads(committed))
    if verify_publication:
        verify_release(value["artifactAuthority"])
    return value
