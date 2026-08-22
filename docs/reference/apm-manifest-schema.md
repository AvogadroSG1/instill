# APM Manifest Schema Reference (v0.3 Working Draft)

Source: https://microsoft.github.io/apm/reference/manifest-schema/

This document is the authoritative reference for `apm.yml` structure within this repository. Consult it before modifying APM manifest generation or validation logic.

---

## Required Fields

| Field | Type | Constraint |
|-------|------|------------|
| `name` | string | Package identifier; no pattern enforced at parse time |
| `version` | string | Semantic version matching `^\d+\.\d+\.\d+`; pre-release/build suffixes allowed |

---

## Optional Top-Level Fields

| Field | Type | Default | Purpose |
|-------|------|---------|---------|
| `description` | string | — | Brief human-readable description |
| `author` | string | — | Package author or organization |
| `license` | string | — | SPDX license expression (e.g., `MIT`, `Apache-2.0`) |
| `target` / `targets` | string or list | Auto-detect | Compilation output targets |
| `type` | enum | — | `instructions`, `skill`, `hybrid`, or `prompts` |
| `scripts` | map\<string, string\> | — | Named shell commands for `apm run` |
| `includes` | string or list | — | Content consent: `auto` or explicit path allow-list |
| `policy` | object | — | Consumer-side org policy controls |
| `registries` | map | — | REST-based APM registry declarations (experimental) |
| `dependencies` | object | — | APM, MCP, and LSP package dependencies |
| `devDependencies` | object | — | Development-only dependencies (excluded from `apm pack`) |
| `compilation` | object | — | Compile behavior settings |
| `marketplace` | object | — | Marketplace authoring metadata for `apm pack` |

---

## `target` / `targets` Field

**Singular form (string or list):**
```yaml
target: copilot
target: [claude, copilot]
```

**Plural form (list only; takes precedence over singular):**
```yaml
targets: [claude, copilot]
```

**Allowed values:** `copilot`, `claude`, `cursor`, `opencode`, `codex`, `gemini`, `windsurf`, `kiro`, `agent-skills`

| Value | Output |
|-------|--------|
| `copilot` | `AGENTS.md` (root + per-directory in distributed mode) |
| `claude` | `CLAUDE.md` (root) |
| `cursor` | `.cursor/rules/`, `.cursor/agents/`, `.cursor/skills/` |
| `opencode` | `.opencode/agents/`, `.opencode/commands/`, `.opencode/skills/` |
| `codex` | `AGENTS.md` + `.agents/skills/` + `.codex/agents/` |
| `gemini` | `GEMINI.md` + `.gemini/commands/`, `.gemini/skills/`, `.gemini/settings.json` |
| `windsurf` | `AGENTS.md` + `.windsurf/rules/`, `.agents/skills/`, `.windsurf/workflows/` |
| `kiro` | `AGENTS.md` + `.kiro/steering/`, `.kiro/skills/`, `.kiro/hooks/` |
| `agent-skills` | `.agents/skills/` |

**Auto-detection:** When omitted, APM detects from folder presence (`.github/`, `.claude/`, `.codex/`, `.gemini/`, `.opencode/`, `.windsurf/`, `.kiro/`).

---

## `type` Field

| Value | Behavior |
|-------|----------|
| `instructions` | Compiled into `AGENTS.md` only; no skill directory created |
| `skill` | Installed as native skill only; no `AGENTS.md` output |
| `hybrid` | Both `AGENTS.md` compilation and skill installation |
| `prompts` | Commands/prompts only; no instructions or skills |

---

## Dependencies Structure

### `dependencies.apm` — APM Package Dependencies

**String form (shorthand):**
```
[owner/repo | https://host/... | ssh://git@... | ./local/path][#ref]
```

Resolution rules:
- **GitHub default:** `microsoft/apm-sample-package` resolves to `github.com`
- **Non-GitHub:** `gitlab.com/acme/coding-standards` (FQDN preserved)
- **Full URLs:** `https://github.com/...`, `git@github.com:...`, `ssh://git@host:PORT/...`
- **Virtual paths:** `owner/repo/skills/security` (subdirectory), `owner/repo/review.prompt.md` (file)
- **Refs:** Branch (`main`), tag (`v1.0.0`), or commit SHA (`a1b2c3d...`)
- **Local paths:** `./packages/my-skills`, `../sibling-repo/my-package`, `~/absolute/path`
- **Azure DevOps:** `dev.azure.com/org/project/_git/repo`

