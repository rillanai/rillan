$ErrorActionPreference = "Stop"

if (Get-Command ollama -ErrorAction SilentlyContinue) {
  Write-Host "ollama already installed"
  exit 0
}

Write-Host "installing ollama"
winget install --id Ollama.Ollama --source winget --accept-source-agreements --accept-package-agreements
