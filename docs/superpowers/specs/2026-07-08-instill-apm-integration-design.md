# instill + APM Integration Design

**Date:** 2026-07-08
**Status:** Approved
**Scope:** Merge instill (skill management) and graft (MCP management) into a single CLI that uses Microsoft APM as its resolution/sync engine.

## Summary

instill becomes a curated UX layer over APM. It owns the **library catalog** (per-type CSV files + content) and the **interactive picker TUI**. When the user selects items, instill writes `apm.yml` and shells out to the `apm` CLI for resolution, lockfile management, security scanning, and harness targeting. graft is archived; its concepts are absorbed into instill.

## Architecture

```
┌─────────────────────────────────────────────┐
│  instill CLI                                │
│  ┌────────┐  ┌────────┐  ┌───────────────┐ │
│  │ Picker │  │ Scan/  │  │ Bootstrap/    │ │
│  │  TUI   │  │ Import │  │ Version Check │ │
│  └────┬───┘  └────┬───┘  └───────┬───────┘ │
│       │            │              │         │
│       ▼            ▼              ▼         │
│  ┌─────────────────────────────────────┐    │
│  │  Library (catalog.csv × 4 + content)│    │
│  └──────────────────┬──────────────────┘    │
│                     │ writes                │
│                     ▼                       │
│            ┌──────────────┐                 │
│            │   apm.yml    │                 │
│            └──────┬───────┘                 │
└───────────────────┼─────────────────────────┘
                    │ apm install / compile
                    ▼
            ┌──────────────┐
            │  APM engine  │ (brew-installed)
            └──────────────┘
```

## Command Surface

| Command | Purpose | APM interaction |
|---------|---------|-----------------|
| `instill init` | Initialize a project — creates `apm.yml` if missing, opens picker TUI | Writes `apm.yml`, calls `apm install` |
| `instill pick` | Unified picker TUI — browse all 4 primitive types, add/remove | Writes `apm.yml`, calls `apm install` / `apm prune` |
| `instill sync` | Reconcile project state with manifest | Calls `apm install` + `apm compile` |
| `instill status` | Show installed items, detect drift | Reads `apm.yml` + `apm.lock.yaml`, compares to library |
| `instill library scan` | Auto-discover entries from disk, rebuild catalog CSVs | None |
| `instill library add` | Manually add an entry to a catalog | None |
| `instill library show` | Browse library contents (non-interactive) | None |
| `instill import` | Import from existing configs (claude, graft, old instill) | Writes catalogs + `apm.yml` |
| `instill bootstrap` | Ensure APM installed via brew, check version | `brew install` / `brew upgrade` |
| `instill add-hooks` | Register SessionStart hook for auto-sync | None |

## Library Structure

The library lives at a single user-configured directory (stored in `~/.config/instill/config.json`, overridable via `INSTILL_LIBRARY_PATH` env var).

```
~/my-library/
├── skills/
│   ├── catalog.csv
│   ├── golang-testing/
│   │   └── SKILL.md
│   ├── cloud/azure/azure-cli/
│   │   └── SKILL.md
│   └── superpowers/brainstorming/
│       └── SKILL.md
├── mcp/
│   ├── catalog.csv
│   ├── local-db/
│   │   └── config.json
│   └── docs-search/
│       └── config.json
├── instructions/
│   ├── catalog.csv
│   └── python-rules/
│       └── INSTRUCTION.md
└── prompts/
    ├── catalog.csv
    └── debug/
        └── PROMPT.md
```

### Catalog Schemas

Each type gets a CSV with columns tailored to its domain.

**`skills/catalog.csv`:**

| Column | Required | Description |
|--------|----------|-------------|
| name | yes | Skill identifier (slash-separated path) |
| category | no | Derived from folder path on scan |
| path | yes | Relative path to SKILL.md from `skills/` dir |
| description | no | Human-readable summary |

**`mcp/catalog.csv`:**

