# Changelog

## Unreleased

### Changed

- The Homebrew tap is now `sercxanto/tap`: install with `brew install sercxanto/tap/fritzvcard`.
  Existing installations from `sercxanto/fritzvcard` keep receiving updates.

## 0.1.0 - 2026-11-24

### Added

- Convert one or more vCard files to FRITZ!Box phonebook XML without connecting to the box.
- Optionally upload contacts directly to an existing FRITZ!Box phonebook. The upload backs up
  the current phonebook, asks before replacing it by default, and verifies the imported contacts.

### Changed

- Release downloads are ready-to-extract archives for macOS, Linux, and Windows, with
  SHA-256 checksums.
