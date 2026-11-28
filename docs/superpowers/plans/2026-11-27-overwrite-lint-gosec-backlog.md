# Plan: Overwrite, Lint, and Gosec Backlog (`fix/overwrite-lint-gosec-backlog`)

## Scope
Complete tracked Beads issues `instill-0m5`, `instill-l7t`, `instill-bh9`, `instill-ekf` (P1), and `instill-f96` (P2) on branch `fix/overwrite-lint-gosec-backlog` with full local quality gates and executable E2E contracts against the built `instill` binary and real APM 0.39.0.

## Agreed Definitions & Contracts
- **Local Gate:** `git ls-files '*.go' | xargs gofmt -l` (empty), `mise run lint` (0 issues on `golangci-lint v2.15.0`), `go vet ./...`, `go test -count=1 ./...`, `bats test`, `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`, and `mise run contracts`.
- **Remote MCP Shapes:** Grounded in APM 0.39.0 `apm_cli/adapters/client/_remote_shapes.py` (recorded in ADR 0009).
- **Subagent Models (user-approved deviation):** `openai/gpt-6.2-sol` for both Implementer and Reviewer (requested `openai/gpt-6.1-terra:medium` unavailable in approved model list; `openai/gpt-6.2-sol` approved without `:medium`).

## Progress
1. `instill-0m5` — Closed (`6d6b280b55`). Single-fd pre-sized manifest read + narrowly scoped line-level `//nolint:gosec` annotations. Broader symlink containment hardening split to `instill-1dq`.
2. `instill-l7t` — Closed (`ebbd27add1`). Bumped `golangci-lint` to `v2.15.0` across `mise.toml`, `build.ps1`, `CONTRIBUTING.md`; resolved `gofmt` and 3 unused helpers (`mise run lint` reports 0 issues).
3. `instill-bh9` — Closed (`6b153fb7dd`, `2bdc48084d`). Fixed HTTP MCP `Env` projection across all 9 HTTP targets, Gemini `httpUrl`, and Antigravity `serverUrl` + retired `httpUrl` with ambiguous-key rejection. Verified E2E via `test/contracts/overwrite_native_http.py`.
4. `instill-ekf` (P1) — In progress. Verify overwrite headers resolved by APM at install time via single-pass env expansion without wildcarding arbitrary literals.
5. `instill-f96` (P2) — Pending. Audit and enforce SSE native transport compatibility (fail pre-reset on `codex`/`antigravity`; verify native SSE shapes on supported targets).
6. Finalize — Update `AGENTS.md`, `CONTEXT.md`, `README.md`, run all contracts & full local gate, prepare PR description.
