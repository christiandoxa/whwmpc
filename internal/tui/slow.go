package tui

import (
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/domain"
	"github.com/christiandoxa/whwmpc/internal/tui/mascot"
	"github.com/christiandoxa/whwmpc/internal/workflow"
)

func (m *Model) slowdownQuestion() tea.Cmd {
	m.remember()
	l := translator(m.options.Language)
	m.screenState = screenState{screen: taskQuestion, active: assistant.Slow, question: m.question, page: Page{
		Found: []string{l("Apakah kelambatannya sedang terjadi?", "Is the slowdown happening now?")},
		Meaning: []string{
			fmt.Sprintf(l("Pantau CPU, RAM, dan proses sekitar %d detik. Esc menghentikan pengamatan.", "Watch CPU, RAM, and processes for about %d seconds. Esc stops observation."), workflow.ObservationWindow/time.Second),
			l("Pilih Belum untuk merekam kondisi saat ini sebagai pembanding.", "Choose Not now to record current conditions for comparison."),
		},
		Choices: []Choice{
			{l("Ya, pantau sekarang", "Yes, watch now"), "slow-observe", ""},
			{l("Belum, ambil kondisi awal", "Not now, take a baseline"), "slow-baseline", ""},
		},
	}}
	m.show(mascot.Listening, "", false)
	return nil
}

func (m Model) observationLabel(elapsed, window time.Duration) string {
	return fmt.Sprintf(m.tr(Words{"Memantau CPU, RAM, proses · %d dtk (sekitar %d dtk)", "Watching CPU, RAM, processes · %d s (about %d s)"}), elapsed/time.Second, window/time.Second)
}

func observeChoice(lang string) Choice {
	return Choice{Words{"Pantau saat PC lambat", "Watch during the slowdown"}.Text(lang), "slow-observe", ""}
}

func summarizeObservation(s *ResultSummary, snap workflow.Snapshot, lang string) {
	l := translator(lang)
	if snap.ObservedFor == 0 {
		s.Advice = l("Kondisi awal. Penyebab kelambatan sebelumnya belum dapat disimpulkan.", "Baseline. The cause of the earlier slowdown remains undetermined.")
		return
	}
	o := snap.Input.Overview
	s.Advice = fmt.Sprintf(l("Pengamatan %d dtk. Perubahan beban menjadi petunjuk selama gejala.", "%d s observed. Load changes provide clues during the slowdown."), snap.ObservedFor.Round(time.Second)/time.Second)
	mem := o.Memory.Use
	if latest, ok := mem.Latest(); ok && validPercent(latest) && len(mem.Samples) > 1 {
		s.Metrics = slices.Clone(s.Metrics)
		low, high := latest, latest
		for _, sample := range mem.Samples {
			if validPercent(sample) {
				low, high = min(low, sample), max(high, sample)
			}
		}
		for i := range s.Metrics {
			if s.Metrics[i].Category == "memory" {
				s.Metrics[i].Note = number(low, 1) + "–" + number(high, 1) + "% · " + s.Metrics[i].Note
			}
		}
	}
	process := resultStatus{Category: "process", Label: l("Proses teratas", "Top observed process"), Availability: o.ProcessStatus}
	if hasData(process.Availability) && len(o.Processes) > 0 && validPercent(o.Processes[0].CPU) {
		p := o.Processes[0]
		process.Value = fmt.Sprintf("%s · %s%% CPU", domain.SafeText(p.Name), number(p.CPU, 1))
	}
	s.Statuses = append(s.Statuses, process)
}
