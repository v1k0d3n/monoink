# Needs a Go toolchain. On a host without Go, run ../package.sh, which runs
# these targets inside the build container defined by ../Containerfile.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test previews clean

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
		-ldflags "-s -w -X main.version=$(VERSION)" -o out/monoinkd ./cmd/monoinkd

test:
	go vet ./...
	go test ./...

# Render every screen (light and dark) into the docs. Commit the result:
# CI fails if a change to a screen isn't reflected in these images.
previews:
	go run ./cmd/previews -out ../docs/images/screens

clean:
	rm -rf out
