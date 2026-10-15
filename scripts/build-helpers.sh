#!/bin/sh
# Build the askshell-remote helper for every supported remote target and
# place the results where internal/remote embeds them. Safe to re-run; the
# outputs are committed so most workflows never need this.
set -eu

cd "$(dirname "$0")/.."

for target in linux/amd64 linux/arm64; do
    goos=${target%/*}
    goarch=${target#*/}
    output="internal/remote/helper_${goos}_${goarch}"
    CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch \
        go build -trimpath -ldflags "-s -w" -o "$output" ./cmd/askshell-remote
done
