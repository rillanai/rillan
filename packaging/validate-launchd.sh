#!/usr/bin/env bash
set -euo pipefail

if command -v plutil >/dev/null 2>&1; then
plutil -lint "packaging/launchd/com.rillanai.rillan.plist"
	return 0 2>/dev/null || exit 0
fi

python - <<'PY'
import pathlib
import plistlib

path = pathlib.Path("packaging/launchd/com.rillanai.rillan.plist")
with path.open("rb") as fh:
    plistlib.load(fh)
print(f"validated {path}")
PY
