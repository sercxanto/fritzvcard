# First Public Release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a CLI that verifies the numbers it uploaded, protects contact data in exports and diagnostics, explains how to get help, and releases only checked binaries.

**Architecture:** Keep the existing CLI, XML inspector, transport recorder and fake box. Implement the following six independently reviewable changes in order; the neutral timeout wording lands before the final help text. The release workflow checks the same checkout it builds.

**Tech stack:** Go 1.27, standard library, existing `golang.org/x/sys` if needed on Windows, Go Task, GitHub Actions.

**Spec:** [First public release: behavior and scope](../specs/2026-11-22-release-readiness-design.md)

## Global constraints

- Start with `CONTRIBUTING.md`. Use synthetic fixtures, in-process transports and the existing fake box only. Do not run the probe or use real credentials or recordings.
- Keep `--phonebook` mandatory, matching exactly one name; preserve the export-name check, exclusive backup before import, user prompt and `--yes`. Do not select or reject ID 0 specially.
- Support linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 and windows/amd64. No new runtime dependency. Repository text and user-facing CLI copy are in English.
- Stage only task-specific changes, and run `task lint` before each commit. Keep unrelated working-tree/index changes separate.

## Review focus

1. Unchanged names and changed numbers (including duplicate names or numbers) must not yield false success.
2. A transport failure before the first response must not put private multipart data into a trace.
3. Failure midway through replacing an existing export must leave its bytes intact and no temp file.
4. A slow or timed-out response cannot identify its cause or whether the import took effect.
5. A help token used as an option value or after `--` must not accidentally trigger help.

## File map and work items

Each item lists its production, test and documentation files. Close its entry in `docs/follow-ups.md` only after verification; retain unrelated follow-ups and historical probe findings.

### 1. Compare phone numbers per contact

**Files:** `internal/fbxml/{inspect.go,fbxml_test.go}`, `internal/cli/{upload.go,upload_test.go}`, `README.md`.
**Interface:** Add `fbxml.ContactIdentity{Name string; Numbers []string}` (struct fields) and `Entries []ContactIdentity` on `fbxml.Info`; keep the existing `Name`, `Names` and `Contacts` for their callers. Add `contactDiff(want, got []fbxml.ContactIdentity) int` in `cli`.

- [ ] Add `TestInspect_ContactNumbers` with multiple contacts, two numbers on one and a number-like element outside `contact/telephony/number`. Assert exact grouping and decoded text. In `TestUpload_IgnoredNumberOnlyChange`, use `newUp`, matching names, one stale number and `ImportIgnored`; assert exit 1, no success line, retained backup and no stale number in the error. In `TestContactDiff`, cover reordered contacts/numbers (zero differences), exchanged numbers between **distinct** names, duplicate names with different number lists, and losing a repeated number (nonzero).
- [ ] Run `go test ./internal/fbxml ./internal/cli -run 'ContactNumbers|NumberOnlyChange|ContactDiff'`; expect missing interface or failure on the stale-number case.
- [ ] Extract numbers as trimmed XML text; compare an unordered multiset of `(name, sorted list of numbers)` using an unambiguous key (e.g. JSON encoding the tuple). Preserve multiplicity. Do not strip dial prefixes, punctuation or internal extensions on speculation. Keep the current count/name error paths; for number differences report only the mismatch count. Update the README's success guarantee.
- [ ] Run `go test ./internal/fbxml ./internal/cli`; expect PASS including existing backup, name mismatch and duplicate-name cases. Commit `fix(upload): verify numbers per contact`.

### 2. Omit private request bodies before transport errors

**Files:** `internal/trace/{recorder.go,trace_test.go}`, `README.md` (“Bug reports”).
**Interface:** Keep `OmitBody`, `Recorder.RoundTrip` and `Recorder.WriteTo` signatures. The omission policy applies when building the entry, not just when rendering a successful response.

