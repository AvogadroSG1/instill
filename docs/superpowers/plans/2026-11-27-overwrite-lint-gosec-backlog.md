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
4. `instill-ekf` (P1) — Closed (`33ace574b5`). Single-pass env-expanded runtime header verification with per-header case-variant matching; strict lockfile verification preserved. Verified E2E via `test/contracts/overwrite_resolved_headers.py`.
5. `instill-f96` (P2) — Closed (`8a1d9e7e7a`). Pre-reset SSE rejection for `codex`/`antigravity` (root MCPs, package `apm.yml`, native plugin JSON) and APM 0.39.0 native SSE verification across supported targets. Verified E2E via `test/contracts/overwrite_sse_transport.py`.
6. Finalize — Updated `AGENTS.md`, `CONTEXT.md`, `README.md`; verified full local gate and all E2E contracts end-to-end.

## Follow-up Issues Filed
- `instill-1dq` (P3): Harden catalog and Pi plugin symlink containment without regressing search-only dirs.
- `instill-xq5` (P1): Support shared workspace `.mcp.json` for Copilot/VS Code targets under APM 0.39.0.
