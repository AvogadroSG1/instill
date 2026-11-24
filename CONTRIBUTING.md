# Contributing to instill

## Prerequisites

- Go matching the version required by `go.mod` (`go version`)
- [mise](https://mise.jdx.dev/getting-started.html) (verified with `2026.11.2`)
- [bats](https://bats-core.readthedocs.io/) or `npx` (for integration tests)
- [golangci-lint](https://golangci-lint.run/) v2.6.2 (run via `mise run lint` — no separate install needed)

## Setup

```bash
git clone https://github.com/AvogadroSg1/instill
cd instill
mise run build    # verify the build
mise run test     # run unit + integration tests
```

## Development Workflow

Run `mise tasks` to list available tasks, or bare `mise run` to build (`build` is aliased as `default`). The ported tasks require a POSIX shell; native Windows continues to use `build.ps1`.

```bash
mise run build        # compile binary to ./instill (default task)
mise run unit-test    # go test ./...
mise run bats-test    # integration tests (requires bats or npx)
mise run test         # unit + bats
mise run vet          # go vet ./...
mise run lint         # golangci-lint
mise run install      # build and install binary into ${PREFIX}/bin/${BINARY}
mise run clean        # remove dist and instill build outputs
```

Configure task overrides via environment variables before `mise run` (for example, `BINARY=custom mise run build`, not `mise run build BINARY=custom`):

- `BINARY` (default: `instill`) — output binary name for `build` and `install`
- `PREFIX` (default: `$HOME/.local`) — install prefix for `install` (`${PREFIX}/bin/${BINARY}`)
- `GOFLAGS` (default: unset) — Go toolchain flags consumed natively by `go`
- `GOLANGCI_LINT_VERSION` (default: `v2.6.2`) — golangci-lint module version for `lint`
- `BATS_VERSION` (default: `1.12.0`) — Bats version for the `npx` fallback in `bats-test`

```bash
BINARY=instill-dev GOFLAGS=-trimpath mise run build
PREFIX="$HOME/.local" mise run install
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
- Tests must pass before merge: `mise run test`.
- Follow existing code style; run `mise run lint` and `mise run vet` before opening a PR.
- Exit codes are the spec contract — use `NewExitError(ExitXxx, "error: ...")`, never `os.Exit` directly.
- All manifest writes go through `WriteManifestAtomic`; skill names must pass `IsValidSkillName`.
