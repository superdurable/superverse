#!/bin/sh
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0


set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
scratch_directory=$(mktemp -d "${TMPDIR:-/tmp}/superverse-v2-generated.XXXXXX")
trap 'rm -r "$scratch_directory"' EXIT HUP INT TERM

cp -R "$repository_root/internal/dexai/agents/session/toolinput/generated" "$scratch_directory/tool-input-schema"
skill_copy="$repository_root/internal/dexai/agents/session/conversation/prompts/v1/connector_contributor_skill.md"
cp "$skill_copy" "$scratch_directory/connector_contributor_skill.md"

GOWORK=off go generate "$repository_root/internal/dexai/agents/session/toolinput"
GOWORK=off go generate "$repository_root/internal/dexai/agents/session/conversation"

diff -ru "$scratch_directory/tool-input-schema" "$repository_root/internal/dexai/agents/session/toolinput/generated"
# The develop subagent's prompt embeds the connector contributor skill that the Coding image ships.
diff -u "$scratch_directory/connector_contributor_skill.md" "$skill_copy"
