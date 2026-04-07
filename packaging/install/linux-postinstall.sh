#!/usr/bin/env bash
set -euo pipefail

if ! id -u rillan >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/rillan --create-home --shell /usr/sbin/nologin rillan || true
fi

mkdir -p /var/lib/rillan
chown -R rillan:rillan /var/lib/rillan

/usr/share/rillan/install-ollama.sh || true

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  systemctl enable --now rillan.service || true
fi
