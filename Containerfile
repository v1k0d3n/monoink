# The complete build environment for monoink: Go for the backend, Node and
# pnpm for the Decky panel, plus make and zip for packaging. Local builds
# (./package.sh) and CI releases both run inside this image, so everyone
# builds with exactly the same toolchain.
#
#   podman build -t monoink-build -f Containerfile .     (or docker build)

FROM docker.io/library/golang:1.26 AS go

FROM docker.io/library/node:22-bookworm

COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH \
    CGO_ENABLED=0

RUN apt-get update \
 && apt-get install -y --no-install-recommends zip \
 && rm -rf /var/lib/apt/lists/*

# Install the pnpm version pinned in package.json ("packageManager") into
# the image, so builds never download tools at run time.
RUN npm install -g pnpm@12.9.1 && pnpm --version

WORKDIR /src
