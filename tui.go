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
	"github.com/evertras/bubble-table/table"
)

var (
	subtle    = lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#5C5C5C"}
	highlight = lipgloss.AdaptiveColor{Light: "#2E6BD1", Dark: "#7B9FEF"}

	titleStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(highlight).
			Bold(true)

	previewLabel = lipgloss.NewStyle().
			Foreground(subtle)

	subtleStyle = lipgloss.NewStyle().Foreground(subtle)
)

const (
	columnKeyName        = "name"
	columnKeyCount       = "count"
	columnKeyTS          = "ts"
	columnKeyTitle       = "title"
	columnKeyModel       = "model"
	columnKeyFolderPath  = "folder_path"
	columnKeySessionPath = "session_path"
)

type view int

const (
	foldersView view = iota
	sessionsView
)

type model struct {
	db           *sql.DB
	view         view
	folders      []FolderInfo
	sessions     []Session
	folderTable  table.Model
	sessionTable table.Model
	session      *Session
	ready        bool
	width        int
	height       int
	quitting     bool
	msg          string
}

func initialModel(db *sql.DB) model {
	return model{db: db, view: foldersView}
}

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
	if s.CWD != "" {
		os.Chdir(s.CWD)
	}
	cmd := exec.Command("pi", "--session", s.Path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return resumeResult{err: err}
		}
		return resumeResult{}
	})
}

func findSessionByPath(ss []Session, path string) *Session {
	for i := range ss {
		if ss[i].Path == path {
			return &ss[i]
		}
	}
	return nil
}

func (m model) highlightedFolderName() string {
	row := m.folderTable.HighlightedRow()
	if name, ok := row.Data[columnKeyFolderPath].(string); ok && name != "" {
		return name
	}
	idx := m.folderTable.GetHighlightedRowIndex()
	if idx >= 0 && idx < len(m.folders) {
		return m.folders[idx].Name
	}
	return ""
}

func (m model) highlightedSessionPath() string {
	row := m.sessionTable.HighlightedRow()
	if path, ok := row.Data[columnKeySessionPath].(string); ok && path != "" {
		return path
	}
	idx := m.sessionTable.GetHighlightedRowIndex()
	if idx >= 0 && idx < len(m.sessions) {
		return m.sessions[idx].Path
	}
	return ""
}

func (m model) Init() tea.Cmd {
	return loadFolders(m.db)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		if m.width > 2 {
			w := m.width - 2
			if m.folderTable.TotalRows() > 0 {
				m.folderTable = m.folderTable.WithTargetWidth(w)
			}
			if m.sessionTable.TotalRows() > 0 {
				m.sessionTable = m.sessionTable.WithTargetWidth(w)
			}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case foldersLoadedMsg:
		m.folders = msg.folders
		if m.folders == nil {
			m.folders = []FolderInfo{}
		}
		m.buildFolderTable()
		return m, nil

	case sessionsLoadedMsg:
		m.sessions = msg.sessions
		if m.sessions == nil {
			m.sessions = []Session{}
		}
		m.buildSessionTable()
		return m, nil

	case resumeResult:
		if msg.err != nil {
			m.msg = fmt.Sprintf("pi exited: %v", msg.err)
		}
		return m, tea.Quit
	}

	return m, nil
}

func (m model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.quitting {
		return m, tea.Quit
	}

	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit

	case "enter":
		return m.handleEnter()

	case "left", "esc", "backspace":
		return m.handleBack()

	case " ":
		return m.handleSpace()
	}

	var cmd tea.Cmd
	if m.view == foldersView {
		m.folderTable, cmd = m.folderTable.Update(msg)
	} else {
		m.sessionTable, cmd = m.sessionTable.Update(msg)
	}
	return m, cmd
}

func (m model) handleEnter() (tea.Model, tea.Cmd) {
	if m.view == foldersView {
		folder := m.highlightedFolderName()
		if folder != "" {
			m.view = sessionsView
			m.session = nil
			return m, loadSessions(m.db, folder)
		}
		return m, nil
	}

	path := m.highlightedSessionPath()
	if path != "" {
		s := findSessionByPath(m.sessions, path)
		if s != nil {
			return m, resumeSession(*s)
		}
	}
	return m, nil
}

func (m model) handleBack() (tea.Model, tea.Cmd) {
	if m.view == sessionsView {
		m.view = foldersView
		m.session = nil
	}
	return m, nil
}

