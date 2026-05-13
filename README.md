# instill

[![Go Version](https://img.shields.io/github/go-mod/go-version/AvogadroSg1/instill)](https://go.dev/)
[![License](https://img.shields.io/github/license/AvogadroSg1/instill)](./LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/AvogadroSg1/instill/test.yml?branch=main)](https://github.com/AvogadroSg1/instill/actions)

Curate and sync a project-specific skill library for Claude Code and other AI coding agents.

`instill` keeps a manifest of the skills your project needs, creates symlinks so
Claude Code can discover them, and wires a session hook that reconciles those
symlinks automatically every time you open a session.

## Getting Started

**Install:**

```bash
# From source
go install github.com/AvogadroSg1/instill@latest

# Or build locally
make install   # installs to ~/.local/bin/instill
```

**Configure your skill library** (one-time setup):

```bash
export SKILL_LIBRARY_PATH=~/path/to/skills   # or let instill prompt you
```

**Initialize a project:**

```bash
cd your-project
instill init-project        # launches interactive skill picker
instill init-project --skills golang-testing,golang-error-handling  # headless
```

**Result:**

```
initialized: .claude/skill-manifest.json
created:     .claude/skills/
updated:     .gitignore (+.claude/skills/)
created: golang-testing -> /home/user/skills/golang-testing
created: golang-error-handling -> /home/user/skills/golang-error-handling
ok: 2 skills linked
```

## How It Works

```
~/.config/instill/config.json       ← library path (or SKILL_LIBRARY_PATH)
~/skills/
  golang-testing/SKILL.md           ← skill definition
  golang-error-handling/SKILL.md
  ...

your-project/
  .claude/
    skill-manifest.json             ← committed to git: ["golang-testing", ...]
    skills/                         ← gitignored: symlinks managed by instill
      golang-testing -> ~/skills/golang-testing
      golang-error-handling -> ~/skills/golang-error-handling
```

`instill check-skills` reconciles the symlinks whenever the manifest changes.
Run `instill add-hooks` once to wire it as a Claude Code `SessionStart` hook so
reconciliation happens automatically.

## Commands

| Command | Description |
|---------|-------------|
| `instill init-project` | Initialize a manifest in the current project |
| `instill pick-skills [name...]` | Add or remove skills interactively or by name |
| `instill check-skills` | Reconcile symlinks with the manifest |
| `instill show-library` | List available skills in the configured library |
| `instill add-hooks` | Register `check-skills` as a Claude Code `SessionStart` hook |

### `init-project`

```bash
instill init-project                        # interactive TUI skill picker
instill init-project --skills foo,bar       # headless: add specific skills
instill init-project --force                # overwrite an existing manifest
```

### `pick-skills`

```bash
instill pick-skills                         # open interactive TUI
instill pick-skills foo bar                 # add skills by name
instill pick-skills --remove foo,bar        # remove skills
```

### `show-library`

```bash
instill show-library                        # list all skills
instill show-library --filter golang        # filter by substring
```

## Configuration

| Source | Precedence |
|--------|-----------|
| `SKILL_LIBRARY_PATH` environment variable | Highest |
| `~/.config/instill/config.json` | — |
| Interactive TTY prompt (first run) | Lowest |

`~/.config/instill/config.json` format:

```json
{
  "library_path": "~/ObsidianNotes/agent_config/skills"
}
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error (bad args, malformed manifest, unknown skill) |
| `2` | Environment error (library not found, no TTY) |
| `3` | Filesystem error (cannot read/write files) |

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

MIT — see [LICENSE](./LICENSE).
