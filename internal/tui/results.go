package tui

import (
	"slices"

	"github.com/christiandoxa/whwmpc/internal/assistant"
	"github.com/christiandoxa/whwmpc/internal/privread"
)

func (m *Model) updateResultContext(previous screenState) {
	if previous.page.Snapshot == nil || m.page.Snapshot == nil {
		return
	}
	m.back = slices.Clone(m.back)
	for i := range m.back {
		if m.back[i].page.Snapshot == previous.page.Snapshot && m.back[i].active == m.active {
			m.back[i].page, m.back[i].request = m.page, m.request
		}
	}
	if previous.screen == detail {
		m.screen, m.component = detail, previous.component
		m.choice, m.scroll = min(previous.choice, len(m.choices())-1), min(previous.scroll, m.maxScroll())
	} else if previous.screen == resultOptions && len(m.back) > 0 && m.back[len(m.back)-1].page.Snapshot == m.page.Snapshot {
		m.back = m.back[:len(m.back)-1]
	}
}

func (m Model) saveChoice() Choice {
	return Choice{m.tr(Words{"Simpan hasil ini", "Save these results"}), "save-snapshot", ""}
}

func (m Model) evidenceChoice() Choice {
	return Choice{m.tr(Words{"Bukti lengkap", "Full evidence"}), "evidence", ""}
}

func (m Model) backChoice() Choice {
	return Choice{m.tr(Words{"Kembali", "Back"}), "back", ""}
}

func (m Model) resultChoices() []Choice {
	primary := m.page.Summary.Primary
	if primary.Kind == "complete-read" {
		primary = m.retryReadChoice(privread.Operation(primary.Value))
	}
	choices := []Choice{primary}
	if m.active == assistant.Slow && m.page.Snapshot != nil && m.page.Snapshot.ObservedFor == 0 {
		choices = appendChoice(choices, observeChoice(m.options.Language))
	}
	if len(m.page.Findings)+len(m.page.Evidence)+len(m.page.Found) > 0 {
		choices = appendChoice(choices, detailsChoice(m.options.Language))
	}
	if m.page.Snapshot != nil {
		choices = append(choices, m.saveChoice())
	}
	if m.screen == outcome {
		for _, c := range m.page.Choices {
			if c.Kind == "back" {
				choices = appendChoice(choices, c)
			}
		}
	}
	if len(m.additionalResultChoices()) > 1 {
		choices = append(choices, Choice{m.tr(Words{"Pilihan lain", "More options"}), "result-options", ""})
	}
	return choices
}

func (m Model) additionalResultChoices() []Choice {
	primary := m.page.Summary.Primary
	var choices []Choice
	for _, c := range m.page.Choices {
		if (c.Kind == primary.Kind && c.Value == primary.Value) || c.Kind == "back" {
			continue
		}
		if c.Kind == "complete-read" {
			c = m.retryReadChoice(privread.Operation(c.Value))
		}
		choices = appendChoice(choices, c)
	}
	if len(m.page.Findings)+len(m.page.Evidence) > 0 {
		choices = appendChoice(choices, m.evidenceChoice())
	}
	if m.request != nil && startsInspection(*m.request) && m.request.Plan == nil {
		c := Choice{m.tr(Words{"Periksa ulang", "Check again"}), "rescan", ""}
		if m.active == assistant.Slow {
			c = observeChoice(m.options.Language)
		}
		if c.Kind != primary.Kind {
			choices = appendChoice(choices, c)
		}
		if m.options.Privilege != nil {
			choices = appendChoice(choices, Choice{m.tr(menuNames[assistant.Settings]), "navigate", string(assistant.Settings)})
		}
	}
	if primary.Kind != "finish" {
		choices = appendChoice(choices, finishChoice(m.options.Language))
	}
	return append(choices, m.backChoice())
}
