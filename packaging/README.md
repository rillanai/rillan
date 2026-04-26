# Rillan User-Service Packaging

These artifacts package `rillan serve` as a user-level service without adding custom daemonization logic to the binary.

## Shared service contract

- managed process: `rillan serve`
- config path: user-local runtime config
- local API binding remains unchanged from foreground mode
- logs stay OS-native (`launchd` file targets on macOS, journal on Linux)
- Ollama remains separately managed

## macOS launchd

Artifact:

- `packaging/launchd/com.rillanai.rillan.plist`

Preparation:

- replace `__RILLAN_WORKDIR__` with the intended working directory before install
- ensure `~/.local/bin/rillan` and `~/.config/rillan/config.yaml` exist or adjust the command to match your layout

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

## Linux systemd --user

Artifact:

- `packaging/systemd/rillan.service`

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

## Foreground parity check

The packaged service should expose the same API behavior as foreground mode:

```bash
go run ./cmd/rillan serve --config ~/.config/rillan/config.yaml
curl http://127.0.0.1:8420/healthz
curl http://127.0.0.1:8420/readyz
```

## Notes

- These are user-level service artifacts, not root/system-wide installers.
- Release signing and artifact provenance are intentionally outside this milestone.
