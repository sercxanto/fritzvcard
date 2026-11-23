# fritzvcard

`fritzvcard` converts vCard (`.vcf`) files, for example a Nextcloud address book export, into a
FRITZ!Box phonebook and can upload the result to the box. It is a single binary without
runtime dependencies.

Why it exists: Nextcloud does not need your FRITZ!Box credentials, and the FRITZ!Box does not
need to be reachable from Nextcloud. You run `fritzvcard` on your own computer inside your home
network, so no credentials are stored on the box or on a server.

Upload was tested with a separately created phonebook on a FRITZ!Box 6690 Cable
(FRITZ!OS 267.08.25).

## Installation

Download the binary for your system from the
[Releases](../../releases) page. Assets are named `fritzvcard-<os>-<arch>[.exe]`, for example
`fritzvcard-linux-amd64`, `fritzvcard-darwin-arm64` or `fritzvcard-windows-amd64.exe`
(operating systems: linux, darwin = macOS, windows; architectures: amd64, arm64).

Make it executable on Linux and macOS (`chmod +x fritzvcard-darwin-arm64`). macOS blocks
downloaded binaries; remove the quarantine flag once:

```sh
xattr -d com.apple.quarantine fritzvcard-darwin-arm64
```

`fritzvcard version` prints the version. The examples below call the binary `fritzvcard`.
Run `fritzvcard --help` to list the commands. Use `fritzvcard upload --help` or
`fritzvcard --help upload` to see the upload options and their defaults.

## Export contacts from Nextcloud

In Nextcloud open Contacts, open the menu (`...`) of the address book and choose Download.
This gives you a `.vcf` file.

## Convert to XML: `export`

```sh
fritzvcard export [-o file] [--name name] <file.vcf>...
```

`-o` is the output file (default `-`, standard output); `--name` is the phonebook name
(default `Nextcloud`). You can pass several `.vcf` files. A summary goes to standard error.

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
