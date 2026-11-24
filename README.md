# fritzvcard

Convert vCard (`.vcf`) exports to FRITZ!Box phonebook XML. Export offline or upload directly
from your computer. Nextcloud never needs your FRITZ!Box credentials.

[![CI status](https://github.com/sercxanto/fritzvcard/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/sercxanto/fritzvcard/actions/workflows/ci.yml?query=branch%3Amain)
[![Go coverage](https://codecov.io/gh/sercxanto/fritzvcard/branch/main/graph/badge.svg)](https://app.codecov.io/gh/sercxanto/fritzvcard/tree/main)
[![MIT License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

[Installation](#installation) · [Quick Start](#quick-start) · [Upload](#upload-to-the-fritzbox-upload) · [Changelog](CHANGELOG.md)

## Installation

`fritzvcard` is a single binary with no runtime dependencies. Download the archive for
your system and `SHA256SUMS` from the same [GitHub Release](../../releases):

| System | Archive |
| --- | --- |
| macOS, Apple silicon | `fritzvcard-darwin-arm64.tar.gz` |
| macOS, Intel | `fritzvcard-darwin-amd64.tar.gz` |
| Linux, x86-64 | `fritzvcard-linux-amd64.tar.gz` |
| Linux, ARM64 | `fritzvcard-linux-arm64.tar.gz` |
| Windows, x64 | `fritzvcard-windows-amd64.zip` |

**macOS and Linux:** Replace the archive name below with the one you downloaded.
Compare the printed SHA-256 hash with its entry in `SHA256SUMS` before extracting.

```sh
shasum -a 256 fritzvcard-darwin-arm64.tar.gz # on Linux, sha256sum also works
tar -xzf fritzvcard-darwin-arm64.tar.gz
./fritzvcard version
```

If macOS blocks the binary, see Apple's [guidance for opening an app from an unidentified developer](https://support.apple.com/en-us/102445).
Only make an exception for this file if you trust the project and have verified the download.

**Windows (PowerShell):** Compare the printed hash with the ZIP's entry in `SHA256SUMS`
before extracting. Hash comparison is case-insensitive.

```powershell
Get-FileHash .\fritzvcard-windows-amd64.zip -Algorithm SHA256
Expand-Archive .\fritzvcard-windows-amd64.zip -DestinationPath .\fritzvcard
.\fritzvcard\fritzvcard.exe version
```

To run `fritzvcard` from any directory, put the binary in a directory already on
your `PATH` or add its directory to your user `PATH`. Otherwise, use the executable
path shown above in place of `fritzvcard` in the examples below.

**Build from source:** If no releases are available, clone the repository and run
`task build` from the project root. See [CONTRIBUTING.md](CONTRIBUTING.md) for setup.
On macOS and Linux, run the result as `./dist/fritzvcard`.

Run `fritzvcard --help` to list the commands. Use `fritzvcard upload --help` or
`fritzvcard --help upload` to see the upload options and their defaults.

## Quick Start

Start with a `.vcf` file, such as a [Nextcloud address book export](#export-contacts-from-nextcloud).
Convert it to XML without connecting to the FRITZ!Box:

```sh
fritzvcard export -o phonebook.xml contacts.vcf
```

In the FRITZ!Box web interface, open **Telephony → Phonebook**. Back up the target phonebook
before importing `phonebook.xml` to replace its contents.
You can also [upload directly](#upload-to-the-fritzbox-upload) instead.

## Export contacts from Nextcloud

In Nextcloud open Contacts, open the menu (`...`) of the address book and choose Download.
This gives you a `.vcf` file.

## Convert to XML: `export`

```sh
fritzvcard export [-o file] [--name name] <file.vcf>...
```

`-o` is the output file (default `-`, standard output); `--name` is the phonebook name
(default `Nextcloud`). You can pass several `.vcf` files. A summary goes to standard error.

<details>
<summary>Example XML output</summary>

```console
$ fritzvcard export contacts.vcf
Converted 2 contacts (3 numbers).
<?xml version="1.0" encoding="utf-8"?>
<phonebooks>
  <phonebook name="Nextcloud">
    <contact>
      <category>0</category>
      <person>
        <realName>Erika Mustermann</realName>
      </person>
      <telephony>
        <number type="mobile" prio="1" id="0">+49 170 0000001</number>
        <number type="home" prio="0" id="1">030 0000002</number>
      </telephony>
      <services>
        <email classifier="private" id="0">erika@example.org</email>
      </services>
      <setup></setup>
    </contact>
    ...
  </phonebook>
</phonebooks>
```

</details>

## Upload to the FRITZ!Box: `upload`

Upload was tested with a separately created phonebook on a FRITZ!Box 6690 Cable
(FRITZ!OS 267.08.25).

### Prepare the FRITZ!Box

- Create a dedicated FRITZ!Box user for this tool. The user tested here had the rights Dial,
  App, HomeAuto, BoxAdmin and Phone; the minimal set of rights is unknown.
- Enable "Allow access for applications" (TR-064) in the home network settings. The tool reads
  the list of phonebooks over TR-064.
- Create the target phonebook in the web interface first (Telephony > Phonebook). The tool never
  creates phonebooks.

### Run it

**Warning: Upload replaces the entire target phonebook.**

```sh
export FRITZBOX_USER=phonebook
export FRITZBOX_PASSWORD=...          # optional; otherwise you are prompted
fritzvcard upload --phonebook Nextcloud contacts.vcf
```

The password is read from `FRITZBOX_PASSWORD` or from an echo-less prompt, never from a flag.
The user comes from `--user` or `FRITZBOX_USER`.

Before uploading, `fritzvcard` exports the current phonebook into a backup file
(`fritzvcard-backup-*.xml`, mode 0600) in `--backup-dir` (default: the current directory), then
asks for confirmation:

```
Phonebook "Nextcloud" (ID 1, currently 12 entries) will be replaced by 2 contacts. Continue? [y/N]
```

`--yes` skips the question. After the import the tool exports the phonebook again and checks
the contact count, contact names and phone numbers for each contact. Contact and number
order may differ; repeated entries must occur the same number of times. On success it prints `Phonebook "Nextcloud" replaced:
2 contacts.` and the backup path and exits with 0; errors exit with 1, usage errors with 2. The
path of the backup is printed to standard error as soon as it is written, before the question.

To restore the previous state, open the FRITZ!Box web interface, go to Telephony > Phonebook,
select the phonebook and use the restore/import function with the `fritzvcard-backup-*.xml`
file. Restore was tested on the separately created phonebook using a backup of test data.
The phonebook is replaced as a whole: fields that `fritzvcard` does not write are
lost in the replaced phonebook and remain only in the backup. These are quick-dial and vanity
numbers, the VIP category, ringtones, photos (`imageURL`) and `uniqueid`.

If the import request is still pending after 10 seconds, the tool prints a waiting message.
`--timeout` limits the import request to 5 minutes by default, starting after the overwrite
prompt. If it times out, the outcome is unknown. Check the phonebook before retrying;
the backup remains at the path printed before the import.

Flags: `--phonebook <name>` (required), `--host` (default `http://fritz.box`), `--user`,
`--backup-dir`, `--yes`, `--insecure` (skip TLS certificate verification, for https with a
self-signed certificate), `--timeout`, `--verbose`.

### Bug reports

`--verbose` prints a request trace to standard error at exit, starting at
`--- trace (anonymized) ---`. Review the trace before sharing it in an issue.
Do not paste all of standard error: earlier lines may contain skipped contacts' names,
the phonebook name and the backup path.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md).
