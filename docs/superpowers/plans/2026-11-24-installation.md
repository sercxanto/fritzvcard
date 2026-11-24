# Installation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `fritzvcard` installable via Homebrew on macOS and Linux and via WinGet on Windows, with direct downloads as a fallback.

**Architecture:** Keep the existing Go builds and release workflow. Package their outputs once, then generate Homebrew and WinGet metadata from those exact release hashes, without publishing to either catalog from CI.

**Tech Stack:** Go 1.27, Task, Python standard library and `unittest`, GitHub Actions, Homebrew Ruby DSL, WinGet YAML.

**Spec:** [Installation design](../specs/2026-11-24-installation-design.md)

## Global Constraints

- Keep stable `vX.Y.Z` tags, changelog-derived release notes, and the existing five-target matrix.
- Publish `.tar.gz` on macOS and Linux and `.zip` on Windows, with the CLI at the archive root and a `SHA256SUMS` file for the archives. Do not package `fritzprobe`.
- Use the public `sercxanto/homebrew-fritzvcard` tap and fully qualified formula name; use the proposed `Sercxanto.Fritzvcard` catalog ID and `fritzvcard` alias on Windows x64.
- GitHub Releases are the only binary source. No project-owned install scripts, no new runtime dependencies, no FRITZ!Box access.
- Do not tag, create the external tap, or submit the catalog PR during in-repo implementation. Do not advertise package commands to users until they work publicly.

## Review Focus

1. A missing cross-build output fails packaging instead of producing a partial release (Task 1).
2. The tar executable bit survives unpacking; Windows ZIP has the exact portable executable path (Task 1).
3. A stale checksum file with a duplicate, missing asset, or invalid hash cannot produce metadata (Task 2).
4. The same CPU on different operating systems gets different release assets and hashes (Task 2).
5. A WinGet update has an unprefixed catalog version and a `v`-prefixed immutable release path (Task 2).

## File Map

- `.github/scripts/package_release.py` and `test_package_release.py`: archive contract, checksums, end-to-end packaging checks.
- `.github/workflows/ci.yml` and `release.yml`: test and package the existing binaries; attach the exact six assets on release.
- `.github/scripts/package_metadata.py` and `test_package_metadata.py`: generate both package definitions from published release metadata.
- `README.md`, `CHANGELOG.md`, `CONTRIBUTING.md`: user-facing fallback, release notes, and maintainer handoff.

---

### Task 1: Publish and verify installable archives

