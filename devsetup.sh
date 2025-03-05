#!/bin/sh

cd "$(dirname $0)"
git config core.hooksPath internal/devtools/githooks
