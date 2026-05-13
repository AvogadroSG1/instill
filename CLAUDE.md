# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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
make bats-test       # integration tests via bats (requires bats or npx)
make test            # unit + bats
make lint            # golangci-lint (v2.6.2)
make vet             # go vet
make install         # install to ~/.local/bin/instill
```

Run a single Go test package: `go test ./internal/instill/...`
Run a single bats test file: `bats test/instill.bats`

## Architecture

`instill` is a Go CLI built with Cobra + Bubbletea. The module is `github.com/AvogadroSG1/instill`.

**Two packages:**

- `internal/instill/` — pure domain logic; all functions accept explicit paths/writers; no direct `os.Std*` usage
- `internal/cli/` — Cobra command wiring; each command receives a `commandConfig` (stdin/stdout/stderr/isTTY/cwd) and passes it into the domain layer

**Key domain concepts:**

| Type | File | Purpose |
|------|------|---------|
| `Project` | `project.go` | Root + manifest path + symlink dir; discovered by walking up from cwd via `FindProject` |
| `Manifest` | `manifest.go` | `{"skills": [...]}` — always written atomically, always normalized (deduped + sorted) |
| `Library` | `library.go` | Directory of skill subdirs each containing `SKILL.md` |
| `ExitError` | (error type) | Carries exit code (0/1/2/3); `cli/root.go` extracts code via `ExitCode()` and message via `ErrorMessage()` |

**Config resolution order** (`config.go:ResolveLibraryPath`):
1. `SKILL_LIBRARY_PATH` env var
2. `~/.config/instill/config.json`
3. TTY prompt → writes config
4. Exit 2 (no TTY)

**Reconcile flow** (`reconcile.go:ReconcileManifest`):
1. Remove symlinks in `.claude/skills/` not in manifest
2. Remove manifest entries whose library directory no longer exists (prints `removed: <name>`)
3. Create symlinks for manifest entries that are missing
4. Rewrite manifest atomically if it changed

**Atomic writes:** All manifest and config writes use `writeFileAtomic` (write `.tmp`, rename) to prevent partial-write corruption.

## Conventions

- All manifest writes use `WriteManifestAtomic`; skill names are always validated with `IsValidSkillName` and normalized (deduped + sorted) before writing.
- Commands must be testable without real I/O — pass `commandConfig` with injected stdin/stdout/stderr, never `os.Std*` directly.
- Exit codes are the spec contract (0/1/2/3); use `NewExitError(ExitXxx, "error: ...")` in domain code, never `os.Exit` directly.
- The TUI (`pick-skills` interactive mode) uses Bubbletea and is injected via `commandConfig.pickSkillsTUI` for testability.
- `internal/instill` tests use `t.TempDir()` for isolation; bats tests set `SKILL_LIBRARY_PATH` to a temp dir and build a fresh binary in `setup_file`.
