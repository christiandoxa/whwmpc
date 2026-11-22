#!/usr/bin/env python3
"""Exercise a read-only helper with the hosted runner's authorization policy."""

import ctypes
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def main():
    source = Path(sys.argv[1]).resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix="whwmpc-read-") as directory:
        binary = Path(directory) / source.name
        shutil.copy2(source, binary)
        result = subprocess.run(
            [str(binary), "--privileged-read", "trim", "--json"],
            stdin=subprocess.DEVNULL, capture_output=True, text=True, encoding="utf-8", timeout=55,
        )
        if os.name == "nt" and not ctypes.windll.shell32.IsUserAnAdmin():
            if result.returncode != 1 or "UAC requires an interactive Windows session" not in result.stderr:
                raise RuntimeError("a Windows CLI without authorization did not fail promptly")
            print("Windows non-interactive CLI reported missing authorization without opening UAC.")
            return
        result.check_returncode()
        audit = json.loads(result.stdout)
        if audit["demo"] or not audit["trim"]["availability"] or not audit["trim"]["captured_at"]:
            raise RuntimeError("native helper did not return a typed TRIM audit")
        print("Native helper authorized, returned a read-only TRIM audit, and exited.")


if __name__ == "__main__":
    main()
