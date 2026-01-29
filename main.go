package main

import (
	"fmt"
	"os"

	"unhexed/internal/editor"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	files := os.Args[1:]

	model, err := editor.NewModel(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running program: %v\n", err)
		os.Exit(1)
	}
}
