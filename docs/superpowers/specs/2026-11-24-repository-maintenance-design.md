# Repository maintenance design

Keep dependency updates reviewable and give users a concise history before the first release.

- Configure weekly Dependabot version updates for Go modules and GitHub Actions at `/`, on the default branch. Group minor and patch updates per ecosystem; leave major updates eligible for individual PRs. Do not configure auto-merge.
- Add an English `CHANGELOG.md` with `Unreleased` and a short summary of the current CLI. In `CONTRIBUTING.md`, explain how to record user-facing changes and move them into a dated version section before tagging the release commit.
- In a separate step, pin the external actions in CI and Release to their full upstream commit SHAs. Preserve the commits selected by the existing tags, and retain readable version comments.
- In a separate step, use the body of the matching `## X.Y.Z - YYYY-MM-DD` section from the tagged commit's `CHANGELOG.md` as the GitHub Release notes for `vX.Y.Z`. Fail before publishing if the section is missing, empty, or duplicated. Do not append generated notes.
- Give the English README a concise opening, a CI status badge for `main`, an MIT license badge, and links to Installation, Quick Start, Upload, and the changelog. Lead with offline export and manual import; keep direct upload optional and put the overwrite warning next to the upload example. Document building with `task build` while there are no release downloads, and fold the existing XML example into a disclosure section. Defer the Latest Release badge until the first release exists.

## Approaches

Use grouped weekly Dependabot PRs rather than one PR per update to limit noise without hiding major updates. Maintain the changelog by hand rather than generating it from commit history so the project can explain changes in user-facing terms. Reuse those entries as the GitHub Release notes instead of publishing a separate generated list that can diverge. Use a compact Markdown README instead of a banner so visitors can get to the first command quickly.

Apart from external action refs and their version comments, change only the source of notes in the Release workflow. Keep its tag trigger, permissions, checks, and five assets. Do not add checksums or create a tag or release as part of this work.
