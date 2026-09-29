"""Release safety tests. Run with Python and PyYAML (included with MkDocs)."""

import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = yaml.load((ROOT / ".github/workflows/release.yml").read_text(), Loader=yaml.BaseLoader)


class ReleaseTests(unittest.TestCase):
    def test_tags(self):
        for tag in ("v1.2.3", "v0.0.0-rc.1", "v1.2.3+build.42", "v1.2.3-alpha-beta"):
            self.assertEqual(self.check_tag(tag), 0, tag)
        for tag in ("1.2.3", "v1.2", "v01.2.3", "v1.2.3-01", "v1.2.3-", "v1.2.3/evil", "v1.2.3\n"):
            self.assertNotEqual(self.check_tag(tag), 0, tag)

    def check_tag(self, tag):
        return subprocess.run([sys.executable, ROOT / "scripts/check-release-tag.py", tag],
                              capture_output=True).returncode

    def test_publication_boundary(self):
        self.assertEqual(set(WORKFLOW["on"]), {"push", "workflow_dispatch"})
        self.assertEqual(WORKFLOW["permissions"], {"contents": "read"})
        publish = WORKFLOW["jobs"]["publish"]
        self.assertEqual(publish["if"], "github.event_name == 'push'")
        self.assertEqual(publish["needs"], "build")
        self.assertEqual(publish["environment"], "release")
        build = WORKFLOW["jobs"]["build"]
        self.assertTrue(any("make check" in step.get("run", "") for step in build["steps"]))
        self.assertFalse(any("secrets." in str(step) for step in build["steps"]))

    def test_documentation_failures_block_release(self):
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, "dg.md").write_text("stale reference\n")
            stale = subprocess.run(
                ["make", "docs-check", f"DOCS_REFERENCE={directory}", "MKDOCS=true"],
                cwd=ROOT, capture_output=True)
            self.assertNotEqual(stale.returncode, 0)
        site = subprocess.run(["make", "docs-check", "MKDOCS=false"],
                              cwd=ROOT, capture_output=True)
        self.assertNotEqual(site.returncode, 0)

    def run_publication(self, failure="", tag="v1.2.3"):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            assets = root / "assets"
            assets.mkdir()
            (assets / "install.sh").write_bytes((ROOT / "install.sh").read_bytes())
            checksums = []
            for index in range(7):
                name = f"archive-{index}.tar.gz"
                data = f"archive {index}".encode()
                (assets / name).write_bytes(data)
                checksums.append(f"{hashlib.sha256(data).hexdigest()}  {name}\n")
            (assets / f"delegator_{tag[1:]}_checksums.txt").write_text("".join(checksums))
            if failure == "local-corruption":
                (assets / "archive-0.tar.gz").write_text("corrupt")
            if failure == "missing-installer":
                (assets / "install.sh").unlink()
            if failure == "empty-installer":
                (assets / "install.sh").write_text("")
            mock = root / "gh"
            mock.write_text(r"""#!/usr/bin/env python3
import os
from pathlib import Path
import shutil
import sys
root = Path(os.environ['MOCK_ROOT'])
args = sys.argv[1:]
with (root / 'calls').open('a') as out:
    out.write(' '.join(args) + '\n')
failure = os.environ['FAILURE']
if args[:2] == ['release', 'create'] and failure in ('upload', 'existing'):
    sys.exit(1)
if args[:2] == ['release', 'download']:
    if failure == 'download':
        sys.exit(1)
    dest = Path(args[args.index('--dir') + 1])
    for asset in (root / 'assets').iterdir():
        shutil.copy(asset, dest)
    if failure == 'remote-corruption':
        (dest / 'archive-0.tar.gz').write_text('corrupt')
    if failure == 'missing':
        (dest / 'archive-0.tar.gz').unlink()
    if failure == 'remote-installer-corruption':
        (dest / 'install.sh').write_text('#!/bin/sh\nexit 1\n')
    if failure == 'remote-missing-installer':
        (dest / 'install.sh').unlink()
""")
            mock.chmod(0o755)
            env = dict(os.environ, PATH=f"{root}:{os.environ['PATH']}", MOCK_ROOT=str(root),
                       FAILURE=failure, RELEASE_TAG=tag, GH_REPO="example/delegator")
            script = WORKFLOW["jobs"]["publish"]["steps"][-1]["run"]
            result = subprocess.run(["bash", "-c", script], cwd=root, env=env, capture_output=True)
            calls = (root / "calls").read_text() if (root / "calls").exists() else ""
            return result, calls

    def test_complete_release_publishes_last(self):
        for tag in ("v1.2.3", "v1.2.3-rc.1", "v1.2.3+build-info"):
            result, calls = self.run_publication(tag=tag)
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            self.assertIn("--draft", calls.splitlines()[0])
            self.assertIn("./install.sh", calls.splitlines()[0])
            self.assertTrue(calls.splitlines()[-1].startswith("release edit"))
            self.assertIn("--prerelease=" + str("-" in tag.split("+")[0]).lower(), calls.splitlines()[-1])

    def test_failures_never_publish(self):
        for failure in ("local-corruption", "upload", "existing", "download", "remote-corruption", "missing",
                        "missing-installer", "empty-installer", "remote-installer-corruption", "remote-missing-installer"):
            with self.subTest(failure=failure):
                result, calls = self.run_publication(failure)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("release edit", calls)


if __name__ == "__main__":
    unittest.main()
