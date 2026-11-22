package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/domain"
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