**Files:** Create `.github/scripts/package_release.py` and `.github/scripts/test_package_release.py`. Modify `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `README.md`, `CHANGELOG.md`, and the release asset list in `CONTRIBUTING.md`.

**Interfaces:** `package_release(dist: pathlib.Path) -> None`, exposed as `python3 .github/scripts/package_release.py dist`. Consumes `task build:all` outputs. Produces `fritzvcard-{linux,darwin}-{amd64,arm64}.tar.gz`, `fritzvcard-windows-amd64.zip`, and `SHA256SUMS` in `dist/`. Each sum line is lowercase SHA-256, two spaces, and the archive basename, in filename order.

- [ ] **Write failing packaging tests.** Run the script as a subprocess in each test against a temporary `dist` of fake binaries with distinct bytes. In `test_packages_all_targets`, assert exit `0`; open all five archives and assert that the only member is `fritzvcard` (tar) or `fritzvcard.exe` (ZIP), that it contains the exact matching fixture bytes, and that the tar file mode is `0o755`. Assert the checksum file has exactly the five expected lines with `hashlib.sha256(archive.read_bytes()).hexdigest()`. Include a `fritzprobe` decoy. In `test_missing_binary_fails_before_packaging`, omit Windows, assert a nonzero exit, `fritzvcard-windows-amd64.exe` in stderr, and no archives or checksum file in the clean temp directory.
- [ ] **Run the red tests.** `python3 -B -m unittest discover -s .github/scripts -p test_package_release.py` fails because the script does not exist.
- [ ] **Implement the packager.** Explicitly map all five build output paths to assets, check all inputs before writing, use `tarfile` and `zipfile`, and hash the finished files. Set the tar member to mode `0o755` regardless of cross-build source mode. Print missing inputs to stderr and exit nonzero.
- [ ] **Wire CI and release.** After `task build:all`, run the script tests, package the binaries, and run `(cd dist && sha256sum --check SHA256SUMS)` in both workflows. Make Release attach exactly the five archives and checksum file, using explicit paths. Do not change the tag checks, main coverage logic, or release note process.
- [ ] **Document the direct install path.** README lists the supported architectures, GitHub Releases, unpack-and-run examples for POSIX and PowerShell, optional PATH setup, checksum verification, and the existing source fallback. Keep macOS security guidance scoped to a trusted, verified download; do not advise disabling system-wide protections. Record the archive change in `CHANGELOG.md` under `Unreleased`; update the asset list and local build/package instructions in `CONTRIBUTING.md`.
- [ ] **Verify.** Run `task lint`, `task test`, `python3 -B -m unittest discover -s .github/scripts`, and `task build:all VERSION=v0.0.1`; run the packager and check all five sums. On Linux x64, extract the Linux x64 tarball in a fresh temp directory and verify `./fritzvcard version` prints `fritzvcard v0.0.1` without a manual `chmod`.
- [ ] **Commit.** `feat(release): publish installable archives with checksums`

### Task 2: Generate and hand off package-manager definitions

**Files:** Create `.github/scripts/package_metadata.py` and `.github/scripts/test_package_metadata.py`. Modify `CONTRIBUTING.md`.

**Interfaces:** `generate_metadata(tag: str, checksums: pathlib.Path, output: pathlib.Path) -> None`, exposed as `python3 .github/scripts/package_metadata.py vX.Y.Z path/to/SHA256SUMS output-dir`. Produces `Formula/fritzvcard.rb` and `manifests/s/Sercxanto/Fritzvcard/X.Y.Z/Sercxanto.Fritzvcard{,.locale.en-US,.installer}.yaml` under `output-dir`.

- [ ] **Write failing generator tests.** With `v1.2.3` and unique SHA-256 fixture values reused from the archive contract, `test_homebrew_formula_maps_platforms` asserts four correct URL/hash pairs in the macOS/Linux × Intel/ARM branches and `bin.install "fritzvcard"`. `test_winget_manifest_is_portable_zip` asserts the three files have `Sercxanto.Fritzvcard` and `PackageVersion: 1.2.3`; the installer file contains the `v1.2.3` Windows URL, Windows checksum, `Architecture: x64`, `InstallerType: zip`, `NestedInstallerType: portable`, `RelativeFilePath: fritzvcard.exe`, and `PortableCommandAlias: fritzvcard`. `test_rejects_invalid_release_inputs` checks duplicate and missing asset names, a nonhex digest, and a prerelease tag; each must fail before writing metadata.
- [ ] **Run the red tests.** `python3 -B -m unittest discover -s .github/scripts -p test_package_metadata.py` fails because the script does not exist.
- [ ] **Implement the generator.** Require exactly one valid digest per expected archive filename and a stable tag before writing. For every asset use `https://github.com/sercxanto/fritzvcard/releases/download/{tag}/{filename}`, never `/latest/`. Emit a binary-install Homebrew formula with `on_macos`/`on_linux` and `on_intel`/`on_arm`, explicit version `X.Y.Z`, MIT license, and a `test do` block checking `fritzvcard vX.Y.Z`. Emit WinGet 1.12.0 version, installer, and `en-US` default-locale manifests; use the project's GitHub URL and MIT license. Surface bad input on stderr with nonzero exit. Share the filenames with the packager rather than maintaining a second matrix.
- [ ] **Document the release handoff.** In `CONTRIBUTING.md`, show how to download the tagged release and checksum file, verify hashes, run the generator, and validate the definitions before publication. Document testing the formula on macOS and Linux (including `brew test`), running `winget validate` and testing the local manifest in a clean Windows environment, then publishing the formula in the tap and submitting the version directory to `microsoft/winget-pkgs`. In a separate post-publication README update, add each install command only after its public install succeeds and `fritzvcard version` matches the release. Keep release downloads available while WinGet is in review.
- [ ] **Verify.** Run `task lint` and `python3 -B -m unittest discover -s .github/scripts`. Generate metadata from the fixture or local package result and check Ruby syntax with `ruby -c output-dir/Formula/fritzvcard.rb` where Ruby is available. Record real package-manager validation as a maintainer release gate, not as a test that can pass without a release.
- [ ] **Commit.** `feat(packaging): generate Homebrew and WinGet metadata`
