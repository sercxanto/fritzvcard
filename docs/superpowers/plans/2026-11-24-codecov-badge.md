# Codecov Badge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a live Go coverage badge for `main` in the README.

**Architecture:** The existing Linux CI job writes a Go coverage profile and sends it to Codecov only after successful checks on pushes to `main`. The README embeds the Codecov badge and links to the same branch's report.

**Tech Stack:** Go coverage, GitHub Actions, Codecov, Markdown

**Spec:** [Codecov Badge Design](../specs/2026-11-24-codecov-badge-design.md)

## Global Constraints

- Keep `contents: read`, pin the Codecov action to a full commit SHA, and reference `CODECOV_TOKEN` only in that step.
- Do not hardcode a percentage or introduce a coverage threshold.
- Local tests, the Windows job, and the release workflow stay unchanged.

## Review Focus

- Fork PRs, non-`main` pushes, and tag pushes still test but never upload.
- Failed checks skip the upload; a Codecov upload error does not fail CI.
- Only the generated Go coverage profile is sent.
- The report and badge link target `main`, not just the repository default.
- Hosted ingestion and badge rendering need a successful push to `main`; local verification cannot prove either.

---

### Task 1: Publish coverage and link the badge

**Files:**
- Modify: `.github/workflows/ci.yml` — generate and upload the report
- Modify: `README.md` — display and link the badge

**Interfaces:**
- Consumes: `${{ secrets.CODECOV_TOKEN }}` provisioned by the maintainer
- Produces: `./coverage.out` in the Linux CI job and a Codecov badge for `sercxanto/fritzvcard` on `main`

- [x] Before editing, run a local workflow/README check for the following requirements and confirm it fails: coverage command present, one upload step guarded by a successful non-deletion push to exactly `refs/heads/main`, only `./coverage.out` selected, upload errors non-blocking, secret scoped to upload, and both README URLs refer to `main`. Check the shell guard with normal pushes, deletions, a tag, and a differently cased branch.
- [x] In the Linux `check` job, replace `task test` with `CGO_ENABLED=0 go test -coverpkg=./... -coverprofile=coverage.out ./...`, preserving the Taskfile's Go environment. After the cross-build, add the official Codecov action at a verified full release SHA. For a `push` event, use Bash to check `github.event.ref` case-sensitively and reject `github.event.deleted`; upload only if that check enables it. Set `files: ./coverage.out`, `disable_search: true`, `fail_ci_if_error: false`, and `token: ${{ secrets.CODECOV_TOKEN }}`. Mark the whole step `continue-on-error: true` so setup errors inside the action are non-blocking too.
- [x] Place the Codecov badge next to CI in `README.md`. Use `https://codecov.io/gh/sercxanto/fritzvcard/branch/main/graph/badge.svg` for the image and `https://app.codecov.io/gh/sercxanto/fritzvcard/tree/main` for the link.
- [x] Re-run the local workflow/README check, validate the workflow with `actionlint`, and run the coverage command with a temporary output path. Verify all Go packages pass and `go tool cover -func` reads the profile. Run `git diff --check`.
- [ ] After the change reaches `main`, the maintainer checks the Upload coverage step, then opens the badge link to confirm Codecov shows the matching commit and a percentage.
