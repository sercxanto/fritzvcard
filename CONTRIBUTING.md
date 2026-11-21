# Contributing to fritzbook

`fritzbook` is a Go command-line tool that converts vCard files into FRITZ!Box phonebook XML.
This file is the single source of truth for developers, human and agent alike.

## Dev setup

Development happens in a DevContainer (Go, Node, Claude Code and Task preinstalled).

- VS Code: open the folder and choose "Reopen in Container".
- Command line:

  ```sh
  devcontainer up --workspace-folder .
  devcontainer exec --workspace-folder . claude
  ```

## Tasks

Builds and checks run through [Task](https://taskfile.dev) (`task --list`).

| Task | What it does |
|---|---|
| `task build` | Builds `dist/fritzbook` for the host platform |
| `task build:all` | Cross-compiles `dist/fritzbook-<os>-<arch>[.exe]` |
| `task test` | Runs `go test ./...` |
| `task lint` | Runs `go vet ./...` and fails if `gofmt -l .` reports files |
| `task probe` | Builds `dist/fritzprobe` (default `GOOS=darwin GOARCH=arm64`, override via vars) |

## Project layout

```
cmd/fritzbook/     CLI entry point
cmd/fritzprobe/    probe program that records FRITZ!Box behavior (added later)
internal/          application packages (added later)
testdata/probe/    anonymized recordings from real boxes
docs/superpowers/  design documents and plans
.devcontainer/     DevContainer definition
Taskfile.yml       build, test and lint tasks
```

## Testing

Tests run without a real FRITZ!Box. Login and upload are tested against a fake box built from
anonymized recordings under `testdata/probe/`. Conversion is tested with sample vCard exports.

## Probe workflow

To be completed. The probe program is run by a human against a real box; its output is reviewed,
anonymized and stored under `testdata/probe/`.

## Conventions

- All repository content is in English.
- Commit messages follow Conventional Commits.
- `task lint` must be clean before committing.

## Release

To be completed. Releases are built with `task build:all` from a version tag.