func (m model) handleSpace() (tea.Model, tea.Cmd) {
	if m.view != sessionsView {
		return m, nil
	}
	path := m.highlightedSessionPath()
	if path == "" {
		return m, nil
	}
	if m.session != nil && m.session.Path == path {
		m.session = nil
	} else {
		s := findSessionByPath(m.sessions, path)
		if s != nil {
			m.session = s
		}
	}
	return m, nil
}

func baseTableStyle() lipgloss.Style {
	return lipgloss.NewStyle().Padding(0, 1)
}

func (m *model) buildFolderTable() {
	columns := []table.Column{
		table.NewFlexColumn(columnKeyName, "Folder", 1),
		table.NewColumn(columnKeyCount, "#", 5),
	}

	rows := make([]table.Row, len(m.folders))
	for i, f := range m.folders {
		rows[i] = table.NewRow(table.RowData{
			columnKeyName:       f.Name,
			columnKeyCount:      f.Count,
			columnKeyFolderPath: f.Name,
		})
	}

	t := table.New(columns).
		WithRows(rows).
		Focused(true).
		WithPageSize(clamp(8, 30, len(m.folders))).
		WithBaseStyle(baseTableStyle()).
		HighlightStyle(lipgloss.NewStyle().
			Background(highlight).
			Foreground(lipgloss.Color("#FFFFFF"))).
		WithHeaderVisibility(true).
		WithFooterVisibility(false).
		WithPaginationWrapping(true)

	if m.width > 2 {
		t = t.WithTargetWidth(m.width - 2)
	}
	m.folderTable = t
}

func (m *model) buildSessionTable() {
	columns := []table.Column{
		table.NewColumn(columnKeyTS, "Timestamp", 20),
		table.NewFlexColumn(columnKeyTitle, "Session", 1),
		table.NewColumn(columnKeyModel, "Model", 15),
	}

	rows := make([]table.Row, len(m.sessions))
	for i, s := range m.sessions {
		ts := truncate(s.LastTS, 19)
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		safeTitle := strings.NewReplacer("\n", " ", "\r", "").Replace(title)

		rows[i] = table.NewRow(table.RowData{
			columnKeyTS:          ts,
			columnKeyTitle:       truncate(safeTitle, 70),
			columnKeyModel:       truncate(s.Model, 25),
			columnKeySessionPath: s.Path,
		})
	}

	t := table.New(columns).
		WithRows(rows).
		Focused(true).
		WithPageSize(clamp(5, 30, len(m.sessions))).
		WithBaseStyle(baseTableStyle()).
		HighlightStyle(lipgloss.NewStyle().
			Background(highlight).
			Foreground(lipgloss.Color("#FFFFFF"))).
		WithHeaderVisibility(true).
		WithFooterVisibility(false).
		WithPaginationWrapping(true)

	if m.width > 2 {
		t = t.WithTargetWidth(m.width - 2)
	}
	m.sessionTable = t
}

func clamp(min, max, val int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func (m model) View() string {
	if !m.ready {
		return "Loading\u2026"
	}
	if m.quitting {
		return ""
	}

	w := m.contentWidth()

	if m.view == foldersView {
		return m.foldersView(w)
	}
	return m.sessionsView(w)
}

func (m model) contentWidth() int {
	w := m.width - 2
	if w < 40 {
		w = 40
	}
	return w
}

func (m model) foldersView(w int) string {
	var b strings.Builder

	b.WriteString(header("Folders", w))
	b.WriteString("\n")
	b.WriteString(m.folderTable.View())
	b.WriteString("\n")
	b.WriteString(footer(len(m.folders), 0, w))

	return b.String()
}

func (m model) sessionsView(w int) string {
	var b strings.Builder

	folderName := ""
	if len(m.sessions) > 0 {
		folderName = m.sessions[0].Folder
	}
	b.WriteString(header(folderName, w))
	b.WriteString("\n")
	b.WriteString(m.sessionTable.View())

	if m.session != nil {
		b.WriteString("\n")
		b.WriteString(buildPreview(*m.session))
	}

	b.WriteString("\n")
	b.WriteString(footer(len(m.sessions), 1, w))

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

func header(title string, width int) string {
	w := width
	if w < 20 {
		w = 20
	}
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

func footer(count, viewType, width int) string {
	var hints string
	if viewType == 0 {
		hints = "\u2191\u2193 navigate  \u23ce open folder  q/ctrl+c quit"
	} else {
		hints = "\u2191\u2193 navigate  \u23ce resume  \u2190 back  space preview  q quit"
	}
	f := fmt.Sprintf(" %d session", count)
	if count != 1 {
		f += "s"
	}
	padLen := width - lipgloss.Width(f) - lipgloss.Width(hints) - 2
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
