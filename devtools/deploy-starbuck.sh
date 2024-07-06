#!/usr/bin/env bash

tmpdir="$(mktemp -d)"
cleanup() {
	rm -rf "$tmpdir"
}
trap cleanup INT EXIT

GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -buildid=" -trimpath -o "$tmpdir/starbuck" ./cmd/starbuck
scp "$tmpdir/starbuck" "astrophena@exp.astrophena.name:"
scp cmd/starbuck/starbuck.service "astrophena@exp.astrophena.name:"
ssh astrophena@exp.astrophena.name doas install -m755 -o root -g root starbuck /usr/local/bin/starbuck
ssh astrophena@exp.astrophena.name doas install -m644 -o root -g root starbuck.service /etc/systemd/system/starbuck.service
ssh astrophena@exp.astrophena.name doas systemctl daemon-reload
ssh astrophena@exp.astrophena.name doas systemctl restart starbuck