| Column | Required | Description |
|--------|----------|-------------|
| name | yes | MCP server identifier |
| transport | yes | `stdio`, `sse`, or `http` |
| command | conditional | Required for stdio |
| args | no | Command arguments |
| url | conditional | Required for sse/http |
| env | no | Comma-separated `KEY=${VAR}` pairs |
| description | no | Human-readable summary |

**`instructions/catalog.csv`:**

| Column | Required | Description |
|--------|----------|-------------|
| name | yes | Instruction identifier |
| apply_to | no | Glob pattern for file targeting |
| path | yes | Relative path to INSTRUCTION.md |
| description | no | Human-readable summary |

**`prompts/catalog.csv`:**

| Column | Required | Description |
|--------|----------|-------------|
| name | yes | Prompt identifier (becomes slash command) |
| path | yes | Relative path to PROMPT.md |
| description | no | Human-readable summary |

### Catalog Authoring

Three mechanisms populate catalogs:

1. **Auto-scan** (`instill library scan`) — walks the library directory tree, discovers content by marker files (`SKILL.md`, `INSTRUCTION.md`, `PROMPT.md`, `config.json` for MCP), and regenerates CSVs. Manual additions are preserved if content still exists on disk.
2. **CLI add** (`instill library add --type=mcp --name=foo ...`) — appends a single row.
3. **Import** (`instill import ...`) — bulk-populates from existing systems.

The CSV is authoritative — `library scan` can regenerate it from disk, but the CSV is what the picker reads. During scan, entries whose content file no longer exists on disk are removed from the CSV and reported as `removed: <name> (content not found)`.

## APM Integration Layer

### Bootstrap

On commands that interact with APM (`init`, `pick`, `sync`, `status`, `import`), instill ensures APM is available. Library-only commands (`library scan`, `library add`, `library show`) skip this check.

1. Check `apm` on `$PATH`.
2. If missing: verify `brew` exists → `brew install apm` → verify. If the formula name changes, the constant `APM_BREW_FORMULA` in source is updated.
3. If present: check `apm --version` >= `MIN_APM_VERSION` (a constant in instill source).
4. If outdated: `brew upgrade $APM_BREW_FORMULA`.
5. If `brew` itself is missing: exit 2 with `"error: brew required to install apm; install from https://brew.sh"`.
6. Proceed with command.

`MIN_APM_VERSION` is bumped when instill relies on new APM features.

### Picker → APM Mapping

When the user confirms selections in the TUI:

| Library type | APM mechanism | What instill writes |
|---|---|---|
| Skill | Local path dependency | `dependencies.apm: ["<library-path>/skills/<name>"]` |
| MCP | mcp dependency block | `dependencies.mcp: [{name, command, args, env, url}]` |
| Instruction | `.apm/instructions/` file | Copies (not symlinks) markdown into `.apm/instructions/<name>.instructions.md` |
| Prompt | `.apm/prompts/` file | Copies (not symlinks) markdown into `.apm/prompts/<name>.prompt.md` |

Copies are used rather than symlinks so that `apm compile` operates on stable content and the project remains portable (no dependency on the library path at runtime).

After writing, instill calls `apm install` to resolve and sync.

### `instill sync`

```
apm install     → resolve deps, update lockfile, write harness files
apm compile     → compile .apm/ local content into harness targets
report          → "ok: synced N skills, M mcp servers, P instructions, Q prompts"
```

### SessionStart Hook

`instill add-hooks` registers `instill sync` as a Claude Code SessionStart hook in `.claude/settings.json`. This replaces the current `instill check-skills` hook.

### Drift Detection (`instill status`)

Compares `apm.yml` + `apm.lock.yaml` against library catalogs and reports:

- Items in project but removed from library
- Items in library not yet in project (available to add)
- Content hash mismatches between lock and current library state

Informational only — `instill sync` resolves drift.

## Picker TUI

Bubbletea-based, four-type unified picker.

### Navigation