- [ ] Add `TestRecorder_OmitBodyOnTransportError`. Use the existing `roundTripFunc`, an unregistered synthetic marker in a request body, and a stub returning a fixed transport error after reading that body. Assert the transport received the marker, the caller received the error, and `WriteTo` emits the error and an omission marker **without** the private marker. Use plain marker text that anonymization rules would not mask.
- [ ] Run `go test ./internal/trace -run TestRecorder_OmitBodyOnTransportError`; expect the trace assertion to fail.
- [ ] In `RoundTrip`, populate `e.reqBody` with only the size placeholder when `omit` is set, **before** invoking the next transport; forward the actual payload normally. Keep response omission and header filtering. In the README identify the `--- trace (anonymized) ---` boundary: review the trace before sharing and do not paste all stderr (skipped names, phonebook name and backup path may precede it).
- [ ] Run `go test ./internal/trace ./internal/cli -run 'OmitBody|Verbose'`; expect PASS. Commit `fix(trace): omit private bodies on transport failure`.

### 3. Replace XML exports privately and atomically

**Files:** `internal/cli/{export.go,export_test.go}`, `.github/workflows/ci.yml`; new `internal/cli/export_file.go` for the file-only writer. If platform-specific replacement is needed, isolate it in `export_replace_windows.go` and `export_replace_unix.go` in that directory.
**Interface:** `writeExportFile(path string, write func(io.Writer) error) error`. Only the named-file branch uses it; do not change stdout or reuse `writeBackup` (different collision policy).

- [ ] Add `TestExport_PrivateReplacement`: precreate an existing file with public read bits, export over it and check valid XML plus `mode.Perm() == 0o600` on Unix. Add `TestWriteExportFile_FailurePreservesTarget`: callback writes a prefix then returns a synthetic error; assert original bytes and exactly the original directory entry remain. Also cover a failed final rename (e.g. target is a directory); expect an error and no temp residue.
- [ ] Run `go test ./internal/cli -run 'PrivateReplacement|FailurePreservesTarget|FailedRename'`; expect missing helper or failure on mode.
- [ ] Use `os.CreateTemp(filepath.Dir(path), ".fritzbook-export-*")`, write, sync and close successfully before replacement; clean up on every failure. On Unix use same-directory rename. For Windows use a single same-volume replacement operation (e.g. `windows.MoveFileEx` with `MOVEFILE_REPLACE_EXISTING`); never delete the destination first or fall back to copying over it. Do not promise POSIX modes on Windows.
- [ ] Run `go test ./internal/cli -run 'TestExport|TestWriteExportFile'` locally and cross-compile the Windows CLI. Add a `windows-latest` CI job running the same focused test command; cross-compilation alone does not test filesystem semantics. Commit `fix(export): replace output via a private temporary file`.

### 4. Describe import waits and device coverage accurately

**Files:** `internal/fritz/{client.go,phonebook.go,phonebook_test.go}`, `internal/cli/{upload.go,upload_test.go}`, `README.md`, `docs/follow-ups.md`.
**Interface:** Rename `Client.ConfirmAfter` to `WaitHintAfter`; use `ImportPhonebook(ctx context.Context, id int, xml []byte, onWait func()) error`. Return a deadline error that matches `context.DeadlineExceeded`, independent of whether the wait hint fired.

- [ ] Adapt the existing delayed, fast, cancelled and timed-out import tests: slow success triggers one hint; fast success none; timeout before **or** after the hint matches the same deadline error. CLI tests assert neutral waiting text, a neutral timeout with unknown outcome, no phone-code wording, and continued availability of the backup. Keep the independent setup, prompt, import and verification budgets.
- [ ] Run `go test ./internal/fritz ./internal/cli -run 'Import|Hint|Timeout|FailureMentionsBackup'`; expect outdated interface or wording failures.
- [ ] Keep the ten-second hint and default five-minute import timeout; remove the second-factor-specific sentinel and callback copy. Put user-facing text in the CLI; distinguish deadline expiry from cancellation. Preserve the user's local overwrite confirmation. Update README scope to the separately created book on the 6690 Cable. In follow-ups record only that the ID 0 backup export was not exercised; retire the speculative second-factor requirement.
- [ ] Run `go test ./internal/fritz ./internal/cli`; expect PASS. Commit `fix(upload): report import waits without guessing the cause`.

