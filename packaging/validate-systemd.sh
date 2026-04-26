#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Rillan AI LLC
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

tmpfile=$(mktemp --suffix=.service)
trap 'rm -f "$tmpfile"' EXIT

sed 's|^ExecStart=.*|ExecStart=/usr/bin/true|' "packaging/systemd/rillan.service" > "$tmpfile"
systemd-analyze --user verify "$tmpfile"
