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
testdata/probe/                   reviewed recordings (optional, only if the owner decides)
docs/superpowers/                 design specs and implementation plans
.github/workflows/ci.yml, release.yml
```

## Testing

Tests run without a real FRITZ!Box. Login and upload are tested against a fake box
(`internal/fritz/fritzfake`) built from the probe findings. Conversion is tested with sample vCard exports.

## Probe workflow

The probe (`cmd/fritzprobe`) records how a real box behaves, so the fake box and the upload
code can be built from facts. A human runs it; agents never do (see AGENTS.md).

1. Prepare the box: export a backup of all phonebooks first (Telephony > Phonebook > Backup);
   an import replaces the whole target phonebook and the probe cannot rule out that the box
   ignores the requested phonebook ID. Then log in to the web interface, create exactly one phonebook named `Test`
   (Telephony > Phonebook), and use a user that may edit contacts. The probe writes three
   synthetic contacts (`Probe Contact 1-3`) into `Test` and nothing else; it refuses to write
   if TR-064 does not confirm exactly one phonebook named `Test`. There is no fallback listing.
   Everything the probe reads is read-only until the typed `yes`: the TR-064 phonebook list,
   one multipart `firmwarecfg` export experiment for `Test`, and a GET of each phonebook's
   TR-064 download URL (`phonebook.lua`). The other phonebooks are read only to compare their
   contact counts before and after the import; their names and contents are never recorded.
   The probe contacts carry a per-run tag (`Probe Contact 1 <HHMMSS>`); after the import the probe
   re-reads `Test` and reports a verdict: `replaced`, `appended`, `import not visible`,
   `ambiguous (Test was empty before)`, `unknown` or `unexpected`. `Test`'s contents are
   recorded verbatim (before and after) in the shared file, so empty `Test` or check its
   contents before the run. If a count of another phonebook cannot be read it prints `UNVERIFIED`;
   then compare the phonebooks manually against your backup.
2. Build it on a machine that can reach the box: `task probe` produces `dist/fritzprobe`
   (default `darwin/arm64`; override with `GOOS=... GOARCH=...`).
3. Run it on the host, with the password in the environment or typed at the echo-less prompt:

   ```sh
   FRITZBOX_PASSWORD=... ./dist/fritzprobe --host http://fritz.box --user <name>
   ```

   Options: `-o <file>`, `--insecure` (https with a self-signed certificate), `--timeout 5m`.
   It asks you to type `yes` before the write. If the box or your phone asks for a
   confirmation during the import, confirm it, then answer the two follow-up questions. On
   one box the import completed in about a second without any confirmation; other boxes may
   still ask for one.
4. Review the output file (default `fritzprobe-<YYYYMMDD-HHMMSS>.txt`) by reading all of it.
   Credentials, session IDs, serials, hostnames and all contact data outside `Test` are
   replaced by placeholders, and the probe refuses to write a file in which a registered
   secret survived. Still check for anything personal before sharing.
5. Raw recordings stay local and git-ignored (`/fritzprobe-*.txt` in `.gitignore`). Committing
   a reviewed recording under `testdata/probe/` is optional and only done when the owner
   explicitly decides to (message like `test(probe): add <model> FRITZ!OS <version> recording`).
   The findings that later work relies on live in the "Probe findings" section at the end of
   `docs/superpowers/plans/2026-11-21-fritzbook.md`.

## Conventions

- All repository content is in English.
- Commit messages follow Conventional Commits.
- `task lint` must be clean before committing.

## Release

1. Make sure `task lint` and `task test` pass on the commit to release.
2. Optional local check: `task build:all VERSION=vX.Y.Z` writes the five binaries to `dist/`
   (`fritzbook-linux-amd64`, `-linux-arm64`, `-darwin-amd64`, `-darwin-arm64`,
   `-windows-amd64.exe`); `dist/fritzbook-linux-amd64 version` (on Linux) prints the version.
3. The human creates the GitHub repository (once) and pushes a tag `vX.Y.Z`:

   ```sh
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

4. The release workflow (`.github/workflows/release.yml`) runs `task build:all
   VERSION=<tag>` and creates a GitHub release with the five binaries and generated notes.
   CI (`.github/workflows/ci.yml`) runs lint, tests and the cross-build on every push and pull request.
