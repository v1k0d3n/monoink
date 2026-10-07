# Runs inside the build container or CI (needs a Go toolchain).
# On a host without Go, use ./build.sh, which wraps these targets in podman.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test clean

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
		-ldflags "-s -w -X main.version=$(VERSION)" -o out/monoinkd ./cmd/monoinkd

test:
	go vet ./...
	go test ./...

clean:
	rm -rf out
