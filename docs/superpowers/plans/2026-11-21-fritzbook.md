# fritzbook Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single-binary Go CLI that converts Nextcloud vCard exports into a FRITZ!Box phonebook XML and can replace a phonebook on the box directly, including the box's second-factor confirmation.

**Architecture:** Pure conversion (`internal/contacts` → `internal/fbxml`) is independent of the box. Box access lives in `internal/fritz` (login, TR-064 reads, `firmwarecfg` import/export), always wrapped by the anonymizing recorder in `internal/trace`. A probe program (`cmd/fritzprobe`), run by the human against their real box, produces anonymized recordings; those recordings define the fake box (`internal/fritz/fritzfake`) used by all automated tests. `internal/cli` wires everything into `fritzbook export|upload|version`.

**Tech Stack:** Go 1.27 (stdlib: `net/http`, `encoding/xml`, `crypto/pbkdf2`, `crypto/md5`, `flag`), `github.com/emersion/go-vcard v0.1.0`, `golang.org/x/term v0.48.0`, Task v3.54.0, DevContainer image `mcr.microsoft.com/devcontainers/go:2-1.27-trixie`, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-21-vcf-fritzbox-phonebook-design.md`

## Global Constraints

- Go module path `fritzbook`, `go 1.27`; builds with `CGO_ENABLED=0`.
- Only third-party deps: `github.com/emersion/go-vcard v0.1.0`, `golang.org/x/term v0.48.0`. Anything else needs the human's OK.
- Targets: linux/amd64, linux/arm64, darwin/arm64, darwin/amd64, windows/amd64.
- All repository files (code, comments, docs, commit messages) and all CLI output in English.
- **Agents never contact a FRITZ!Box or any other device on the local network**, never ask for or handle real credentials, and work only with anonymized material (the "Probe findings" below and reviewed recordings the human shares). When real-box behavior is needed, ask the human to run the probe.
- Password only from `FRITZBOX_PASSWORD` or an echo-less prompt. No `--password` flag. Never written to disk or logs.
- Login only via `login_sid.lua?version=2` (PBKDF2). A non-`2$` challenge is an error ("FRITZ!OS 7.24 or newer required"), no MD5 fallback.
- Phonebook writes only via `POST /cgi-bin/firmwarecfg` (multipart `sid`, `PhonebookId`, `PhonebookImportFile`). TR-064 is used read-only.
- The probe writes only to a phonebook named exactly `Test`, and only synthetic contacts.
- The tool never creates phonebooks.
- Build tool is Task (`Taskfile.yml`); no Makefile. CI calls the same tasks.
- Commit after each task with Conventional Commit messages, ending with the `Co-Authored-By` line the session prescribes.

## Review Focus

1. **Folded lines and umlauts in Nextcloud vCard 3.0** — `FN:Jürgen Müller` and a folded `NOTE`/`TEL` line must decode to correct UTF-8. Test: `TestParse_FoldedLineAndUmlauts` (Task 2).
2. **Multi-valued TYPE params** (`TYPE=WORK,VOICE`, `TYPE="cell,voice"`, `TYPE=work;TYPE=fax`, vCard 4 `VALUE=uri:tel:+49…`) must map to the right Fritz type and strip `tel:`. Test: `TestParse_TypeMapping` table (Task 2).
3. **XML-special characters in names/emails** (`Müller & Söhne <GmbH>`) must be escaped, and the result must re-parse. Test: `TestWrite_EscapesSpecialChars` (Task 3).
4. **Box unreachable, wrong password, login blocked** (`BlockTime>0`) — a one-line error, exit code 1, and no backup/import attempted. Test: `TestUpload_LoginErrors` (Task 10).
5. **Failure after the backup** (import fails, confirmation times out, count mismatch) — the backup file already exists and the error message names its path. Test: `TestUpload_FailureMentionsBackup` (Task 10).

---

## File Structure

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
.github/workflows/ci.yml, release.yml
```

---

### Task 1: Repository scaffold and DevContainer (runs on the Mac, in the planning session)

**Files:**
- Create: `go.mod`, `.gitignore` (`dist/`, `fritzprobe-*.txt`, `fritzbook-backup-*.xml`), `Taskfile.yml`, `.devcontainer/devcontainer.json`, `AGENTS.md`, `CLAUDE.md`, `CONTRIBUTING.md`, `LICENSE`, `cmd/fritzbook/main.go`

**Interfaces:**
- Produces: tasks `build` (→ `dist/fritzbook`), `build:all` (→ `dist/fritzbook-<os>-<arch>[.exe]`), `test` (`go test ./...`), `lint` (`go vet ./...` + fail if `gofmt -l .` prints anything), `probe` (`GOOS=darwin GOARCH=arm64` → `dist/fritzprobe`, parametrised by `GOOS`/`GOARCH` vars). `main.version` string set via `-ldflags "-X main.version={{.VERSION}}"`, `VERSION` from `git describe --tags --always --dirty`, default `dev`.

