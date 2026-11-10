#!/usr/bin/env bash
# Copies site/ into dist/openv6/ so the Worker serves it under /openv6/.
set -euo pipefail
cd "$(dirname "$0")"
rm -rf dist && mkdir -p dist/openv6
cp -R ../../site/. dist/openv6/
