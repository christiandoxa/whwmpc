#!/usr/bin/env python3
"""Exercise whwmpc on a real Linux PTY or record its demo as asciicast v2."""

import argparse
import codecs
import fcntl
import json
import os
import pty
import re
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time
from pathlib import Path


ANSI = re.compile(rb"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[()][ -~]|[@-_])")
ENTER_ALT = b"\x1b[?1049h"
LEAVE_ALT = b"\x1b[?1049l"


class Terminal:
    def __init__(self, binary, width, height, state, recording, app_arguments):
        self.master, self.slave = pty.openpty()
        self.process = None
        self.output = bytearray()
        self.events = []
        self.recording = recording
        self.decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self.started = time.monotonic()
        try:
            self.resize(width, height)
            self.original = termios.tcgetattr(self.slave)
            env = os.environ | {
                "TERM": "xterm-256color",
                "COLORTERM": "truecolor",
                "LC_ALL": "C.UTF-8",
                "XDG_CONFIG_HOME": str(state / "config"),
                "XDG_STATE_HOME": str(state / "state"),
                "XDG_CACHE_HOME": str(state / "cache"),
            }
            if recording:
                env.pop("NO_COLOR", None)
            else:
                env["NO_COLOR"] = "1"
            self.process = subprocess.Popen(
                [str(binary), "--demo", "--lang", "id", *app_arguments],
                stdin=self.slave,
                stdout=self.slave,
                stderr=self.slave,
                env=env,
                start_new_session=True,
            )
        except BaseException:
            self.close()
            raise

    def resize(self, width, height):
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        if self.process is not None:
            # Popen's inherited slave lacks a controlling-terminal foreground group.
            self.process.send_signal(signal.SIGWINCH)

    def key(self, data):
        os.write(self.master, data)

    def read(self, timeout):
        if not select.select([self.master], [], [], timeout)[0]:
            return
        data = os.read(self.master, 65536)
        self.output.extend(data)
        if len(self.output) > 16 * 1024 * 1024:
            raise RuntimeError("PTY output exceeded 16 MiB")
        if self.recording:
            text = self.decoder.decode(data)
            if text:
                self.events.append((round(time.monotonic() - self.started, 6), "o", text))

    def expect(self, label, start=0, timeout=8):
        marker = label.encode()
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if marker in ANSI.sub(b"", self.output[start:]):
                return
            if self.process.poll() is not None:
                break
            self.read(0.05)
        raise RuntimeError(f"expected {label!r} in terminal output")

    def hold(self, duration):
        deadline = time.monotonic() + duration
        while (remaining := deadline - time.monotonic()) > 0:
            if self.process.poll() is not None:
                raise RuntimeError("whwmpc exited before recording finished")
            self.read(min(remaining, 0.1))

    def finish(self):
        self.key(b"q")
        deadline = time.monotonic() + 8
        while self.process.poll() is None and time.monotonic() < deadline:
            self.read(0.05)
        if self.process.poll() != 0:
            raise RuntimeError(f"expected clean exit, got {self.process.poll()}")
        while select.select([self.master], [], [], 0)[0]:
            self.read(0)
        if ENTER_ALT not in self.output or LEAVE_ALT not in self.output.rsplit(ENTER_ALT, 1)[-1]:
            raise RuntimeError("alternate screen was not restored")
        if termios.tcgetattr(self.slave) != self.original:
            raise RuntimeError("terminal attributes were not restored")

    def close(self):
        if self.process is not None and self.process.poll() is None:
            self.process.kill()
            self.process.wait()
        os.close(self.slave)
        os.close(self.master)


def press(terminal, key):
    terminal.key(key)
    terminal.hold(0.8 if terminal.recording else 0.22)


def choose_level(terminal, deep, scope="SMART"):
    terminal.expect("Mulai tanpa administrator")
    if deep:
        terminal.expect(scope)
        press(terminal, b"\x1b[A")
    offset = len(terminal.output)
    press(terminal, b"\r")
    terminal.expect("Mendalam" if deep else "Biasa", offset)
    if deep and (terminal.output.count(ENTER_ALT) < 2 or LEAVE_ALT not in terminal.output):
        raise RuntimeError("authorization did not release and restore the alternate screen")
    return offset