- [ ] **Step 1:** `git init`, `go mod init fritzbook`, set `go 1.27`. `cmd/fritzbook/main.go` prints `fritzbook <version>` for `version` and exits 2 with usage otherwise (replaced in Task 4).
- [ ] **Step 2:** Write `Taskfile.yml` (version 3) with the tasks above; all builds use `CGO_ENABLED=0` and `-trimpath -ldflags "-s -w -X main.version=…"`.
- [ ] **Step 3:** Write `.devcontainer/devcontainer.json`: image `mcr.microsoft.com/devcontainers/go:2-1.27-trixie`; features `ghcr.io/devcontainers/features/node:1` and `ghcr.io/anthropics/devcontainer-features/claude-code:1`; `postCreateCommand`: `go install github.com/go-task/task/v3/cmd/task@v3.54.0`; mounts: named volume `fritzbook-claude-config` → `/home/vscode/.claude`, named volume `fritzbook-history` → `/commandhistory`; `containerEnv` `CLAUDE_CONFIG_DIR=/home/vscode/.claude`, `HISTFILE=/commandhistory/.bash_history`. No firewall.
- [ ] **Step 4:** Write `AGENTS.md` (≤ 25 lines): "Read CONTRIBUTING.md first — it is the source of truth for setup, tasks, layout, tests and conventions." plus the agent-only rules from Global Constraints (no box/LAN access, no real credentials, only `testdata/probe/`, ask the human to run the probe). `CLAUDE.md` contains exactly `@AGENTS.md`.
- [ ] **Step 5:** Write `CONTRIBUTING.md` sections: Dev setup (DevContainer via VS Code "Reopen in Container" or `devcontainer up --workspace-folder .` + `devcontainer exec --workspace-folder . claude`), Tasks, Project layout (tree above), Testing (fake box from recordings), Probe workflow (stub; completed in Task 8), Conventions (English only, Conventional Commits, `task lint` clean), Release (stub; completed in Task 11). `LICENSE`: MIT, "Copyright (c) 2026 the fritzbook contributors".
- [ ] **Step 6: Verify on the Mac**
  Run: `task build && ./dist/fritzbook version && task test && task lint`
  Expected: prints `fritzbook dev` (or a git describe string); test and lint exit 0.
- [ ] **Step 7: Verify the container**
  Run: `devcontainer up --workspace-folder . && devcontainer exec --workspace-folder . bash -lc 'go version && task --version && claude --version && task build'`
  Expected: `go1.27.x`, `3.54.0`, a Claude Code version, build succeeds.
- [ ] **Step 8: Commit** — `chore: scaffold repository, Taskfile and DevContainer`

**Handoff:** after this task the human reopens the project in the container; Tasks 2+ run there.

---

### Task 2: vCard → contact model

**Files:**
- Create: `internal/contacts/contacts.go`, `internal/contacts/vcf.go`, `internal/contacts/vcf_test.go`, `testdata/vcf/*.vcf`

**Interfaces:**
- Produces:
  ```go
  type Contact struct { Name string; Numbers []Number; Emails []Email }
  type Number  struct { Value string; Type string /* "home"|"mobile"|"work"|"fax_work" */; Prio bool }
  type Email   struct { Value string; Classifier string /* "private"|"work" */ }
  type Skipped struct { Name string; Reason string } // Reason: "no phone number" | "no name"
  func Parse(r io.Reader) ([]Contact, []Skipped, error)
  func ParseFiles(paths []string) ([]Contact, []Skipped, error) // concatenates in argument order, no dedup
  ```

Rules (spec "Conversion"): Name = `FN`, else `N` as "Given Family" (trimmed, single space), else first `ORG` component; none → skipped "no name". Numbers: every `TEL`; types lower-cased and comma-split across all TYPE params; any `fax` → `fax_work`, else `cell` → `mobile`, else `work` → `work`, else `home`. Exactly one number per contact has `Prio=true`: the first with `pref` type or `PREF` param, else the first number. Value: `strings.TrimSpace`, strip leading `tel:`. Emails: `work` if TYPE contains work, else `private`. Contacts with no numbers → skipped "no phone number" (Name as computed, or `(unnamed)`).

- [ ] **Step 1: Write failing tests** with synthetic fixtures (`testdata/vcf/nextcloud-v3.vcf`, `v4.vcf`):
  - `TestParse_Basic`: `FN:Erika Mustermann`, `TEL;TYPE=CELL:+49 170 0000001`, `TEL;TYPE=HOME:030 0000002`, `EMAIL;TYPE=HOME:erika@example.org` → `Contact{Name:"Erika Mustermann", Numbers:[{"+49 170 0000001","mobile",true},{"030 0000002","home",false}], Emails:[{"erika@example.org","private"}]}`.
  - `TestParse_TypeMapping` (table, Review Focus 2): `TYPE=WORK,VOICE`→work; `TYPE="cell,voice"`→mobile; `TYPE=work;TYPE=fax`→fax_work; `TYPE=fax`→fax_work; no TYPE→home; v4 `TEL;VALUE=uri;TYPE=cell:tel:+49-170-0000003`→`{"+49-170-0000003","mobile"}`; `TYPE=home,pref` on the 2nd number → only that one `Prio`.
  - `TestParse_NameFallbacks`: N only → "Max Muster"; ORG only `ORG:ACME GmbH;Sales` → "ACME GmbH"; nothing → skipped "no name".
  - `TestParse_SkipsWithoutNumber` → `Skipped{"Nur Mail","no phone number"}`.
  - `TestParse_FoldedLineAndUmlauts` (Review Focus 1): `FN:Jürgen Müller` and a TEL value folded over two lines (`TEL;TYPE=CELL:+49 170 00\r\n 00004`) → `"Jürgen Müller"`, `"+49 170 0000004"`.
  - `TestParseFiles_ConcatenatesInOrder`.
