#!/usr/bin/env bash
# Bundles site/ into dist/worker.js (served under /openv6/).
set -euo pipefail
node "$(dirname "$0")/build.mjs"
