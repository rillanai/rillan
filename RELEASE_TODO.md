# Release and CI Hardening TODO (Durable)

This checklist tracks work required to move from functional packaging to production-grade releases.

## 1) GitHub repository/release plumbing

- [ ] Create branch protection for `main` and require CI (`CI` workflow).
- [ ] Restrict who can push tags matching `v*`.
- [ ] Document release process (`vX.Y.Z` tag -> release workflow) in `docs/development.md`.
- [ ] Add environment protection rules for release jobs (manual approvals if required).

## 2) Secrets and identities

- [ ] Configure maintainers' GPG/Sigstore identities for release traceability.
- [ ] Decide whether to keep keyless-only signing or add key-pair fallback.
- [ ] If key-based signing is needed, store keys in GitHub Actions secrets with rotation policy.

## 3) Linux package quality

- [ ] Validate `.deb` and `.rpm` install/upgrade/remove on supported distros.
- [ ] Verify systemd service hardening options against real deployment needs.
- [ ] Decide whether service should auto-start by default or require explicit enable.
- [ ] Add integration tests for package scripts (`postinstall`, `preremove`).

## 4) macOS release hardening

- [ ] Enroll in Apple Developer Program for signing/notarization.
- [ ] Sign binaries with `codesign` in CI.
- [ ] Notarize DMG (or PKG) and staple notarization ticket.
- [ ] Validate LaunchAgent behavior on clean macOS hosts.

## 5) Windows release hardening

- [ ] Acquire Authenticode certificate and establish secure signing flow.
- [ ] Sign `rillan.exe` and installer `.exe`.
- [ ] Finalize Windows service manager strategy (WinSW vs NSSM vs native SCM wrapper).
- [ ] Add install/uninstall upgrade tests on Windows runners.

## 6) Ollama dependency strategy

- [ ] Decide policy for offline installs where Ollama cannot be downloaded.
- [ ] Decide whether Ollama install should be opt-in instead of automatic.
- [ ] Add explicit version pinning policy for Ollama.
- [ ] Add post-install verification (`ollama --version`) and diagnostics logging.

## 7) Supply-chain security and attestations

- [ ] Expand GitHub attestations from `checksums.txt` to all release artifacts.
- [ ] Publish SLSA provenance level target and roadmap.
- [ ] Add SBOM generation (CycloneDX or SPDX) for binaries/packages.
- [ ] Attach SBOMs and provenance documents to each GitHub release.
- [ ] Add verification instructions for users (cosign verify-blob, provenance validation).

## 8) CI reliability and governance

- [ ] Add concurrency controls to prevent overlapping releases.
- [ ] Cache Go modules/build outputs where safe.
- [ ] Add scheduled workflow that runs a dry-run release (`--snapshot`) weekly.
- [ ] Add chat/email alerting for failed release workflows.

## 9) Documentation and operator UX

- [ ] Publish an operator install matrix (mac/linux/windows) in `README.md`.
- [ ] Document service log locations and troubleshooting for each OS.
- [ ] Provide rollback instructions for failed upgrades.
- [ ] Add explicit support policy for CPU architectures and OS versions.