- [ ] **Step 2:** Run `go test ./internal/contacts/...` — expect FAIL (undefined).
- [ ] **Step 3:** Implement with `vcard.NewDecoder(r).Decode()` until `io.EOF`.
- [ ] **Step 4:** Run `task test` — expect PASS; `task lint` clean.
- [ ] **Step 5: Commit** — `feat(contacts): parse vCard exports into contact model`

---

### Task 3: FRITZ!Box XML writer and inspector

**Files:**
- Create: `internal/fbxml/write.go`, `internal/fbxml/inspect.go`, `internal/fbxml/fbxml_test.go`, `internal/fbxml/testdata/basic.golden.xml`

**Interfaces:**
- Consumes: `contacts.Contact` (Task 2)
- Produces:
  ```go
  func Write(w io.Writer, phonebookName string, cs []contacts.Contact) error
  type Info struct { Name string; Contacts int }
  func Inspect(r io.Reader) (Info, error) // first <phonebook>: name attr + number of <contact>
  ```

Output: header `<?xml version="1.0" encoding="utf-8"?>`, two-space indent, per contact `<category>0</category>`, `<person><realName>`, `<telephony>` with `<number type=… prio="1|0" id="<index>">`, `<services>` with `<email classifier=… id="<index>">` (omitted when no emails), `<setup></setup>`. *Decision (Claude): no `uniqueid`/`nid` — the box assigns them, as with carddav2fb; this refines the spec's structure sketch.*

- [ ] **Step 1: Write failing tests**:
  - `TestWrite_Golden`: Erika contact from Task 2 + name "Nextcloud" equals `basic.golden.xml` byte-for-byte (write the golden file by hand from the rules above).
  - `TestWrite_EscapesSpecialChars` (Review Focus 3): Name `Müller & Söhne <GmbH>` → output contains `Müller &amp; Söhne &lt;GmbH&gt;`; `Inspect` on the output → `Info{"Nextcloud",1}`.
  - `TestInspect_BoxExport`: a hand-written export with 3 contacts, name "Test" → `Info{"Test",3}`; malformed XML → error.
- [ ] **Step 2:** `go test ./internal/fbxml/...` — expect FAIL.
- [ ] **Step 3:** Implement with `encoding/xml` structs and `xml.Encoder.Indent("", "  ")`.
- [ ] **Step 4:** `task test` PASS, `task lint` clean.
- [ ] **Step 5: Commit** — `feat(fbxml): write and inspect FRITZ!Box phonebook XML`

---

### Task 4: `fritzbook export` command

**Files:**
- Create: `internal/cli/cli.go`, `internal/cli/args.go`, `internal/cli/export.go`, `internal/cli/export_test.go`
- Modify: `cmd/fritzbook/main.go`

**Interfaces:**
- Consumes: `contacts.ParseFiles`, `fbxml.Write`
- Produces:
  ```go
  type Env struct {
      Stdin io.Reader; Stdout, Stderr io.Writer
      Getenv func(string) string
      ReadPassword func() (string, error) // nil when stdin is not a terminal
      Now func() time.Time
      HTTPClient *http.Client            // nil → default; tests inject fake transport
  }
  func Run(args []string, env Env) int    // 0 ok, 1 runtime error, 2 usage error
  func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) // flags may follow positional args
  ```
  `main` sets `version` into `cli.Version` and calls `os.Exit(cli.Run(os.Args[1:], …))`, wiring `ReadPassword` via `golang.org/x/term` only when `term.IsTerminal(int(os.Stdin.Fd()))`.

`export` flags: `-o` (default `-` = stdout), `--name` (default `Nextcloud`). At least one `.vcf` → else usage error. Summary to stderr: `Converted N contacts (M numbers).` and, if any, `Skipped K:` followed by one `  - <name>: <reason>` line each.

- [ ] **Step 1: Write failing tests**: `TestExport_WritesFile` (`export testdata/vcf/nextcloud-v3.vcf -o <tmp>/out.xml --name X` → exit 0, `fbxml.Inspect` gives name X and expected count, stderr starts with `Converted`); `TestExport_FlagsBeforeAndAfterFiles`; `TestExport_NoFiles` → exit 2; `TestExport_MissingFile` → exit 1, stderr names the path; `TestVersion`.
- [ ] **Step 2:** `go test ./internal/cli/...` — FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** `task test`, `task lint`, `task build && ./dist/fritzbook export testdata/vcf/nextcloud-v3.vcf` prints XML.
- [ ] **Step 5: Commit** — `feat(cli): add export command`

