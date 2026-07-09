package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Styles ──────────────────────────────────────────────────────────────

var (
	subtle    = lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#5C5C5C"}
	highlight = lipgloss.AdaptiveColor{Light: "#2E6BD1", Dark: "#7B9FEF"}

	titleStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(highlight).
			Bold(true)

	folderLine = lipgloss.NewStyle().
			Padding(0, 1).
			Width(50)

	folderActive = lipgloss.NewStyle().
			Padding(0, 1).
			Width(50).
			Background(highlight).
			Foreground(lipgloss.Color("#FFFFFF"))

	emptyStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(subtle).
			Italic(true)

	previewLabel = lipgloss.NewStyle().
			Foreground(subtle)

	subtleStyle = lipgloss.NewStyle().Foreground(subtle)
)

// ── Model ───────────────────────────────────────────────────────────────

type view int

const (
	foldersView view = iota
	sessionsView
)

type model struct {
	db       *sql.DB
	view     view
	folders  []FolderInfo
	sessions []Session
	session  *Session
	cursor   int
	ready    bool
	width    int
	height   int
	quitting bool
	msg      string
}

func initialModel(db *sql.DB) model {
	return model{db: db, view: foldersView}
}

// ── Messages ────────────────────────────────────────────────────────────

type foldersLoadedMsg struct {
	folders []FolderInfo
}

type sessionsLoadedMsg struct {
	sessions []Session
}

type resumeResult struct {
	err error
}

func loadFolders(db *sql.DB) tea.Cmd {
	return func() tea.Msg {
		ff, err := listFolders(db)
		if err != nil {
			return foldersLoadedMsg{}
		}
		return foldersLoadedMsg{folders: ff}
	}
}

func loadSessions(db *sql.DB, folder string) tea.Cmd {
	return func() tea.Msg {
		ss, err := listSessions(db, folder)
		if err != nil {
			return sessionsLoadedMsg{}
		}
		return sessionsLoadedMsg{sessions: ss}
	}
}

func resumeSession(s Session) tea.Cmd {
	return func() tea.Msg {
		if s.CWD != "" {
			os.Chdir(s.CWD)
		}
		cmd := exec.Command("pi", "--session", s.Path)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		if err := cmd.Run(); err != nil {
			return resumeResult{err: err}
		}
		return resumeResult{}
	}
}

// ── Bubble Tea lifecycle ────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return loadFolders(m.db)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		if m.quitting {
			return m, tea.Quit
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

		if m.view == foldersView {
			return updateFolders(m, msg)
		}
		return updateSessions(m, msg)

	case foldersLoadedMsg:
		m.folders = msg.folders
		return m, nil

	case sessionsLoadedMsg:
		m.sessions = msg.sessions
		m.session = nil
		m.cursor = 0
		return m, nil

	case resumeResult:
		if msg.err != nil {
			m.msg = fmt.Sprintf("pi exited: %v", msg.err)
		}
		return m, tea.Quit
	}

	return m, nil
}

func updateFolders(m model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.folders)-1 {
			m.cursor++
		}
	case "enter", "right", "l":
		f := m.folders[m.cursor]
		m.view = sessionsView
		m.cursor = 0
		return m, loadSessions(m.db, f.Name)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.folders) - 1
	}
	return m, nil
}

func updateSessions(m model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.session = nil
		}
	case "down", "j":
		if m.cursor < len(m.sessions)-1 {
			m.cursor++
			m.session = nil
		}
	case "home", "g":
		m.cursor = 0
		m.session = nil
	case "end", "G":
		m.cursor = len(m.sessions) - 1
		m.session = nil
	case "enter":
		if len(m.sessions) > 0 {
			s := m.sessions[m.cursor]
			return m, resumeSession(s)
		}
	case "left", "esc", "backspace":
		m.view = foldersView
		m.sessions = nil
		m.session = nil
		m.cursor = 0
		return m, nil
	case " ":
		if m.session != nil && m.session.Path == m.sessions[m.cursor].Path {
			m.session = nil
		} else {
			m.session = &m.sessions[m.cursor]
		}
	}
	return m, nil
}

// ── Views ───────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return "Loading\u2026"
	}
	if m.quitting {
		return ""
	}

	if m.view == foldersView {
		return m.foldersView()
	}
	return m.sessionsView()
}

