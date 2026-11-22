import json
import os
from pathlib import Path
import plistlib
import re
import subprocess
import sys
import tempfile


HDIUTIL = "/usr/bin/hdiutil"
DISKUTIL = "/usr/sbin/diskutil"


def run(*args):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, timeout=150).stdout


def plist(*args):
    return plistlib.loads(run(*args))


def only(records, failure):
    if len(records) != 1:
        raise RuntimeError(failure)
    return records[0]


def disk_name(device):
    match = re.fullmatch(r"(disk[0-9]+)(?:s[0-9]+)*", device)
    if match is None:
        raise RuntimeError("Invalid disk identifier from macOS")
    return match[1]


def digest(image):
    return run("/usr/bin/shasum", "-a", "256", image).split()[0]


def main(checker):
    if sys.platform != "darwin" or os.getenv("GITHUB_ACTIONS") != "true" or os.getenv("RUNNER_ENVIRONMENT") != "github-hosted":
        raise RuntimeError("APFS smoke requires a GitHub-hosted macOS runner")

    boot = plist(DISKUTIL, "info", "-plist", "/")["APFSContainerReference"]
    boot_container = only(
        [c for c in plist(DISKUTIL, "apfs", "list", "-plist")["Containers"] if c["ContainerReference"] == boot],
        "Cannot identify the startup APFS container",
    )
    startup_disks = {disk_name(store["DeviceIdentifier"]) for store in boot_container["PhysicalStores"]}
    if not startup_disks:
        raise RuntimeError("Cannot identify the startup APFS physical stores")
    startup_disks.add(disk_name(boot))

    with tempfile.TemporaryDirectory(prefix="whwmpc-apfs-", dir=os.environ["RUNNER_TEMP"]) as directory:
        image = Path(directory, "fixture.dmg").resolve()
        label = "WHWMPC-APFS-" + image.parent.name.rsplit("-", 1)[1]
        run(HDIUTIL, "create", "-size", "128m", "-layout", "GPTSPUD", "-fs", "APFS", "-volname", label, image)
        before = digest(image)
        run(HDIUTIL, "attach", "-readonly", "-nomount", image)
        attached = only(
            [item for item in plist(HDIUTIL, "info", "-plist")["images"] if item.get("image-path") and Path(item["image-path"]).resolve() == image],
            "Cannot associate the attached disk with the temporary image",
        )
        entries = attached["system-entities"]
        whole = only(
            [entry["dev-entry"] for entry in entries if entry.get("content-hint") == "GUID_partition_scheme"],
            "The temporary image does not have one GPT disk",
        )
        if not re.fullmatch(r"/dev/disk[0-9]+", whole) or whole[5:] in startup_disks:
            raise RuntimeError("The image attachment is not a separate disk")

        try:
            stores = {entry["dev-entry"][5:] for entry in entries if re.fullmatch(whole + r"s[0-9]+", entry.get("dev-entry", ""))}
            container = only(
                [c for c in plist(DISKUTIL, "apfs", "list", "-plist")["Containers"]
                 if len(c["PhysicalStores"]) == 1 and c["PhysicalStores"][0]["DeviceIdentifier"] in stores],
                "Cannot map the image partition to one APFS container",
            )
            volume = only(container["Volumes"], "The temporary container does not have exactly one APFS volume")
            device = "/dev/" + volume["DeviceIdentifier"]
            if (container["ContainerReference"] == boot or disk_name(volume["DeviceIdentifier"]) in startup_disks
                    or not re.fullmatch(r"/dev/disk[0-9]+s[0-9]+", device) or volume["Name"] != label or volume["Locked"]):
                raise RuntimeError("Unexpected APFS fixture volume identity")

            inventory = json.loads(run(checker, "filesystem", "--json"))
            found = only([v for v in inventory["volumes"] if v["name"] == device], "The checker cannot identify the APFS fixture")
            if found["fstype"] != "apfs" or found["uuid"] != volume["APFSVolumeUUID"] or found.get("mountpoints"):
                raise RuntimeError("The APFS checker target is mounted or does not match the temporary volume UUID")

            result = json.loads(run("/usr/bin/sudo", "-n", checker, "filesystem", "--check", device, "--json"))
            if (result.get("device"), result.get("availability"), result.get("state"), result.get("exit_code")) != (device, "available", "clean", 0):
                raise RuntimeError(f"Unexpected APFS fixture check result: {result}")
        finally:
            run(HDIUTIL, "detach", whole)

        if digest(image) != before:
            raise RuntimeError("APFS fixture image changed during the read-only check")
        print(f"APFS fixture {label} check finished with state clean and unchanged image checksum")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit(f"Usage: {sys.argv[0]} WHWMPC_BINARY")
    main(Path(sys.argv[1]).resolve(strict=True))
