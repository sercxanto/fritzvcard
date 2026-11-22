# Follow-ups

Known, deliberately deferred items from the first implementation round (all reviewed as
non-blocking). Pick from here for future work; remove an item when it is done.

## Before relying on it more widely

- **Name check may reject a good upload.** After an import, `upload` requires the exported
  names to equal the uploaded ones (multiset). If a box normalizes names (truncation,
  whitespace, control characters, Unicode normalization), it reports a mismatch although the
  import was applied. It fails closed; relax or normalize if it shows up in practice.
- The backup export of phonebook ID 0 was not exercised on the tested box.

## CLI

- `export` prints `fritzbook export: <err>`, `upload` prints `error: <err>`: unify the prefixes.
- A declined prompt is reported as `error: aborted…` with exit 1.
- `parseInterspersed` treats `--` as the separator even when it is a flag's value (`-o --`).
- Backup wording differs: "Backup of the current phonebook" (stderr) vs "previous" (stdout).
- The README console example shows `Converted…` (stderr) before the XML (stdout).
- README: give the exact TR-064 menu path (Home Network → Network → Network Settings).

## Conversion

- The folded `NOTE` in `testdata/vcf/nextcloud-v3.vcf` is not asserted (Contact has no NOTE).
- `fbxml` tests: escaping covers names only (not emails/phonebook attribute); no test for
  missing `<phonebook>`, nested elements, or an empty `Type`.
- Name check: add a test for `[A,A]` against `[A,B]` (a set-instead-of-multiset mutation is
  currently not caught).

## Trace / anonymizer (affects `--verbose` and the probe)

- Not redacted: `vanity`/`quickdial`/`category`/`ringtone`/`uniqueid`/`mod_time` fields,
  `Proxy-Authorization`, SOAP `<Auth>`/`<Nonce>`/`<UserName>` unless registered as secrets,
  hostnames outside `NewPhonebookURL`.
- Compressed or binary bodies are recorded raw.
- IPv6 edge cases: `::ffff:1.2.3.4` remnant, `%zone` kept, trailing `::`, `std::string` false
  positive; bracketed host `[::1]` gets an ugly placeholder.
- A non-Test book named like a technical word (`Phone`, `Tel`, `Box`, `Probe`, `Tes`) makes
  the trace harder to read (the name guard garbles it); structural words are skipped.
- `scanString` over-redacts on odd double-encoded input (safe direction).
- Test gaps: `Note("")`, `::1`, `WWW-Authenticate`.

## Client / fake box

- PBKDF2 iteration counts have no upper bound; `Logout` drains the body unbounded.
- `TR064URL` is built by hand (use `net.JoinHostPort`, clear userinfo and path).
- Test gaps: unreachable host / cancelled context, non-200 or garbage XML, wrong user name,
  https/IPv6 `TR064URL`, `version=2` and Content-Type assertions.
- The 3xx import message always says "login page"; a TR-064 SOAP fault with HTTP 403 gets no
  "enable access for applications" hint; duplicate/empty book names pass through to
  `FindPhonebook`.
- Fake box: logout flag is not sticky; `GetPhonebook` does not XML-escape names.

## Probe

- Probe tag is `HHMMSS` without date (a collision needs the same time of day while the old
  run's contacts are still in `Test`); numbers `+49 30 0000<HHMMSS><i>` look less obviously fake.
- No test for the `unexpected` verdict branch or an unreadable before-state only.
- The probe still writes when `Test` cannot be exported beforehand (verdict `unknown`).
- Tripwire compares counts only and warns after the write.
- CONTRIBUTING: instead of "empty Test", say to leave one or two harmless synthetic contacts
  (an empty `Test` always gives the verdict `ambiguous`).
- Never run the probe while `Test` holds real contacts (it is recorded verbatim).

## Upload

- A partial backup is removed on failed write/sync; the backup-write-failure test is skipped
  when run as root.
- `TestUpload_TimeoutsAreIndependent…` uses a 100 ms `setupTimeout`: raise to ~200 ms /
  500 ms prompt delay if CI shows flakes.

## Build and CI

- `release.yml` interpolates `${{ github.ref_name }}` unquoted in `run:`; harden via
  `env: VERSION: ${{ github.ref_name }}`.
- The workflows were reviewed by hand only (no `actionlint` in the container).
- `.devcontainer/devcontainer-lock.json` is untracked: commit it or add it to `.gitignore`.
