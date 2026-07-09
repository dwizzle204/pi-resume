// pi-resume — fzf-based pi session picker with folder-tree navigation.
//
// Cache: SQLite under ~/.local/share/pi-resume/sessions.db
// TUI:   fzf subprocess for folders → sessions two-level picker
//
// Usage:
//   pi-resume          interactive picker
//   pi-resume refresh  rebuild cache from disk
//   pi-resume list     dump all sessions

package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

type Session struct {
	Folder  string
	CWD     string
	Model   string
	Title   string
	Path    string
	SID     string
	LastTS  string
	Created string
	MTime   int64
}

type FolderInfo struct {
	Name      string
	Count     int
	LastTS    string
	LastTitle string
	LastModel string
	LastPath  string
	LastCWD   string
}

func dataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "pi-resume")
}

func dbPath() string {
	return filepath.Join(dataDir(), "sessions.db")
}

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
    folder  TEXT NOT NULL,
    cwd     TEXT NOT NULL DEFAULT '',
    model   TEXT NOT NULL DEFAULT '',
    title   TEXT NOT NULL DEFAULT '',
    path    TEXT NOT NULL PRIMARY KEY,
    sid     TEXT NOT NULL DEFAULT '',
    last_ts TEXT NOT NULL DEFAULT '',
    created TEXT NOT NULL DEFAULT '',
    mtime   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_folder ON sessions(folder);
CREATE INDEX IF NOT EXISTS idx_mtime  ON sessions(mtime);
`

func openDB() (*sql.DB, error) {
	os.MkdirAll(dataDir(), 0755)
	db, err := sql.Open("sqlite", dbPath()+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func refresh() error {
	sessionsDir := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return fmt.Errorf("reading sessions dir: %w", err)
	}

	var all []Session
	for _, de := range entries {
		if !de.IsDir() || !strings.HasPrefix(de.Name(), "--") {
			continue
		}
		projPath := filepath.Join(sessionsDir, de.Name())
		files, _ := os.ReadDir(projPath)
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			s, err := parseFile(filepath.Join(projPath, f.Name()))
			if err != nil {
				continue
			}
			all = append(all, s)
		}
	}

	sort.Slice(all, func(i, j int) bool { return all[i].MTime > all[j].MTime })

	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec("DELETE FROM sessions"); err != nil {
		return err
	}

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT INTO sessions
		(folder,cwd,model,title,path,sid,last_ts,created,mtime)
		VALUES (?,?,?,?,?,?,?,?,?)`)

	for _, s := range all {
		stmt.Exec(s.Folder, s.CWD, s.Model, s.Title, s.Path, s.SID, s.LastTS, s.Created, s.MTime)
	}
	tx.Commit()

	seen := map[string]int{}
	for _, s := range all {
		seen[s.Folder]++
	}
	fmt.Printf("Refreshed: %d sessions from %d folders\n", len(all), len(seen))
	return nil
}

