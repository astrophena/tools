#!/usr/bin/env bash

cd "$(dirname $0)"

template='## `{{ .ImportPath }}`

{{ printf "%s\n\n" .Doc }}'

echo -e "This repository holds personal tools:\n" >README.md
go list -f "$template" ./cmd/... >>README.md
truncate -s -1 README.md
