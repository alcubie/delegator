"""Explicitly promote a verified published release to Latest."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


def gh(*args):
    return subprocess.check_output(["gh", *args], text=True)


def verify_assets(directory, tag):
    prefix = f"delegator_{tag[1:]}"
    archives = {f"{prefix}_{system}_{arch}.{extension}"
                for system, extension in (("linux", "tar.gz"), ("darwin", "tar.gz"), ("windows", "zip"))
                for arch in ("amd64", "arm64")}
    archives.add(f"{prefix}_source.tar.gz")
    checksum = f"{prefix}_checksums.txt"
    if {path.name for path in directory.iterdir()} != archives | {checksum, "install.sh", "install.ps1"}:
        raise ValueError("expected seven archives, checksums, and both installers")
    seen = set()
    for line in (directory / checksum).read_text().splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})\s+\*?([^/\\]+)", line)
        if match is None:
            raise ValueError("invalid checksum entry")
        digest, name = match.groups()
        if name not in archives or name in seen:
            raise ValueError(f"unexpected or duplicate archive: {name}")
        if hashlib.sha256((directory / name).read_bytes()).hexdigest() != digest:
            raise ValueError(f"checksum mismatch: {name}")
        seen.add(name)
    if seen != archives:
        raise ValueError("missing archive checksums")
    for name in ("install.sh", "install.ps1"):
        if not (directory / name).stat().st_size:
            raise ValueError(f"empty installer: {name}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag")
    parser.add_argument("--repo", default=os.environ.get("GH_REPO", "alcubie/delegator"))
    args = parser.parse_args()
    subprocess.run([sys.executable, Path(__file__).with_name("check-release-tag.py"), args.tag], check=True)
    if "-" in args.tag.split("+")[0]:
        raise ValueError("only a stable version tag can be promoted")
    release = json.loads(gh("release", "view", args.tag, "--repo", args.repo,
                            "--json", "isDraft,isPrerelease"))
    if release["isDraft"] or release["isPrerelease"]:
        raise ValueError("only a published stable release can be promoted")
    with tempfile.TemporaryDirectory(prefix="delegator-latest-") as directory:
        gh("release", "download", args.tag, "--repo", args.repo, "--dir", directory)
        verify_assets(Path(directory), args.tag)
    gh("release", "edit", args.tag, "--repo", args.repo, "--verify-tag", "--latest=true")
    print(f"Latest is now {args.tag}")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        sys.exit(str(error))
