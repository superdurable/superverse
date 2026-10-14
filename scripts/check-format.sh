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
# Check source changes, including new files, without traversing ignored local
# diagnostics or build caches nested below a source directory.
unformatted_go=$(
  cd "$repository_root"
  git ls-files -z --cached --others --exclude-standard -- \
    'cmd/*.go' 'internal/*.go' 'gen/*.go' 'sandbox/coding/cmd/*.go' 'sandbox/coding/internal/*.go' |
    xargs -0 gofmt -l
)
if [ -n "$unformatted_go" ]; then
  printf '%s\n' "Go files require gofmt:" "$unformatted_go" >&2
  exit 1
fi
