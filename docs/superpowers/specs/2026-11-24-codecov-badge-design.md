# Codecov Badge Design

Show current Go coverage for `main` next to the CI badge in `README.md`, with a link to the breakdown.

## Approaches

- **Codecov (selected):** Upload from the existing GitHub Actions job; Codecov serves the badge and coverage breakdown. The maintainer has configured the repository's `CODECOV_TOKEN` Actions secret.
- **GitHub Pages:** Avoids an external coverage service but requires maintaining badge generation and publishing.
- **Static badge:** Simplest to add, but goes stale unless updated manually.

## Scope

Generate a coverage profile in the existing Linux CI job's Go test run, including cross-package coverage. Upload only after successful checks on pushes to `main`; pull requests and other refs still run the tests without uploading. Keep the workflow's read-only repository permissions and scope the secret to the pinned Codecov upload step. A failed upload should not fail CI. Do not hardcode a percentage or introduce a coverage threshold. Local tests, the Windows job, and the release workflow stay unchanged.

The badge shows Codecov's reported coverage, which need not match Go's statement coverage. It needs a successful upload from `main` before it can show a percentage.
