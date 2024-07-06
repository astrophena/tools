#!/usr/bin/env bash

tmpdir="$(mktemp -d)"
cleanup() {
	rm -rf "$tmpdir"
}
trap cleanup INT EXIT

run() {
	ssh astrophena@exp.astrophena.name "$@"
}

# Cross-compile Starbuck binary. Why? Because most of the time this script runs
# on my ARM64 Android tablet.
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -buildid=" -trimpath -o "$tmpdir/starbuck" ./cmd/starbuck

# Copy built binary and systemd service to the server.
scp "$tmpdir/starbuck" "astrophena@exp.astrophena.name:"
scp cmd/starbuck/starbuck.service "astrophena@exp.astrophena.name:"
run doas install -m755 -o root -g root starbuck /usr/local/bin/starbuck
run doas install -m644 -o root -g root starbuck.service /etc/systemd/system/starbuck.service

# Reload systemd state and restart Starbuck.
run doas systemctl daemon-reload
run doas systemctl restart starbuck

# Remove leftovers from deploy.
run rm /home/astrophena/starbuck /home/astrophena/starbuck.service
