#!/usr/bin/env bash

# See https://github.com/golang/go/issues/44695 for why we are using
# "-linkmode=external".
CGO_ENABLED=1 GOOS="linux" GOARCH="amd64" CC="zig cc -target x86_64-linux-musl" \
	go build -ldflags="-s -w -buildid= -linkmode=external" -trimpath
strip starlet