**Object form:**
```yaml
- git: <clone-url or parent>
  path: <virtual-path or local-path>
  ref: <branch|tag|sha|semver-range>
  alias: <local-name>
  targets: [copilot, claude]  # optional; restricts install reach
```

**Marketplace dependency:**
```yaml
- name: <plugin-id>
  marketplace: <marketplace-name>
  version: "~2.1.0"  # optional; semver range or exact version
```

**Registry dependency:**
```yaml
- registry: jf-skills
  id: acme/toolkit
  path: <sub-path>  # optional
  version: ^2.0.0
  alias: <local-name>
```

**Monorepo sibling:**
```yaml
- git: parent
  path: skills/shared
```

**`ref` validation:** Accepts semver ranges (`^1.2.0`, `~1.4`, `>=2.0 <3`, `1.5.x`). Lockfile records original constraint, resolved tag, version, commit, and timestamp.

---

### `dependencies.mcp` — MCP Server Dependencies

**String form:** Plain registry reference: `io.github.github/github-mcp-server`

**Object form:**
```yaml
- name: <server-id>                          # REQUIRED
  transport: stdio|sse|http|streamable-http  # CONDITIONAL
  registry: true|false|<custom-url>          # default: true
  command: <binary-path>                     # REQUIRED for stdio + registry:false
  url: <https-endpoint>                      # REQUIRED for http/sse + registry:false
  env:
    VAR_NAME: ${VAR}|${env:VAR}|${input:id}
  headers:
    X-Header: "${VAR}"
  args: [list]|{dict}
  version: <version-pin>
  package: npm|pypi|oci
  tools: ["*"]                               # default; restrict with tool names
```

**Variable interpolation:**
- `${VAR}` or `${env:VAR}`: Host environment variable
- `${input:<id>}`: VS Code runtime input prompt (other targets unsupported)
- `${{ ... }}`: GitHub Actions templates (pass-through unchanged)

**Passthrough fields:** Non-standard keys preserved and broadcast to all targets. Reserved key names (`transport`, `command`, `url`, etc.) dropped with warnings.

**Self-defined server validation (registry: false):**
- MUST declare `transport`
- `stdio` requires `command` (single binary path, no embedded whitespace unless `args` also present)
- `http`/`sse`/`streamable-http` require `url`

---

### `dependencies.lsp` — LSP Server Dependencies

**String form:** Plain server name: `gopls`, `pyright`

**Object form:**
```yaml
- name: <server-id>                    # REQUIRED; pattern: ^[a-zA-Z0-9@_][a-zA-Z0-9._@/:=-]{0,127}$
  command: <binary-path>               # REQUIRED (no .. segments)
  extensionToLanguage:                 # REQUIRED; non-empty dict
    ".py": python
    ".go": go
  args: [list]
  transport: stdio|socket              # default: stdio
  env: {map}
  initializationOptions: any
  settings: any
  workspaceFolder: <path>
  startupTimeout: <int-ms>
  shutdownTimeout: <int-ms>
  restartOnCrash: <bool>
  maxRestarts: <int>
```

**Validation:** Both `command` and `extensionToLanguage` mandatory in object form. Manifest uses camelCase; snake_case aliases accepted on input.

**Output locations:**
- Claude Code: `.lsp.json` (project) or `~/.claude.json` (user)
- GitHub Copilot CLI: `.github/lsp.json` (project) or `~/.copilot/lsp-config.json` (user)

---

## `devDependencies`

Same structure as `dependencies`. Installed locally but excluded from plugin bundles produced by `apm pack`. Contains `apm`, `mcp`, `lsp` sub-keys.

---

## `compilation` Object

| Field | Type | Default | Constraint |
|-------|------|---------|-----------|
| `target` | enum | `all` | Same values as top-level `target` field |
| `strategy` | enum | `distributed` | `distributed` (per-directory) or `single-file` (monolithic) |
| `output` | string | `AGENTS.md` | Custom output file path |
| `chatmode` | string | — | Chatmode filter for compilation |
| `resolve_links` | bool | `true` | Resolve relative Markdown links in primitives |
| `source_attribution` | bool | `false` | Include source-file origin comments |
| `exclude` | list or string | `[]` | Glob patterns to skip (e.g., `apm_modules/**`) |
| `placement` | object | — | See below |
| `agents_md` | object | — | See below |

