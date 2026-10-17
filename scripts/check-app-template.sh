#!/bin/sh
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0


# The embedded template archives with the previous ones, the catalog, the search
# index and match rules, the Studio initial design and Studio's copies of the
# search index and the example categories are generated from app-template/.
# Regenerate them and fail when the committed copies differ.
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

# The generator composes every example of app-template/ and refuses one whose
# AGENTS.md exceeds the 6 KiB the Agent's repository context inlines, whose
# card the Session's card code refuses or whose trace names a missing test, and
# fails when a labelled request starts from another example than expected.
scratch_directory=$(mktemp -d "${TMPDIR:-/tmp}/superverse-app-template.XXXXXX")
trap 'rm -r "$scratch_directory"' EXIT HUP INT TERM

outputs="$repository_root/internal/dexai/app/apptemplate"
design="$repository_root/web/studio/lib/generated/template-design/app-template.json"
studio_catalog="$repository_root/web/studio/lib/generated/example-catalog"
mkdir "$scratch_directory/before"
cp "$outputs/app-template.tar" "$design" "$scratch_directory/before/"
cp -R "$outputs/examples" "$scratch_directory/before/examples"
cp -R "$studio_catalog" "$scratch_directory/before/example-catalog"

GOWORK=off go generate "$repository_root/internal/dexai/app/apptemplate"

if ! cmp -s "$scratch_directory/before/app-template.tar" "$outputs/app-template.tar" ||
  ! cmp -s "$scratch_directory/before/app-template.json" "$design" ||
  ! diff -r "$scratch_directory/before/examples" "$outputs/examples" >/dev/null ||
  ! diff -r "$scratch_directory/before/example-catalog" "$studio_catalog" >/dev/null; then
  echo "app-template/ changed: run make generate-app-template and commit the archives, the catalog, the search index, the match rules and the design" >&2
  exit 1
fi

# The server and Studio rank the examples with the same index.
if ! cmp -s "$outputs/examples/example-search.json" "$studio_catalog/example-search.json"; then
  echo "the server's and Studio's example search indexes differ" >&2
  exit 1
fi
