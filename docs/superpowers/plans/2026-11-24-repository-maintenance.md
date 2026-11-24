# Repository Maintenance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Schedule dependency update PRs, pin workflow actions, use the changelog for GitHub Release notes, and make the README easier to start from.

**Architecture:** Dependabot runs through GitHub's version update service, not a new workflow. The manually maintained changelog is the single source for release notes; the Release workflow extracts the matching section from the checked-out tag.

**Tech Stack:** GitHub Dependabot v2, YAML, Markdown, Python 3 standard library, GitHub CLI.

**Spec:** [Repository maintenance design](../specs/2026-11-24-repository-maintenance-design.md)

## Global Constraints

- Write all repository content in English.
- Do not configure auto-merge. Workflow changes are limited to action refs and version comments and the Release notes source. Keep the Release tag trigger, permissions, checks, and five assets.
- Do not add checksums or create a tag or release.

## Review Focus

- Unmatched major updates remain eligible for individual PRs.
- Dependabot reads the root Go module and the workflows without overriding the default branch.
- The first release must not appear as already published in the changelog.
- The release instructions must place the changelog update in the tagged commit.
- Both workflows must retain the same action code, triggers, permissions, and inputs after pinning.
- A missing, blank, or duplicate version section must fail rather than publish empty or ambiguous notes.
- Before the first release, the README must offer a usable build path, with offline export before optional upload and no empty release badge.

### Task 1: Schedule dependency version updates

**Files:** Create `.github/dependabot.yml`.

**Interface:** GitHub reads this file from the default branch; there are no cross-task dependencies.

- [ ] Prepare a temporary YAML check asserting v2; exactly `gomod` and `github-actions` at `/`; `schedule.interval: weekly`; one catch-all group per ecosystem with exactly `minor` and `patch`; and no `ignore` or `target-branch` override. Run it before editing and verify it fails because the file is missing.
- [ ] Add the configuration using groups `go-minor-patch` and `actions-minor-patch` with `patterns: ["*"]`. Leave security-update settings unchanged.
- [ ] Repeat the check and run `git diff --check`. GitHub's actual update jobs can only be confirmed after the file lands on the default branch.

### Task 2: Add changelog and release instructions

**Files:** Create `CHANGELOG.md`; update the conventions and release sections of `CONTRIBUTING.md`.

**Interface:** Maintainers use `## Unreleased` and move entries into `## X.Y.Z - YYYY-MM-DD` before tagging.

- [ ] Add `# Changelog`, `## Unreleased`, and a short `### Added` list covering vCard-to-XML conversion and optional upload. Add no dated version heading yet.
- [ ] Link the changelog from `CONTRIBUTING.md`; explain that only user-facing changes need entries and that the dated heading belongs in the commit selected for tagging. Leave the release notes description unchanged until Task 4.
- [ ] Check that the relative link resolves, that `Unreleased` is the only version section, and that neither workflow differs from `HEAD`. Run `git diff --check`, `task lint`, and `task test`.

### Task 3: Pin external GitHub Actions by commit SHA

**Files:** Modify `.github/workflows/ci.yml` and `.github/workflows/release.yml`.

**Interface:** Every existing external `uses:` keeps its upstream repository and changes from `@tag` to `@<full commit SHA> # <version>`.

- [ ] Record the commit currently selected by `actions/checkout@v7`, `actions/setup-go@v7`, and `go-task/setup-task@v2` using the official upstream repositories. Resolve annotated tags to commits, not tag object IDs. Use a full release tag in the comment if it points to that exact commit; otherwise keep the original tag as the comment.
- [ ] Prepare a check that each external `uses:` entry matches the recorded SHA and that restoring just those lines gives the original workflow files. Confirm the SHA check fails on the current tag-based files.
- [ ] Replace the refs in both workflows consistently. Do not change action inputs such as Go or Task versions.
- [ ] Run the workflow comparison and `git diff --check`. If the changes are pushed, check the existing CI run; do not trigger the release workflow just to test the pins.

### Task 4: Publish the matching changelog section as release notes

**Files:** Create `.github/scripts/release_notes.py` and `.github/scripts/test_release_notes.py`; update `.github/workflows/release.yml` and `CONTRIBUTING.md`.

**Interface:** `python3 .github/scripts/release_notes.py vX.Y.Z CHANGELOG.md` writes only the Markdown body of `## X.Y.Z - YYYY-MM-DD` to stdout. It exits nonzero with a diagnostic on stderr for a missing, empty, or duplicated section.

- [ ] Add tests for exact output from a section between `Unreleased` and an older version, including a `### Added` subheading. Also test a matching section at EOF, a near-match version (e.g. `0.1.01` is not `0.1.0`), a whitespace-only section, and two sections for the same version with different dates. Error cases must have nonzero exit and no notes on stdout. Run `python3 -m unittest discover -s .github/scripts`.
- [ ] Implement the extractor: match the exact version and date-shaped heading, stop at the next level-two heading or EOF, preserve the body Markdown, and check uniqueness and non-emptiness before printing. Repeat the tests.
- [ ] After checkout and before building, run the extractor with `"$VERSION"` and redirect its output to `"$RUNNER_TEMP/release-notes.md"`. In `gh release create`, replace `--generate-notes` with `--notes-file "$RUNNER_TEMP/release-notes.md"`, retaining `--verify-tag` and the existing asset list.
- [ ] Update `CONTRIBUTING.md` to say that the version section in the tagged commit becomes the release notes, and that publication is blocked without a unique, non-empty section.
- [ ] Verify the workflow uses the same output path for extraction and publication, keeps the original trigger and assets, and no longer uses `--generate-notes`. Run the extractor tests, `git diff --check`, `task lint`, and `task test`. Use fixtures, not a real tag, to check note generation.

### Task 5: Add README badges and a safe Quick Start

**Files:** Modify `README.md`. The changelog link depends on Task 2.

**Interface:** GitHub renders the README with a native CI badge for `.github/workflows/ci.yml` on `main`, an MIT badge linking to `LICENSE`, and links to Installation, Quick Start, Upload, and `CHANGELOG.md`.

- [ ] Add the badges and navigation after the title and concise English summary. Explain that XML conversion works offline and Nextcloud does not need FRITZ!Box credentials. Do not add a banner or a Latest Release badge.
- [ ] In Installation, retain the Releases link and add `task build` and `dist/fritzvcard` as the source-build path, linking to `CONTRIBUTING.md` for setup.
- [ ] Put `fritzvcard export -o phonebook.xml contacts.vcf` in Quick Start, followed by manual import through the FRITZ!Box web interface. Link to the optional direct upload instructions and put the whole-phonebook replacement warning immediately above the upload command.
- [ ] Move the XML example into a GitHub-rendered `<details>` section, preserving the example and existing compatibility, backup, and restore guidance.
- [ ] Check the badge targets, navigation anchors, and local documentation links. Run `task build`, then exercise the Quick Start export on `testdata/vcf/nextcloud-v3.vcf` with an output file in a temporary directory. Confirm it produces phonebook XML without trying the upload. Run `git diff --check`.
