"""Extract the release notes for a tag from its changelog."""

import argparse
import re
import sys
from pathlib import Path


def extract_notes(changelog, tag):
    heading = re.compile(
        rf"##[ \t]+{re.escape(tag[1:])} - [0-9]{{4}}-[0-9]{{2}}-[0-9]{{2}}[ \t]*"
    )
    sections = []
    current = None
    fence = None

    for line in changelog.splitlines(keepends=True):
        if fence is not None:
            if re.fullmatch(rf" {{0,3}}{fence[0]}{{{len(fence)},}}[ \t]*\n?", line):
                fence = None
        else:
            opening = re.match(r" {0,3}(`{3,})[^`\n]*$| {0,3}(~{3,})", line)
            if opening:
                fence = opening[1] or opening[2]
            elif re.match(r"^##(?:[ \t]|$)", line):
                current = [] if heading.fullmatch(line.rstrip("\n")) else None
                if current is not None:
                    sections.append(current)
                continue
        if current is not None:
            current.append(line)

    if not sections:
        raise ValueError(f"missing changelog section for {tag}")
    if len(sections) != 1:
        raise ValueError(f"duplicate changelog sections for {tag}")
    notes = "".join(sections[0])
    if not notes.strip():
        raise ValueError(f"empty changelog section for {tag}")
    return notes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag")
    parser.add_argument("changelog", type=Path)
    args = parser.parse_args()
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", args.tag):
        parser.error("expected a stable release tag of the form vX.Y.Z")
    try:
        sys.stdout.write(extract_notes(args.changelog.read_text(encoding="utf-8"), args.tag))
    except (OSError, ValueError) as error:
        parser.exit(1, f"{parser.prog}: {error}\n")


if __name__ == "__main__":
    main()
