import hashlib
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("package_metadata.py")
BASE_URL = "https://github.com/sercxanto/fritzvcard/releases/download/v1.2.3/"
UNIX_ASSETS = (
    ("macos", "intel", "fritzvcard-darwin-amd64.tar.gz"),
    ("macos", "arm", "fritzvcard-darwin-arm64.tar.gz"),
    ("linux", "intel", "fritzvcard-linux-amd64.tar.gz"),
    ("linux", "arm", "fritzvcard-linux-arm64.tar.gz"),
)
WINDOWS_ASSET = "fritzvcard-windows-amd64.zip"


class PackageMetadataTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.sums = Path(temp.name) / "SHA256SUMS"
        self.output = Path(temp.name) / "metadata"
        self.hashes = {
            asset: hashlib.sha256(asset.encode()).hexdigest()
            for asset in (*(name for _, _, name in UNIX_ASSETS), WINDOWS_ASSET)
        }
        self.lines = [f"{digest}  {asset}" for asset, digest in self.hashes.items()]

    def generate(self, lines, tag="v1.2.3"):
        self.sums.write_text("\n".join(lines) + "\n", encoding="ascii")
        return subprocess.run(
            [sys.executable, "-B", str(SCRIPT), tag, str(self.sums), str(self.output)],
            capture_output=True,
            text=True,
        )

    def test_homebrew_formula_maps_platforms(self):
        result = self.generate(self.lines)
        self.assertEqual(result.returncode, 0, result.stderr)
        formula = (self.output / "Formula/fritzvcard.rb").read_text()
        for os, arch, asset in UNIX_ASSETS:
            with self.subTest(os=os, arch=arch):
                platform = formula.split(f"  on_{os} do\n", 1)[1].split("\n  end", 1)[0]
                target = platform.split(f"    on_{arch} do\n", 1)[1].split("\n    end", 1)[0]
                self.assertIn(f'url "{BASE_URL}{asset}"', target)
                self.assertIn(f'sha256 "{self.hashes[asset]}"', target)
        self.assertIn('version "1.2.3"', formula)
        self.assertIn('bin.install "fritzvcard"', formula)
        self.assertIn("fritzvcard v1.2.3", formula)

    def test_winget_manifest_is_portable_zip(self):
        result = self.generate(self.lines)
        self.assertEqual(result.returncode, 0, result.stderr)
        directory = self.output / "manifests/s/Sercxanto/Fritzvcard/1.2.3"
        for suffix, schema in (
            (".yaml", "version"),
            (".locale.en-US.yaml", "defaultLocale"),
            (".installer.yaml", "installer"),
        ):
            content = (directory / f"Sercxanto.Fritzvcard{suffix}").read_text()
            self.assertTrue(content.startswith(
                f"# yaml-language-server: $schema=https://aka.ms/winget-manifest.{schema}.1.10.0.schema.json\n"
            ))
            self.assertIn("PackageIdentifier: Sercxanto.Fritzvcard\n", content)
            self.assertIn("PackageVersion: 1.2.3\n", content)
            self.assertIn("ManifestVersion: 1.10.0\n", content)
        installer = (directory / "Sercxanto.Fritzvcard.installer.yaml").read_text()
        for field in (
            f"InstallerUrl: {BASE_URL}{WINDOWS_ASSET}",
            f"InstallerSha256: {self.hashes[WINDOWS_ASSET]}",
            "Architecture: x64",
            "InstallerType: zip",
            "NestedInstallerType: portable",
            "RelativeFilePath: fritzvcard.exe",
            "PortableCommandAlias: fritzvcard",
        ):
            self.assertIn(f"{field}\n", installer)

    def test_rejects_invalid_release_inputs(self):
        for lines, tag, error in (
            (self.lines + self.lines[:1], "v1.2.3", "duplicate"),
            (self.lines[:-1], "v1.2.3", "missing"),
            (["x" + self.lines[0][1:], *self.lines[1:]], "v1.2.3", "invalid checksum"),
            (self.lines, "v1.2.3-rc1", "stable release tag"),
        ):
            with self.subTest(error=error):
                result = self.generate(lines, tag)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)
                self.assertFalse(self.output.exists())
