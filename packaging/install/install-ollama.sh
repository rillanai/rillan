#!/usr/bin/env bash
set -euo pipefail

if command -v ollama >/dev/null 2>&1; then
  echo "ollama already installed"
  exit 0
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to install ollama" >&2
  exit 1
fi

echo "installing ollama"
curl -fsSL https://ollama.com/install.sh | sh
