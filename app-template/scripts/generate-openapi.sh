#!/usr/bin/env bash
set -euo pipefail

root_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
temporary_parent="${root_directory}/.local"
mkdir -p "${temporary_parent}"
temporary_directory="$(mktemp -d "${temporary_parent}/openapi-codegen.XXXXXX")"
trap 'rm -rf -- "${temporary_directory}"' EXIT

go -C "${root_directory}/tools/openapi" tool ogen \
  --target "${temporary_directory}/go" \
  --package generated \
  ../../openapi/openapi.yaml

(
  cd "${root_directory}/web"
  OPENAPI_OUTPUT="${temporary_directory}/web" npm run generate
)

rm -rf -- \
  "${root_directory}/internal/api/generated" \
  "${root_directory}/web/src/api/generated"
mkdir -p \
  "${root_directory}/internal/api" \
  "${root_directory}/web/src/api"
mv "${temporary_directory}/go" "${root_directory}/internal/api/generated"
mv "${temporary_directory}/web" "${root_directory}/web/src/api/generated"
