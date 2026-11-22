import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import struct
import subprocess
import sys
import tempfile
import uuid


SUDO = ("/usr/bin/sudo", "-n")
LOSETUP = "/usr/sbin/losetup"
PART_START = 2048
PART_SECTORS = 24 * 2048
SECTOR_SIZE = 512


def run(*args, exit_code=0, env=None):
    result = subprocess.run(args, capture_output=True, text=True, timeout=90, env=env)
    if result.returncode != exit_code:
        raise RuntimeError(f"{args[0]} exited {result.returncode}, expected {exit_code}\n{result.stdout}\n{result.stderr}")
    return result.stdout


def digest(image):
    with image.open("rb") as contents:
        return hashlib.file_digest(contents, "sha256").digest()


def make_image(image, filesystem_uuid):
    volume = image.with_name("volume.img")
    with volume.open("xb") as file:
        file.truncate(PART_SECTORS * SECTOR_SIZE)
    run("/usr/sbin/mke2fs", "-q", "-F", "-t", "ext4", "-U", filesystem_uuid, volume)
    run("/usr/sbin/debugfs", "-w", "-R", "set_inode_field <2> links_count 8", volume)
    run("/usr/sbin/debugfs", "-w", "-R", "set_super_value state 2", volume)

    # One Linux primary partition in an MBR. All fixture writes above target a regular file.
    mbr = bytearray(SECTOR_SIZE)
    struct.pack_into("<B3sB3sII", mbr, 446, 0, b"\x00\x02\x00", 0x83, b"\xff\xff\xff", PART_START, PART_SECTORS)
    mbr[510:512] = b"\x55\xaa"
    with image.open("xb") as disk, volume.open("rb") as data:
        disk.write(mbr)
        disk.seek(PART_START * SECTOR_SIZE)
        shutil.copyfileobj(data, disk)


def owns_loop(loop, image):
    if re.fullmatch(r"/dev/loop[0-9]+", str(loop)) is None:
        return False
    attributes = Path("/sys/devices/virtual/block", loop.name, "loop")
    try:
        return (attributes / "backing_file").read_text().strip() == str(image)
    except FileNotFoundError:
        return False


def fixture_partition(loop, image):
    if not owns_loop(loop, image):
        raise RuntimeError("Loop device is not backed by the temporary image")
    sys_disk = Path("/sys/devices/virtual/block", loop.name)
    partition = loop.with_name(loop.name + "p1")
    sys_part = sys_disk / partition.name
    partitions = [node for node in sys_disk.iterdir() if (node / "partition").is_file()]
    if (partitions != [sys_part] or (sys_part / "partition").read_text().strip() != "1"
            or (sys_part / "start").read_text().strip() != str(PART_START)
            or (sys_part / "size").read_text().strip() != str(PART_SECTORS)
            or (sys_disk / "loop" / "offset").read_text().strip() != "0"
            or (sys_disk / "loop" / "sizelimit").read_text().strip() != "0"):
        raise RuntimeError("Loop geometry does not match the one-partition fixture")
    info = partition.lstat()
    block_id = f"{os.major(info.st_rdev)}:{os.minor(info.st_rdev)}"
    if (not stat.S_ISBLK(info.st_mode) or info.st_uid != 0 or info.st_mode & stat.S_IWOTH
            or block_id != (sys_part / "dev").read_text().strip()):
        raise RuntimeError("Loop partition node does not match its kernel identity")
    with Path("/proc/self/mountinfo").open() as mounts:
        if any(line.split()[2] == block_id for line in mounts):
            raise RuntimeError("The temporary ext4 partition is mounted")
    return partition, block_id


