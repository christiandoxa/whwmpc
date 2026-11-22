package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/christiandoxa/whwmpc/internal/actions"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func actionCommand(ctx context.Context, args []string, m *workflow.Maintenance, stdin, stdout *os.File, stderr io.Writer) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	cmd := args[0]
	switch cmd {
	case "clean", "power", "trim", "restore", "apply":
	default:
		return 0, false
	}
	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "keluaran JSON")
	var target, receipt, writePlan, inputPlan, confirm *string
	var dryRun, enableTimer *bool
	if cmd != "apply" {
		writePlan = flags.String("write-plan", "", "simpan rencana ke file baru untuk apply")
	}
	switch cmd {
	case "clean":
		dryRun = flags.Bool("dry-run", false, "inventaris dan pratinjau cache paket")
	case "power":
		target = flags.String("plan", "", "profil yang ingin ditinjau")
	case "trim":
		target = flags.String("mount", "", "mountpoint absolut yang ingin ditinjau")
		enableTimer = flags.Bool("enable-timer", false, "tinjau aktivasi jadwal TRIM vendor")
	case "restore":
		receipt = flags.String("action", "", "ID aksi terdahulu yang terverifikasi")
	case "apply":
		inputPlan = flags.String("file", "", "file rencana hasil preview")
		confirm = flags.String("confirm", "", "ID rencana yang telah ditinjau")
	}
	badUsage := func() (int, bool) {
		fmt.Fprintln(stderr, "Opsi tindakan belum lengkap atau bertentangan.")
		flags.PrintDefaults()
		return 2, true
	}
	if err := flags.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
		return 0, true
	} else if err != nil {
		return 2, true
	}
	if flags.NArg() > 0 {
		return badUsage()
	}
	var plan actions.Plan
	var err error
	switch cmd {
	case "clean":
		if !*dryRun {
			return badUsage()
		}
		inventory, err := m.Cleanup(ctx)
		if err != nil && inventory.Availability == "" {
			return printError(stderr, err), true
		}
		var preview *actions.Plan
		if *writePlan != "" {
			kind := inventory.Kind
			if kind == "" {
				kind = actions.CleanAPT
			}
			plan, err = m.Preview(ctx, kind, "", true)
			if err != nil {
				return printError(stderr, err), true
			}
			if err := writeJSON(*writePlan, plan); err != nil {
				return printError(stderr, err), true
			}
			preview = &plan
		}
		if *asJSON {
			return printJSON(stdout, stderr, struct {
				Demo      bool            `json:"demo"`
				Inventory actions.Cleanup `json:"inventory"`
				Plan      *actions.Plan   `json:"plan,omitempty"`
			}{m.Demo, inventory, preview}), true
		}
		if m.Demo {
			fmt.Fprintln(stdout, "DEMO")
		}
		fmt.Fprintf(stdout, "%s · %s · %d B teramati pada %d file\n", inventory.Category, inventory.Availability, inventory.KnownBytes, inventory.KnownFiles)
		for _, scope := range inventory.Scope {
			fmt.Fprintln(stdout, domain.SafeText(scope))
		}
		for _, path := range inventory.Unknown {
			fmt.Fprintln(stdout, "Belum terukur:", domain.SafeText(path))
		}
		fmt.Fprintln(stdout, domain.SafeText(inventory.Impact))
		if inventory.Detail != "" {
			fmt.Fprintln(stdout, domain.SafeText(inventory.Detail))
		}
		if preview != nil {
			showPlan(stdout, *preview)
			fmt.Fprintln(stdout, "Rencana tersimpan:", domain.SafeText(*writePlan))
		}
		return 0, true
	case "power":
		if *target == "" {
			return badUsage()
		}
		plan, err = m.Preview(ctx, actions.PowerProfile, *target, *writePlan != "")
	case "trim":
		if (*target != "") == *enableTimer {
			return badUsage()
		}
		kind := actions.TrimMount
		if *enableTimer {
			kind = actions.EnableTrimTimer
		}
		plan, err = m.Preview(ctx, kind, *target, *writePlan != "")
	case "restore":
		if *receipt == "" {
			return badUsage()
		}
		plan, err = m.Restore(ctx, *receipt)
	case "apply":
		if *inputPlan == "" || *confirm == "" {
			return badUsage()
		}
		if err := m.Available(); err != nil {
			return printError(stderr, err), true
		}
		plan, err := readPlanFile(*inputPlan)
		if err != nil {
			return printError(stderr, err), true
		}
		if plan.Privilege == actions.Sudo && term.IsTerminal(stdin.Fd()) && term.IsTerminal(stdout.Fd()) && terminalWriter(stderr) {
			if err := m.Authenticate(ctx, plan, *confirm, stdin, stderr, stderr); err != nil {
				return printError(stderr, err), true
			}
		}
		outcome, err := m.Execute(ctx, plan, *confirm, nil)
		result := outcome.Result
		if outcome.HistoryErr != nil {
			fmt.Fprintln(stderr, "Riwayat gagal disimpan:", domain.SafeText(outcome.HistoryErr.Error()))
		}
		var outputCode int
		if *asJSON {
			outputCode = printJSON(stdout, stderr, result)
		} else {
			fmt.Fprintf(stdout, "%s · %s\n", result.Status, domain.SafeText(result.Message))
			fmt.Fprintf(stdout, "Dimulai %t · penghentian diminta %t\n", result.Started, result.CancellationRequested)
			if result.PotentialDiscardBytes != nil {
				fmt.Fprintf(stdout, "%d B berpotensi diajukan untuk discard\n", *result.PotentialDiscardBytes)
			}
			if result.RemovedBytes != nil {
				fmt.Fprintf(stdout, "%d B teramati terhapus\n", *result.RemovedBytes)
			}
			if result.FreeSpaceDelta != nil {
				fmt.Fprintf(stdout, "%+d B perubahan ruang bebas filesystem arsip\n", *result.FreeSpaceDelta)
			}
		}
		if outputCode != 0 || err != nil || result.Status != actions.Verified || outcome.HistoryErr != nil {
			return 1, true
		}
		return 0, true
	}
	if err != nil {
		return printError(stderr, err), true
	}
	if *writePlan != "" {
		if err := writeJSON(*writePlan, plan); err != nil {
			return printError(stderr, err), true
		}
	}
	if *asJSON {
		return printJSON(stdout, stderr, plan), true
	}
	showPlan(stdout, plan)
	if *writePlan != "" {
		fmt.Fprintln(stdout, "Rencana tersimpan:", domain.SafeText(*writePlan))
	}
	return 0, true
}

