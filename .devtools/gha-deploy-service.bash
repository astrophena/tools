#!/usr/bin/env bash

set -euo pipefail
cd "$(dirname $0)/.."

service="${1:-}"
if [[ -z "$service" ]]; then
	echo "usage: ./devtools/gha-deploy-service.bash [service]" >&2
	exit 1
fi

build_dir="$(mktemp -d)"
cleanup() {
	rm -rf "$build_dir"
}
trap cleanup EXIT

mkdir -p "$build_dir/build/bin" "$build_dir/build/etc/systemd/system"
# See https://github.com/golang/go/issues/44695#issuecomment-973685193.
CGO_ENABLED=1 GOOS=linux CC="zig cc -target x86_64-linux-musl" GOOS=linux GOARCH=amd64 \
	go build -ldflags="-linkmode=external -s -w -buildid=" -trimpath -o "$build_dir/build/bin/$service" "./cmd/$service"
strip "$build_dir/build/bin/$service"
cp cmd/$service/systemd/* "$build_dir/build/etc/systemd/system"
tar -czf "$build_dir/archive.tar.gz" -C "$build_dir/build" .
go tool deploy -type service "$service" "$build_dir/archive.tar.gz"