---

### Task 5: Anonymizing trace recorder

**Files:**
- Create: `internal/trace/anonymize.go`, `internal/trace/recorder.go`, `internal/trace/trace_test.go`

**Interfaces:**
- Produces:
  ```go
  type Anonymizer struct{ /* … */ }
  func NewAnonymizer(keepPhonebook string) *Anonymizer
  func (a *Anonymizer) AddSecret(value, kind string) // kind e.g. "PASSWORD","USER","SID"; placeholder "<KIND>"
  func (a *Anonymizer) Apply(s string) string
  type Recorder struct{ /* … */ }
  func NewRecorder(next http.RoundTripper, a *Anonymizer) *Recorder // implements http.RoundTripper
  func (r *Recorder) Note(format string, args ...any)               // free-text step notes in the trace
  func (r *Recorder) WriteTo(w io.Writer) (int64, error)            // error if any registered secret survives
  ```
  Entries are stored raw and anonymized only in `WriteTo`, so secrets registered later (the SID after login) are still covered. The leak check in `WriteTo` is a final safety net.

Each entry records: step number, method, URL, request headers + body, status, response headers + body, duration (ms). Rules applied to everything (consistent numbering: the same value always gets the same placeholder, e.g. `<NAME-3>`):
registered secrets (longest first); `sid=` params and non-zero `<SID>` content → `<SID>`; `<Challenge>` content → `<CHALLENGE>`; `response=` param → `<RESPONSE>`; `<User …>` text → `<USER-n>`; headers `Authorization`, `WWW-Authenticate` nonce/opaque, `Cookie`, `Set-Cookie` → `<REDACTED>`; MAC `([0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}` → `<MAC-n>`; IPv4 → `<IPv4-n>`; IPv6 (≥ 2 colons, hex groups) → `<IPv6-n>`; `NewSerialNumber`/`Serial` content → `<SERIAL>`; inside any `<phonebook name="X">` with `X != keepPhonebook`: name → `<PHONEBOOK-n>`, `realName`/`number`/`email`/`imageURL` text → `<NAME-n>`/`<NUMBER-n>`/`<EMAIL-n>`/`<URL-n>`; TR-064 `NewPhonebookName` ≠ keep → `<PHONEBOOK-n>`; `NewPhonebookURL` → host and sid redacted by the rules above. Tag names, attributes names, status codes, message texts stay.

- [ ] **Step 1: Write failing tests**: `TestApply_Secrets` (password/user/sid never in output); `TestApply_ConsistentPlaceholders` (same MAC twice → same `<MAC-1>`); `TestApply_PhonebookKeepsTest` (export with books "Test" and "Privat": Test contacts verbatim, Privat names/numbers/emails replaced, name `<PHONEBOOK-1>`); `TestApply_LoginXML` (SessionInfo with Challenge/SID/Users anonymized, `BlockTime` kept); `TestRecorder_RecordsAndRedacts` (httptest server; trace contains status, duration, redacted `Authorization`); `TestWriteTo_FailsOnLeak` (test sets an unexported `disableRules` flag on the anonymizer so a registered secret survives → `WriteTo` returns an error and writes nothing); `TestRecorder_SecretRegisteredLater` (SID added after the request was recorded is still redacted).
- [ ] **Step 2:** FAIL. **Step 3:** Implement (regexp + string replace; no XML re-encoding, so the original shape survives). **Step 4:** `task test`, `task lint` PASS.
- [ ] **Step 5: Commit** — `feat(trace): add anonymizing HTTP recorder`

---

### Task 6: Login (PBKDF2, session ID) and fake box skeleton

**Files:**
- Create: `internal/fritz/client.go`, `internal/fritz/login.go`, `internal/fritz/login_test.go`, `internal/fritz/fritzfake/fake.go`

**Interfaces:**
- Produces:
  ```go
  type Client struct { BaseURL, TR064URL *url.URL; HTTP *http.Client; SID string; user, password string }
  func New(baseURL string, httpClient *http.Client) (*Client, error) // default "http://fritz.box"; TR064URL = same host, port 49000 (http) / 49443 (https); tests override TR064URL
  func SolveChallenge(challenge, password string) (string, error)    // "2$iter1$salt1$iter2$salt2"
  func (c *Client) Login(ctx context.Context, user, password string) error
  func (c *Client) Logout(ctx context.Context) error                  // GET login_sid.lua?logout=1&sid=…
  var ErrBadCredentials = errors.New("login failed: wrong user name or password")
  var ErrOldFirmware   = errors.New("FRITZ!OS 7.24 or newer required")
  type BlockedError struct{ Seconds int } // "login blocked by the FRITZ!Box for N seconds"
  // fritzfake
  type Phonebook struct { ID int; Name string; XML []byte }
  type Box struct { User, Password string; Phonebooks []Phonebook; BlockTime int; Requests []string /* "METHOD path", for order assertions */ }
  func (b *Box) Start(t *testing.T) *httptest.Server // one server for web and TR-064 paths; set Client.TR064URL to its URL
  ```