func (m model) foldersView() string {
	var b strings.Builder

	b.WriteString(header("Folders"))
	b.WriteString("\n\n")

	for i, f := range m.folders {
		line := fmt.Sprintf("  %s  %d", f.Name, f.Count)
		if i == m.cursor {
			b.WriteString(folderActive.Render(line))
		} else {
			b.WriteString(folderLine.Render(line))
		}
		b.WriteString("\n")
	}

	if len(m.folders) == 0 {
		b.WriteString(emptyStyle.Render("  No sessions found"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(footer(len(m.folders), 0))

	return b.String()
}

func (m model) sessionsView() string {
	var b strings.Builder

	folderName := ""
	if m.cursor < len(m.sessions) {
		folderName = m.sessions[m.cursor].Folder
	} else if len(m.sessions) > 0 {
		folderName = m.sessions[0].Folder
	}

	b.WriteString(header(folderName))
	b.WriteString("\n\n")

	if len(m.sessions) == 0 {
		b.WriteString(emptyStyle.Render("  No sessions"))
		b.WriteString("\n")
	} else {
		listWidth := 60

		for i, s := range m.sessions {
			ts := truncate(s.LastTS, 19)
			title := s.Title
			if title == "" {
				title = "(untitled)"
			}
			modelName := truncate(s.Model, 25)

			line := fmt.Sprintf("  %s  %s  %s",
				ts, truncate(title, listWidth-28), modelName)

			if i == m.cursor {
				b.WriteString(folderActive.Render(line))
			} else {
				b.WriteString(folderLine.Render(line))
			}
			b.WriteString("\n")

			if i == m.cursor {
				b.WriteString(buildPreview(s))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(footer(len(m.sessions), 1))

	return b.String()
}

func buildPreview(s Session) string {
	pad := "    "
	var b strings.Builder

	b.WriteString(fmt.Sprintf("%s%s Created  %s\n", pad, previewLabel.Render("\u2502"), shortTS(s.Created)))
	b.WriteString(fmt.Sprintf("%s%s CWD      %s\n", pad, previewLabel.Render("\u2502"), s.CWD))
	b.WriteString(fmt.Sprintf("%s%s Session  %s\n", pad, previewLabel.Render("\u2502"), shortID(s.SID)))
	if s.Model != "" {
		b.WriteString(fmt.Sprintf("%s%s Model    %s\n", pad, previewLabel.Render("\u2502"), s.Model))
	}

	f, err := os.Open(s.Path)
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		var msgs []string
		for sc.Scan() {
			line := sc.Text()
			if !strings.Contains(line, `"role":"user"`) {
				continue
			}
			var raw map[string]any
			if json.Unmarshal([]byte(line), &raw) != nil {
				continue
			}
			msg, _ := raw["message"].(map[string]any)
			if msg == nil {
				continue
			}
			c, _ := msg["content"]
			var txt string
			switch v := c.(type) {
			case string:
				txt = v
			case []any:
				for _, item := range v {
					m, ok := item.(map[string]any)
					if !ok || m["type"] != "text" {
						continue
					}
					if t, ok := m["text"].(string); ok {
						txt = t
						break
					}
				}
			}
			if txt != "" {
				txt = strings.NewReplacer("\n", " ", "\r", "").Replace(txt)
				msgs = append(msgs, truncate(txt, 80))
				if len(msgs) >= 5 {
					break
				}
			}
		}
		if len(msgs) > 0 {
			b.WriteString("\n")
			for _, msg := range msgs {
				b.WriteString(fmt.Sprintf("%s%s %s\n", pad, previewLabel.Render("\u2502"), msg))
			}
		}
	}

	return b.String()
}

func header(title string) string {
	w := 60
	if len(title) > w-4 {
		title = truncate(title, w-7)
	}
	left := " " + title + " "
	right := w - lipgloss.Width(left)
	if right < 1 {
		right = 1
	}
	return titleStyle.Render(left + strings.Repeat("\u2500", right))
}

func footer(count int, viewType int) string {
	var hints string
	if viewType == 0 {
		hints = "\u2191\u2193 navigate  \u23ce open folder  q/ctrl+c quit"
	} else {
		hints = "\u2191\u2193 navigate  \u23ce resume  \u2190 back  space preview  q quit"
	}
	f := fmt.Sprintf(" %d sessions ", count)
	padLen := 60 - lipgloss.Width(f) - lipgloss.Width(hints) - 2
	if padLen < 1 {
		padLen = 1
	}
	return subtleStyle.Render(hints) + strings.Repeat(" ", padLen) + subtleStyle.Render(f)
}

func shortTS(ts string) string {
	if len(ts) > 19 {
		return ts[:19]
	}
	return ts
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
