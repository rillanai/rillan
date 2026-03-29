#!/usr/bin/env bash
set -euo pipefail

tmpfile=$(mktemp --suffix=.service)
trap 'rm -f "$tmpfile"' EXIT

sed 's|^ExecStart=.*|ExecStart=/usr/bin/true|' "packaging/systemd/rillan.service" > "$tmpfile"
systemd-analyze --user verify "$tmpfile"
