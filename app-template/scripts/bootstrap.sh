#!/usr/bin/env bash
# make bootstrap: restore the dependencies the lockfiles pin. One run at a time
# holds .local/bootstrap.lock where flock exists, so a run that starts while
# another restores waits for it and then finds nothing left to do; make
# generate waits for a run in progress the same way.
set -euo pipefail

root_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${root_directory}"
mkdir -p .local
if command -v flock >/dev/null; then
  exec 9>>.local/bootstrap.lock
  flock 9
fi

# fingerprint prints one SHA-256 over the names and contents of the files.
fingerprint() {
  python3 -c 'import hashlib, sys
digest = hashlib.sha256()
for name in sys.argv[1:]:
    with open(name, "rb") as source:
        digest.update(name.encode() + b"\0" + source.read() + b"\0")
print(digest.hexdigest())' "$@"
}

# A development image may ship a Go build cache of this template; it is
# copied once into the local cache, whose entries Go verifies on every read.
go_cache="$(go env GOCACHE)"
if [[ -d "${SUPERVERSE_GO_BUILD_CACHE_SEED:-}" && ! -e "${go_cache}/superverse-seeded" ]]; then
  mkdir -p "${go_cache}"
  cp -R -n "${SUPERVERSE_GO_BUILD_CACHE_SEED}/." "${go_cache}/"
  touch "${go_cache}/superverse-seeded"
  echo "bootstrap: copied the image's Go build cache"
fi

# The module caches may be new even when the workspace is not; a current cache
# answers at once.
go mod download
go -C tools/openapi mod download

# web/node_modules lives in the workspace, so it is restored again only when
# the npm lockfile changed or the installation is missing.
web_stamp=".local/bootstrap-web.sha256"
web_lock="$(fingerprint web/package.json web/package-lock.json)"
if [[ -f web/node_modules/.package-lock.json && -f "${web_stamp}" && "$(cat "${web_stamp}")" == "${web_lock}" ]]; then
  echo "bootstrap: web/node_modules already matches web/package-lock.json"
  exit 0
fi
if [[ -d "${SUPERVERSE_NPM_CACHE_SEED:-}" ]]; then
  npm_cache="${npm_config_cache:-${HOME}/.npm}"
  mkdir -p "${npm_cache}"
  cp -R -n "${SUPERVERSE_NPM_CACHE_SEED}/." "${npm_cache}/"
fi
npm --prefix web ci --prefer-offline
printf '%s\n' "${web_lock}" > "${web_stamp}"
echo "bootstrap: restored web/node_modules"
