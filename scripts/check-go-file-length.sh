#!/bin/sh
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0


# Hand-written Go files stay at most 1000 lines. A Dex Flow DSL file (*_flow.go)
# may reach 1600 lines because the pinned dexcli analyzes only that file, so it
# must declare every Step, RPC and primitive of its Flow. In a Flow DSL file
# (app-template/ included) a comment block is at most two lines: consecutive //
# lines form one block, "// dex:" annotation lines do not count, and the bare //
# line that separates a description from its annotation lines ends the block.
# Only the exact seven-line Sustainable Use License header at the file start is exempt.
set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repository_root"

status=0

go_files=$(git ls-files --cached --others --exclude-standard -- '*.go' \
  ':(exclude)gen/*' ':(exclude)*/generated/*' ':(exclude)*_test.go' ':(exclude)app-template/*')
for file in $go_files; do
  [ -f "$file" ] || continue
  limit=1000
  case "$file" in *_flow.go) limit=1600 ;; esac
  lines=$(($(wc -l <"$file")))
  if [ "$lines" -gt "$limit" ]; then
    printf '%s: %s lines, limit %s\n' "$file" "$lines" "$limit" >&2
    status=1
  fi
done

flow_files=$(git ls-files --cached --others --exclude-standard -- '*_flow.go' ':(exclude)*/generated/*' ':(exclude)*_test.go')
if [ -n "$flow_files" ]; then
  # shellcheck disable=SC2086
  awk '
    BEGIN {
      header[1] = "// Copyright (c) 2026 Super Durable, Inc."
      header[2] = "//"
      header[3] = "// Licensed under the Sustainable Use License 1.0."
      header[4] = "// You may not use this file except in compliance with the License."
      header[5] = "// See the LICENSE file in the repository root."
      header[6] = "//"
      header[7] = "// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0"
    }
    function close_block() {
      if (count > 2) {
        printf "%s:%d: comment block of %d lines, limit 2\n", FILENAME, start, count > "/dev/stderr"
        bad = 1
      }
      count = 0
    }
    FNR == 1 { close_block(); pending = 0; exact_header = 1 }
    FNR <= 7 {
      if ($0 != header[FNR]) exact_header = 0
      if (FNR == 7 && exact_header) { count = 0; pending = 0; next }
    }
    /^[ \t]*\/\/ dex:/ {
      if (pending) { pending = 0; close_block() }
      next
    }
    /^[ \t]*\/\/[ \t]*$/ {
      if (pending) count++
      else if (count == 0) start = FNR
      pending = 1
      next
    }
    /^[ \t]*\/\// {
      if (pending) { count++; pending = 0 }
      if (count == 0) start = FNR
      count++
      next
    }
    { if (pending) { count++; pending = 0 } close_block() }
    END { if (pending) count++; close_block(); exit bad }
  ' $flow_files || status=1
fi

[ "$status" -eq 0 ] || exit "$status"
echo "Go file length: at most 1000 lines, 1600 for Flow DSL files; Flow DSL comment blocks at most 2 lines"
