#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <version> <archive-path>" >&2
  exit 1
fi

VERSION="$1"
ARCHIVE_PATH="$2"
WORKDIR="$(mktemp -d)"
STAGE_DIR="$WORKDIR/stage"
APP_DIR="$STAGE_DIR/rillan"
DMG_NAME="rillan_${VERSION}_darwin_universal.dmg"

mkdir -p "$APP_DIR"
tar -xzf "$ARCHIVE_PATH" -C "$APP_DIR"

cat > "$APP_DIR/INSTALL.txt" <<'TXT'
Install rillan:
1. Copy the rillan binary to /usr/local/bin or ~/.local/bin.
2. Copy com.rillanai.rillan.plist to ~/Library/LaunchAgents.
3. Run install-ollama.sh if local inference is desired.
TXT

hdiutil create -volname "Rillan" -srcfolder "$APP_DIR" -ov -format UDZO "$DMG_NAME"
mv "$DMG_NAME" dist/
rm -rf "$WORKDIR"
