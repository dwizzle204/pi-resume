# Changelog

## 0.2.0 (2026-07-09)

- Native Bubble Tea TUI — no more fzf or jq dependency
- Split into three files: main.go, session.go, tui.go
- Inline preview pane for selected session
- Fixed Windows console support (no cmd /c wrapper needed)
- Removed list/refresh subcommands (always auto-refresh)
- Single SQLite connection throughout

## 0.1.0 (2026-07-09)

- Initial release
- Two-level fzf picker: folders → sessions
- SQLite cache with WAL mode
- Session preview with metadata and recent user messages
