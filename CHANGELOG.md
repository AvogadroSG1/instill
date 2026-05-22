# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- Interactive skill picker with category pane (`skill_picker.go`)
- `categories.go`: `LoadCategories`, `LoadCategoriesWithWarnings`, `CategoryForSkill` — reads `.categories.json` from the library root to group skills in the TUI
- `add-hooks` command: registers `instill check-skills` as a Claude Code `SessionStart` hook in `.claude/settings.json`

### Changed

- `init` now accepts `--skills` flag for headless initialization without launching the TUI
- Skill names are always normalized (deduped + sorted) before writing the manifest

## [0.1.0] - 2026-01-01

### Added

- `init` command: initialize `.claude/skill-manifest.json` and `.claude/skills/` symlink directory; adds `.claude/skills/` to `.gitignore` automatically
- `pick-skills` command: add or remove skills from the manifest; interactive TUI (Bubbletea) or headless via positional args / `--remove` flag
- `check-skills` command: reconcile `.claude/skills/` symlinks with the manifest; removes stale symlinks and creates missing ones
- `show-library` command: list all skills in the configured library; `--filter` flag for substring search; annotates selected skills when run inside a project
- Config resolution: `SKILL_LIBRARY_PATH` env var → `~/.config/instill/config.json` → interactive TTY prompt
- Atomic writes for all manifest and config files (write `.tmp`, rename)
- Exit codes: 0 success, 1 general, 2 environment, 3 filesystem