Top level shows primitive types with counts:

```
▶ Skills (47 available, 12 installed)
  MCP Servers (8 available, 3 installed)
  Instructions (5 available, 2 installed)
  Prompts (9 available, 4 installed)
```

Drilling into a type shows the category tree with checkboxes:

```
Skills > cloud > azure
  [x] azure-cli
  [ ] azure-compute
  [ ] azure-storage
```

### Behaviors

- Pre-checked items = already in `apm.yml`. Unchecking queues removal.
- Category folders are navigable, not selectable.
- Type-to-filter narrows entries by name substring.
- `a` key applies — shows add/remove summary, confirms, calls APM.
- `instill pick --type=mcp` — jump to a specific type.
- `instill pick <name>...` — non-interactive add.
- `instill pick --remove <name>...` — non-interactive remove.

## Migration & Import

### `instill import old-instill`

- Reads `skill-manifest.json` from current project.
- Maps each skill to a library catalog entry.
- Writes entries into `apm.yml` as local-path dependencies.
- Removes `skill-manifest.json`, `.claude/skills/` symlinks, managed `settings.local.json` permissions.

### `instill import graft`

- Reads `graft.lock` and registered graft libraries.
- Adds MCP definitions to `mcp/catalog.csv`.
- Adds them to current project's `apm.yml`.
- Removes `graft.lock`, graft-managed `.mcp.json`.

### `instill import claude`

- Reads `~/.claude.json` or `$CLAUDE_CONFIG_DIR/claude.json`.
- Extracts global and project-scoped MCP definitions.
- Redacts secrets into `${VAR}` placeholders.
- Populates `mcp/catalog.csv`.

### `instill import directory <path>`

- Scans arbitrary directory for content (marker files, JSON configs, markdown).
- Populates appropriate catalogs.
- General-purpose onboarding for existing content.

## What Gets Deleted

### From instill (current codebase):

- `reconcile.go` — APM handles file placement
- `settings_local.go` — APM compile handles permissions
- `manifest.go` — `apm.yml` replaces `skill-manifest.json`
- `library.go` — CSV catalog replaces directory walking
- `categories.go` / `categorize.go` — folder structure + CSV replaces `.categories.json`
- All symlink logic — APM copies/compiles content

### From graft (archived):

- Entire repo archived after migration
- Concepts absorbed: paste-to-add → `instill library add`, drift → `instill status`, hooks → `instill add-hooks`, TUI → `instill pick`, `graft.lock` → `apm.lock.yaml`

### Retained from current instill:

- Cobra command structure (`internal/cli/`)
- Bubbletea TUI (generalized for 4 types)
- Config resolution (`config.go`) — adapted for new library structure
- `ExitError` pattern (exit codes 0/1/2/3)
- Atomic file writes
- `commandConfig` injection for testability
- SessionStart hook registration

## Per-Project File Changes

| Before | After |
|--------|-------|
| `.claude/skill-manifest.json` | `apm.yml` (project root) |
| `.claude/skills/` (symlinks) | Removed — APM places content |
| `.agents/skills/` (symlinks) | Removed — APM targets codex natively |
| `.claude/settings.local.json` (managed perms) | APM manages via compile |
| `graft.lock` | `apm.lock.yaml` |
| `.mcp.json` (graft-written) | `.mcp.json` (APM-written) |

## Deliverables

- Unified `instill` CLI binary (Go, Cobra + Bubbletea)
- `SKILL.md` for instill itself (ships in repo, installable into any library)
- Migration commands for seamless transition from old instill + graft
- Updated CI (test, lint, bats, cross-build)
- Updated `CLAUDE.md` and `README.md`
- graft repo archived with pointer to instill

## Non-Goals

- instill does NOT reimplement APM's resolution logic, security scanning, or harness rendering.
- instill does NOT manage remote APM packages directly — use `apm install <ref>` for those.
- instill does NOT provide a package registry or publishing mechanism.
