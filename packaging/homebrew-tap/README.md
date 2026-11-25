# sercxanto/tap

Homebrew formulae by sercxanto, for macOS and Linux.

| Formula | Description |
| --- | --- |
| [`fritzvcard`](https://github.com/sercxanto/fritzvcard) | Convert vCard files to FRITZ!Box phonebook XML |

## Install

```sh
brew install sercxanto/tap/fritzvcard
```

The fully qualified name trusts only this formula. Afterwards, `brew upgrade fritzvcard`
works with the short name.

## Updates

The `Update fritzvcard` workflow checks daily for a new stable fritzvcard release. It
generates the formula from the release's `SHA256SUMS`, installs and tests it on macOS and
Linux (Intel and ARM), and only then commits it here. Start it by hand from the Actions tab
or with:

```sh
gh workflow run update-fritzvcard.yml --repo sercxanto/homebrew-tap
```

The workflow is maintained in
[`packaging/homebrew-tap/`](https://github.com/sercxanto/fritzvcard/tree/main/packaging/homebrew-tap)
of the fritzvcard repository.
