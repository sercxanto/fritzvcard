# fritzvcard Rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `fritzvcard` the active project, module, executable, and user-facing CLI name.

**Architecture:** Rename the Go command and module/import paths together, then update CLI-facing strings and generated files. Align task builds, release assets, and current documentation; preserve historical plans, probe identity, old backup ignore rule, and existing DevContainer volumes.

**Tech Stack:** Go 1.27, Task, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-23-fritzvcard-rename-design.md`

## Global Constraints

- No real-device traffic or real credentials.
- Do not rename `fritzprobe` or old DevContainer volume mounts.
- Retain `.gitignore` protection for both old and new backup prefixes.
- Historical specs/plans are not mechanically rewritten.

## Review Focus

- `version` and help should show the new name, not the old one.
- Upload should write and report new-prefix backups while older backups remain ignored.
- Export temporary files should use the new prefix without altering atomic replacement.
- Multipart import's filename should match the fake-box expectation.
- Tagged cross-build should produce and publish exactly the new platform asset names.

---

### Task 1: CLI identity and runtime files

**Files:** `internal/cli/*`, `internal/fritz/firmwarecfg.go`, `internal/fritz/fritzfake/services.go`, `cmd/fritzbook/main.go`, `cmd/fritzprobe/*.go`, `go.mod`, Go imports.

**Interfaces:** `cli.Run` remains unchanged; command path becomes `./cmd/fritzvcard`; module path becomes `fritzvcard`.

- [x] Update tests to assert `fritzvcard` in version/help and backup filename; add a check for the export temp prefix. The existing upload integration test verifies the multipart filename against the fake box.
- [x] Run `go test ./internal/cli ./internal/fritz/...` and observe expected old-name failures.
- [x] Rename command and module/import references, CLI output, backup/temp prefixes and multipart filename; align fake-box behavior.
- [x] Run `go test ./...` and confirm all tests pass.

### Task 2: Build, release, and documentation

**Files:** `Taskfile.yml`, `.github/workflows/release.yml`, `.devcontainer/devcontainer.json`, `.gitignore`, `README.md`, `CONTRIBUTING.md`, `LICENSE`, `docs/follow-ups.md`.

**Interfaces:** `task build` → `dist/fritzvcard`; `task build:all` → five `fritzvcard-<os>-<arch>` binaries; `task probe` unchanged.

- [x] Update active docs/configuration and retain ignored legacy backup files and persistent volume names.
- [x] Run `task lint`, `task test`, `task build:all VERSION=v0.1.0`; check five output filenames and `dist/fritzvcard-linux-amd64 version`.
- [x] Inspect active references, release workflow, and diff for accidental changes to probe/history/state.
