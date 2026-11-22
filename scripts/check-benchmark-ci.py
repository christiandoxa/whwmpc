import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile


IMAGE = "fedora@sha256:15da97ed9aa94dd3d92c3925eb89520f6c80e07ea7ee24cba848d1f8b6912b3e"


def main(binary):
    if sys.platform != "linux" or os.getenv("GITHUB_ACTIONS") != "true" or os.getenv("RUNNER_ENVIRONMENT") != "github-hosted":
        raise RuntimeError("Benchmark smoke requires a GitHub-hosted Linux runner")
    with tempfile.TemporaryDirectory(prefix="whwmpc-benchmark-", dir=os.environ["RUNNER_TEMP"]) as directory:
        sys_class = Path(directory, "sys-class")
        sensor = sys_class / "hwmon" / "hwmon0"
        sensor.mkdir(parents=True)
        (sys_class / "thermal").mkdir()
        (sensor / "name").write_text("coretemp\n")
        reading = sensor / "temp1_input"

        def benchmark(mode, duration, status):
            command = [
                "docker", "run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
                "--cpus", "0.5", "--memory", "192m", "--pids-limit", "32", "--user", "65534:65534",
                "--tmpfs", "/tmp:mode=1777", "-e", "XDG_STATE_HOME=/tmp/state", "-e", "XDG_CONFIG_HOME=/tmp/config",
                "--mount", f"type=bind,src={binary},dst=/whwmpc,readonly",
                "--mount", f"type=bind,src={sys_class},dst=/sys/class,readonly",
                IMAGE, "/whwmpc", "benchmark", mode, "--duration", duration,
            ]
            run = subprocess.run(command, capture_output=True, text=True, timeout=90)
            if run.returncode != status:
                raise RuntimeError(f"{mode} exited {run.returncode}, expected {status}: {run.stdout}\n{run.stderr}")
            return json.loads(run.stdout)

        reading.write_text("55000\n")
        cpu = benchmark("cpu", "200ms", 0)
        stress = benchmark("stress", "1400ms", 0)
        if not (cpu["complete"] and cpu["processed_bytes"] > 0 and not cpu["thermal_guard"]
                and stress["complete"] and stress["processed_bytes"] > 0
                and stress["thermal_guard"] and stress["peak_celsius"] == 55):
            raise RuntimeError(f"Expected measured CPU and supervised stress samples: {cpu}, {stress}")

        reading.write_text("85000\n")
        hot = benchmark("stress", "1400ms", 1)
        reading.unlink()
        missing = benchmark("stress", "1400ms", 1)
        if any(result["complete"] or result["processed_bytes"] for result in (hot, missing)):
            raise RuntimeError(f"Unsafe stress started: {hot}, {missing}")
        print("Bounded benchmark and stress passed with fixture temperatures. Hot and missing sensors refused the workload.")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit(f"Usage: {sys.argv[0]} WHWMPC_BINARY")
    main(Path(sys.argv[1]).resolve(strict=True))
