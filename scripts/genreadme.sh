#!/usr/bin/env bash

cd "$(dirname $0)/.."

template='- {{ .Doc }}'

echo -e "This repository holds personal tools:\n" >README.md
go list -f "$template" ./cmd/... >>README.md
cat <<'EOF' >>README.md

Install them:

```sh
go install go.astrophena.name/tools/cmd/...@master
```
EOF
