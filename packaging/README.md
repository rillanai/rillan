# Rillan Packaging and Install Media

This directory contains cross-platform packaging assets for running `rillan serve` as an operating-system service and installing local inference dependencies.

## Goals covered

- Build cross-platform binaries with GoReleaser.
- Produce Linux `.deb` and `.rpm` packages.
- Produce archive artifacts for macOS and Windows.
- Provide install-media builder scripts for macOS (`.dmg`) and Windows (`.exe` installer).
- Install Ollama during package install flows (best effort).
- Ship service definitions for system service management.

## Service definitions

### Linux (system-wide systemd)

- Unit file: `packaging/systemd/rillan.system.service`
- Installed by Linux packages to: `/usr/lib/systemd/system/rillan.service`
- Runs as dedicated `rillan` system user.

Linux package post-install script:

- creates `rillan` system user if needed
- creates `/var/lib/rillan`
- installs Ollama (best effort)
- enables and starts `rillan.service` (best effort)

### Linux (user systemd)

- Unit file: `packaging/systemd/rillan.service`
- Intended for local non-root usage.

Validation:

```bash
systemd-analyze --user verify packaging/systemd/rillan.service
```

Install / start:

```bash
mkdir -p ~/.config/systemd/user
cp packaging/systemd/rillan.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now rillan.service
systemctl --user status rillan.service
```

Stop / uninstall:

```bash
systemctl --user disable --now rillan.service
rm ~/.config/systemd/user/rillan.service
systemctl --user daemon-reload
```

### macOS (launchd user agent)

- LaunchAgent: `packaging/launchd/com.rillanai.rillan.plist`
- Default command: `$HOME/.local/bin/rillan serve --config $HOME/.config/rillan/config.yaml`

Validation:

```bash
plutil -lint packaging/launchd/com.rillanai.rillan.plist
```

Install / start:

```bash
cp packaging/launchd/com.rillanai.rillan.plist ~/Library/LaunchAgents/
launchctl bootstrap gui/$UID ~/Library/LaunchAgents/com.rillanai.rillan.plist
launchctl print gui/$UID/com.rillanai.rillan
```

Stop / uninstall:

```bash
launchctl bootout gui/$UID ~/Library/LaunchAgents/com.rillanai.rillan.plist
rm ~/Library/LaunchAgents/com.rillanai.rillan.plist
```

### Windows (service wrapper template)

- Wrapper XML template: `packaging/windows/rillan-service.xml`
- Installer script for Ollama: `packaging/install/install-ollama.ps1`

## Ollama installers

- Unix-like: `packaging/install/install-ollama.sh`
- Windows: `packaging/install/install-ollama.ps1`

Both scripts are idempotent and skip installation when `ollama` already exists on `PATH`.

## Foreground parity check

The packaged service should expose the same API behavior as foreground mode:

```bash
go run ./cmd/rillan serve --config ~/.config/rillan/config.yaml
curl http://127.0.0.1:8420/healthz
curl http://127.0.0.1:8420/readyz
```

## GoReleaser outputs

Configured in `.goreleaser.yaml`:

- binaries for `linux`, `darwin`, and `windows` (`amd64`, `arm64`)
- archives (`tar.gz` on Unix-like systems, `.zip` on Windows)
- Linux packages (`.deb` + `.rpm`) via `nfpm`
- checksums and draft GitHub release creation

Run a local snapshot build:

```bash
goreleaser release --snapshot --clean
```

## Install media generation

GoReleaser handles most artifacts. Additional install media builders are provided:

- macOS DMG builder: `packaging/macos/build-dmg.sh`
- Windows EXE builder (Inno Setup): `packaging/windows/build-installer.ps1`

These are intended to be wired into CI release jobs after GoReleaser produces per-platform archives.

## CI workflows

- `.github/workflows/ci.yml`
  - `go mod tidy` consistency check
  - `go test ./...`
  - `go build ./...`
  - GoReleaser snapshot smoke test

- `.github/workflows/release.yml`
  - release on `v*` tags using GoReleaser
  - keyless cosign signature for `checksums.txt`
  - GitHub artifact attestation for `checksums.txt`

## Notes

- Package signing, macOS notarization, and Windows Authenticode signing are tracked in `RELEASE_TODO.md`.
- Windows service registration is environment-specific and should be finalized in release hardening.
