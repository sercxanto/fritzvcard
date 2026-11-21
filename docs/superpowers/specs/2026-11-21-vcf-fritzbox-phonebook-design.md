# Design: VCF → FRITZ!Box phonebook (Go CLI)

Date: 2026-11-21

## Intent and why

Contacts are maintained in Nextcloud. Every few months the FRITZ!Box phonebook should be
updated from them — triggered by hand, with a single command.

The FRITZ!Box can subscribe to CardDAV phonebooks itself, but that would require storing the
Nextcloud credentials on the box. The box is exposed to the internet, and the Nextcloud
credentials unlock far more than just contacts. So the sync runs from the user's own computer:
the computer logs in to the box, not the box to Nextcloud.

Previously `andig/carddav2fb` was used for this. It worked, but its PHP dependencies were a
nuisance and the project is barely maintained. Existing alternatives (vcard2fritzXML in Java,
python-fritzbox) only convert, cannot upload, and are cumbersome to invoke.

## Goals

- A single self-contained executable (no runtime required) for Linux, macOS and Windows.
- Input: one or more `.vcf` files as exported by Nextcloud (vCard 3.0 and 4.0).
- Two output paths:
  1. **Generate XML** in the FRITZ!Box phonebook format, for a manual "restore" in the web UI.
  2. **Upload directly**: log in to the FRITZ!Box and fully replace one phonebook.
- The FRITZ!Box's additional confirmation (code entered on a phone / button pressed on the box)
  is supported: the tool prompts for it, waits, and then continues.
- Credentials are never stored.

## Non-goals

- Contact photos on FRITZ!Fon handsets.
- Preserving or setting quick-dial, vanity or internal numbers.
- Fetching directly from Nextcloud via CardDAV (the VCF export is done by hand).
- Filtering by groups/categories.
- Config file, background service, scheduling.
- Entry-by-entry sync (changing/deleting individual entries). Nextcloud is the single source
  of truth; the target phonebook is replaced completely.

## Anti-goals (a failure even if it technically works)

- Installing or running it requires a runtime or long command chains (exactly what was
  annoying about carddav2fb and vcard2fritzXML).
- The tool no longer builds or runs in a year or two because dependencies went stale →
  stick to the Go standard library plus very few, stable packages.
- The wrong phonebook gets overwritten by accident without anyone noticing.
- The password ends up in shell history, log files or on disk.

## Constraints

- Language: **Go** (decided). Build with `CGO_ENABLED=0` so binaries are static and
  dependency-free.
- Target platforms: linux/amd64, linux/arm64, darwin/arm64, darwin/amd64, windows/amd64.
- Used every few months with a few hundred contacts — performance is not a concern.
- FRITZ!OS 7.39+ enforces a second-factor confirmation for some actions, which cannot be
  disabled. With the previous tool a code had to be entered on a phone during upload — so
  uploading is affected, and the `firmwarecfg` path works with it.
- The user's hardware: **FRITZ!Box 6690 Cable**, on the same network as the development
  machine (Mac).
- **Claude gets no direct access to the box.** No command Claude runs talks to the FRITZ!Box,
  and no raw data from the box (credentials, session IDs, contacts, serial number, MAC/IP
  addresses, device names) enters the conversation. Tests against the real box are run by the
  user with a probe program (see below) that records the box's responses in anonymized form.
  The user reviews the recording and then hands it over. The user needs step-by-step
  instructions for this; they enter the confirmation codes on the phone themselves.
- **Publishing** on GitHub is intended, but with no claim that it works on every box:
  "works for me" (6690 Cable, current FRITZ!OS) is the bar. Failures on other boxes should be
  reported clearly, nothing more.
- **Language of repository files:** all documents, code comments, README and commit messages
  in the repository are written in English, regardless of the language used in conversation.

## Development environment: DevContainer

The user wants to kick off implementation and all further work from a Claude Code session in a
DevContainer. Available on the Mac: Docker Desktop, `devcontainer` CLI, VS Code.

- `.devcontainer/` in the repository (standard `devcontainer.json`, usable with VS Code
  "Reopen in Container" and with the `devcontainer` CLI).