def task_flow(terminal, flow, resize=False, deep=False, observe=False, cleanup_result=False):
    terminal.expect("Mau dibantu apa?")
    index = ("condition", "slow", "cleanup", "ssd").index(flow)
    for _ in range(index):
        press(terminal, b"\x1b[B")
    press(terminal, b"\r")
    if flow == "ssd":
        terminal.expect("Kesehatan SSD")
        press(terminal, b"\r")
        offset = choose_level(terminal, False)
        terminal.expect("Lengkapi pemeriksaan SSD", offset)
        press(terminal, b"\r")
        terminal.expect("Kembali ke hasil")
        terminal.expect("SSD / SMART")
        offset = len(terminal.output)
        press(terminal, b"\r")
        terminal.expect("Lengkapi pemeriksaan SSD", offset)
        press(terminal, b"\r")
        press(terminal, b"\x1b[A")
        offset = len(terminal.output)
        press(terminal, b"\r")
        terminal.expect("Lolos", offset)
        if terminal.output.count(ENTER_ALT) < 2 or LEAVE_ALT not in terminal.output:
            raise RuntimeError("SSD authorization did not restore the alternate screen")
        done(terminal, offset)
        return
    if flow == "slow":
        terminal.expect("Apakah kelambatannya sedang terjadi?")
        if not observe:
            press(terminal, b"\x1b[B")
        press(terminal, b"\r")
    started = time.monotonic()
    offset = choose_level(terminal, deep and flow != "cleanup", "CPU dan daya" if flow == "slow" else "SMART")
    if flow == "cleanup":
        terminal.expect("file teramati", offset)
        offset = len(terminal.output)
        press(terminal, b"\r")
        terminal.expect("Penghapusan permanen pada aksi nyata", offset)
        if resize:
            terminal.resize(60, 18)
            terminal.hold(0.3)
            terminal.resize(80, 24)
            terminal.hold(0.3)
        offset = len(terminal.output)
        press(terminal, b"\r")
        terminal.expect("file teramati", offset)
        if cleanup_result:
            offset = len(terminal.output)
            press(terminal, b"\r")
            terminal.expect("Simulasikan pembersihan", offset)
            press(terminal, b"\x1b[6~")
            press(terminal, b"\x1b[B")
            offset = len(terminal.output)
            press(terminal, b"\r")
            terminal.expect("Pembersihan cache terverifikasi", offset)
            terminal.expect("Teramati terhapus", offset)
            terminal.expect("Selisih ruang bebas", offset)
            done(terminal, offset)
        return
    if observe:
        terminal.expect("Memantau CPU, RAM, proses", offset)
        terminal.expect("build-worker", offset, timeout=25)
        if time.monotonic() - started < 11.5:
            raise RuntimeError("observation did not wait for its sampling window")
    terminal.expect("selesai", offset)
    for measurement in (("41%", "59%") if observe else ("24%", "56%")) + ("71%", "148.0 GiB"):
        terminal.expect(measurement, offset)
    if flow == "slow" and not observe:
        terminal.expect("Pantau saat PC lambat", offset)
        terminal.expect("Kondisi awal", offset)
    else:
        terminal.expect("Selesai", offset)
    terminal.expect("Detail komponen", offset)
    press(terminal, b"\x1b[B")
    offset = len(terminal.output)
    press(terminal, b"\r")
    terminal.expect("Pilih komponen", offset)
    offset = len(terminal.output)
    press(terminal, b"\r")
    terminal.expect("Sumber dan waktu", offset)
    if resize:
        offset = len(terminal.output)
        terminal.resize(40, 14)
        terminal.expect("Perbesar terminal (48x18)", offset)
        terminal.resize(60, 18)
        terminal.hold(0.3)
        terminal.resize(80, 24)
        terminal.hold(0.3)
    offset = len(terminal.output)
    press(terminal, b"\x1b")
    terminal.expect("Pilih komponen", offset)
    offset = len(terminal.output)
    press(terminal, b"\x1b")
    terminal.expect("Detail komponen", offset)
    if flow != "slow" or observe:
        press(terminal, b"\x1b[A")
        done(terminal, offset)


def done(terminal, offset):
    terminal.expect("Selesai", offset)
    offset = len(terminal.output)
    press(terminal, b"\r")
    terminal.expect("Pemeriksaan terakhir", offset)


