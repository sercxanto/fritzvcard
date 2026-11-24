"""Extract the release notes for a tag from its changelog."""

import argparse
import re
import sys
from pathlib import Path


INLINE_MARKUP = re.compile(r"(?<!`)(`+)(?!`).*?(?<!`)\1(?!`)|<!--")
LEVEL_TWO = re.compile(r"^ {0,3}##(?:[ \t]|$)")
NON_PARAGRAPH = re.compile(
    r"^ {0,3}(?:#{1,6}(?:[ \t]|$)|>|(?:[-+*]|[0-9]{1,9}[.)])(?:[ \t]|$))"
)


def without_comments(line, in_comment):
    """Keep visible Markdown for heading and emptiness checks."""
    visible = []
    while line:
        if in_comment:
            _, end, line = line.partition("-->")
            if not end:
                break
            in_comment = False
            continue
        markup = INLINE_MARKUP.search(line)
        if not markup:
            visible.append(line)
            break
        if markup[1]:
            visible.append(line[:markup.end()])
        else:
            visible.append(line[:markup.start()])
            in_comment = True
        line = line[markup.end():]
    return "".join(visible), in_comment


def extract_notes(changelog, tag):
    heading = re.compile(
        rf" {{0,3}}##[ \t]+{re.escape(tag[1:])} - [0-9]{{4}}-[0-9]{{2}}-[0-9]{{2}}[ \t]*"
    )
    sections = []
    current = None
    fence = None
    in_comment = False
    paragraph_start = None

    for line in changelog.splitlines(keepends=True):
        if fence is not None:
            if re.fullmatch(rf" {{0,3}}{fence[0]}{{{len(fence)},}}[ \t]*\n?", line):
                fence = None
            visible = line
        else:
            opening = re.match(r" {0,3}(`{3,})[^`\n]*$| {0,3}(~{3,})", line)
            if opening and not in_comment:
                fence = opening[1] or opening[2]
                paragraph_start = None
                visible = line
            else:
                visible, in_comment = without_comments(line, in_comment)
                if LEVEL_TWO.match(line) and LEVEL_TWO.match(visible):
                    current = [] if heading.fullmatch(visible.rstrip("\n")) else None
                    paragraph_start = None
                    if current is not None:
                        sections.append(current)
                    continue
                if current is not None and paragraph_start is not None and re.fullmatch(
                    r" {0,3}-+[ \t]*\n?", visible
                ):
                    # A Setext title is the whole paragraph before its underline.
                    del current[paragraph_start:]
                    current = None
                    paragraph_start = None
                    continue
                if not visible.strip() or NON_PARAGRAPH.match(visible) or visible.startswith(
                    ("    ", "\t")
                ):
                    paragraph_start = None
                elif current is not None and paragraph_start is None:
                    paragraph_start = len(current)
        if current is not None:
            current.append((line, visible))

    if not sections:
        raise ValueError(f"missing changelog section for {tag}")
    if len(sections) != 1:
        raise ValueError(f"duplicate changelog sections for {tag}")
    if not any(visible.strip() for _, visible in sections[0]):
        raise ValueError(f"empty changelog section for {tag}")
    return "".join(line for line, _ in sections[0])


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