### `compilation.placement`
```yaml
placement:
  min_instructions_per_file: 1
```

### `compilation.agents_md`
```yaml
agents_md:
  mode: full|managed_section  # default: full
  start_marker: "<!-- apm:start -->"
  end_marker: "<!-- apm:end -->"
```

**Managed-section mode:** Replaces only the block between markers, leaving surrounding content untouched. Both markers must appear exactly once in the file.

---

## `policy` Object

```yaml
policy:
  fetch_failure_default: warn|block     # default: warn
  hash: "sha256:<hex-digest>"           # optional pin on org policy bytes
  hash_algorithm: sha256|sha384|sha512  # default: sha256
```

- `fetch_failure_default`: Posture when no enforceable policy is available
- `hash`: Pin on raw bytes of fetched org policy; verified before YAML parsing
- `hash_algorithm`: Digest algorithm; MD5 and SHA-1 rejected at parse time

---

## `registries` Object (Experimental)

Requires `apm experimental enable registries`.

```yaml
registries:
  jf-skills:
    url: https://artifactory.example.com/artifactory/api/skills/jf-skills-local
  default: jf-skills  # optional; route plain deps through this registry
```

- Registry names: lowercase letters, digits, `-`, `.`
- URLs must start with `https://` or `http://`
- Only one default registry active at a time

---

## `scripts` Map

```yaml
scripts:
  start: "copilot -p 'README.prompt.md'"
  review: "copilot -p 'code-review.prompt.md'"
```

- Script name `start` is the default for bare `apm run`
- Supports `--param key=value` substitution (`{key}` placeholders replaced)
- Publishable packages SHOULD include `start`

---

## `includes` Field (Content Consent)

Three forms:

1. **Undeclared** (omitted): Legacy auto-publish all `.apm/` content; flagged by `apm audit`
2. **`includes: auto`**: Explicit consent to publish all `.apm/` content
3. **`includes: [path, ...]`**: Explicit allow-list (strongest governance)

Allow-list only; no `exclude:` form.

---

## `marketplace` Object

### Inheritance

`name`, `description`, `version` inherit from top-level unless overridden.

### Fields

```yaml
marketplace:
  name: <string>
  description: <string>
  version: <string>                # validated as semver
  owner:
    name: <string>                 # REQUIRED
    email: <string>
    url: <string>
  sourceBase: <https-url>
  output: <path>                   # default: .claude-plugin/marketplace.json
  metadata: {object}               # free-form
  build:
    tagPattern: "v{version}"       # must contain {version} or {name}
  packages:
    - name: <string>               # REQUIRED
      source: <string>             # REQUIRED
      subdir: <string>
      version: <string>            # conditional; semver range
      ref: <string>                # conditional; explicit git ref
      tag_pattern: <string>
      include_prerelease: <bool>   # default: false
      description: <string>
      homepage: <string>
      tags: [list]                 # up to 50 tags, 100 chars each
      keywords: [list]             # merged into tags (deduplicated)
      author: <string|object>      # string=name; object: {name, email?, url?}
      license: <string>            # SPDX
      repository: <string>
```

**Package source validation:**
- Remote packages MUST declare at least one of `version` or `ref`
- Local packages (`./` prefix) skip git resolution
- Path traversal (`..`), userinfo, ports, query strings, non-`https` schemes rejected

**`sourceBase` constraints:**
- Must start with `https://`
- FQDN host, at least one path segment
- No userinfo, ports, query strings, fragments, trailing `.git`
- Segments: letters, digits, `.`, `_`, `-`; empty/`.`/`..` segments refused

---

## Lockfile (`apm.lock.yaml`)

**Mandatory fields per dependency:**
- `repo_url`
- `resolved_commit`
- `deployed_files`
- `lockfile_version`

**Resolver behavior:**
1. First install: resolve all dependencies, write lockfile
2. Subsequent installs: read lockfile, use locked commit SHAs
3. `--update` flag: re-resolve from manifest, overwrite lockfile

---

## Complete Example

