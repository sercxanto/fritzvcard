import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("release_notes.py")


class ReleaseNotesTests(unittest.TestCase):
    def extract(self, changelog):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "CHANGELOG.md"
            path.write_text(changelog, encoding="utf-8")
            return subprocess.run(
                [sys.executable, str(SCRIPT), "v0.1.0", str(path)],
                capture_output=True,
                text=True,
            )

    def assert_rejected(self, changelog, diagnostic):
        result = self.extract(changelog)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn(diagnostic, result.stderr)

    def test_extracts_only_the_requested_section(self):
        result = self.extract(
            "# Changelog\n\n## Unreleased\n\nFuture change\n\n"
            "## 0.1.0 - 2026-11-24\n\n### Added\n\n- Offline export.\n\n"
            "## 0.0.1 - 2026-11-20\n\nOld change\n"
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "\n### Added\n\n- Offline export.\n\n")

    def test_extracts_section_at_end_of_file(self):
        result = self.extract("## 0.1.0 - 2026-11-24\n- Final change.")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "- Final change.")

    def test_preserves_headings_inside_fenced_code(self):
        body = "````markdown\n```\n## Example heading\n```\n````\n\n- Another change.\n"
        result = self.extract(f"## 0.1.0 - 2026-11-24\n{body}## Unreleased\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, body)

    def test_rejects_a_near_match(self):
        self.assert_rejected("## 0.1.01 - 2026-11-24\n- Not this version.\n", "missing")

    def test_rejects_a_whitespace_only_section(self):
        self.assert_rejected("## 0.1.0 - 2026-11-24\n \n\t\n", "empty")

    def test_rejects_duplicate_sections_even_with_different_dates(self):
        self.assert_rejected(
            "## 0.1.0 - 2026-11-24\n- First.\n\n"
            "## 0.1.0 - 2026-11-25\n- Second.\n",
            "duplicate",
        )
