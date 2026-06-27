# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`instill` is a Go CLI that curates a **project-specific skill library** for Claude Code and
other AI coding agents. It keeps a committed manifest of the skills a project needs, creates
symlinks so agents can discover them, grants local agent permissions, and wires a
`SessionStart` hook that reconciles all of that automatically each time a session opens.

The module is `github.com/AvogadroSG1/instill`. Built with **Cobra** (command tree) +
**Bubbletea** (interactive picker TUI).

<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->

## Build & Test

```bash
make build           # build binary to ./instill
make unit-test       # go test ./...
make bats-test       # integration tests via bats (falls back to npx bats if not installed)
make test            # unit + bats
make lint            # golangci-lint (pinned v2.6.2, run via `go run`)
make vet             # go vet ./...
make install         # install to ~/.local/bin/instill (PREFIX overridable)
make clean           # remove dist/ and ./instill
```

Run a single Go test package: `go test ./internal/instill/...`
Run a single bats test file: `bats test/instill.bats`

CI (`.github/workflows/ci.yml`) runs on push/PR to `main`: `go test`, `go test -race`,
`go vet`, `make lint`, `bats`, a `go mod tidy` cleanliness check, goreleaser config
validation, and a cross-build matrix (darwin/linux × amd64/arm64). Keep all of these green.

## Architecture

**Two packages, strict layering:**

- `internal/instill/` — pure domain logic. Every function takes explicit paths and
  `io.Writer`s; **no direct `os.Std*` usage**. Fully testable without a real terminal or
  filesystem (`t.TempDir()` isolation).
- `internal/cli/` — Cobra command wiring. Each command receives a `commandConfig`
  (stdin/stdout/stderr/args/cwd/isTTY/pickSkillsTUI) and passes it into the domain layer.
  `main.go` calls `cli.Execute()`, which returns the process exit code.

**Commands** (registered in `internal/cli/root.go`):

| Command | Domain entry | Purpose |
|---------|--------------|---------|
| `instill init` | `InitProject` | Create manifest + symlink dirs + `.gitignore` entries; launches TUI picker unless `--skills` is given (`--force` only allows overwriting an existing manifest, it does not make `init` headless) |
| `instill pick-skills [name...]` | `PickSkills` / `RunPickSkillsTUI` | Add/remove skills by name, `--remove`, or interactive TUI |
| `instill check-skills` | `ReconcileManifest` | Reconcile symlinks + local permissions with the manifest (the hook target) |
| `instill show-library` | `ShowLibrary` | List library skills; `--filter` substring, `--category` prefix |
| `instill categorize` | `CategorizeLibrary` | Create/update the library `.categories.json` registry |
| `instill add-hooks` | `AddHooks` | Register `instill check-skills` as a Claude Code `SessionStart` hook |

Note: the `init` command was formerly `init-project` — it is now just `init`.

**Key domain concepts:**

| Type / file | Purpose |
|-------------|---------|
| `Project` (`project.go`) | Root + manifest path + two symlink dirs (`.claude/skills`, `.agents/skills`); discovered by walking up from cwd via `FindProject` |
| `Manifest` (`manifest.go`) | `{"skills": [...]}` — always written atomically, always normalized (deduped + sorted), every name validated by `IsValidSkillName` |
| `Library` (`library.go`) | Directory of skill dirs, each a leaf containing `SKILL.md`; walked to arbitrary depth |
| `ExitError` (`errors.go`) | Carries exit code (0/1/2/3); `cli/root.go` extracts code via `ExitCode()` and message via `ErrorMessage()` |

## Layout on disk

```
~/.config/instill/config.json       ← library path (or SKILL_LIBRARY_PATH env var)
~/skills/                            ← the Library (developer-local, not committed)
  golang-testing/SKILL.md            ← flat skill
  cloud/azure/azure-cli/SKILL.md     ← nested skill (arbitrary depth)
  .categories.json                   ← optional category registry

your-project/
  .claude/
    skill-manifest.json              ← COMMITTED: {"skills": ["golang-testing", "cloud/azure/azure-cli"]}
    settings.json                    ← committed: holds the SessionStart hook (add-hooks)
    settings.local.json              ← gitignored: local Skill(...) permissions
    skills/                          ← gitignored: symlinks managed by instill (Claude Code)
      golang-testing -> ~/skills/golang-testing
      cloud:azure:azure-cli -> ~/skills/cloud/azure/azure-cli
  .agents/
    skills/                          ← gitignored: same symlinks for OpenAI Codex
```

### Skill names, nesting, and link names

- A skill name is a safe relative path of one or more slash-separated segments
  (`docker`, `superpowers/brainstorming`, `cloud/azure/azure-cli`). `IsValidSkillName`
  rejects empty, absolute, backslash, `.`, or `..` segments.
- `ListLibrarySkills` walks the library to arbitrary depth (capped at `maxSkillDepth = 32`
  to defeat symlink cycles): **any directory containing `SKILL.md` is a leaf skill**; a
  directory without one is a category node and is recursed into.
