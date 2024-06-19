#!/bin/sh

cd "$(dirname $0)"

template='- {{ .Doc }}'

echo "This repository holds personal tools:\n" >README.md
echo "[![Open in GitHub Codespaces](https://github.com/codespaces/badge.svg)](https://codespaces.new/astrophena/tools?quickstart=1)\n" >README.md
go list -f "$template" ./cmd/... >>README.md
cat <<'EOF' >>README.md

Install them:

```sh
go install go.astrophena.name/tools/cmd/...@master
```
EOF
