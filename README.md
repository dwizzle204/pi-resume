# pi-resume

A native terminal UI for browsing and resuming [pi](https://github.com/obra/pi) coding agent sessions, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Features

- **Folder view** — sessions grouped by project directory (CWD), fuzzy-filterable
- **Session view** — drill into a folder; preview pane shows metadata + recent messages
- **Teleport** — select a session to resume it via `pi --session` in its original working directory
- **Auto-refresh** — always up to date; no manual refresh needed

## Usage

```
pi-resume
```

**Folder view:**
| Key | Action |
|---|---|
| `↑`/`↓` or `k`/`j` | Navigate |
| `Enter` or `→` or `l` | Open folder |
| `q` or `Ctrl+C` | Quit |

**Session view:**
| Key | Action |
|---|---|
| `↑`/`↓` or `k`/`j` | Navigate |
| `Enter` | Resume selected session |
| `Space` | Toggle preview below session |
| `←` or `Esc` | Back to folders |
| `q` or `Ctrl+C` | Quit |

## How it works

`pi-resume` scans `~/.pi/agent/sessions/` — where pi stores session files in folders named `--<path>--/<timestamp>_<uuid>.jsonl`. Each session file is parsed for its header (CWD, session ID, creation time) and first user message (for the title). Results are cached in a local SQLite database at `~/.local/share/pi-resume/sessions.db` and auto-refreshed on every launch.

The TUI is built entirely with **Bubble Tea** (Go) — no external dependencies, no subprocess invocation. Two views: a folder picker and a session picker with an inline preview panel.

## Requirements

- [pi](https://github.com/obra/pi) — the coding agent whose sessions are browsed

## Installation

```bash
go install github.com/dwizzle204/pi-resume@latest
```

Or build from source:

```bash
git clone https://github.com/dwizzle204/pi-resume.git
cd pi-resume
go build -o pi-resume .
```

## License

MIT