```yaml
name: my-project
version: 1.0.0
description: AI-native web application
author: Contoso
license: MIT
target: [claude, copilot]
type: hybrid
includes: auto

scripts:
  start: "copilot -p 'README.prompt.md'"
  review: "copilot -p 'code-review.prompt.md'"

dependencies:
  apm:
    - microsoft/apm-sample-package#v1.0.0
    - gitlab.com/acme/coding-standards#main
    - git: https://gitlab.com/acme/repo.git
      path: instructions/security
      ref: v2.0
  mcp:
    - io.github.github/github-mcp-server
    - name: my-private-server
      registry: false
      transport: stdio
      command: ./bin/my-server
      env:
        API_KEY: ${{ secrets.KEY }}
  lsp:
    - name: pyright
      command: pyright-langserver
      args: ["--stdio"]
      extensionToLanguage:
        ".py": python
        ".pyi": python

devDependencies:
  apm:
    - owner/test-helpers

compilation:
  target: all
  strategy: distributed
  exclude:
    - "apm_modules/**"
  placement:
    min_instructions_per_file: 1

policy:
  fetch_failure_default: warn

marketplace:
  owner:
    name: contoso
    url: https://github.com/contoso
  packages:
    - name: code-review
      source: contoso/code-review
      version: "^1.0.0"
      tags: [review, quality]
```

---

## Gap Analysis: instill vs. APM Schema

Instill parses the complete manifest into an authoritative `yaml.Node` document. `APMManifest` is a non-authoritative typed projection used for validation, identity matching, and planning. Instill MUST serialize the node document, not the projection, so unmodeled schema fields and opaque dependency forms survive supported updates.

Instill owns only these fields during the corresponding operation:

- Mutating commands repair absent `name` and `version`; read-only and instruction/prompt operations do not.
- Target updates own plural `targets`. Singular `target` is preserved as an unknown compatibility field.
- Skill and Plugin selection own catalog-matched Git dependencies and ordinary `!!str` local paths that exactly match catalog-derived paths or are stale paths contained under their Library roots. Tagged scalars are never owned.
- MCP selection and sync own `name`, `transport`, `registry`, `command`, `args`, `env`, and `url` only for catalog-matched mappings. MCP removal rejects names absent from the active catalog rather than removing a user-owned named mapping. Self-defined Library and Graft-imported entries always write their transport and `registry: false` explicitly.
- Unsupported scalars and mappings, including tagged scalars, remote APM shorthand, marketplace/registry mappings, and MCP shorthand, remain opaque.

The adapter preserves node kind, tag, value, ordering, style, comments, anchors, aliases, and unknown children outside an operation's exact ownership. A genuine mutation may re-render whitespace that `yaml.v3` does not model. A no-op performs no write.

Existing manifest replacements retain the original permission bits. New manifests request mode `0o644`, with the process umask applied when the temporary replacement file is created.

| Schema Field | instill Status | Notes |
|---|---|---|
| `name` | **Implemented** | Set from `filepath.Base(root)` |
| `version` | **Implemented** | New manifests use `0.1.0`; mutating commands repair an absent value |
| `description` | Missing | Optional |
| `author` | Missing | Optional |
| `license` | Missing | Optional |
| `target`/`targets` | Partial | Instill owns plural `targets`; singular `target` is preserved but not interpreted |
| `type` | Missing | Controls compilation behavior |
| `dependencies.apm` | Partial, preservation-safe | Typed local and Git forms are managed; every other form is opaque and preserved |
| `dependencies.mcp` | Partial, preservation-safe | Catalog-owned fields are managed; headers, tools, version, package, and unknown keys are preserved |
| `dependencies.lsp` | Preserved, not managed | Retained as authoritative nodes |
| `devDependencies` | Preserved, not managed | Retained as authoritative nodes |
| `compilation` | Preserved, not managed | Retained as authoritative nodes |
| `scripts` | Preserved, not managed | Retained as authoritative nodes |
| `includes` | Preserved, not managed | Retained as authoritative nodes |
| `policy` | Preserved, not managed | Retained as authoritative nodes |
| `registries` | Preserved, not managed | Retained as authoritative nodes |
| `marketplace` | Preserved, not managed | Retained as authoritative nodes |

Instill MUST NOT expand the typed projection merely to preserve a field. New managed fields require an explicit ownership decision and narrow node mutation contract.