- Symlink filenames are **flattened**: slashes become colons (`cloud/azure/azure-cli` →
  `cloud:azure:azure-cli`) via `skillLinkName`, so the skills dir stays flat. Older
  group-dir layouts (`group/leaf` symlinks) are still recognized when scanning existing links.

## Config resolution order (`config.go:ResolveLibraryPath`)

1. `SKILL_LIBRARY_PATH` env var
2. `~/.config/instill/config.json` (`{"library_path": "..."}`)
3. Interactive TTY prompt → writes config (default `~/ObsidianNotes/agent_config/skills`)
4. Exit 2 if no TTY

`~`-prefixed paths are expanded; the resolved path must be an existing directory or exit 2.

## Reconcile flow (`reconcile.go:ReconcileManifestWithPrevious`)

This is the heart of the tool. `ReconcileManifest` calls it with `previous == current`;
`PickSkills`/`ApplySkillSelection` pass the **previous** manifest so removed permissions can
be revoked.

1. Ensure `.claude/`, `.claude/skills/`, `.agents/`, and `.agents/skills/` are real
   directories (refuse to write through symlinks). `FindProject`/`InitProject` always
   populate the `.agents` paths, so the `.agents` dirs are reconciled on every run.
2. Drop manifest entries whose library skill no longer exists (prints
   `removed: <name> (no longer in library)`).
3. Reconcile `.claude/skills/` (primary, full output) then `.agents/skills/` (silent):
   remove orphan symlinks not in the selected set, create/repair symlinks for current skills.
4. Rewrite the manifest atomically if it changed.
5. Reconcile `.claude/settings.local.json` permissions (see below). `.agents` has no
   permissions equivalent.
6. Print `ok: N skills linked` if symlinks or the manifest changed. (Permission-only
   writes to `settings.local.json` do not flip the `changed` flag, so a reconcile that
   only adjusts permissions prints nothing.)

## Permission ownership boundary (`settings_local.go`)

instill writes `"Skill(<linkName>)"` entries to `permissions.allow` in
`.claude/settings.local.json`. **The manifest is the ownership boundary**
(`docs/adr/0001-manifest-as-permission-ownership-boundary.md`): instill only adds or removes
a permission for a skill that is (or was) in the manifest. A `Skill(...)` entry the developer
added manually for a skill *not* in the manifest is left untouched across reconciles. The
legacy slash form `Skill(group/leaf)` is recognized so it can be migrated to the colon form
on the next run.

## Category registry (`categories.go`, `categorize.go`)

Optional `.categories.json` at the library root maps category paths to skill-name lists.
`instill categorize` auto-assigns by name prefix (`golang-*` → `golang`, `azure-*` →
`cloud/azure`, `dd-*` → `datadog`, `k8s-*`/`docker` → `cloud`) and prints
`uncategorized: <skill>` for the rest. `check-skills` also warns about uncategorized skills
when the registry exists. Note: the **pick-skills TUI** derives its category tree directly
from the library folder structure (`buildCategoryTree` in `skill_picker.go`), independent of
`.categories.json`; the registry feeds `show-library --category` and the categorize/warn flows.

## Conventions

- **Atomic writes only.** All manifest, config, categories, and settings writes go through
  `writeFileAtomic` (`atomic.go`): write a unique `.tmp` in the same dir, chmod, rename.
- **Normalize before writing.** Skill lists are always deduped + sorted (`normalizeSkills`)
  and validated (`IsValidSkillName`) before being persisted.
- **Exit codes are the spec contract** (0 success, 1 general, 2 environment, 3 filesystem).
  Use `NewExitError(ExitXxx, "error: ...")` in domain code — never `os.Exit` directly. Error
  messages are lowercase and prefixed `error:` (or `warning:`).
- **No real I/O in tests.** Commands must be drivable with an injected `commandConfig`
  (stdin/stdout/stderr/cwd/isTTY); never reach for `os.Std*` inside a command. The TUI is
  injected via `commandConfig.pickSkillsTUI` so tests can stub it.
- **Refuse to write through symlinks** for `.claude`, `.agents`, `.gitignore`,
  `settings.json`, and `settings.local.json` — always `Lstat` and check `ModeSymlink` first.
- **Keep `.claude/skills/` and `.agents/skills/` symmetric.** Anything that creates or
  removes a symlink must do so for both dirs (Claude Code + OpenAI Codex).
- **Tests:** `internal/instill` tests use `t.TempDir()` for isolation; bats tests set
  `SKILL_LIBRARY_PATH` to a temp dir and build a fresh binary in `setup_file`.

## Related docs

- `README.md` — user-facing install/usage
- `CONTEXT.md` — domain glossary (Skill, Library, Manifest, Project, Reconcile, Agent permission)
- `docs/adr/0001-manifest-as-permission-ownership-boundary.md` — why the manifest bounds permissions
- `CHANGELOG.md` — notable changes; `AGENTS.md` — agent/beads quick reference
