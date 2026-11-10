#!/usr/bin/env bash
# Manual deploy (needs CLOUDFLARE_API_TOKEN). Normally Cloudflare Workers Builds
# deploys automatically on every push to main.
set -euo pipefail
cd "$(dirname "$0")"
./build.sh
npx --yes wrangler@4 deploy "$@"
