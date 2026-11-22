package tui

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/inspection"
	"github.com/christiandoxa/whwmpc/internal/report"
	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func reportPage(lang string) Page {
	l := translator(lang)
	return Page{
		Availability: domain.Available,
		Found:        []string{l("Buat laporan lengkap", "Create a full report")},
		Meaning:      []string{l("Pemeriksaan tambahan akan dijalankan sebelum laporan dibuat.", "Additional checks will run before the report is created.")},
		Advice:       []string{l("Nama pengguna, hostname, serial, nama proses, dan path pribadi disaring dari laporan.", "Usernames, hostnames, serial numbers, process names, and personal paths are filtered from the report.")},
		Choices: []Choice{
			{Label: l("Ekspor Markdown", "Export Markdown"), Kind: "export-select", Value: "markdown"},
			{Label: l("Ekspor JSON", "Export JSON"), Kind: "export-select", Value: "json"},
		},
	}
}

func exportReport(ctx context.Context, scanner *inspection.Scanner, store state.Store, req Request, demo bool) Page {
	l := translator(req.Language)
	var doc report.Report
	var historyErr, err error
	if req.Snapshot != nil {
		doc = report.Build(req.Snapshot.Input)
	} else {
		var entries []state.Entry
		if !demo {
			entries, historyErr = store.History()
		}
		doc, err = workflow.ScanReport(ctx, scanner, req.Overview, req.Language, demo, entries)
	}
	if err == nil {
		var data []byte
		data, err = report.Encode(doc, req.Choice.Value)
		if err == nil {
			if req.Printing != nil {
				req.Printing()
			}
			if err = ctx.Err(); err == nil {
				err = state.WriteNew(req.Output, data)
			}
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return Page{Availability: domain.Unknown, Pose: mascot.Cancelled, Found: []string{
				l("Ekspor dihentikan sebelum selesai.", "Export stopped before completion."), domain.SafeText(err.Error()),
			}}
		}
		return Page{Availability: domain.Error, Pose: mascot.Error, Found: []string{
			l("Ekspor gagal.", "Export failed."), domain.SafeText(err.Error()),
		}, Choices: []Choice{{Label: l("Pilih file baru", "Choose a new file"), Kind: "navigate", Value: string(assistant.Report)}}}
	}
	path, err := filepath.Abs(req.Output)
	if err != nil {
		path = req.Output
	}
	page := Page{
		Availability: domain.Available, Pose: mascot.Printing,
		Found:   []string{l("Laporan tersimpan", "Report saved"), domain.SafeText(path)},
		Meaning: []string{l("File ditulis secara lokal.", "The file was written locally.")},
	}
	if historyErr != nil {
		page.Advice = []string{l("Riwayat tidak terbaca", "History could not be read") + " · " + domain.SafeText(historyErr.Error())}
	}
	return page
}

func historyPage(store state.Store, maintenance *workflow.Maintenance, lang string) Page {
	l := translator(lang)
	page := Page{Availability: domain.Available}
	if maintenance.Demo {
		page.Found = []string{l("DEMO · riwayat host tidak dibaca.", "DEMO · host history is not read.")}
		return page
	}
	entries, err := store.History()
	if err != nil {
		return Page{Availability: domain.Error, Found: []string{l("Riwayat tidak terbaca.", "Could not read history."), domain.SafeText(err.Error())}}
	}
	if len(entries) == 0 {
		page.Found = []string{l("Belum ada riwayat lokal.", "Local history is empty.")}
		page.Advice = []string{l("Simpan ringkasan dari overview atau jalankan tindakan yang kamu tinjau.", "Save a summary from the overview or run a reviewed action.")}
		return page
	}
	for _, entry := range entries {
		line := entry.At.Local().Format("02 Jan 15:04") + " · " + entry.Category
		if a := entry.Action; a != nil {
			line += " · " + string(a.Status)
			if a.CancellationRequested {
				line += " · " + l("penghentian diminta", "cancel requested")
			}
			if a.CanRestore && maintenance.Available() == nil {
				short := entry.PlanID[:min(8, len(entry.PlanID))]
				page.Choices = append(page.Choices, Choice{
					Label: l("Tinjau pemulihan", "Preview restore") + " · " + entry.Category + " " + short,
					Kind:  "restore", Value: entry.PlanID,
				})
			}
		} else if f := entry.Findings; f != nil {
			line += fmt.Sprintf(" · %d %s · %d %s", f.Total, l("temuan", "findings"), f.Critical, l("kritis", "critical"))
		}
		page.Found = append(page.Found, line)
		page.Evidence = append(page.Evidence, line)
		if entry.PlanID != "" {
			page.Evidence = append(page.Evidence, "ID · "+entry.PlanID)
		}
	}
	page.Meaning = []string{l("Riwayat menyimpan ringkasan hingga 100 entri terbaru.", "History retains summaries of the latest 100 entries.")}
	return page
}

type printingMsg struct{ job uint64 }

func saveScanChoice(lang string) Choice {
	return Choice{Label: translator(lang)("Simpan ringkasan", "Save summary"), Kind: "save-scan"}
}

func saveScan(scanner *inspection.Scanner, store state.Store, req Request, demo bool) Page {
	l := translator(req.Language)
	if demo || len(req.Findings) == 0 {
		return Page{Availability: domain.Unsupported, Found: []string{l("Ringkasan tidak tersedia.", "Summary unavailable.")}}
	}
	if err := store.RecordScan(string(req.Intent), scanner.Now(), req.Findings); err != nil {
		return Page{Availability: domain.Error, Found: []string{l("Ringkasan gagal disimpan.", "Could not save summary."), domain.SafeText(err.Error())}}
	}
	return Page{Availability: domain.Available, Found: []string{l("Ringkasan tersimpan di riwayat.", "Summary saved to history.")},
		Choices: []Choice{{Label: l("Lihat riwayat", "View history"), Kind: "navigate", Value: string(assistant.History)}}}
}
