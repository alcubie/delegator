"""Release safety tests. Run with Python and PyYAML (included with MkDocs)."""

import hashlib
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = yaml.load((ROOT / ".github/workflows/release.yml").read_text(), Loader=yaml.BaseLoader)
SPEC = importlib.util.spec_from_file_location("release_latest", ROOT / "scripts/release-latest.py")
LATEST = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(LATEST)


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
        self.assertEqual(publish["needs"], ["build", "windows"])
        self.assertNotIn("continue-on-error", publish)
        windows = WORKFLOW["jobs"]["windows"]
        self.assertEqual(windows["needs"], "build")
        self.assertEqual(windows["runs-on"], "windows-latest")
        self.assertEqual(windows["strategy"]["matrix"]["shell"], ["powershell", "pwsh"])
        self.assertNotIn("if", windows)
        self.assertNotIn("continue-on-error", windows)
        download = next(step for step in windows["steps"] if "download-artifact" in step.get("uses", ""))
        self.assertEqual(download["with"], {"name": "release-assets", "path": "assets"})
        smoke = windows["steps"][-1]
        self.assertEqual(windows["defaults"]["run"]["shell"], "${{ matrix.shell }}")
        self.assertEqual(smoke["run"], "./scripts/test-install.ps1 -InstallerPath ./assets/install.ps1")
        self.assertFalse(any("continue-on-error" in step for step in windows["steps"]))
        self.assertEqual(publish["environment"], "release")
        build = WORKFLOW["jobs"]["build"]
        self.assertTrue(any("make check" in step.get("run", "") for step in build["steps"]))
        self.assertFalse(any("secrets." in str(step) for step in build["steps"]))

    def test_dry_run_retains_exact_installers(self):
        build = WORKFLOW["jobs"]["build"]
        retain = next(step for step in build["steps"] if step.get("id") == "installers")
        upload = build["steps"][-1]
        self.assertEqual(upload["with"]["name"], "release-assets")
        self.assertEqual(set(upload["with"]["path"].splitlines()), {
            "dist/*.tar.gz", "dist/*.zip", "dist/*_checksums.txt", "dist/install.sh", "dist/install.ps1"})
        self.assertNotIn("if", retain)
        self.assertNotIn("if", upload)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "dist").mkdir()
            for name in ("install.sh", "install.ps1"):
                (root / name).write_bytes((ROOT / name).read_bytes())
            output = root / "output"
            result = subprocess.run(["bash", "-e", "-c", retain["run"]], cwd=root,
                                    env=dict(os.environ, GITHUB_OUTPUT=str(output)), capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            for name, key in (("install.sh", "shell_sha256"), ("install.ps1", "powershell_sha256")):
                data = (ROOT / name).read_bytes()
                self.assertEqual((root / "dist" / name).read_bytes(), data)
                self.assertIn(f"{key}={hashlib.sha256(data).hexdigest()}", output.read_text())
                self.assertEqual(build["outputs"][key], "${{ steps.installers.outputs." + key + " }}")
                self.assertEqual(WORKFLOW["jobs"]["publish"]["steps"][-1]["env"][key.upper()],
                                 "${{ needs.build.outputs." + key + " }}")

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

    def run_publication(self, failure="", tag="v1.2.3", manual=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            assets = root / "assets"
            assets.mkdir()
            for name in ("install.sh", "install.ps1"):
                (assets / name).write_bytes((ROOT / name).read_bytes())
            checksums = []
            names = [f"delegator_{tag[1:]}_{system}_{arch}.{extension}"
                     for system, extension in (("linux", "tar.gz"), ("darwin", "tar.gz"), ("windows", "zip"))
                     for arch in ("amd64", "arm64")]
            names.append(f"delegator_{tag[1:]}_source.tar.gz")
            for index, name in enumerate(names):
                data = f"archive {index}".encode()
                (assets / name).write_bytes(data)
                checksums.append(f"{hashlib.sha256(data).hexdigest()}  {name}\n")
            (assets / f"delegator_{tag[1:]}_checksums.txt").write_text("".join(checksums))
            if failure == "local-corruption":
                (assets / names[0]).write_text("corrupt")
            if failure == "missing-installer":
                (assets / "install.sh").unlink()
            if failure == "empty-installer":
                (assets / "install.sh").write_text("")
            if failure == "missing-powershell-installer":
                (assets / "install.ps1").unlink()
            if failure == "empty-powershell-installer":
                (assets / "install.ps1").write_text("")
            if failure == "local-powershell-corruption":
                (assets / "install.ps1").write_text("Write-Host 'corrupt'\n")
            if failure == "local-installer-corruption":
                (assets / "install.sh").write_text("#!/bin/sh\nexit 1\n")
            mock = root / "gh"
            mock.write_text(r"""#!/usr/bin/env python3
import os
import json
from pathlib import Path
import shutil
import sys
root = Path(os.environ['MOCK_ROOT'])
args = sys.argv[1:]
with (root / 'calls').open('a') as out:
    out.write(' '.join(args) + '\n')
failure = os.environ['FAILURE']
if args[:2] == ['release', 'view']:
    print(json.dumps({'isDraft': failure == 'draft', 'isPrerelease': failure == 'prerelease'}))
if args[:2] == ['release', 'create'] and failure in ('upload', 'existing'):
    sys.exit(1)
if args[:2] == ['release', 'download']:
    if failure == 'download':
        sys.exit(1)
    dest = Path(args[args.index('--dir') + 1])
    dest.mkdir(exist_ok=True)
    for asset in (root / 'assets').iterdir():
        shutil.copy(asset, dest)
    if failure == 'remote-corruption':
        next(dest.glob('*_linux_amd64.tar.gz')).write_text('corrupt')
    if failure == 'missing':
        next(dest.glob('*_linux_amd64.tar.gz')).unlink()
    if failure == 'remote-installer-corruption':
        (dest / 'install.sh').write_text('#!/bin/sh\nexit 1\n')
    if failure == 'remote-missing-installer':
        (dest / 'install.sh').unlink()
    if failure == 'remote-powershell-corruption':
        (dest / 'install.ps1').write_text("Write-Host 'corrupt'\n")
    if failure == 'remote-missing-powershell-installer':
        (dest / 'install.ps1').unlink()
    if failure == 'remote-empty-powershell-installer':
        (dest / 'install.ps1').write_text('')
""")
            mock.chmod(0o755)
            env = dict(os.environ, PATH=f"{root}:{os.environ['PATH']}", MOCK_ROOT=str(root),
                       FAILURE=failure, RELEASE_TAG=tag, GH_REPO="example/delegator",
                       SHELL_SHA256=hashlib.sha256((ROOT / "install.sh").read_bytes()).hexdigest(),
                       POWERSHELL_SHA256=hashlib.sha256((ROOT / "install.ps1").read_bytes()).hexdigest())
            script = WORKFLOW["jobs"]["publish"]["steps"][-1]["run"]
            command = (["make", "-f", str(ROOT / "Makefile"), "release-latest", tag] if manual
                       else ["bash", "-c", script])
            result = subprocess.run(command, cwd=ROOT if manual else root, env=env, capture_output=True)
            calls = (root / "calls").read_text() if (root / "calls").exists() else ""
            return result, calls

    def test_complete_release_publishes_last(self):
        for tag in ("v1.2.3", "v1.2.3-rc.1", "v1.2.3+build-info"):
            result, calls = self.run_publication(tag=tag)
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            self.assertIn("--draft", calls.splitlines()[0])
            self.assertIn("./install.sh", calls.splitlines()[0])
            self.assertIn("./install.ps1", calls.splitlines()[0])
            self.assertTrue(calls.splitlines()[-1].startswith("release edit"))
            self.assertIn("--prerelease=" + str("-" in tag.split("+")[0]).lower(), calls.splitlines()[-1])
            self.assertIn("--latest=false", calls.splitlines()[-1])

    def test_manual_latest(self):
        result, calls = self.run_publication(manual=True)
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        self.assertIn("--latest=true", calls.splitlines()[-1])
        self.assertEqual(sum(line.startswith("release download ") for line in calls.splitlines()), 1)
        self.assertNotIn("--draft=false", calls)
        self.assertFalse(any(line.startswith("api ") for line in calls.splitlines()))
        for failure in ("draft", "prerelease", "download", "remote-corruption", "missing",
                        "remote-missing-installer", "remote-empty-powershell-installer"):
            result, calls = self.run_publication(failure, manual=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("release edit", calls)
        result, calls = self.run_publication(tag="v1.2.3-rc.1", manual=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("release edit", calls)

    def test_manual_checksums(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tag = "v1.2.3"
            prefix = "delegator_1.2.3"
            names = [f"{prefix}_{system}_{arch}.{extension}"
                     for system, extension in (("linux", "tar.gz"), ("darwin", "tar.gz"), ("windows", "zip"))
                     for arch in ("amd64", "arm64")] + [f"{prefix}_source.tar.gz"]
            for name in names + ["install.sh", "install.ps1"]:
                (root / name).write_bytes(b"asset")
            lines = [f"{hashlib.sha256(b'asset').hexdigest()}  {name}\n" for name in names]
            checksum = root / f"{prefix}_checksums.txt"
            checksum.write_text("".join(lines))
            LATEST.verify_assets(root, tag)
            for content in ("".join(lines[:-1]), "".join(lines + [lines[0]]),
                            "".join(lines).replace(names[0], "../escape"), "invalid\n"):
                checksum.write_text(content)
                with self.assertRaises(ValueError):
                    LATEST.verify_assets(root, tag)

    def test_failures_never_publish(self):
        for failure in ("local-corruption", "upload", "existing", "download", "remote-corruption", "missing",
                        "missing-installer", "empty-installer", "local-installer-corruption",
                        "remote-installer-corruption", "remote-missing-installer",
                        "missing-powershell-installer", "empty-powershell-installer", "local-powershell-corruption",
                        "remote-powershell-corruption", "remote-missing-powershell-installer",
                        "remote-empty-powershell-installer"):
            with self.subTest(failure=failure):
                result, calls = self.run_publication(failure)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("release edit", calls)


if __name__ == "__main__":
    unittest.main()
