#!/bin/sh
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0


# The embedded template archive and the Studio initial design are generated from
# app-template/. Regenerate both and fail when the committed copies differ.
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

# The Agent's repository context inlines only the first 6 KiB of AGENTS.md
# (maximumAgentInstructions in repository_context.go of the workspace Git package).
if [ "$(wc -c < "$repository_root/app-template/AGENTS.md")" -gt 6144 ]; then
  echo "app-template/AGENTS.md exceeds the 6 KiB the Agent's repository context inlines" >&2
  exit 1
fi
scratch_directory=$(mktemp -d "${TMPDIR:-/tmp}/superverse-app-template.XXXXXX")
trap 'rm -r "$scratch_directory"' EXIT HUP INT TERM

archive="$repository_root/internal/dexai/app/apptemplate/app-template.tar"
design="$repository_root/web/studio/lib/generated/template-design/app-template.json"
cp "$archive" "$scratch_directory/app-template.tar"
cp "$design" "$scratch_directory/app-template.json"

GOWORK=off go generate "$repository_root/internal/dexai/app/apptemplate"

if ! cmp -s "$scratch_directory/app-template.tar" "$archive" || ! cmp -s "$scratch_directory/app-template.json" "$design"; then
  echo "app-template/ changed: run make generate-app-template and commit the archive and design" >&2
  exit 1
fi
