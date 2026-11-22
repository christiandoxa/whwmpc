package main

import (
	"fmt"
	"os"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

var version = "dev"

type welcome struct{}

func (welcome) Init() tea.Cmd { return nil }

func (w welcome) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if pressed, ok := msg.(tea.KeyPressMsg); ok && key.Matches(pressed, key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"))) {
		return w, tea.Quit
	}
	return w, nil
}

func (welcome) View() tea.View {
	v := tea.NewView(lipgloss.NewStyle().Bold(true).Render("What Happens With My PC") +
		"\n\nHai! Mau cek apa di komputermu hari ini?\n\nq  Keluar")
	v.AltScreen = true
	return v
}

func main() {
	args := os.Args[1:]
	switch {
	case len(args) == 1 && args[0] == "--version":
		fmt.Println("whwmpc", version)
	case len(args) == 1 && (args[0] == "--help" || args[0] == "-h"):
		fmt.Println("whwmpc [--version]")
	case len(args) != 0:
		fmt.Fprintln(os.Stderr, "Gunakan whwmpc --help.")
		os.Exit(2)
	case term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()):
		if _, err := tea.NewProgram(welcome{}).Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Println("What Happens With My PC")
	}
}
