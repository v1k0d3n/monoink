#!/usr/bin/env bash
# Build the backend and frontend in containers and produce a Decky plugin
# zip at out/monoink.zip (install via Decky → Developer → Install from ZIP).
set -euo pipefail

cd "$(dirname "$0")"
ENGINE=${ENGINE:-$(command -v podman || command -v docker)}
NODE_IMAGE=${NODE_IMAGE:-docker.io/library/node:22}
VERSION=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' package.json | head -1)

echo "==> backend (monoinkd $VERSION)"
VERSION=$VERSION backend/build.sh test
VERSION=$VERSION backend/build.sh build

echo "==> frontend"
"$ENGINE" run --rm -v "$PWD":/src:Z -w /src -v monoink-pnpm:/root/.local/share/pnpm \
	-e COREPACK_ENABLE_DOWNLOAD_PROMPT=0 "$NODE_IMAGE" \
	sh -c 'corepack enable >/dev/null 2>&1 && pnpm install --frozen-lockfile >/dev/null && pnpm typecheck && pnpm build'

echo "==> package"
stage=out/stage/monoink
rm -rf out/stage out/monoink.zip
mkdir -p "$stage/dist" "$stage/bin"
cp plugin.json package.json main.py LICENSE NOTICE README.md "$stage/"
cp dist/index.js "$stage/dist/"
cp backend/out/monoinkd "$stage/bin/"
chmod 755 "$stage/bin/monoinkd"
(cd out/stage && zip -qr ../monoink.zip monoink)
rm -rf out/stage
ls -l out/monoink.zip
