#!/usr/bin/env bash
# Runs the VPN-bypass routing test inside a private network namespace on a
# real kernel (needs root/sudo, iproute2 and util-linux). The host network is
# not touched.
set -euo pipefail
cd "$(dirname "$0")/.."
go test -c -o /tmp/openv6-netcfg.test ./internal/netcfg
SUDO=""
if [ "$(id -u)" != "0" ]; then SUDO="sudo"; fi
exec $SUDO unshare -n env OPENV6_NETNS_TEST=1 /tmp/openv6-netcfg.test -test.run 'TestBypassInNetns' -test.v
