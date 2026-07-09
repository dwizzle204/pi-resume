# pi-resume

A two-level fzf session picker for [pi](https://github.com/obra/pi) coding agent sessions.

## Features

- **Folder view** — browse sessions grouped by project directory (CWD)
- **Session view** — drill into a folder to see individual sessions with previews
- **Teleport** — select a session to resume it directly via `pi --session`

## Usage

```
pi-resume           Interactive picker
pi-resume list      List all sessions in the terminal
pi-resume refresh   Rebuild the session cache from disk
```

When launched interactively:

1. Pick a folder → press Enter (or right arrow)
2. Pick a session → press Enter
3. pi resumes the selected session in its original working directory

Use **Esc** or **Left Arrow** to go back from the session view to the folder view.

## How it works

`pi-resume` scans `~/.pi/agent/sessions/` — where pi stores session files in folders named `--<path>--/<timestamp>_<uuid>.jsonl`. Each session file is parsed for its header (CWD, session ID, creation time) and first user message (for the title). Results are cached in a local SQLite database at `~/.local/share/pi-resume/sessions.db`.

The TUI uses [fzf](https://github.com/junegunn/fzf) with a two-level design:

- **First fzf instance**: lists project folders with session counts
- **Second fzf instance**: lists individual sessions in the chosen folder, with a preview pane showing session metadata and recent user messages

## Requirements

- [pi](https://github.com/obra/pi) — the coding agent whose sessions are browsed
- [fzf](https://github.com/junegunn/fzf) — fuzzy finder for the interactive TUI
- [jq](https://jqlang.github.io/jq/) — JSON processing for the preview pane

## Installation

```bash
# From a release
curl -LO https://github.com/dwizzle204/pi-resume/releases/latest/download/pi-resume
chmod +x pi-resume
sudo mv pi-resume /usr/local/bin/

# Or build from source
go install github.com/dwizzle204/pi-resume@latest
```

## Building

```bash
git clone https://github.com/dwizzle204/pi-resume.git
cd pi-resume
go build -o pi-resume .
```

## License

MIT
