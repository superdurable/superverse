#!/bin/sh
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0


# Install one reviewed release binary after verifying its pinned SHA-256.
# Usage: install-pinned-binary.sh dexcli|osv-scanner|buf|gitleaks DESTINATION VERSION
set -eu

tool=${1:?tool name is required}
destination=${2:?destination path is required}
version=${3:?release version is required}

# Reviewed pins. scripts/update-dex-versions.py rewrites the dexcli lines.
dexcli_commit=dfb1a1e05c99eb4b3211b921f2bf1b9ed273dbf3
pinned_checksum() {
  case "$1" in
    "dexcli v1.14.2 darwin_amd64") echo 40f81562f499e926a20df2a7e2e4c1d1230b408bc4982ede25e9b2ab143aa5c8 ;;
    "dexcli v1.14.2 darwin_arm64") echo 40f3d5f8724380546ae5690b843d605e5d075037dd12eadd0f19bae838ae2f0c ;;
    "dexcli v1.14.2 linux_amd64") echo 380052570e021339e32dd50f20a3c92ea94fccf0ad2f611745f9e304b53ae409 ;;
    "dexcli v1.14.2 linux_arm64") echo 39b0588346025ba0818f2c450ff3bb162aec0ae01f46fc78d44f5b2ffd1db611 ;;
    "osv-scanner v2.5.1 darwin_amd64") echo 9f89beb6c3d784893cb1cae0a3d56c529bfe91075418c2f9440c45b79654198b ;;
    "osv-scanner v2.5.1 darwin_arm64") echo 75c44d6332f892a1e56286f4105a98ed751ae28d215ca0a8b65cc00d84103054 ;;
    "osv-scanner v2.5.1 linux_amd64") echo f9f25499a2c8cc367b3af45df2ea7eeca7fbccceab9c35079968f4b3652194be ;;
    "osv-scanner v2.5.1 linux_arm64") echo 3d0f5aa5a6baa8eb32bcef247388e149ef6030a6634ccae6fa0d62681fb27a6d ;;
    "buf v1.72.0 darwin_amd64") echo eb815a2708d4a43d31799049d5a2987ea81d0a9e98b53976d47bd1e78d154a8f ;;
    "buf v1.72.0 darwin_arm64") echo 5176f23a6118b9978de1340c3e3301a4ed0d48e16a669510be44b4c355170d57 ;;
    "buf v1.72.0 linux_amd64") echo 8720830e26a733da55bb89bcd3cb44849c0965fc0c44fb5d691cccdc64dca5af ;;
    "buf v1.72.0 linux_arm64") echo bdbb275fb9624104ef4d8513d269cc410a153138646e67136cb3f8cc185be289 ;;
    "gitleaks v8.30.0 darwin_amd64") echo ca221d012d247080c2f6f61f4b7a83bffa2453806b0c195c795bbe9a8c775ed5 ;;
    "gitleaks v8.30.0 darwin_arm64") echo b251ab2bcd4cd8ba9e56ff37698c033ebf38582b477d21ebd86586d927cf87e7 ;;
    "gitleaks v8.30.0 linux_amd64") echo 79a3ab579b53f71efd634f3aaf7e04a0fa0cf206b7ed434638d1547a2470a66e ;;
    "gitleaks v8.30.0 linux_arm64") echo b4cbbb6ddf7d1b2a603088cd03a4e3f7ce48ee7fd449b51f7de6ee2906f5fa2f ;;
    *) return 1 ;;
  esac
}

case "$(uname -s)" in
  Darwin) operating_system=darwin ;;
  Linux) operating_system=linux ;;
  *) echo "unsupported operating system: $(uname -s)" >&2; exit 64 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) architecture=arm64 ;;
  x86_64|amd64) architecture=amd64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 64 ;;
esac
platform=${operating_system}_${architecture}
checksum=$(pinned_checksum "$tool $version $platform") || {
  echo "no checksum is pinned for $tool $version $platform" >&2; exit 64
}

case "$tool" in
  dexcli)
    asset="dexcli_${version}_${platform}.tar.gz"
    url="https://github.com/superdurable/dex/releases/download/cli-$version/$asset"
    # The archive holds exactly the binary and its license notices.
    archive_members="LEGACY_NOTICES.md LICENSE LICENSES/ LICENSES/Apache-2.0.txt LICENSES/EPL-2.0.txt LICENSES/MIT.txt dexcli" ;;
  osv-scanner)
    asset="osv-scanner_${platform}"
    url="https://github.com/google/osv-scanner/releases/download/$version/$asset"
    archive_members="" ;;
  buf)
    case "$operating_system" in
      darwin) release_system=Darwin ;;
      linux) release_system=Linux ;;
    esac
    case "$platform" in
      darwin_arm64) release_architecture=arm64 ;;
      linux_arm64) release_architecture=aarch64 ;;
      *_amd64) release_architecture=x86_64 ;;
    esac
    asset="buf-$release_system-$release_architecture"
    url="https://github.com/bufbuild/buf/releases/download/$version/$asset"
    archive_members="" ;;
  gitleaks)
    case "$platform" in
      *_amd64) release_architecture=x64 ;;
      *_arm64) release_architecture=arm64 ;;
    esac
    asset="gitleaks_${version#v}_${operating_system}_${release_architecture}.tar.gz"
    url="https://github.com/gitleaks/gitleaks/releases/download/$version/$asset"
    archive_members="LICENSE README.md gitleaks" ;;
  *) echo "unknown pinned tool: $tool" >&2; exit 64 ;;
esac

temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/superverse-v2-$tool.XXXXXX")
trap 'rm -r "$temporary_directory"' EXIT HUP INT TERM
download="$temporary_directory/$asset"
curl --fail --location --silent --show-error \
  --proto '=https' --proto-redir '=https' --tlsv1.2 \
  --retry 5 --retry-all-errors --retry-delay 2 \
  --connect-timeout 15 --max-time 300 --max-filesize 134217728 \
  --output "$download" "$url"

if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum=$(sha256sum "$download" | awk '{print $1}')
else
  actual_checksum=$(shasum -a 256 "$download" | awk '{print $1}')
fi
if [ "$actual_checksum" != "$checksum" ]; then
  echo "$tool $version checksum mismatch" >&2
  exit 65
fi

binary=$download
if [ -n "$archive_members" ]; then
  actual_members=$(tar -tzf "$download" | LC_ALL=C sort)
  expected_members=$(printf '%s\n' $archive_members | LC_ALL=C sort)
  if [ "$actual_members" != "$expected_members" ]; then
    echo "$tool archive contains unexpected paths" >&2
    exit 65
  fi
  mkdir "$temporary_directory/unpacked"
  tar -xzf "$download" -C "$temporary_directory/unpacked"
  binary="$temporary_directory/unpacked/$tool"
fi
if [ ! -f "$binary" ] || [ -L "$binary" ]; then
  echo "$tool release did not contain one regular binary" >&2
  exit 65
fi

if [ "$tool" = dexcli ]; then
  case "$("$binary" version)" in
    "dexcli $version (commit $dexcli_commit,"*|"dexcli $version (commit $dexcli_commit)"*) ;;
    *) echo "dexcli version or commit differs from the reviewed pin" >&2; exit 65 ;;
  esac
fi
if [ "$tool" = buf ]; then
  chmod 0755 "$binary"
  if [ "$("$binary" --version)" != "${version#v}" ]; then
    echo "buf version differs from the reviewed pin" >&2
    exit 65
  fi
fi

mkdir -p "$(dirname "$destination")"
install -m 0755 "$binary" "$destination"