def smoke(terminal):
    terminal.key(b"?")
    terminal.expect("Bantuan keyboard")
    press(terminal, b"\x1b")
    task_flow(terminal, "condition", resize=True)
    press(terminal, b"\x1b")
    press(terminal, b"/")
    terminal.expect("Apa yang ingin kamu tanyakan?")
    # Incremental input frames can advance over a blank cell without printing a space.
    offset = len(terminal.output)
    terminal.key(b"ssd trim q?d\r")
    terminal.expect("beberapa kemungkinan.", offset)
    terminal.expect("Pertanyaan · ssd trim q?d", offset)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path, help="path to the built whwmpc binary")
    parser.add_argument("--record", type=Path, help="write an asciicast v2 demo")
    parser.add_argument("--width", type=int, default=120, help="initial PTY columns")
    parser.add_argument("--height", type=int, default=30, help="initial PTY rows")
    parser.add_argument("--duration", type=float, default=5, help="seconds to capture after startup")
    parser.add_argument("--gallery", action="store_true", help="record the normal fixture flow in the Piko gallery")
    parser.add_argument("--app-arg", action="append", default=[], help="extra demo flag in a recording")
    parser.add_argument("--flow", choices=("condition", "slow", "cleanup", "ssd"))
    parser.add_argument("--deep", action="store_true", help="choose deep inspection using fixtures")
    parser.add_argument("--observe", action="store_true", help="observe a slowdown in real time")
    parser.add_argument("--cleanup-result", action="store_true", help="confirm the fixture-only cleanup")
    args = parser.parse_args()
    if not 80 <= args.width <= 240 or not 24 <= args.height <= 80 or not 0.5 <= args.duration <= 30:
        parser.error("use 80–240 columns, 24–80 rows and 0.5–30 seconds")
    if args.observe and args.flow != "slow":
        parser.error("--observe requires --flow slow")
    if args.cleanup_result and args.flow != "cleanup":
        parser.error("--cleanup-result requires --flow cleanup")
    if args.app_arg and (args.record is None or any(
        not arg.startswith("--") or arg == "--" or arg.startswith("--demo") for arg in args.app_arg
    )):
        parser.error("extra flags require --record and preserve demo mode")
    if args.gallery:
        if args.record is None:
            parser.error("--gallery requires --record")
        args.app_arg.extend(["demo", "animations"])
    if args.flow == "ssd":
        args.app_arg.append("--demo-scenario=ssd-permission")
    if args.cleanup_result:
        args.app_arg.append("--demo-scenario=cleanup-result")

    with tempfile.TemporaryDirectory(prefix="whwmpc-pty-") as tempdir:
        terminal = Terminal(
            args.binary.resolve(), args.width, args.height, Path(tempdir), args.record is not None, args.app_arg
        )
        try:
            terminal.expect("DEMO")
            if args.flow:
                if args.record is not None:
                    terminal.hold(1.2)
                task_flow(terminal, args.flow, resize=args.record is None, deep=args.deep, observe=args.observe,
                          cleanup_result=args.cleanup_result)
                if args.record is not None:
                    terminal.hold(1)
                    recording = terminal.events[:]
            elif args.record is None:
                smoke(terminal)
            else:
                if args.gallery:
                    terminal.expect("Galeri Piko")
                    terminal.hold(0.7)
                    for _ in range(8):
                        terminal.key(b"\r")
                        terminal.hold(max(0.4, args.duration / 8))
                    terminal.expect("Terbukti")
                else:
                    terminal.hold(args.duration)
                recording = terminal.events[:]
            terminal.finish()
        except RuntimeError as error:
            tail = ANSI.sub(b"", terminal.output[-3000:]).decode(errors="replace")
            raise SystemExit(f"{error}\nPTY output tail: {tail!r}") from error
        finally:
            terminal.close()

    if args.record is None:
        print("PTY smoke passed: task navigation and terminal restoration")
        return
    header = {"version": 2, "width": args.width, "height": args.height, "title": "What Happens With My PC · Demo"}
    with args.record.open("x", encoding="utf-8") as output:
        print(json.dumps(header, ensure_ascii=False), file=output)
        for event in recording:
            print(json.dumps(event, ensure_ascii=False), file=output)
    print(f"Recorded {len(recording)} terminal events to {args.record}")


if __name__ == "__main__":
    main()
