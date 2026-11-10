#!/usr/bin/env bash
# Cloudflare Workers Builds entry point: prepares the landing (see deploy/cloudflare).
exec bash "$(dirname "$0")/deploy/cloudflare/build.sh"
