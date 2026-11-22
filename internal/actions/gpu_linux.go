//go:build linux

package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/christiandoxa/whwmpc/internal/hardware"
)

func (p linuxPlatform) gpu(ctx context.Context, kind, target string) (prepared, error) {
	change, err := hardware.Prepare(ctx, p.runner, kind, target)
	if err != nil {
		return prepared{}, err
	}
	boot, err := bootIdentity()
	if err != nil {
		return prepared{}, err
	}
	before := change.Before
	read := func(ctx context.Context) (hardware.Settings, error) {
		return hardware.ReadSettings(ctx, p.runner, kind, before.ID)
	}

	var inv invocation
	var operation []string
	impact := fmt.Sprintf("%s pada GPU %s berubah dari %d menjadi %d %s.",
		before.Control.Label, before.PCI, before.Control.Current, change.Desired, before.Control.Unit)
	risk := "Batas daya dapat memengaruhi konsumsi daya, panas, dan performa. NVIDIA dapat mengembalikan batas default saat driver dilepas."
	var files gpuFiles
	var execute func(context.Context) attempt
	if before.SysfsDir == "" {
		inv, err = newInvocation("nvidia-smi", change.NVIDIAArgs(), true)
		if err != nil {
			return prepared{}, err
		}
		operation = inv.spec
		execute = func(ctx context.Context) attempt {
			state, err := read(ctx)
			if err != nil || !sameGPUState(before, state) {
				return attempt{err: errors.New("target atau konfigurasi GPU berubah sebelum eksekusi")}
			}
			return inv.run(ctx, 20*time.Second)
		}
	} else {
		inv, err = newInvocation("dd", nil, true)
		if err != nil {
			return prepared{}, err
		}
		files, err = inspectGPUFiles(before.SysfsDir)
		if err != nil {
			return prepared{}, err
		}
		impact += fmt.Sprintf(" Mode DPM %s → %s. Pengaturan berlaku saat driver berjalan.", before.Mode, change.Mode)
		risk = "Perubahan clock atau voltase bisa menyebabkan hang, reset GPU, atau kehilangan pekerjaan. Rentang driver bukan jaminan stabilitas. Pengaturan dapat berubah saat driver dilepas atau perangkat di-reset."
		for _, step := range amdWrites(change) {
			operation = append(operation, strings.Join(inv.spec, " ")+fmt.Sprintf(" of=<descriptor dari %s> status=none conv=notrunc, stdin=%q", step.path, step.value))
		}
		execute = func(ctx context.Context) attempt {
			current, err := inspectGPUFiles(before.SysfsDir)
			if err != nil || current != files {
				return attempt{err: errors.New("identitas atribut OverDrive berubah sebelum eksekusi")}
			}
			return executeOverdrive(ctx, change, read, func(ctx context.Context, path, value string) attempt {
				attr := files.od
				if path == hardware.PerformanceFile {
					attr = files.mode
				}
				return attr.write(ctx, inv, value)
			})
		}
	}
	identityFor := func(state hardware.Settings, files gpuFiles) string {
		return digest(boot, state, files.od.id, files.mode.id, inv.identity())
	}
	return prepared{
		plan: Plan{Kind: kind, Target: target, Initial: before.InitialTarget(), Identity: identityFor(before, files),
			Operation: operation, Privilege: inv.privilege, Impact: impact, Risk: risk, Rollback: !strings.HasPrefix(kind, "restore-")},
		run: execute,
		verify: func(ctx context.Context, step attempt) Result {
			after, err := read(ctx)
			if err != nil {
				return Result{Status: Unknown, Message: withStepError("Konfigurasi GPU belum dapat dibaca kembali", step)}
			}
			if after.SysfsDir != "" {
				observed, err := inspectGPUFiles(after.SysfsDir)
				if err != nil || observed != files {
					return Result{Status: Unknown, Message: withStepError("Identitas atribut OverDrive berubah setelah tindakan", step)}
				}
			}
			return verifyGPU(change, after, identityFor(after, files), step)
		},
	}, nil
}

func sameGPUState(before, after hardware.Settings) bool {
	return before.SameDevice(after) && before.Mode == after.Mode && before.Control.Current == after.Control.Current && before.Table == after.Table
}

type gpuAttr struct {
	path string
	id   [2]uint64
}

type gpuFiles struct{ od, mode gpuAttr }

