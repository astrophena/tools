#!/usr/bin/env bash

set -euo pipefail

where="$(dirname $0)"
usage="usage: aistudio2md.bash [-h] src dst"

what="${1:-}"
to="${2:-}"
if [[ "$what" == "" || "$where" == "" ]]; then
	echo "$usage"
	exit 1
fi
if [[ "$what" == "-h" || "$what" == "--help" ]]; then
	echo "$usage"
	exit 0
fi

tmpdir="$(mktemp -d)"
cleanup() {
	rm -rf "$tmpdir"
}
trap cleanup EXIT

python "$where/aistudio2vertex.py" "$what" "$tmpdir/vertex.json"
python "$where/vertex2md.py" "$tmpdir/vertex.json" "$to"
