#!/usr/bin/env python3
"""Validate a release archive and run its executable on the matching runner."""

import argparse
import hashlib
import json
import subprocess
import tarfile
import tempfile
import zipfile
from pathlib import Path


def smoke(directory, version, system, arch):
    name = "whwmpc.exe" if system == "windows" else "whwmpc"
    suffix = "zip" if system == "windows" else "tar.gz"
    package = directory / f"whwmpc_{version}_{system}_{arch}.{suffix}"
    matches = [
        fields[0]
        for line in (directory / "SHA256SUMS").read_text().splitlines()
        if len(fields := line.split()) == 2 and fields[1] == package.name
    ]
    with package.open("rb") as archive:
        actual = hashlib.file_digest(archive, "sha256").hexdigest()
    if matches != [actual]:
        raise ValueError(f"checksum mismatch or ambiguous manifest for {package.name}")

    with tempfile.TemporaryDirectory(prefix="whwmpc-smoke-") as tmp:
        executable = Path(tmp) / name
        if suffix == "zip":
            with zipfile.ZipFile(package) as archive:
                executable.write_bytes(archive.read(name))
        else:
            with tarfile.open(package) as archive:
                binary = archive.extractfile("./" + name)
                if binary is None:
                    raise ValueError("executable missing from archive")
                with binary:
                    executable.write_bytes(binary.read())
        executable.chmod(0o700)
        value = subprocess.check_output([str(executable), "--version"], text=True, timeout=15).strip()
        if value != f"whwmpc {version}":
            raise ValueError(f"unexpected version {value!r}")
        output = subprocess.check_output([str(executable), "--demo", "check", "--json"], timeout=15)
        if json.loads(output).get("source") != "demo":
            raise ValueError("fixture report missing")
    print(f"{package.name}: checksum and runtime passed")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("version")
    parser.add_argument("system", choices=("linux", "darwin", "windows"))
    parser.add_argument("arch", choices=("amd64", "arm64"))
    args = parser.parse_args()
    smoke(args.directory, args.version, args.system, args.arch)