func readPlanFile(path string) (actions.Plan, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return actions.Plan{}, err
	}
	if !info.Mode().IsRegular() {
		return actions.Plan{}, errors.New("rencana harus berupa file reguler")
	}
	f, err := state.OpenRegular(path)
	if err != nil {
		return actions.Plan{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return actions.Plan{}, errors.New("file rencana berubah saat dibuka")
	}
	return actions.ReadPlan(f)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return state.WriteNew(path, append(data, '\n'))
}

func showPlan(out io.Writer, plan actions.Plan) {
	fmt.Fprintf(out, "ID %s\nVersi %d · %s\nTarget %s\nKondisi awal %s\nOperasi %q\nIzin %s\nDampak %s\nRisiko %s\nKedaluwarsa %s · pemulihan %t\n",
		plan.ID, plan.Version, plan.Kind, domain.SafeText(plan.Target), domain.SafeText(plan.Initial), plan.Operation, plan.Privilege,
		domain.SafeText(plan.Impact), domain.SafeText(plan.Risk), plan.ExpiresAt.Format("2006-01-02 15:04:05 MST"), plan.Rollback)
	if plan.EstimatedBytes != nil {
		fmt.Fprintf(out, "%d B estimasi teramati\n", *plan.EstimatedBytes)
	}
}

func printError(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "whwmpc:", domain.SafeText(err.Error()))
	return 1
}

func terminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}
