package filesystem

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

func runNativeCheck(ctx context.Context, runner sysrun.Runner, path string, args ...string) sysrun.Result {
	if native, ok := runner.(sysrun.Native); ok && native.Timeout <= 0 {
		native.Timeout = 2 * time.Minute
		runner = native
	}
	if err := ctx.Err(); err != nil {
		return sysrun.Result{Err: err, ExitCode: -1}
	}
	return runner.Run(ctx, path, args...)
}

func nativeNotStarted(target string, status domain.Availability, detail string) CheckResult {
	return CheckResult{Availability: status, State: "not_started", Device: domain.SafeText(target), ExitCode: -1, Detail: detail}
}

func nativeCheckError(target string, raw sysrun.Result) CheckResult {
	result := CheckResult{Availability: domain.Unknown, State: "unknown", Device: target, ExitCode: raw.ExitCode}
	switch {
	case errors.Is(raw.Err, context.Canceled):
		result.State, result.Detail = "cancelled", "Pemeriksaan dibatalkan sebelum ada hasil."
	case errors.Is(raw.Err, context.DeadlineExceeded):
		result.State, result.Detail = "inconclusive", "Batas waktu pemeriksaan tercapai sebelum ada hasil."
	case errors.Is(raw.Err, os.ErrPermission):
		result.Availability, result.Detail = domain.PermissionRequired, "Izin untuk membaca volume diperlukan."
	case errors.Is(raw.Err, os.ErrNotExist):
		result.Availability, result.Detail = domain.DependencyMissing, "Alat pemeriksaan bawaan sistem tidak tersedia."
	default:
		result.Availability, result.Detail = domain.Error, "Alat pemeriksaan berhenti sebelum hasil lengkap tersedia."
	}
	return result
}

func interpretAPFSCheck(raw sysrun.Result, target string, live bool) CheckResult {
	if raw.Err != nil {
		return nativeCheckError(target, raw)
	}
	result := CheckResult{Device: target, ExitCode: raw.ExitCode}
	severity, evidence := apfsCheckEvidence(raw)
	switch {
	case severity == "permission":
		result.Availability, result.State, result.Detail = domain.PermissionRequired, "unknown", evidence
	case severity == "error":
		result.Availability, result.State, result.Detail = domain.Partial, "errors_reported", "fsck_apfs melaporkan kendala. "+evidence
	case raw.ExitCode != 0:
		result.Availability, result.State, result.Detail = domain.Error, "inconclusive", "fsck_apfs berhenti dengan kode nonnol sebelum ada hasil yang dapat disimpulkan."
	case severity == "warning":
		result.Availability, result.State, result.Detail = domain.Partial, "review_required", evidence
	case severity != "complete":
		result.Availability, result.State, result.Detail = domain.Unknown, "inconclusive", "fsck_apfs tidak melaporkan bahwa pemeriksaan volume selesai."
	case live:
		result.Availability, result.State, result.Detail = domain.Partial, "no_errors_observed", "fsck_apfs selesai tanpa kesalahan yang dilaporkan pada kontainer aktif."
	default:
		result.Availability, result.State, result.Detail = domain.Available, "clean", "Pemeriksaan struktur APFS offline selesai tanpa kesalahan yang dilaporkan."
	}
	return result
}

func apfsCheckEvidence(raw sysrun.Result) (string, string) {
	var severity, evidence string
	for _, stream := range [][]byte{raw.Output, raw.Stderr} {
		for line := range strings.SplitSeq(string(stream), "\n") {
			lower := strings.ToLower(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(lower, "error:"):
				if strings.Contains(lower, "permission denied") || strings.Contains(lower, "operation not permitted") {
					return "permission", domain.SafeText(line)
				}
				return "error", domain.SafeText(line)
			case strings.HasPrefix(lower, "warning:"), strings.HasPrefix(lower, "skipping "),
				strings.Contains(lower, "could not be verified completely"):
				if severity != "warning" {
					severity, evidence = "warning", domain.SafeText(line)
				}
			case strings.HasPrefix(lower, "the volume ") && strings.Contains(lower, "appears to be ok"):
				if severity == "" {
					severity = "complete"
				}
			}
		}
	}
	return severity, evidence
}

func interpretNTFSCheck(raw sysrun.Result, target string) CheckResult {
	if raw.Err != nil {
		return nativeCheckError(target, raw)
	}
	result := CheckResult{Availability: domain.Partial, Device: target, ExitCode: raw.ExitCode}
	switch raw.ExitCode {
	case 0:
		result.State, result.Detail = "no_errors_observed", "CHKDSK tidak melaporkan kesalahan pada volume aktif."
	case 1:
		result.State, result.Detail = "inconclusive", "CHKDSK mengembalikan status yang tidak sesuai dengan pemeriksaan baca-saja."
	case 2:
		result.State, result.Detail = "review_required", "CHKDSK melaporkan status pembersihan yang ambigu pada volume aktif."
	case 3:
		result.State, result.Detail = "inconclusive", "CHKDSK tidak dapat menyelesaikan pemeriksaan atau melaporkan kesalahan yang masih perlu ditinjau."
	default:
		result.Availability, result.State, result.Detail = domain.Error, "unknown", "CHKDSK mengembalikan status yang tidak dikenali."
	}
	return result
}
