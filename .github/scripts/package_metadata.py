"""Generate package-manager definitions for a published fritzvcard release."""

import argparse
from pathlib import Path
import re

from package_release import ARCHIVES


PROJECT_URL = "https://github.com/sercxanto/fritzvcard"
PACKAGE_ID = "Sercxanto.Fritzvcard"
DESCRIPTION = "Convert vCard files to FRITZ!Box phonebook XML"
# WinGet 1.11 validates against its embedded 1.10 schema, which supports
# portable executables in ZIP archives.
MANIFEST_VERSION = "1.10.0"


def read_checksums(path: Path) -> dict[str, str]:
    checksums = {}
    for line_number, line in enumerate(path.read_text(encoding="ascii").splitlines(), 1):
        match = re.fullmatch(r"([0-9a-f]{64})  (\S+)", line)
        if not match:
            raise ValueError(f"invalid checksum line {line_number}")
        digest, name = match.groups()
        if name not in ARCHIVES:
            raise ValueError(f"unexpected archive in checksums: {name}")
        if name in checksums:
            raise ValueError(f"duplicate checksum for {name}")
        checksums[name] = digest
    missing = ARCHIVES.keys() - checksums.keys()
    if missing:
        raise ValueError(f"missing checksums for {', '.join(sorted(missing))}")
    return checksums


def homebrew_formula(version: str, base_url: str, checksums: dict[str, str]) -> str:
    platforms = []
    for os, asset_os in (("macos", "darwin"), ("linux", "linux")):
        platforms.append(f"  on_{os} do\n")
        for arch, asset_arch in (("intel", "amd64"), ("arm", "arm64")):
            name = f"fritzvcard-{asset_os}-{asset_arch}.tar.gz"
            platforms.append(
                f"    on_{arch} do\n"
                f'      url "{base_url}/{name}"\n'
                f'      sha256 "{checksums[name]}"\n'
                "    end\n"
            )
        platforms.append("  end\n\n")
    return f"""class Fritzvcard < Formula
  desc "{DESCRIPTION}"
  homepage "{PROJECT_URL}"
  version "{version}"
  license "MIT"

{"".join(platforms)}  def install
    bin.install "fritzvcard"
  end

  test do
    assert_equal "fritzvcard v{version}", shell_output("#{{bin}}/fritzvcard version").strip
  end
end
"""


def winget_manifests(version: str, url: str, digest: str) -> dict[str, str]:
    header = f"PackageIdentifier: {PACKAGE_ID}\nPackageVersion: {version}\n"
    manifests = {
        ".yaml": ("version", "DefaultLocale: en-US\n"),
        ".locale.en-US.yaml": (
            "defaultLocale",
            f"""PackageLocale: en-US
Publisher: sercxanto
PublisherUrl: https://github.com/sercxanto
PackageName: fritzvcard
PackageUrl: {PROJECT_URL}
License: MIT
LicenseUrl: {PROJECT_URL}/blob/v{version}/LICENSE
ShortDescription: {DESCRIPTION}
""",
        ),
        ".installer.yaml": (
            "installer",
            f"""InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
  - RelativeFilePath: fritzvcard.exe
    PortableCommandAlias: fritzvcard
Installers:
  - Architecture: x64
    InstallerUrl: {url}
    InstallerSha256: {digest}
""",
        ),
    }
    return {
        f"{PACKAGE_ID}{suffix}": (
            "# yaml-language-server: $schema=https://aka.ms/"
            f"winget-manifest.{kind}.{MANIFEST_VERSION}.schema.json\n\n"
            f"{header}{body}ManifestType: {kind}\nManifestVersion: {MANIFEST_VERSION}\n"
        )
        for suffix, (kind, body) in manifests.items()
    }


def generate_metadata(tag: str, checksums: Path, output: Path) -> None:
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag):
        raise ValueError("expected a stable release tag of the form vX.Y.Z")
    version = tag[1:]
    digests = read_checksums(checksums)
    base_url = f"{PROJECT_URL}/releases/download/{tag}"
    windows_asset = "fritzvcard-windows-amd64.zip"
    manifest_dir = output / "manifests/s/Sercxanto/Fritzvcard" / version
    files = {
        output / "Formula/fritzvcard.rb": homebrew_formula(version, base_url, digests),
        **{
            manifest_dir / name: content
            for name, content in winget_manifests(
                version, f"{base_url}/{windows_asset}", digests[windows_asset]
            ).items()
        },
    }
    for path, content in files.items():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8", newline="\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag")
    parser.add_argument("checksums", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    try:
        generate_metadata(args.tag, args.checksums, args.output)
    except (OSError, ValueError) as exc:
        parser.exit(1, f"{exc}\n")
