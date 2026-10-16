#!/usr/bin/env bash
set -euo pipefail

root_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${root_directory}"
temporary_parent="${root_directory}/.local"
mkdir -p "${temporary_parent}"

# fingerprint prints one SHA-256 over the names and contents of the files and
# of every file under the directories; a missing path counts as absent.
fingerprint() {
  python3 -c 'import hashlib, os, sys
digest = hashlib.sha256()
for root in sys.argv[1:]:
    names = [root] if os.path.isfile(root) else sorted(os.path.join(d, f) for d, _, fs in os.walk(root) for f in fs)
    digest.update(root.encode() + (b"\1" if names else b"\2"))
    for name in names:
        with open(name, "rb") as source:
            digest.update(name.encode() + b"\0" + source.read() + b"\0")
print(digest.hexdigest())' "$@"
}

# The generators run again only when an input or a generated file changed since
# the last generation, so make test-app after make check-static generates once.
stamp="${temporary_parent}/generate.sha256"
inputs="$(fingerprint openapi/openapi.yaml tools/openapi/go.mod tools/openapi/go.sum web/package-lock.json web/openapi-ts.config.ts scripts/generate-openapi.sh)"
if [[ -f "${stamp}" && "$(cat "${stamp}")" == "${inputs} $(fingerprint internal/api/generated web/src/api/generated)" ]]; then
  echo "generate: internal/api/generated and web/src/api/generated match openapi/openapi.yaml"
  exit 0
fi
rm -f "${stamp}"

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
printf '%s %s\n' "${inputs}" "$(fingerprint internal/api/generated web/src/api/generated)" > "${stamp}.$$"
mv "${stamp}.$$" "${stamp}"
