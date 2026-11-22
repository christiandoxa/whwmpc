package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/report"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func reportCommand(ctx context.Context, args []string, scanner *inspection.Scanner, store state.Store, demo bool, lang string, stdout, stderr io.Writer) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	cmd := args[0]
	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	flags.SetOutput(stderr)
	var format, output *string
	var asJSON, record *bool
	switch cmd {
	case "report":
		format = flags.String("format", "markdown", "format json atau markdown")
		output = flags.String("output", "", "tulis ke file baru")
	case "check":
		asJSON = flags.Bool("json", false, "keluaran JSON berskema")
		record = flags.Bool("record", false, "simpan ringkasan pemeriksaan ke riwayat lokal")
	case "history":
		asJSON = flags.Bool("json", false, "keluaran JSON")
	default:
		return 0, false
	}
	if err := flags.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
		return 0, true
	} else if err != nil {
		return 2, true
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "Opsi tidak dikenal.")
		return 2, true
	}
	if cmd == "history" {
		var entries []state.Entry
		var err error
		if !demo {
			entries, err = store.History()
			if err != nil {
				return printError(stderr, err), true
			}
		}
		if *asJSON {
			return printJSON(stdout, stderr, struct {
				Demo    bool          `json:"demo"`
				Entries []state.Entry `json:"entries"`
			}{demo, entries}), true
		}
		if demo {
			fmt.Fprintln(stdout, "DEMO")
		}
		if len(entries) == 0 {
			fmt.Fprintln(stdout, "Riwayat lokal kosong.")
		}
		for _, entry := range entries {
			fmt.Fprintf(stdout, "%s · %s", entry.At.Format("2006-01-02 15:04 MST"), entry.Category)
			if f := entry.Findings; f != nil {
				fmt.Fprintf(stdout, " · %d temuan · %d peringatan · %d kritis · %d data tak tersedia", f.Total, f.Warning, f.Critical, f.Unavailable)
			}
			if a := entry.Action; a != nil {
				fmt.Fprintf(stdout, " · %s · ID %s", a.Status, entry.PlanID)
				if a.CanRestore {
					fmt.Fprint(stdout, " · pemulihan dapat ditinjau")
				}
			}
			fmt.Fprintln(stdout)
		}
		return 0, true
	}
	exportFormat := "markdown"
	if format != nil {
		exportFormat = *format
	} else if *asJSON {
		exportFormat = "json"
	}
	if exportFormat != "json" && exportFormat != "markdown" {
		fmt.Fprintln(stderr, "Format harus json atau markdown.")
		return 2, true
	}
	if cmd == "check" && *record && demo {
		fmt.Fprintln(stderr, "DEMO: riwayat lokal terkunci.")
		return 2, true
	}
	var entries []state.Entry
	if cmd == "report" && !demo {
		var err error
		entries, err = store.History()
		if err != nil {
			fmt.Fprintln(stderr, "Riwayat tidak terbaca:", domain.SafeText(err.Error()))
		}
	}
	doc, err := workflow.ScanReport(ctx, scanner, domain.Overview{}, lang, demo, entries)
	if err != nil {
		return printError(stderr, err), true
	}
	data, err := report.Encode(doc, exportFormat)
	if err != nil {
		return printError(stderr, err), true
	}
	if cmd == "check" && *record {
		if err := store.RecordScan("overview", doc.GeneratedAt, doc.Findings); err != nil {
			return printError(stderr, err), true
		}
	}
	if output != nil && *output != "" {
		if err := state.WriteNew(*output, data); err != nil {
			return printError(stderr, err), true
		}
		fmt.Fprintln(stdout, domain.SafeText(*output))
	} else if _, err := stdout.Write(data); err != nil {
		return printError(stderr, err), true
	}
	return 0, true
}
