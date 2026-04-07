param(
  [Parameter(Mandatory=$true)][string]$Version
)

$ErrorActionPreference = "Stop"

if (-not (Get-Command iscc -ErrorAction SilentlyContinue)) {
  throw "iscc (Inno Setup Compiler) is required"
}

iscc "/DAppVersion=$Version" packaging/windows/rillan.iss