func parseFile(fpath string) (Session, error) {
	var s Session
	s.Path = fpath
	fi, err := os.Stat(fpath)
	if err != nil {
		return s, err
	}
	s.MTime = fi.ModTime().Unix()

	f, _ := os.Open(fpath)
	defer f.Close()

	sc := bufio.NewScanner(f)
	buf := make([]byte, 8192)
	sc.Buffer(buf, 8192)

	readHeader := false
	n := 0
	for sc.Scan() && n < 200 {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		n++
		var raw map[string]any
		if json.Unmarshal([]byte(line), &raw) != nil {
			continue
		}
		typ, _ := raw["type"].(string)

		if !readHeader {
			if typ != "session" {
				continue
			}
			readHeader = true
			s.CWD, _ = raw["cwd"].(string)
			s.Created, _ = raw["timestamp"].(string)
			s.LastTS = s.Created
			s.SID, _ = raw["id"].(string)
			s.Folder = s.CWD
			if s.CWD == "" {
				// skip sidebar/ghost sessions with no cwd
				return s, fmt.Errorf("empty cwd")
			}
			continue
		}

		ts, _ := raw["timestamp"].(string)
		if ts != "" && ts > s.LastTS {
			s.LastTS = ts
		}

		switch typ {
		case "session_info":
			if name, ok := raw["name"].(string); ok && name != "" && s.Title == "" {
				s.Title = truncate(name, 100)
			}
		case "message":
			msg, _ := raw["message"].(map[string]any)
			if msg == nil {
				continue
			}
			role, _ := msg["role"].(string)
			if s.Title == "" && role == "user" {
				s.Title = extractTitle(msg)
			}
			if s.Model == "" && role == "assistant" {
				p, _ := msg["provider"].(string)
				m, _ := msg["model"].(string)
				if p != "" && m != "" {
					s.Model = p + "/" + m
				}
			}
		case "model_change":
			if s.Model == "" {
				p, _ := raw["provider"].(string)
				m, _ := raw["modelId"].(string)
				if p != "" && m != "" {
					s.Model = p + "/" + m
				}
			}
		}
	}
	return s, nil
}

func extractTitle(msg map[string]any) string {
	c, ok := msg["content"]
	if !ok {
		return ""
	}
	switch v := c.(type) {
	case string:
		return truncate(v, 100)
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok || m["type"] != "text" {
				continue
			}
			if txt, ok := m["text"].(string); ok {
				return truncate(strings.NewReplacer("\n", " ", "\r", "").Replace(txt), 100)
			}
		}
	}
	return ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\u2026"
}

