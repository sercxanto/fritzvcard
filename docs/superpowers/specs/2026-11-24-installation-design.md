# Simplify installation

## Goal and approaches

Give users one install command per platform when the package manager is already
available. The command must make `fritzvcard` available without knowing the CPU
architecture, renaming a file, or editing `PATH` by hand.

- **Downloads only:** the least maintenance, but users still select and install
  an archive themselves. Keep this as the fallback.
- **Project-owned install scripts:** work without an existing package manager
  but leave installation, upgrades, and removal for the project to maintain.
  Do not ship shell or PowerShell installers in this iteration.
- **Package managers with release downloads as a fallback (chosen):** use the
  same Homebrew tap on macOS and Linux, as requested. Use WinGet on Windows
  rather than asking users to download and execute PowerShell code.

## Distribution

- Keep the stable-tag release checks, changelog notes, and existing build
  matrix. Publish five archives (`.tar.gz` on macOS and Linux, `.zip` on
  Windows) containing `fritzvcard` or `fritzvcard.exe` at the root, plus a
  `SHA256SUMS` file for the archives. Package only the user-facing CLI, not
  `fritzprobe`.
- **macOS and Linux:** target `brew install sercxanto/fritzvcard/fritzvcard`
  from a public `sercxanto/homebrew-fritzvcard` tap. The formula selects the
  matching prebuilt archive by OS and architecture, verifies its pinned
  checksum, and installs without Go. Use the fully qualified formula name
  rather than instructing users to trust the entire tap.
- **Windows x64:** target `winget install --id Sercxanto.Fritzvcard --exact`
  (proposed ID). Prepare a portable ZIP manifest with a versioned download URL,
  a checksum, and `fritzvcard` as its command alias. Public availability
  depends on acceptance into Microsoft's community catalog after a release
  exists; the Windows ZIP is usable without WinGet.
- Keep GitHub Releases as the only binary source. Document how maintainers
  prepare and validate package definitions against the actual release assets.
  Publishing the first tag, creating the external tap, and submitting a catalog
  PR are separate publication steps. Do not advertise either package command
  as available before it works publicly.

## Checks

Check all five archives and checksums in CI before publication. Smoke-test the
package-manager paths where available with `fritzvcard version` and confirm the
installed version matches the release; no connection to a FRITZ!Box is needed.
Keep manual downloads and the build-from-source fallback in the README.
