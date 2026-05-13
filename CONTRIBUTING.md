# Contributing to instill

## Prerequisites

- Go 1.22+ (`go version`)
- [bats](https://bats-core.readthedocs.io/) or `npx` (for integration tests)
- [golangci-lint](https://golangci-lint.run/) v2.6.2 (run via `make lint` — no separate install needed)

## Setup

```bash
git clone https://github.com/AvogadroSg1/instill
cd instill
make build        # verify the build
make test         # run unit + integration tests
```

## Development Workflow

```bash
make build        # compile binary to ./instill
make unit-test    # go test ./...
make bats-test    # integration tests (requires bats or npx)
make test         # unit + bats
make vet          # go vet
make lint         # golangci-lint
```

Run a single package: `go test ./internal/instill/...`

Run a single bats file: `bats test/instill.bats`

## Architecture

Two packages:

- `internal/instill/` — pure domain logic; all functions accept explicit paths and writers; no direct `os.Std*` usage
- `internal/cli/` — Cobra command wiring; each command receives a `commandConfig` (stdin/stdout/stderr/isTTY/cwd) and passes it into the domain layer

The TUI is injected via `commandConfig.pickSkillsTUI` for testability. Tests use
`t.TempDir()` for isolation and set `SKILL_LIBRARY_PATH` to a temp dir.

## Pull Request Guidelines

- Keep PRs focused and under 300 lines of diff when possible.
- Tests must pass before merge: `make test`.
- Follow existing code style; run `make lint` and `make vet` before opening a PR.
- Exit codes are the spec contract — use `NewExitError(ExitXxx, "error: ...")`, never `os.Exit` directly.
- All manifest writes go through `WriteManifestAtomic`; skill names must pass `IsValidSkillName`.
