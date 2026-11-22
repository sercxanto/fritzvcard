# First public release: behavior and scope

The five changes below define readiness for the first public `fritzbook` release. `upload` still requires a uniquely named, explicitly selected phonebook and a backup of that book before import. The observed device behavior is replacement of a separate local book on a FRITZ!Box 6690 Cable. ID 0 has no special meaning to the CLI.

## Decisions

- Report a verified replacement only if contact counts, names and the phone numbers **belonging to each contact** match the re-export. Preserve duplicates, ignore ordering, and report discrepancies without printing contact data.
- In a verbose trace, omit private request and response bodies even on transport failure. Explain that the rest of stderr can contain names, paths and other identifying information.
- Release only after lint and tests pass on the tagged commit. Validate the version tag and pass it safely into the cross-build.
- For `export -o`, write a private temporary file beside the destination and replace the destination only after the write completes; request mode `0600` on Unix. Keep stdout streaming behavior and the distinct exclusive-backup policy.
- Show short options with one dash and long options with two. Keep previously accepted spellings. Document `fritzbook <command> --help` with an example; also support `fritzbook --help <command>`. Explicit help succeeds on stdout; invalid usage remains an error on stderr.

## Approaches

The agreed approach addresses verified risks in the shipped CLI rather than clearing the entire backlog. Count/name checks alone miss number-only updates; byte-for-byte XML equality would instead reject harmless device metadata or ordering differences. Documenting only one help order would leave the other silently showing the wrong page; accepting both lets the documentation recommend one form without penalizing users of the other.

Keep the import timeout and a neutral waiting message, without inferring a second-factor challenge. Record the untested ID 0 backup export precisely rather than introducing a new phonebook role. No new device probe, tag, or publication is needed to implement or test these changes.
