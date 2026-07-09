package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbletea"
)

func main() {
	os.Exit(run())
}

func run() int {
	db, err := openDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "DB error: %v\n", err)
		return 1
	}
	defer db.Close()

	if err := refresh(db); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	p := tea.NewProgram(initialModel(db), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	m := final.(model)
	if m.msg != "" {
		fmt.Println(m.msg)
	}
	return 0
}
