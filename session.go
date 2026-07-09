package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
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

func refresh(db *sql.DB) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home dir: %w", err)
	}
	sessionsDir := filepath.Join(home, ".pi", "agent", "sessions")
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

	if _, err := db.Exec("DELETE FROM sessions"); err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO sessions
		(folder,cwd,model,title,path,sid,last_ts,created,mtime)
		VALUES (?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return fmt.Errorf("prepare stmt: %w", err)
	}

	for _, s := range all {
		if _, err := stmt.Exec(s.Folder, s.CWD, s.Model, s.Title, s.Path, s.SID, s.LastTS, s.Created, s.MTime); err != nil {
			return fmt.Errorf("insert %s: %w", s.Path, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

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

	f, err := os.Open(fpath)
	if err != nil {
		return s, err
	}
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
	return string(r[:n]) + "…"
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
