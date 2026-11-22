package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/filesystem"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const filesystemOperand = "/proc/{executor_pid}/fd/{filesystem_fd}"

func (p linuxPlatform) filesystemRepair(ctx context.Context, target string) (prepared, error) {
	v, err := filesystem.Discover(ctx, p.runner).Select(target)
	if err != nil {
		return prepared{}, err
	}
	if v.ReadOnly {
		return prepared{}, errors.New("partisi hanya-baca; perbaikan tidak tersedia")
	}
	file, pinned, err := filesystem.Pin(v)
	if err != nil {
		return prepared{}, err
	}
	err = filesystem.Probe(ctx, p.runner, v, filesystem.Operand(file))
	file.Close()
	if err != nil {
		return prepared{}, err
	}
	tool, err := trustedExecutable("e2fsck")
	if err != nil {
		return prepared{}, err
	}
	version, err := filesystem.CheckVersion(ctx, p.runner, tool.path)
	if err != nil {
		return prepared{}, err
	}
	inv, err := newInvocation("env", filesystem.E2fsckArgs(tool.path, filesystemOperand, true), true)
	if err != nil {
		return prepared{}, err
	}
	host, err := bootIdentity()
	if err != nil {
		return prepared{}, err
	}
	executableID := digest(inv.identity(), tool.id, version)
	var exitCode int
	return prepared{
		plan: Plan{
			Kind: "filesystem-repair", Target: target,
			Initial:  fmt.Sprintf("%s %s · UUID %s · tidak terpasang", target, v.Filesystem, v.UUID),
			Identity: digest(host, v, pinned, executableID), Operation: inv.spec, Privilege: inv.privilege,
			Impact: "e2fsck memilih perbaikan metadata yang dapat dilakukan otomatis. Volume harus tetap offline. Target descriptor dibuka ulang dan diperiksa sebelum tindakan.",
			Risk:   "Cadangkan data sebelum perbaikan. Metadata dapat berubah atau file rusak dapat hilang. Perbaikan tidak menyediakan pemulihan.",
		},
		run: func(ctx context.Context) attempt {
			selected, err := filesystem.Discover(ctx, p.runner).Select(target)
			if err != nil || digest(selected) != digest(v) {
				return attempt{err: errors.New("status volume berubah sebelum perbaikan")}
			}
			file, current, err := filesystem.Pin(selected)
			if err != nil {
				return attempt{err: err}
			}
			defer file.Close()
			if current != pinned {
				return attempt{err: errors.New("descriptor bukan lagi media yang terpilih")}
			}
			operand := filesystem.Operand(file)
			if err := filesystem.Probe(ctx, p.runner, v, operand); err != nil {
				return attempt{err: err}
			}
			envNow, envErr := trustedExecutable(inv.exe.path)
			toolNow, toolErr := trustedExecutable(tool.path)
			if envErr != nil || toolErr != nil || envNow != inv.exe || toolNow != tool {
				return attempt{err: errors.New("executable pemeriksaan berubah")}
			}
			if inv.privilege == Sudo {
				sudoNow, err := trustedExecutable(inv.sudo.path)
				if err != nil || sudoNow != inv.sudo {
					return attempt{err: errors.New("executable sudo berubah")}
				}
			}
			spec := append([]string(nil), inv.spec...)
			spec[len(spec)-1] = operand
			step, stderr, code := run(ctx, 12*time.Minute, spec[0], spec[1:])
			exitCode = code
			if strings.Contains(string(stderr), "sudo:") {
				return attempt{err: errors.New("sudo menolak perbaikan filesystem")}
			}
			if strings.Contains(strings.ToLower(string(stderr)+string(step.output)), "is mounted") {
				step.err = errors.New("e2fsck menolak volume yang sedang dipakai")
			}
			return step
		},
		verify: func(ctx context.Context, step attempt) Result {
			envNow, envErr := trustedExecutable(inv.exe.path)
			toolNow, toolErr := trustedExecutable(tool.path)
			if envErr != nil || toolErr != nil || envNow != inv.exe || toolNow != tool {
				return Result{Status: Unknown, Message: "Executable pemeriksaan berubah setelah perbaikan."}
			}
			selected, err := filesystem.Discover(ctx, p.runner).Select(target)
			if err != nil {
				return Result{Status: Unknown, Message: withStepError("Volume tidak dapat diidentifikasi ulang", step)}
			}
			file, actual, err := filesystem.Pin(selected)
			if err != nil {
				return Result{Status: Unknown, Message: withStepError("Volume tidak dapat dibuka ulang", step)}
			}
			defer file.Close()
			if actual != pinned || selected.UUID != v.UUID {
				return Result{Status: Unknown, Message: "Identitas volume berubah. Tidak ada klaim keberhasilan."}
			}
			operand := filesystem.Operand(file)
			if err := filesystem.Probe(ctx, p.runner, v, operand); err != nil {
				return Result{Status: Unknown, Message: "Metadata volume tidak dapat diverifikasi ulang."}
			}
			locked, err := filesystem.ExclusiveRead(file)
			if err != nil {
				return Result{Status: Unknown, Message: "Volume tidak lagi tersedia eksklusif untuk pemeriksaan ulang."}
			}
			defer locked.Close()
			outcome, stderr, code := run(ctx, 12*time.Minute, inv.exe.path, filesystem.E2fsckArgs(tool.path, operand, false))
			check := filesystem.InterpretExclusive(sysrun.Result{Output: outcome.output, Stderr: stderr, ExitCode: code, Err: outcome.err}, target, operand)
			return filesystemResult(exitCode, check, step)
		},
	}, nil
}

func filesystemResult(exitCode int, check filesystem.CheckResult, step attempt) Result {
	switch {
	case exitCode != -1 && exitCode&2 != 0:
		return Result{Status: Partial, Message: "e2fsck meminta restart. Pastikan volume offline lagi lalu jalankan whwmpc filesystem --check pada volume yang sama."}
	case check.State == "clean" && step.err == nil && (exitCode == 0 || exitCode == 1):
		return Result{Status: Verified, Changed: boolPointer(exitCode == 1), Message: "Pemeriksaan read-only setelah perbaikan menyatakan filesystem bersih."}
	case check.State == "errors_detected" || check.State == "journal_recovery_needed":
		return Result{Status: Partial, Message: withStepError("Masih ada kesalahan atau pemulihan jurnal yang diperlukan. Jangan ulangi otomatis; periksa cadangan", step)}
	default:
		return Result{Status: Unknown, Message: withStepError("Pemeriksaan ulang belum membuktikan hasil perbaikan", step)}
	}
}
