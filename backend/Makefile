# Needs a Go toolchain. On a host without Go, run ../package.sh, which runs
# these targets inside the build container defined by ../Containerfile.

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