Login: `GET /login_sid.lua?version=2` → `SessionInfo{SID, Challenge, BlockTime}`; `BlockTime>0` → `BlockedError`; challenge not `2$` → `ErrOldFirmware`; `POST /login_sid.lua?version=2` form `username`, `response` = `salt2 + "$" + hex(pbkdf2(pbkdf2(pw, salt1, iter1), salt2, iter2))` (SHA-256, 32 bytes, salts hex-decoded); SID `0000000000000000` → `ErrBadCredentials`.

- [ ] **Step 1: Write failing tests**:
  - `TestSolveChallenge_AVMVector`: `SolveChallenge("2$10000$5A1711$2000$5A1722", "1example!")` == `"5A1722$1798a1672bca7c6463d6b245f82b53703b0f50813401b03e4045a5861e689adb"` (AVM's documented example, verified).
  - `TestLogin_Success` / `TestLogin_WrongPassword` / `TestLogin_Blocked` / `TestLogin_OldFirmware` (fake returns a hex MD5-style challenge) / `TestLogout`.
- [ ] **Step 2:** FAIL. **Step 3:** Implement (`crypto/pbkdf2.Key(sha256.New, …)`). **Step 4:** PASS + lint.
- [ ] **Step 5: Commit** — `feat(fritz): add PBKDF2 login and fake box`

---

### Task 7: Raw box calls and the probe program

**Files:**
- Create: `internal/fritz/tr064.go`, `internal/fritz/digest.go`, `internal/fritz/firmwarecfg.go`, `internal/fritz/raw_test.go`, `cmd/fritzprobe/main.go`, `cmd/fritzprobe/main_test.go`
- Modify: `internal/fritz/fritzfake/fake.go` (TR-064 + firmwarecfg handlers)

**Interfaces:**
- Consumes: `Client`, `Login` (Task 6), `trace.Recorder` (Task 5), `fbxml.Write`/`Inspect` (Task 3)
- Produces:
  ```go
  // TR-064 on port 49000 (http) / 49443 (https) of BaseURL's host, HTTP digest auth with the login credentials
  func (c *Client) TR064(ctx context.Context, controlURL, serviceType, action string, args map[string]string) (map[string]string, error)
  func (c *Client) PhonebookIDsTR064(ctx context.Context) ([]int, error)                 // X_AVM-DE_OnTel GetPhonebookList → "0,1,2"
  func (c *Client) PhonebookTR064(ctx context.Context, id int) (name, url string, err error) // GetPhonebook
  func (c *Client) DeviceInfo(ctx context.Context) (model, firmware string, err error)     // DeviceInfo GetInfo
  // firmwarecfg, raw: no interpretation of the answer
  func (c *Client) ExportPhonebookRaw(ctx context.Context, id int, name string) (status int, body []byte, err error)
  func (c *Client) ImportPhonebookRaw(ctx context.Context, id int, xml []byte) (status int, body []byte, err error)
  ```
  OnTel: control URL `/upnp/control/x_contact`, service `urn:dslforum-org:service:X_AVM-DE_OnTel:1`. DeviceInfo: `/upnp/control/deviceinfo`, `urn:dslforum-org:service:DeviceInfo:1`. Export form: `sid`, `PhonebookId`, `PhonebookExportName`, `PhonebookExport=""`. Import multipart in this order: `sid`, `PhonebookId`, `PhonebookImportFile` (filename `fritzbook.xml`, `text/xml`).

Probe (`fritzprobe --host http://fritz.box --user <name> [-o file] [--insecure] [--timeout 5m]`, password per Global Constraints). Every HTTP call goes through one `trace.Recorder` with `NewAnonymizer("Test")` and the user/password registered as secrets; SID registered right after login. Steps, each preceded by `Note("STEP n: …")`:
1. login; 2. DeviceInfo (model + FRITZ!OS version kept); 3. TR-064 list + GetPhonebook for each ID; 4. fallback listing: `ExportPhonebookRaw` for IDs 0–9 until a non-XML answer, `Inspect` each for its name; 5. pick the ID whose name is exactly `Test` — none or several → abort before any write with "Create exactly one phonebook named 'Test' first"; 6. export `Test` (before); 7. print the planned write and require the typed answer `yes`; 8. start `ImportPhonebookRaw` with three synthetic contacts (`Probe Contact 1–3`, numbers `+49 30 0000000001`…`3`) in a goroutine, immediately print "If your phone or the FRITZ!Box asks for a confirmation now, confirm it.", wait up to `--timeout`, record duration; 9. ask the human "Did the box ask for confirmation? (y/n)" and "Did you confirm it? (y/n)" and record both answers as notes; 10. export `Test` (after) and note `Inspect` count; 11. logout; 12. `WriteTo` the file (default `fritzprobe-<YYYYMMDD-HHMMSS>.txt`), print its path and "Review this file before sharing it."

- [ ] **Step 1: Write failing tests**: `TestSolveDigest_RFC2617` (known vector from RFC 2617 §3.5: user `Mufasa`, pw `Circle Of Life`, realm `testrealm@host.com`, nonce `dcd98b7102dd2f0e8b11d0f600bfb0c093`, uri `/dir/index.html`, qop `auth`, nc `00000001`, cnonce `0a4f113b` → response `6629fae49393a05397450978507c4ef1`); `TestTR064_GetPhonebookList` against fake; `TestExportImportRaw` against fake (multipart field order asserted by the fake); `TestProbe_RefusesWithoutTestPhonebook` (no import request reached the fake); `TestProbe_WritesOnlyToTest` (fake has books "Privat"(0) and "Test"(1); import only for ID 1; trace has no "Privat" contact data, no password, no SID); `TestProbe_AbortsWithoutYes`.
- [ ] **Step 2:** FAIL. **Step 3:** Implement (probe I/O through injectable stdin/stdout like `cli.Env`). **Step 4:** `task test`, `task lint`, `task probe` produces `dist/fritzprobe` (darwin/arm64).
- [ ] **Step 5:** Fill the "Probe workflow" section of `CONTRIBUTING.md` (box preparation, `task probe`, running it on the host, reviewing, storing under `testdata/probe/`, committing).
- [ ] **Step 6: Commit** — `feat(probe): add anonymizing probe program`

---

### Task 8: CHECKPOINT — human runs the probe (no agent box access)

The agent **stops** and guides the human (in the conversation language). Nothing in this task is done by the agent against the box.

- [ ] **Step 1 (human, FRITZ!Box UI):** create a FRITZ!Box user for the tool (System → FRITZ!Box Users) with the rights the import needs (start with "FRITZ!Box Settings"; note in the trace notes if something else was needed); make sure "Allow access for applications" (TR-064) is enabled (Home Network → Network → Network Settings); create a phonebook named exactly `Test` (Telephony → Telephone Book → New Telephone Book).
- [ ] **Step 2 (human, Mac terminal, outside the container):** `task probe` (or use the built `dist/fritzprobe`), then `./dist/fritzprobe --user <user>`; enter the password at the prompt; confirm on the phone when asked; answer the two questions.
- [x] **Step 3 (human):** read the generated file; remove anything that still looks private; save it as `testdata/probe/<YYYY-MM-DD>-6690-cable.txt`. **Done differently (owner's decision, 2026-11-21):** three probe rounds were run; the recordings stay local and git-ignored (`/fritzprobe-*.txt`), nothing was committed. The findings below are the substitute; `testdata/probe/` stays empty.
- [x] **Step 4 (agent):** read the recording and answer, in a short note appended to this plan under "Probe findings". Done (see "Probe findings" at the end of this plan).
- [x] **Step 5 (agent):** update Task 9's "Behavior" bullets with the findings. Done. The recording is **not** committed (see Step 3); the commit is `docs: record probe findings in the plan`.

---

### Task 9: Phonebook operations against the recorded behavior

**Files:**
- Create: `internal/fritz/phonebook.go`, `internal/fritz/phonebook_test.go`
- Modify: `internal/fritz/fritzfake/fake.go` (extend the fake from the "Probe findings" below, including a delayed-answer mode for the unobserved 2FA path; there is no recording to replay)

**Interfaces:**
- Consumes: Task 7 raw calls
- Produces:
  ```go
  type Phonebook struct { ID int; Name string }
  func (c *Client) ListPhonebooks(ctx context.Context) ([]Phonebook, error)
  func (c *Client) FindPhonebook(ctx context.Context, name string) (Phonebook, error) // exact, case-sensitive
  var ErrPhonebookNotFound  // "phonebook %q not found — create it in the FRITZ!Box web UI first"
  var ErrAmbiguousPhonebook // "several phonebooks are named %q"
  func (c *Client) ExportPhonebook(ctx context.Context, id int, name string) ([]byte, error) // multipart firmwarecfg export; the box needs the real name; validated by fbxml.Inspect
  type ConfirmFunc func(hint string) // called once when the box waits for the second factor
  func (c *Client) ImportPhonebook(ctx context.Context, id int, xml []byte, onConfirm ConfirmFunc) error
  var ErrConfirmationTimeout // "confirmation on the FRITZ!Box was not given in time"
  type ImportError struct { Status int; Message string } // box answer, anonymization not needed (no secrets)
  ```

Behavior (from "Probe findings"; Ruling R8: the interface change `ExportPhonebook(ctx, id, name)` follows from finding F3, Task 10 passes `pb.ID, pb.Name` from `FindPhonebook`):
- Listing only via TR-064 (`GetPhonebookList` + `GetPhonebook` per ID → `Phonebook{ID, Name}`); there is no export-loop fallback because the export needs the real name (F3). If TR-064 is unreachable or answers 401/403, the error says to enable "Allow access for applications" and to give the user the right permissions.
- Export (backup and verification): multipart `POST /cgi-bin/firmwarecfg` with fields in this order `sid`, `PhonebookId`, `PhonebookExportName`, `PhonebookExport` (empty); answer 200, `Content-Type: application/octet-stream`, body is plain phonebook XML containing only the user's contacts; a 302 to `/index.lua` (login page) means the session or the request was rejected → error. Never use `phonebook.lua` for backup or counts (F4).
- Import: multipart `POST /cgi-bin/firmwarecfg` as in Task 7; success is never decided from the (localized HTML) answer: status 200 means "accepted", a 302 or non-200 is an `*ImportError`, and Task 10 verifies by exporting again (count must match). The box replaces the target phonebook completely (F5).
- 2FA is unobserved (F6): the author's box did not ask. Design assumption until a recording exists: the import POST stays pending until the user confirms, so the context deadline is the confirmation timeout; `onConfirm` is called once if the POST has not returned after 3 s; the fake gets a delayed-answer mode to test it. If a real box answers differently, a probe round on that box decides.
- Fixtures in tests are built from the findings (no real bytes under `testdata/probe/`).

- [ ] **Step 1: Write failing tests** against the extended fake: `TestListPhonebooks`, `TestListPhonebooks_TR064Disabled` (error names the setting), `TestFindPhonebook_NotFound`, `TestFindPhonebook_Ambiguous`, `TestExportPhonebook`, `TestImport_Success`, `TestImport_WithConfirmation` (onConfirm called exactly once, then success), `TestImport_ConfirmationTimeout` (short ctx → `ErrConfirmationTimeout`), `TestImport_Rejected` (→ `*ImportError`).
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS + lint.
- [ ] **Step 5: Commit** — `feat(fritz): list, export and import phonebooks with 2FA`

---

### Task 10: `fritzbook upload` command

**Files:**
- Create: `internal/cli/upload.go`, `internal/cli/password.go`, `internal/cli/upload_test.go`
- Modify: `internal/cli/cli.go`

**Interfaces:**
- Consumes: `contacts.ParseFiles`, `fbxml.Write/Inspect`, `fritz.New/Login/FindPhonebook/ExportPhonebook/ImportPhonebook/Logout`, `trace.Recorder`, `cli.Env`. Note (Probe findings F3): `ExportPhonebook(ctx, pb.ID, pb.Name)` takes the real name from `FindPhonebook`; the "before" and "after" exports use it, and the verification count comes from the export (never from `phonebook.lua`).

Flags: `--phonebook` (required), `--host` (default `http://fritz.box`), `--user` (else `FRITZBOX_USER`, else usage error), `--yes`, `--backup-dir` (default `.`), `--timeout` (default `5m`), `--insecure` (skip TLS verification), `--verbose` (anonymized trace to stderr at exit, also on failure). Password: `FRITZBOX_PASSWORD`, else `Env.ReadPassword` with prompt `Password for <user>: ` on stderr, else error "no terminal: set FRITZBOX_PASSWORD".

Flow (spec "Upload flow"): convert (zero contacts → error before contacting the box) → login → find → export current → write backup `<backup-dir>/fritzbook-backup-<phonebook>-<YYYYMMDD-HHMMSS>.xml` (mode 0600) → prompt `Phonebook "<name>" (ID <id>, currently <n> entries) will be replaced by <m> contacts. Continue? [y/N] ` (only `y`/`yes`, case-insensitive; skipped by `--yes`) → import with `onConfirm` printing `The FRITZ!Box asks for confirmation: enter the code on a phone or press a button on the box (waiting up to <timeout>)…` → export again, `Inspect` count must equal m → `Phonebook "<name>" replaced: <m> contacts.` → logout (also deferred on errors). Any error after the backup: `error: <msg>\nBackup of the previous phonebook: <path>`.

- [ ] **Step 1: Write failing tests** (fake via `Env.HTTPClient`):
  `TestUpload_Success` (backup file exists with the "before" content, fake saw export → import → export, stdout has the success line); `TestUpload_PromptNo` (no import); `TestUpload_Yes` (no prompt read); `TestUpload_PasswordFromEnv` / `TestUpload_NoTerminalNoEnv` (exit 1, message); `TestUpload_LoginErrors` (table: unreachable host, wrong password, blocked → exit 1, single-line error, no backup file, no import — Review Focus 4); `TestUpload_FailureMentionsBackup` (table: import rejected, confirmation timeout, count mismatch → stderr contains the backup path — Review Focus 5); `TestUpload_PhonebookNotFound`; `TestUpload_Verbose` (stderr trace present, contains no password, no SID); `TestUpload_NoContacts` (no request reaches the fake).
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** `task test`, `task lint` PASS.
- [ ] **Step 5: Commit** — `feat(cli): add upload command with backup, prompt and verification`

---

### Task 11: Release pipeline and documentation

**Files:**
- Create: `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `README.md`
- Modify: `CONTRIBUTING.md` (Release section), `Taskfile.yml` if needed

- [ ] **Step 1:** `ci.yml`: on push/PR — `actions/checkout@v7`, `actions/setup-go@v7` (`go-version: '1.27'`), `go-task/setup-task@v2` (`version: 3.54.0`), `task lint`, `task test`, `task build:all`.
- [ ] **Step 2:** `release.yml`: on tags `v*` — same setup, `task build:all VERSION=${{ github.ref_name }}`, then `gh release create "$GITHUB_REF_NAME" dist/* --generate-notes` (`permissions: contents: write`).
- [ ] **Step 3:** `README.md` (spec "Repository documentation"): what/why (no credentials on the box), download from Releases (macOS: `xattr -d com.apple.quarantine fritzbook` note), FRITZ!Box preparation (dedicated user, target phonebook must exist), Nextcloud export (Contacts → address book menu → Download), `export` and `upload` examples, `FRITZBOX_USER`/`FRITZBOX_PASSWORD`, `--verbose` for bug reports, "Tested on FRITZ!Box 6690 Cable (FRITZ!OS <version from the probe>). No guarantee for other models." Link to CONTRIBUTING.md.
- [ ] **Step 4: Verify:** `task build:all` → five binaries in `dist/`; `ls dist/` shows `fritzbook-windows-amd64.exe` etc.; workflow YAML passes `actionlint` if available, otherwise reviewed by hand.
- [ ] **Step 5: Commit** — `ci: add CI and release workflows; docs: README`

---

### Task 12: Acceptance on the real box (human)

- [ ] **Step 1 (human, Mac):** `./dist/fritzbook upload <nextcloud-export>.vcf --phonebook Test --user <user>` → confirm on the phone → success line; check the "Test" phonebook in the web UI and on a handset (umlauts, number types).
- [ ] **Step 2 (human):** run against the real target phonebook. Keep the backup file.
- [ ] **Step 3 (agent):** fix anything reported via a `--verbose` trace (never via direct access), then hand over to the human for creating the GitHub repository and the first tag.

---

## Probe findings

Three probe rounds on 2026-11-21 against one box: FRITZ!Box 6690 Cable, FRITZ!OS 267.08.25, user with rights Dial/App/HomeAuto/BoxAdmin/Phone (all access level 2), phonebooks `0` (the user's real book, 365 contacts) and `1` (`Test`). Recordings are local only (owner's decision); everything the next tasks need is here.

- **F1 Login:** `login_sid.lua?version=2` with PBKDF2 works as implemented in Task 6. Rights are listed in the SessionInfo after login.
- **F2 TR-064:** works over plain HTTP on port 49000 with Digest authentication (realm `HTTPS Access`, MD5, `qop=auth`; every call first gets a 401 with a fresh nonce). `X_AVM-DE_OnTel#GetPhonebookList` → `<NewPhonebookList>0,1</NewPhonebookList>`; `GetPhonebook(id)` → `NewPhonebookName`, `NewPhonebookExtraID`, `NewPhonebookURL` (`http://<box>:49000/phonebook.lua?sid=<sid>&pbid=<id>`). `DeviceInfo#GetInfo` returns model, firmware and also the full event log (`NewDeviceLog`), which the anonymizer redacts.
- **F3 Export (`firmwarecfg`):** works only as `multipart/form-data` with fields `sid`, `PhonebookId`, `PhonebookExportName` (must be the phonebook's real name), `PhonebookExport` (empty). The same request as `application/x-www-form-urlencoded` is answered with `302 → /index.lua` (login page), even with the real name. Success answer: `200`, `Content-Type: application/octet-stream`, `Content-Disposition: attachment; filename="<...>_<phonebook name>_<hostname>_<model>_<firmware>_<date>.xml"`, body `<?xml version="1.0" encoding="utf-8"?><phonebooks><phonebook name="Test" owner="1"><contact>…</contact>…</phonebook></phonebooks>`; contacts carry the box-assigned `<uniqueid>`, `<telephony nid="1">` and `<setup />`. It contains only the user's contacts.
- **F4 `phonebook.lua` is not a faithful export:** `GET <NewPhonebookURL>` works read-only (200, `text/xml`, `Content-Disposition: attachment; filename=pbook.xml`, `<!-- number of contacts N -->`), but for the author's `Test` it also returned ten virtual entries (internal numbers such as `**9`, `**600`, handset names) on top of the three real contacts (13 instead of 3). Use it, if at all, only for diagnostics, never for backup, counts or verification.
- **F5 Import (`firmwarecfg`):** multipart fields in this order: `sid`, `PhonebookId`, `PhonebookImportFile` (filename `fritzbook.xml`, `text/xml`). Answer after about 1 s: `200 text/html` with the localized page "FRITZ!Box Telefonbuch wiederhergestellt" and a form to `/index.lua` (no contact count, no machine-readable status). The import **replaces** the target phonebook completely (the previous probe contacts were gone, the new ones present, the other phonebook unchanged at 365). Success must be verified by exporting again (F3) and comparing the count, as the spec says.
- **F6 Second factor:** the box did **not** ask for any confirmation (answers `n`/`n`), the import was synchronous. How a box with the second factor enabled behaves (long-pending request, polling page, other endpoint) is unobserved; Task 9 builds on an explicit assumption (see there) and a probe round on such a box would settle it.
- **F7 Rights:** the user above (Dial, App, HomeAuto, BoxAdmin, Phone) was sufficient for TR-064 and import; the exact minimal subset was not narrowed down.
- **F8 Probe quirks worth keeping in mind:** other phonebooks must never be read beyond counts, `Test` contents are recorded verbatim, the probe aborts before any write unless exactly one TR-064 name equals `Test`.