- Contents *(Claude's call, except `task`)*: Go toolchain (current stable), `git`,
  [Task](https://taskfile.dev) (`go-task`, the build tool — decided by the user), Claude Code. Modeled on
  Anthropic's reference DevContainer for Claude Code: Claude Code config and shell history in
  named volumes so login and settings survive container rebuilds.
- **No firewall** in the container (decided by the user). The container can therefore reach the
  home network; the rule "agents never access the FRITZ!Box directly" still applies and is
  written as a working rule into `AGENTS.md` (see "Repository documentation") so every session
  in the container knows it *(Claude's call)*.
- The probe program and the finished tool are cross-compiled for macOS inside the container and
  run by the user **on the Mac** against the box. The user places the reviewed, anonymized
  recordings in the project folder (e.g. `testdata/probe/`), which is visible in the container.
- Order: the DevContainer is the first implementation step and is created in the current
  session on the Mac. The user then opens the project in the container and has the
  implementation plan (stored in the repository) carried out there.

## Build tool: Task

The build tool is **[Task](https://taskfile.dev)** (`Taskfile.yml`), not `make` (decided by the
user). All recurring developer actions are tasks, so humans, agents and CI run the same
commands. Proposed tasks *(Claude's call on names)*:

- `task build` — build for the current platform
- `task build:all` — cross-compile all target platforms into `dist/`
- `task test` — run all tests
- `task lint` — `go vet` plus `gofmt` check
- `task probe` — build the probe program for the host (macOS) so the user can run it

The GitHub Actions release workflow also calls `task build:all` instead of duplicating build
commands.

## Repository documentation (for humans and agents alike)

The repository must be equally understandable for human developers and coding agents
(decided by the user). Single source of truth, no duplication:

- **`README.md`** — for users: what the tool does, installation, FRITZ!Box preparation,
  Nextcloud export, example invocations, "tested on FRITZ!Box 6690 Cable" note.
- **`CONTRIBUTING.md`** — for everyone who develops: dev setup (DevContainer), the Task
  commands, project layout, testing approach (fake server built from anonymized recordings),
  the probe workflow (who runs it, how recordings are reviewed and stored under
  `testdata/probe/`), coding and commit conventions, the "all repository files in English"
  rule, release process.
- **`AGENTS.md`** — short. Refers to `CONTRIBUTING.md` for everything above and only adds what
  is specific to agents, e.g.: never contact a FRITZ!Box or any device on the local network;
  never ask for or handle real credentials; work only with the anonymized recordings; ask the
  human to run the probe program when real-box behavior is needed.
- **`CLAUDE.md`** — a one-line import of `AGENTS.md` (`@AGENTS.md`) so Claude Code picks up the
  same rules *(Claude's call)*.
- Design docs and plans stay under `docs/superpowers/`.

## Approaches considered

| Approach | Assessment |
|---|---|
| **A) Go, single binary** (chosen) | No runtime, trivial cross-compiling, much in the standard library (HTTP, XML, PBKDF2), proven vCard library `github.com/emersion/go-vcard`. FRITZ!Box login and upload must be implemented by hand (manageable). |
| B) Python tool (pipx/uv) built on `fritzconnection` | Python must be installed (an extra step on Windows, aging Python versions) — exactly the kind of dependency to avoid. fritzconnection can only read phonebooks anyway. Rejected. |
| C) Rust, single binary | Equivalent to A for the user, but less proven vCard crates, more dependencies, more awkward cross-compiling. Rejected in favor of A. |
| D) Pure browser page | The browser cannot talk to the FRITZ!Box directly (CORS); only the XML path would work. Rejected. |

Upload mode: **full replacement** of one phonebook (like "restore" in the web UI, and like
carddav2fb) instead of entry-by-entry sync — simpler and more robust, since Nextcloud is the
single source.

## Usage (decided: command line with flags, no config file)

Working name of the program: `fritzbook` (proposal, can change).

```
fritzbook export contacts.vcf [more.vcf ...] -o phonebook.xml [--name Nextcloud]
fritzbook upload contacts.vcf [more.vcf ...] --phonebook Nextcloud \
    [--host http://fritz.box] [--user phonebook] [--yes] [--backup-dir .]
```

- `export`: only generates the XML file. `--name` sets the phonebook name in the XML.
- `upload`: converts, logs in, replaces the given phonebook.
- Password: from environment variable `FRITZBOX_PASSWORD`, otherwise an interactive prompt
  without echo. No `--password` flag (shell history).
- User: `--user`, alternatively `FRITZBOX_USER`. README recommendation: a dedicated FRITZ!Box
  user with only the necessary permissions.

## Conversion vCard → FRITZ!Box XML (proposal, not specified in detail by the user)

- **Name** (`realName`): `FN`; if empty, from `N` as "Given Family"; if also empty, `ORG`.
- **Phone numbers**: all `TEL` entries. Type mapping:
  - `CELL` → `mobile`
  - `FAX` (+ `WORK`) → `fax_work`
  - `WORK` → `work`
  - `HOME` or no type → `home`
  - `PREF` → `prio="1"` (otherwise the first number)
  - Numbers are only trimmed, not reformatted; a `tel:` URI prefix (vCard 4) is stripped.
- **Email**: all `EMAIL` entries in `<services>`, `classifier` `private`/`work` by type.
- Contacts **without a phone number** are skipped.
- Multiple input files are merged; duplicates are not removed.
- Output UTF-8, structure as in the FRITZ!Box export:
  `<phonebooks><phonebook name="…"><contact><category>0</category><person><realName>…`
  `</realName></person><telephony nid="…"><number type="…" prio="…" id="…">…</number></telephony>`
  `<services><email classifier="…" id="…">…</email></services><setup/><uniqueid>…</uniqueid></contact>…`
- A summary at the end: number of contacts and numbers imported, skipped contacts (by name, so
  export mistakes are visible).

## Upload flow

1. Log in via `login_sid.lua?version=2` (PBKDF2 challenge, FRITZ!OS 7.24+) → session ID.
   *(Claude's call: no MD5 fallback for older boxes — "works for me" is enough.)*
2. Resolve the target phonebook by name (`--phonebook`) to its ID. If it does not exist: abort
   with a hint to create it in the web UI (the tool does not create phonebooks).
3. **Backup**: download the current target phonebook as XML and save it with a timestamp
   (`--backup-dir`, default: current directory).
4. **Confirmation prompt**: "Phonebook 'Nextcloud' (ID n, currently X entries) will be replaced
   by Y contacts. Continue? [y/N]" — skippable with `--yes`.
5. Upload via `/cgi-bin/firmwarecfg` (multipart: `sid`, `PhonebookId`, `PhonebookImportFile`),
   as carddav2fb does.
6. **Second-factor confirmation**: if the box requires confirmation, the tool prints a clear
   prompt ("Now enter the displayed code on a phone or press a button on the FRITZ!Box …"),
   waits with a timeout (a few minutes) and then completes the upload.
7. Verify success: download the phonebook again and compare the entry count with what was
   uploaded — independent of the box's language and message texts. *(Claude's call.)*
   On errors, a clear message (wrong password, missing permissions, confirmation timed out,
   box unreachable) plus a pointer to the backup file.

Upload exclusively via `firmwarecfg` (like carddav2fb). *(Claude's call:)* no TR-064 fallback —
TR-064 has no whole-phonebook import, only per-entry calls or deleting/recreating whole
phonebooks (new ID), neither fits this design. If the `firmwarecfg` path with confirmation
unexpectedly does not work in testing, work stops and the approach is re-decided with the user.

A `--verbose` flag prints the same anonymized trace as the probe program, so users on other
boxes can file bug reports without revealing private data. *(Claude's call.)*

Connection: default `http://fritz.box` on the home network; `https://` is supported, and the
box's self-signed certificate can be accepted via a flag.

## Left to the builder

- **The first step is a probe program** (small Go program in the repository, e.g.
  `cmd/fritzprobe`) that the user runs against their FRITZ!Box 6690 Cable themselves.
  User preparation: create a throwaway phonebook "Test" and a dedicated FRITZ!Box user.
  Requirements for the probe program:
  - It writes only to the phonebook "Test" and only **synthetic test contacts**
    (e.g. "Probe Contact 1", obviously fake numbers), never real contacts.
  - It records every step (request, status code, relevant headers, response body) to a local
    file and **anonymizes** it: session ID, challenge/response, user name, password, contact
    names/numbers/emails from existing phonebooks, serial number, MAC/IP addresses, device and
    phonebook names other than "Test" are replaced with placeholders; structure, field/tag
    names, status codes and message texts are kept.
  - The file is human-readable so the user can review it before handing it over.
  - The anonymized recordings then serve as the basis for the fake test server in the
    automated tests.
  - If a recording leaves questions open, another probe round follows — never direct access.

  To be clarified with the probe program:
  - how exactly the second-factor confirmation works with the `firmwarecfg` upload (the box's
    response, how to wait for or poll the confirmation).
  - how phonebook IDs and names are most reliably determined (e.g. TR-064
    `GetPhonebookList`/`GetPhonebook`, read-only, or the web UI) and how the current phonebook
    is exported for backup and success verification.
  - Test data may only be written to the phonebook "Test", never to any other.
- Package layout, choice of CLI library (the standard library `flag` is probably enough).
- Tests: conversion with sample Nextcloud exports (vCard 3 and 4, umlauts, companies without a
  person name, contacts without numbers); login/upload against a fake test server.
- *(Claude's call:)* for publishing, a lean GitHub Actions workflow that runs `task build:all`
  on a version tag and attaches the binaries to a release. Creating and pushing the repository
  is up to the user.
- Exact contents of README, CONTRIBUTING.md and AGENTS.md within the split described in
  "Repository documentation".
- License: MIT *(Claude's call, can change)*.
