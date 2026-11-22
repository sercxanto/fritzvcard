# fritzbook

`fritzbook` converts vCard (`.vcf`) files, for example a Nextcloud address book export, into a
FRITZ!Box phonebook and can upload the result to the box. It is a single binary without
runtime dependencies.

Why it exists: Nextcloud does not need your FRITZ!Box credentials, and the FRITZ!Box does not
need to be reachable from Nextcloud. You run `fritzbook` on your own computer inside your home
network, so no credentials are stored on the box or on a server.

Tested on a FRITZ!Box 6690 Cable (FRITZ!OS 267.08.25). No guarantee for other models.

## Installation

Download the binary for your system from the
[Releases](../../releases) page. Assets are named `fritzbook-<os>-<arch>[.exe]`, for example
`fritzbook-linux-amd64`, `fritzbook-darwin-arm64` or `fritzbook-windows-amd64.exe`
(operating systems: linux, darwin = macOS, windows; architectures: amd64, arm64).

Make it executable on Linux and macOS (`chmod +x fritzbook-darwin-arm64`). macOS blocks
downloaded binaries; remove the quarantine flag once:

```sh
xattr -d com.apple.quarantine fritzbook-darwin-arm64
```

`fritzbook version` prints the version. The examples below call the binary `fritzbook`.

## Export contacts from Nextcloud

In Nextcloud open Contacts, open the menu (`...`) of the address book and choose Download.
This gives you a `.vcf` file.

## Convert to XML: `export`

```sh
fritzbook export [-o file] [--name name] <file.vcf>...
```

`-o` is the output file (default `-`, standard output); `--name` is the phonebook name
(default `Nextcloud`). You can pass several `.vcf` files. A summary goes to standard error.

```console
$ fritzbook export contacts.vcf
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

You can import the XML file yourself in the FRITZ!Box web interface (Telephony > Phonebook).

## Upload to the FRITZ!Box: `upload`

### Prepare the FRITZ!Box

- Create a dedicated FRITZ!Box user for this tool. The user tested here had the rights Dial,
  App, HomeAuto, BoxAdmin and Phone; the minimal set of rights is unknown.
- Enable "Allow access for applications" (TR-064) in the home network settings. The tool reads
  the list of phonebooks over TR-064.
- Create the target phonebook in the web interface first (Telephony > Phonebook). The tool never
  creates phonebooks.

Warning: the upload REPLACES the content of the target phonebook completely.

### Run it

```sh
export FRITZBOX_USER=phonebook
export FRITZBOX_PASSWORD=...          # optional; otherwise you are prompted
fritzbook upload --phonebook Nextcloud contacts.vcf
```

The password is read from `FRITZBOX_PASSWORD` or from an echo-less prompt, never from a flag.
The user comes from `--user` or `FRITZBOX_USER`.

Before uploading, `fritzbook` exports the current phonebook into a backup file
(`fritzbook-backup-*.xml`, mode 0600) in `--backup-dir` (default: the current directory), then
asks for confirmation:

```
Phonebook "Nextcloud" (ID 1, currently 12 entries) will be replaced by 2 contacts. Continue? [y/N]
```

`--yes` skips the question. After the import the tool exports the phonebook again and checks
the contact count. On success it prints `Phonebook "Nextcloud" replaced: 2 contacts.` and exits
with 0; errors exit with 1, usage errors with 2. If the upload fails, the path of the backup is
printed.

If the box asks for a second-factor confirmation (a code on a phone or a button on the box),
the tool waits for it up to `--timeout` (default 5 minutes). This is implemented but was NOT
observed on the tested box, so it is untested against a real box.

Flags: `--phonebook <name>` (required), `--host` (default `http://fritz.box`), `--user`,
`--backup-dir`, `--yes`, `--insecure` (skip TLS certificate verification, for https with a
self-signed certificate), `--timeout`, `--verbose`.

### Bug reports

`--verbose` prints an anonymized request trace to standard error at exit. Contacts, the
phonebook name, the password, the session ID and the user are removed. Still read the trace
before pasting it into an issue.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md).
