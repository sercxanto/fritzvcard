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

Some of these files are created by later tasks.

```
.devcontainer/devcontainer.json   DevContainer (Go 1.27, task, Claude Code, named volumes)
Taskfile.yml                      build, build:all, test, lint, probe
AGENTS.md / CLAUDE.md             agent rules (CLAUDE.md = "@AGENTS.md")
CONTRIBUTING.md / README.md / LICENSE
cmd/fritzbook/main.go             wires os.* into cli.Run
cmd/fritzprobe/main.go            probe program (human runs it on the Mac)
internal/contacts/                vCard → Contact model
internal/fbxml/                   Contact model → FRITZ!Box XML; inspect XML
internal/trace/                   anonymizer + recording http.RoundTripper
internal/fritz/                   Client: login, TR-064, firmwarecfg, phonebook ops
internal/fritz/fritzfake/         fake FRITZ!Box (httptest) for tests
internal/cli/                     export / upload / version commands
testdata/vcf/                     synthetic vCard fixtures
testdata/probe/                   anonymized recordings (added by the human)
docs/superpowers/                 design specs and implementation plans
.github/workflows/ci.yml, release.yml
```

## Testing

Tests run without a real FRITZ!Box. Login and upload are tested against a fake box built from
anonymized recordings under `testdata/probe/`. Conversion is tested with sample vCard exports.

## Probe workflow

The probe (`cmd/fritzprobe`) records how a real box behaves, so the fake box and the upload
code can be built from facts. A human runs it; agents never do (see AGENTS.md).

1. Prepare the box: export a backup of all phonebooks first (Telephony > Phonebook > Backup);
   an import replaces the whole target phonebook and the probe cannot rule out that the box
   ignores the requested phonebook ID. Then log in to the web interface, create exactly one phonebook named `Test`
   (Telephony > Phonebook), and use a user that may edit contacts. The probe writes three
   synthetic contacts (`Probe Contact 1-3`) into `Test` and nothing else; it refuses to write
   if there is no or more than one phonebook named `Test`.
2. Build it on a machine that can reach the box: `task probe` produces `dist/fritzprobe`
   (default `darwin/arm64`; override with `GOOS=... GOARCH=...`).
3. Run it on the host, with the password in the environment or typed at the echo-less prompt:

   ```sh
   FRITZBOX_PASSWORD=... ./dist/fritzprobe --host http://fritz.box --user <name>
   ```

   Options: `-o <file>`, `--insecure` (https with a self-signed certificate), `--timeout 5m`.
   It asks you to type `yes` before the write. If the box or your phone asks for a
   confirmation during the import, confirm it, then answer the two follow-up questions.
4. Review the output file (default `fritzprobe-<YYYYMMDD-HHMMSS>.txt`) by reading all of it.
   Credentials, session IDs, serials, hostnames and all contact data outside `Test` are
   replaced by placeholders, and the probe refuses to write a file in which a registered
   secret survived. Still check for anything personal before sharing.
5. Store the reviewed file under `testdata/probe/` and commit it with a message like
   `test(probe): add <model> FRITZ!OS <version> recording`.

## Conventions

- All repository content is in English.
- Commit messages follow Conventional Commits.
- `task lint` must be clean before committing.

## Release

To be completed. Releases are built with `task build:all` from a version tag.
