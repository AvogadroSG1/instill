# ADR 0009: APM 0.39.0 Remote MCP Shapes & Overwrite Verification Contract

- **Status:** Accepted
- **Date:** 2026-11-27
- **Related Issues:** `instill-bh9`, `instill-ekf`, `instill-f96`

## Context

`instill pick --overwrite` resets target harness MCP configurations, runs `apm install`, and verifies every generated runtime MCP definition against the selected Library catalog entries before declaring success.

Different harnesses emit different JSON/TOML shapes for remote (`http` / `sse`) MCP servers, and APM itself evolved those shapes between releases (e.g., APM ≤ 0.36 vs APM 0.39.0). Relying on ad-hoc per-target assumptions caused silent verification failures (such as Antigravity emitting `serverUrl` in APM 0.39.0 while retired APM ≤ 0.36 emitted `httpUrl`) and unverified SSE behavior.

## Decision

Pin Instill's overwrite runtime MCP verification contract to APM 0.39.0's authoritative `REMOTE_SHAPES` table (`apm_cli/adapters/client/_remote_shapes.py`), including each target's documented `retired` shapes for backwards compatibility with older APM versions:

| Target | Streamable HTTP (`http`) | SSE (`sse`) | Retired / Legacy Keys Accepted | Notes |
| --- | --- | --- | --- | --- |
| `claude` | `url`, `type: "http"` | `url`, `type: "sse"` | — | Runtime `${VAR}` substitution supported |
| `copilot` | `url`, `type: "http"` | `url`, `type: "sse"` | — | Runtime `${VAR}` substitution supported |
| `vscode` | `url`, `type: "http"` | `url`, `type: "sse"` | — | Runtime `${VAR}` substitution supported |
| `gemini` | `httpUrl` (no `type`) | `url` (no `type`) | — | Reject ambiguous `url` + `httpUrl`; install-time env resolution (`_supports_runtime_env_substitution = False`) |
| `antigravity` | `serverUrl` (no `type`) | **UNSUPPORTED** | `httpUrl` (APM ≤ 0.36) | Reject any 2+ of `url`/`httpUrl`/`serverUrl`; **fail before project reset** if SSE selected |
| `codex` | `url` (TOML) | **UNSUPPORTED** | — | **Fail before project reset** if SSE selected |
| `cursor` | `url`, `type: "http"` | `url`, `type: "http"` | — | APM collapses SSE to `type: "http"` |
| `kiro` | `url` (no `type`) | `url` (no `type`) | — | APM omits transport discriminator (URL-only) |
| `opencode` | `url`, `type: "remote"` | `url`, `type: "remote"` | `httpTransport: "sse"` | APM collapses SSE to `type: "remote"` |

### Effective-Header Verification Contract (`instill-ekf`)

For runtime MCP headers whose selected template contains `${VAR}`, `${env:VAR}`, or `{env:VAR}`:
1. **Preserved placeholder:** Normalized placeholder forms (`${VAR}`, `${env:VAR}`, `{env:VAR}`) match the normalized expected template.
2. **Install-time environment resolution:** For Gemini, Antigravity, and compatibility with Claude on older APM releases, a runtime header value is accepted if single-pass expansion of `${VAR}` / `${env:VAR}` / `{env:VAR}` using non-empty values from the current process environment produces the exact runtime header value. APM lockfile verification does not expand placeholders.
3. **Strict rejection:** Unset/empty variables never match arbitrary literals, wrong header values fail verification, wrong URLs fail verification, and expansion is strictly single-pass (matching APM's `_ENV_PLACEHOLDER_RE.sub` semantics).

## Consequences

- `overwriteJSONMCPDefinition` and `equalOverwriteMCPRuntimeDefinition` stay aligned with real APM 0.39.0 output while supporting retired APM ≤ 0.36 keys where unambiguous.
- Unsupported SSE targets (`codex`, `antigravity`) fail fast **before** any destructive project reset in `prepareProjectOverwrite`.
- Executable end-to-end contracts in `test/contracts/` verify the built `instill` binary against real `apm` and are wired into `mise run contracts`.
- **Known follow-up (`instill-xq5`):** In APM 0.39.0 (`copilot_project.py`), Copilot/VS Code project scope writes to workspace-root `.mcp.json` (shared with Claude) instead of APM ≤ 0.36's `.github/mcp.json` / `.vscode/mcp.json`. Until shared-file union semantics are implemented in `overwriteMCPRuntimes`, E2E SSE verification covers `claude`, `gemini`, `cursor`, `kiro`, `opencode`, `codex`, and `antigravity`; `copilot` and `vscode` SSE verification is covered by unit tests.