### 5. Make help consistent and discoverable

**Files:** `internal/cli/{cli.go,export.go,upload.go}`, new `internal/cli/{help.go,help_test.go}`, `README.md`.
**Interface:** Reuse `Run(args []string, env Env) int` as the public entry. Factor command flag registration and help formatting so real flags, defaults, explicit help and usage errors share one definition. Display one-letter options as `-o`/`-h` and long names as `--name`/`--help`; Go's existing single-dash long forms remain accepted.

- [ ] Table-test `TestHelpForms` with `Run`: `--help` and `-h` show the overview, with literal `fritzbook upload --help` and guidance for both orders. For `export`, `upload` and `version`, both `COMMAND --help` and `--help COMMAND` give identical command-specific help, exit 0, empty stderr and no operational side effects. Include `upload -h`, an unknown help target (2/stderr), and a genuine unknown option (2/stderr). Check flag spellings as whole tokens, not substrings.
- [ ] Add `TestHelpRespectsArguments`: `export --name --help <fixture>` exports XML with book name `--help`; `export -- --help` treats the last token as a filename, not a help request. Exercise a legacy `-name` invocation too.
- [ ] Run `go test ./internal/cli -run 'TestHelp'`; expect current subcommand exit code 2, misplaced output or wrong help page.
- [ ] Route leading help plus a known command to its help page; use the flag parser's help result for subcommand flags, not a scan of all argv. Route explicit help to stdout once and invalid usage to stderr. Keep the ordinary `version` output intact. Replace `PrintDefaults()` display with canonical spelling and useful defaults. Add an example in the README and omit resolved help items from follow-ups.
- [ ] Run `go test ./internal/cli` and compare `go run ./cmd/fritzbook upload --help` with `go run ./cmd/fritzbook --help upload`; expect matching output. Commit `fix(cli): show consistent command help in either order`.

### 6. Gate release on the checked tag

**Files:** `.github/workflows/release.yml`, `CONTRIBUTING.md` (Release), `docs/follow-ups.md`; use `Taskfile.yml`'s existing `VERSION` override. Leave ordinary CI in place.
**Interface:** The release input is a push tag matching documented stable `vX.Y.Z`; the output is the same five assets with that version embedded. No publication during implementation.

- [ ] Check the current workflow against these acceptance cases: a normal tag can build; a malformed `v*` tag cannot reach any build command; a failed lint or test cannot reach publication. Today the release job consumes the tag as shell source and does not run those checks.
- [ ] Put the tag in `env: VERSION: ${{ github.ref_name }}`. In Bash validate `^v[0-9]+\.[0-9]+\.[0-9]+$` before any use by Task. On the checked-out tag run `task lint`, `task test`, then `task build:all "VERSION=$VERSION"` in success-dependent steps. Only then invoke `gh release create` for that tag and its five artifacts. Document the gate and reconcile the build-related follow-up.
- [ ] Validate the predicate locally with `v0.1.0` (accept), `v1`, `v1.2.3-extra` and `v1.2.3+foo` (reject). Check the YAML with `actionlint` if available and inspect step ordering; do not substitute a real tag push for workflow validation.
- [ ] Final local check: `task lint`, `task test`, `task build:all VERSION=v0.1.0`, then `dist/fritzbook-linux-amd64 version` on linux/amd64. Expect success, five named platform binaries and `fritzbook v0.1.0`. Commit `ci: verify the tagged commit before releasing binaries`. Actual publication remains a separate maintainer action.
