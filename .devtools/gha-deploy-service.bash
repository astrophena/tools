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
CGO_ENABLED=0 go build -ldflags="-s -w -buildid=" -trimpath -o "$build_dir/build/bin/$service" "./cmd/$service"
cp cmd/$service/systemd/* "$build_dir/build/etc/systemd/system"
tar -czf "$build_dir/archive.tar.gz" -C "$build_dir/build" .
go tool deploy -type service "$service" "$build_dir/archive.tar.gz" 
