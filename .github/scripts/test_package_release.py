import hashlib
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
import zipfile


SCRIPT = Path(__file__).with_name("package_release.py")
TARGETS = (
    ("fritzvcard-darwin-amd64", "fritzvcard-darwin-amd64.tar.gz"),
    ("fritzvcard-darwin-arm64", "fritzvcard-darwin-arm64.tar.gz"),
    ("fritzvcard-linux-amd64", "fritzvcard-linux-amd64.tar.gz"),
    ("fritzvcard-linux-arm64", "fritzvcard-linux-arm64.tar.gz"),
    ("fritzvcard-windows-amd64.exe", "fritzvcard-windows-amd64.zip"),
)


class PackageReleaseTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.dist = Path(temp.name)
        for binary, _ in TARGETS:
            path = self.dist / binary
            path.write_bytes(binary.encode())
            path.chmod(0o644)
        (self.dist / "fritzprobe").write_bytes(b"do not ship")

    def run_packager(self):
        return subprocess.run(
            [sys.executable, "-B", str(SCRIPT), str(self.dist)],
            capture_output=True,
            text=True,
        )

    def test_packages_all_targets(self):
        result = self.run_packager()
        self.assertEqual(result.returncode, 0, result.stderr)
        sums = []
        for binary, asset in TARGETS:
            with self.subTest(asset=asset):
                path = self.dist / asset
                if asset.endswith(".zip"):
                    with zipfile.ZipFile(path) as archive:
                        self.assertEqual(archive.namelist(), ["fritzvcard.exe"])
                        self.assertEqual(archive.read("fritzvcard.exe"), binary.encode())
                else:
                    with tarfile.open(path, "r:gz") as archive:
                        self.assertEqual(archive.getnames(), ["fritzvcard"])
                        member = archive.getmember("fritzvcard")
                        self.assertTrue(member.isfile())
                        self.assertEqual(member.mode, 0o755)
                        with archive.extractfile(member) as executable:
                            self.assertEqual(executable.read(), binary.encode())
                sums.append(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {asset}")
        self.assertEqual((self.dist / "SHA256SUMS").read_text().splitlines(), sums)
        self.assertEqual(
            {p.name for p in self.dist.iterdir()},
            {name for target in TARGETS for name in target} | {"fritzprobe", "SHA256SUMS"},
        )

    def test_missing_binary_fails_before_packaging(self):
        missing = "fritzvcard-windows-amd64.exe"
        (self.dist / missing).unlink()
        result = self.run_packager()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"missing build output: {missing}", result.stderr)
        self.assertEqual(
            {p.name for p in self.dist.iterdir()},
            {binary for binary, _ in TARGETS if binary != missing} | {"fritzprobe"},
        )