func inspectGPUFiles(dir string) (gpuFiles, error) {
	if !strings.HasPrefix(dir, "/sys/devices/pci") {
		return gpuFiles{}, errors.New("atribut GPU tidak berada pada sysfs PCI")
	}
	if info, err := protectedPath(dir, 0, false); err != nil || !info.IsDir() {
		return gpuFiles{}, errors.New("direktori GPU tidak dilindungi root")
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil || stat.Type != 0x62656572 {
		return gpuFiles{}, errors.New("atribut GPU tidak berada pada mount sysfs")
	}
	attr := func(name string) (gpuAttr, error) {
		path := filepath.Join(dir, name)
		info, err := protectedPath(path, 0, false)
		if err != nil {
			return gpuAttr{}, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0200 == 0 {
			return gpuAttr{}, errors.New("kontrol OverDrive tidak tersedia untuk administrator")
		}
		id := identity(info)
		return gpuAttr{path, [2]uint64{id.Device, id.Inode}}, nil
	}
	mode, err := attr(hardware.PerformanceFile)
	if err != nil {
		return gpuFiles{}, err
	}
	od, err := attr(hardware.OverdriveFile)
	return gpuFiles{od: od, mode: mode}, err
}

func (attr gpuAttr) write(ctx context.Context, inv invocation, value string) attempt {
	fd, err := os.OpenFile(attr.path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return attempt{err: err}
	}
	defer fd.Close()
	info, err := fd.Stat()
	if err != nil {
		return attempt{err: err}
	}
	id := identity(info)
	if !info.Mode().IsRegular() || id.UID != 0 || id.Mode&0022 != 0 || [2]uint64{id.Device, id.Inode} != attr.id {
		return attempt{err: errors.New("atribut GPU sudah berubah")}
	}
	operand, err := descriptorPath(os.Getpid(), fd.Fd())
	if err == nil {
		err = checkDescriptorReference(fd, operand)
	}
	if err != nil {
		return attempt{err: err}
	}
	inv.spec = append(slices.Clone(inv.spec), "of="+operand, "status=none", "conv=notrunc")
	return inv.runInput(ctx, 10*time.Second, strings.NewReader(value+"\n"))
}

type gpuWrite struct{ path, value string }

func amdWrites(change hardware.Change) []gpuWrite {
	var steps []gpuWrite
	if change.Before.Mode != "manual" {
		steps = append(steps, gpuWrite{hardware.PerformanceFile, "manual"})
	}
	steps = append(steps, gpuWrite{hardware.OverdriveFile, change.OverdriveValue()}, gpuWrite{hardware.OverdriveFile, "c"})
	if change.Mode != "manual" {
		steps = append(steps, gpuWrite{hardware.PerformanceFile, change.Mode})
	}
	return steps
}

func executeOverdrive(ctx context.Context, change hardware.Change, read func(context.Context) (hardware.Settings, error),
	write func(context.Context, string, string) attempt) attempt {
	before := change.Before
	initial, err := read(ctx)
	if err != nil || !sameGPUState(before, initial) {
		return attempt{err: errors.New("GPU berubah sebelum tindakan OverDrive")}
	}
	var result attempt
	for _, step := range amdWrites(change) {
		if err := ctx.Err(); err != nil {
			result.err, result.stopped = err, true
			return result
		}
		observed, err := read(ctx)
		if err != nil || !before.SameDevice(observed) || before.OtherTable() != observed.OtherTable() {
			result.err = errors.New("GPU atau parameter OverDrive lain berubah selama tindakan")
			return result
		}
		valid := false
		switch step.value {
		case "manual":
			valid = observed.Mode == before.Mode && observed.Control.Current == before.Control.Current
		case "auto":
			valid = observed.Mode == "manual" && observed.Control.Current == change.Desired
		case "c":
			valid = observed.Mode == "manual" && (observed.Control.Current == before.Control.Current || observed.Control.Current == change.Desired)
		default:
			valid = observed.Mode == "manual" && observed.Control.Current == before.Control.Current
		}
		if !valid {
			result.err = errors.New("mode atau nilai GPU berubah sebelum langkah berikutnya")
			return result
		}
		last := write(ctx, step.path, step.value)
		result.started = result.started || last.started
		result.stopped = last.stopped
		result.err = last.err
		if result.err != nil || result.stopped {
			return result
		}
	}
	return result
}

func verifyGPU(change hardware.Change, after hardware.Settings, id string, step attempt) Result {
	before := change.Before
	if !before.SameDevice(after) {
		return Result{Status: Unknown, Message: withStepError("Identitas atau batas GPU berubah setelah tindakan", step)}
	}
	changed := before.Control.Current != after.Control.Current || before.Mode != after.Mode
	message := fmt.Sprintf("Nilai terbaca %d %s", after.Control.Current, after.Control.Unit)
	if after.Mode != "" {
		message += " · mode " + after.Mode
	}
	switch {
	case after.Control.Current == change.Desired && after.Mode == change.Mode && step.err == nil &&
		!step.stopped && before.OtherTable() == after.OtherTable():
		return Result{Status: Verified, Changed: boolPointer(changed), AfterIdentity: id, Message: message}
	case changed || before.OtherTable() != after.OtherTable():
		return Result{Status: Partial, Changed: boolPointer(true), Message: withStepError(message+" · hasil belum terverifikasi penuh", step)}
	case step.stopped || after.SysfsDir != "":
		return Result{Status: Unknown, Message: withStepError(message+" · status commit OverDrive belum pasti", step)}
	default:
		return Result{Status: Failed, Changed: boolPointer(false), Message: withStepError(message, step)}
	}
}