func listFolders(db *sql.DB) ([]FolderInfo, error) {
	rows, err := db.Query(`
		SELECT s1.folder, s1.cnt, s1.last_ts, s.title, s.model, s.path, s.cwd
		FROM (SELECT folder, COUNT(*) cnt, MAX(mtime) max_mt, MAX(last_ts) last_ts
			FROM sessions WHERE folder != "" GROUP BY folder) s1
		JOIN sessions s ON s.mtime = s1.max_mt AND s.folder = s1.folder
		ORDER BY s1.last_ts DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ff []FolderInfo
	for rows.Next() {
		var f FolderInfo
		rows.Scan(&f.Name, &f.Count, &f.LastTS, &f.LastTitle, &f.LastModel, &f.LastPath, &f.LastCWD)
		ff = append(ff, f)
	}
	return ff, nil
}

func listSessions(db *sql.DB, folder string) ([]Session, error) {
	rows, err := db.Query(`SELECT folder,cwd,model,title,path,sid,last_ts,created,mtime
		FROM sessions WHERE folder=? ORDER BY last_ts DESC`, folder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ss []Session
	for rows.Next() {
		var s Session
		rows.Scan(&s.Folder, &s.CWD, &s.Model, &s.Title, &s.Path, &s.SID, &s.LastTS, &s.Created, &s.MTime)
		ss = append(ss, s)
	}
	return ss, nil
}

func safeTab(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", "").Replace(s)
}

func findSession(ss []Session, path string) *Session {
	for _, s := range ss {
		if s.Path == path {
			return &s
		}
	}
	return nil
}

func pickFolder(db *sql.DB) (string, error) {
	ff, err := listFolders(db)
	if err != nil {
		return "", err
	}
	if len(ff) == 0 {
		return "", nil
	}

	var lines []string
	for _, f := range ff {
		title := f.LastTitle
		if title == "" {
			title = "(untitled)"
		}
		lines = append(lines, fmt.Sprintf("▶\t%s\t%d\t%s\t%s\t%s\t%s\t%s",
			safeTab(f.Name), f.Count, safeTab(f.LastTS), safeTab(title),
			truncate(safeTab(f.LastModel), 25), safeTab(f.LastCWD), safeTab(f.LastPath)))
	}

	cmd := exec.Command("fzf",
		"--height=85%", "--layout=reverse", "--border",
		"--prompt=\u25b8 ",
		"--delimiter=\\t",
		"--with-nth=1,2,3",
		"--preview-label", " Folders ",
		"--preview-window=right:50%",
	)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return "", nil
	}

	line := strings.TrimSpace(string(out))
	parts := strings.Split(line, "\t")
	if len(parts) < 3 {
		return "", nil
	}
	return parts[1], nil
}

// Returns a bash snippet for the session preview.
// Uses jq with + concatenation (no \(...) to avoid quoting issues.
func sessionPreviewScript() string {
	return `fpath={4}
[ -n "$fpath" ] && [ -f "$fpath" ] || exit 0
echo "--- Details ---"
head -1 "$fpath" | jq -r '"Created  " + (.timestamp[0:19] // "?") + "\nCWD      " + (.cwd // "?") + "\nSession  " + (.id // "?")' 2>/dev/null
echo ""
head -200 "$fpath" | jq -r 'select(.type == "message" and .message.role == "user") | (.message.content | if type == "string" then .[0:300] else [.[] | select(.type == "text") | .text][0][0:300] // "" end)' 2>/dev/null | head -5
echo ""
wc -l < "$fpath" | tr -d " \n"
echo " entries, $(du -h "$fpath" | cut -f1)"`
}

func pickSession(db *sql.DB, folder string) (*Session, error) {
	ss, err := listSessions(db, folder)
	if err != nil {
		return nil, err
	}
	if len(ss) == 0 {
		return nil, nil
	}

	var lines []string
	for _, s := range ss {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s",
			truncate(safeTab(s.LastTS), 19), safeTab(title),
			truncate(safeTab(s.Model), 25), s.Path, s.SID, s.CWD))
	}

	cmd := exec.Command("fzf",
		"--height=85%", "--layout=reverse", "--border",
		fmt.Sprintf("--prompt=\u25b8 %s > ", folder),
		"--delimiter=\\t",
		"--with-nth=1,2,3",
		"--preview", sessionPreviewScript(),
		fmt.Sprintf("--preview-label= %s ", folder),
		"--preview-window=right:55%",
		"--bind", "left:abort",
		"--bind", "esc:abort",
	)
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	line := strings.TrimSpace(string(out))
	parts := strings.Split(line, "\t")
	if len(parts) < 4 {
		return nil, nil
	}

	return findSession(ss, parts[3]), nil
}

func resume(s *Session) error {
	if s.CWD != "" {
		os.Chdir(s.CWD)
	}
	cmd := exec.Command("pi", "--session", s.Path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

func listAll(db *sql.DB) error {
	ff, err := listFolders(db)
	if err != nil {
		return err
	}
	for _, f := range ff {
		fmt.Printf("\u25b6 %s  (%d)\n", f.Name, f.Count)
		ss, err := listSessions(db, f.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing %s: %v\n", f.Name, err)
			continue
		}
		for _, s := range ss {
			fmt.Printf("  %s  %s  %s\n",
				truncate(s.LastTS, 19), truncate(s.Title, 50), truncate(s.Model, 25))
		}
	}
	return nil
}

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) > 1 && os.Args[1] == "refresh" {
		if err := refresh(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	db, err := openDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "DB error: %v\n", err)
		return 1
	}
	defer db.Close()

	if len(os.Args) > 1 && os.Args[1] == "list" {
		if err := listAll(db); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		return 0
	}

	var cnt int
	db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&cnt)
	if cnt == 0 {
		fmt.Println("Scanning sessions...")
		if err := refresh(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		db.Close()
		db, err = openDB()
		if err != nil {
			fmt.Fprintf(os.Stderr, "DB error: %v\n", err)
			return 1
		}
		defer db.Close()
	}

	for {
		folder, err := pickFolder(db)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		if folder == "" {
			return 0
		}

		s, err := pickSession(db, folder)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		if s == nil {
			continue
		}

		if err := resume(s); err != nil {
			fmt.Fprintf(os.Stderr, "pi exited: %v\n", err)
		}
		return 0
	}
}
