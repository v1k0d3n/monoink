#!/usr/bin/env bash
# Build or test monoinkd inside a container, so the host needs only podman
# (or docker). Usage: ./build.sh [build|test|tidy|shell]
set -euo pipefail

cd "$(dirname "$0")"
GO_IMAGE=${GO_IMAGE:-docker.io/library/golang:1.26}
ENGINE=${ENGINE:-$(command -v podman || command -v docker)}
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}

run() {
	"$ENGINE" run --rm -v "$PWD":/src:Z -w /src \
		-v monoink-gocache:/root/.cache -v monoink-gomod:/go/pkg/mod \
		"$GO_IMAGE" "$@"
}

case "${1:-build}" in
build) run make build VERSION="$VERSION" ;;
test) run make test ;;
tidy) run go mod tidy ;;
shell) "$ENGINE" run --rm -it -v "$PWD":/src:Z -w /src "$GO_IMAGE" bash ;;
*)
	echo "usage: $0 [build|test|tidy|shell]" >&2
	exit 2
	;;
esac
