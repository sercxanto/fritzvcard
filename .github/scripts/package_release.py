"""Package the binaries produced by task build:all."""

import argparse
import hashlib
from pathlib import Path
import tarfile
import zipfile


ARCHIVES = {
    "fritzvcard-darwin-amd64.tar.gz": "fritzvcard-darwin-amd64",
    "fritzvcard-darwin-arm64.tar.gz": "fritzvcard-darwin-arm64",
    "fritzvcard-linux-amd64.tar.gz": "fritzvcard-linux-amd64",
    "fritzvcard-linux-arm64.tar.gz": "fritzvcard-linux-arm64",
    "fritzvcard-windows-amd64.zip": "fritzvcard-windows-amd64.exe",
}


def package_release(dist: Path) -> None:
    for binary in ARCHIVES.values():
        if not (dist / binary).is_file():
            raise FileNotFoundError(f"missing build output: {binary}")

    sums = []
    for name, binary in sorted(ARCHIVES.items()):
        source = dist / binary
        asset = dist / name
        if name.endswith(".zip"):
            with zipfile.ZipFile(asset, "w", compression=zipfile.ZIP_DEFLATED) as archive:
                archive.write(source, "fritzvcard.exe")
        else:
            with tarfile.open(asset, "w:gz") as archive, source.open("rb") as executable:
                member = tarfile.TarInfo("fritzvcard")
                member.mode = 0o755
                member.size = source.stat().st_size
                archive.addfile(member, executable)
        with asset.open("rb") as contents:
            digest = hashlib.file_digest(contents, "sha256").hexdigest()
        sums.append(f"{digest}  {name}\n")
    (dist / "SHA256SUMS").write_text("".join(sums), encoding="ascii", newline="\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("dist", type=Path)
    args = parser.parse_args()
    try:
        package_release(args.dist)
    except OSError as exc:
        parser.exit(1, f"{exc}\n")