def exercise(checker, directory, image, partition, block_id, filesystem_uuid):
    command_env = dict(os.environ, XDG_CONFIG_HOME=str(directory / "config"), XDG_STATE_HOME=str(directory / "state"))

    def cli(*args, exit_code=0):
        return json.loads(run(checker, *args, "--json", exit_code=exit_code, env=command_env))

    inventory = cli("filesystem")
    found = [v for v in inventory["volumes"] if v["name"] == str(partition)]
    if len(found) != 1:
        raise RuntimeError("whwmpc cannot identify the temporary loop partition")
    volume = found[0]
    if ((volume.get("maj:min"), volume.get("type"), volume.get("fstype"), volume.get("uuid"), volume.get("ro"))
            != (block_id, "part", "ext4", filesystem_uuid, False) or any(volume.get("mountpoints") or [])):
        raise RuntimeError(f"Unexpected fixture volume identity: {volume}")

    before = digest(image)
    damaged = cli("filesystem", "--check", partition)
    if damaged.get("state") != "errors_detected" or digest(image) != before:
        raise RuntimeError(f"Read-only check missed or modified the seeded error: {damaged}")

    plan_path = directory / "repair.json"
    plan = cli("filesystem", "--repair", partition, "--write-plan", plan_path)
    plan_id = plan["id"]
    if (plan.get("kind"), plan.get("target"), plan.get("privilege")) != ("filesystem-repair", str(partition), "sudo"):
        raise RuntimeError(f"Unexpected filesystem repair plan: {plan}")
    if re.fullmatch(r"[0-9a-f]{40}", plan_id) is None:
        raise RuntimeError("The preview did not provide a signed plan ID")

    wrong_id = ("1" if plan_id.startswith("0") else "0") + plan_id[1:]
    refused = cli("apply", "--file", plan_path, "--confirm", wrong_id, exit_code=1)
    if refused.get("status") != "not_started" or refused.get("started") is not False or digest(image) != before:
        raise RuntimeError(f"An unconfirmed plan changed the fixture: {refused}")

    repaired = cli("apply", "--file", plan_path, "--confirm", plan_id)
    if ((repaired.get("status"), repaired.get("started"), repaired.get("changed"), repaired.get("plan", {}).get("id"))
            != ("verified", True, True, plan_id)):
        raise RuntimeError(f"The signed repair was not verified: {repaired}")
    after = digest(image)
    if after == before:
        raise RuntimeError("The verified repair did not change the image")
    clean = cli("filesystem", "--check", partition)
    if clean.get("state") != "clean" or digest(image) != after:
        raise RuntimeError(f"Read-only recheck is inconclusive or modified the image: {clean}")


def main(checker):
    if (sys.platform != "linux" or os.geteuid() == 0 or os.getenv("GITHUB_ACTIONS") != "true"
            or os.getenv("RUNNER_ENVIRONMENT") != "github-hosted"):
        raise RuntimeError("Ext4 repair smoke requires a non-root user on a GitHub-hosted Linux runner")

    with tempfile.TemporaryDirectory(prefix="whwmpc-ext4-", dir=os.environ["RUNNER_TEMP"]) as temp:
        directory = Path(temp).resolve(strict=True)
        image = directory / "disk.img"
        filesystem_uuid = str(uuid.uuid4())
        make_image(image, filesystem_uuid)
        loop = Path(run(*SUDO, LOSETUP, "--find", "--nooverlap", "--partscan", "--show", image).strip())
        try:
            run("/usr/bin/udevadm", "settle", "--timeout=15")
            partition, block_id = fixture_partition(loop, image)
            run(*SUDO, "/usr/bin/setfacl", "-m", f"u:{os.getuid()}:r--", partition)
            exercise(checker, directory, image, partition, block_id, filesystem_uuid)
        finally:
            if owns_loop(loop, image):
                run(*SUDO, LOSETUP, "--detach", loop)
        print("Temporary ext4 partition passed signed repair, read-only recheck, and confirmation checks")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit(f"Usage: {sys.argv[0]} WHWMPC_BINARY")
    main(Path(sys.argv[1]).resolve(strict=True))
