package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/filesystem"
	"github.com/christiandoxa/whwmpc/internal/firmware"
	"github.com/christiandoxa/whwmpc/internal/hardware"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func gpuCommand(ctx context.Context, args []string, m *workflow.Maintenance, stdout, stderr io.Writer) (int, bool) {
	if len(args) == 0 || args[0] != "gpu" {
		return 0, false
	}
	flags := flag.NewFlagSet("gpu", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "tampilkan GPU dan batas kontrol")
	kind := flags.String("plan", "", "gpu-power-limit (mW), gpu-core-max (MHz), atau gpu-voltage-offset (mV)")
	pci := flags.String("device", "", "PCI BDF dari inventaris")
	value := flags.String("value", "", "nilai eksplisit dalam unit kontrol")
	file := flags.String("write-plan", "", "simpan rencana ke file baru")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0, true
		}
		return 2, true
	}
	if flags.NArg() != 0 || (*kind == "" && (*pci != "" || *value != "" || *file != "")) {
		return 2, true
	}
	if m.Demo {
		fmt.Fprintln(stderr, "DEMO · akses perangkat terkunci")
		return 1, true
	}
	snap := hardware.Scan(ctx, sysrun.Native{})
	if *kind == "" {
		if *asJSON {
			return printJSON(stdout, stderr, snap), true
		}
		fmt.Fprintln(stdout, snap.Availability, domain.SafeText(snap.Detail))
		for _, device := range snap.Devices {
			fmt.Fprintf(stdout, "%s · %s · %s %s · %s\n", device.PCI, domain.SafeText(device.Model), device.Driver, device.Version, device.Availability)
			for _, control := range device.Controls {
				fmt.Fprintf(stdout, "  %s · saat ini %d · batas %d–%d %s\n", control.Kind, control.Current, control.Min, control.Max, control.Unit)
			}
		}
		return 0, true
	}
	n, err := strconv.ParseInt(*value, 10, 32)
	if err != nil {
		fmt.Fprintln(stderr, "Nilai integer eksplisit diperlukan.")
		return 2, true
	}
	for _, device := range snap.Devices {
		if device.PCI != *pci || device.ID == "" {
			continue
		}
		for _, control := range device.Controls {
			if control.Kind != *kind || n < control.Min || n > control.Max {
				continue
			}
			p, err := m.Preview(ctx, *kind, device.ID+"="+strconv.FormatInt(n, 10), *file != "")
			return displayAdvancedPlan(p, err, *file, *asJSON, stdout, stderr), true
		}
	}
	fmt.Fprintln(stderr, "GPU, kontrol, atau batas nilai tidak cocok. Periksa whwmpc gpu --json.")
	return 1, true
}

func displayAdvancedPlan(plan actions.Plan, err error, path string, asJSON bool, stdout, stderr io.Writer) int {
	if err != nil {
		return printError(stderr, err)
	}
	if path != "" {
		if err = writeJSON(path, plan); err != nil {
			return printError(stderr, err)
		}
	}
	if asJSON {
		return printJSON(stdout, stderr, plan)
	}
	showPlan(stdout, plan)
	if path != "" {
		fmt.Fprintln(stdout, "Rencana tersimpan:", domain.SafeText(path))
	}
	return 0
}

func storageActionCommand(ctx context.Context, args []string, m *workflow.Maintenance, out, stderr io.Writer) (int, bool) {
	if len(args) == 0 || (args[0] != "firmware" && args[0] != "filesystem") {
		return 0, false
	}
	cmd := args[0]
	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "keluaran terstruktur")
	path := flags.String("write-plan", "", "simpan rencana ke file baru")
	var refresh, updates bool
	var preview, verify, resume, check string
	if cmd == "firmware" {
		flags.BoolVar(&refresh, "refresh", false, "tinjau refresh katalog LVFS melalui jaringan")
		flags.BoolVar(&updates, "updates", false, "baca kandidat yang ada di cache lokal")
		flags.StringVar(&preview, "update", "", "tinjau kandidat DEVICEID@VERSION")
		flags.StringVar(&verify, "verify", "", "baca hasil fwupd untuk DEVICEID@VERSION")
		flags.StringVar(&resume, "resume", "", "baca bukti pascarestart dari ID rencana")
	} else {
		flags.StringVar(&preview, "repair", "", "tinjau perbaikan volume offline")
		flags.StringVar(&check, "check", "", "periksa filesystem tanpa mengubahnya")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0, true
		}
		return 2, true
	}
	selected := 0
	for _, wanted := range []bool{refresh, updates, preview != "", verify != "", resume != "", check != ""} {
		if wanted {
			selected++
		}
	}
	if flags.NArg() != 0 || selected > 1 || (*path != "" && !refresh && preview == "") {
		fmt.Fprintln(stderr, "Pilih satu operasi. --write-plan hanya untuk review tindakan.")
		return 2, true
	}
	if m.Demo {
		fmt.Fprintln(stderr, "DEMO · akses perangkat terkunci")
		return 1, true
	}
	runner := sysrun.Native{}
	switch {
	case refresh || preview != "":
		kind, target := actions.FirmwareRefresh, firmware.LVFS
		switch {
		case cmd == "filesystem":
			kind, target = actions.FilesystemRepair, preview
		case !refresh:
			kind, target = actions.FirmwareUpdate, preview
		}
		plan, err := m.Preview(ctx, kind, target, *path != "")
		return displayAdvancedPlan(plan, err, *path, *asJSON, out, stderr), true
	case verify != "" || resume != "":
		if resume != "" {
			var err error
			verify, err = m.Store.FirmwareHandoff(resume)
			if err != nil {
				return printError(stderr, err), true
			}
		}
		result := firmware.Outcome(ctx, runner, verify)
		if *asJSON {
			if rc := printJSON(out, stderr, result); rc != 0 {
				return rc, true
			}
		} else {
			fmt.Fprintln(out, domain.SafeText(result.Detail()))
		}
		if result.State != "verified" {
			return 1, true
		}
		return 0, true
	case cmd == "firmware":
		snap := firmware.Collect(ctx, runner, updates)
		if *asJSON {
			return printJSON(out, stderr, snap), true
		}
		fmt.Fprintln(out, snap.Availability, domain.SafeText(snap.Detail))
		for _, dev := range snap.Devices {
			fmt.Fprintf(out, "%s · %s · %s\n", dev.ID, domain.SafeText(dev.Name), domain.SafeText(dev.Version))
			for _, release := range dev.Releases {
				target := dev.ID + "@" + release.Version
				if _, _, err := firmware.Select(snap, target); err == nil {
					fmt.Fprintln(out, "  Kandidat", target)
				}
			}
		}
	case check != "":
		result := filesystem.Check(ctx, runner, check)
		if *asJSON {
			return printJSON(out, stderr, result), true
		}
		fmt.Fprintln(out, domain.SafeText(result.State)+" · "+domain.SafeText(result.Detail))
		if result.Availability != domain.Available {
			return 1, true
		}
	default:
		snap := filesystem.Discover(ctx, runner)
		if *asJSON {
			return printJSON(out, stderr, snap), true
		}
		fmt.Fprintln(out, snap.Availability, domain.SafeText(snap.Detail))
		for _, volume := range snap.Volumes {
			fmt.Fprintf(out, "%s · %s · mounted=%t\n", domain.SafeText(volume.Device), domain.SafeText(volume.Filesystem), volume.Mounted())
		}
	}
	return 0, true
}
