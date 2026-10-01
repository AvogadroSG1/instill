# instill

[![Go Version](https://img.shields.io/github/go-mod/go-version/AvogadroSg1/instill)](https://go.dev/)
[![License](https://img.shields.io/github/license/AvogadroSg1/instill)](./LICENSE)
[![Build Status][build-badge]][build-url]

`instill` curates a typed Library catalog for project-specific AI agent capabilities and delegates dependency resolution, lockfiles, install, and compile work to APM.

## Model

instill is the curated library UX. APM is the sync engine.

```mermaid
flowchart LR
    Library[Library catalog] --> Pick[instill pick]
    Pick --> Manifest[APM manifest: apm.yml]
    Manifest --> Sync[instill sync]
    Sync --> APM[apm install and apm compile]
    APM --> Project[.apm rendered project content]
```

- The **Library catalog** lives under `INSTILL_LIBRARY_PATH` and uses typed CSV files for skills, plugins, MCP servers, instructions, and prompts.
- The **APM manifest** is the project-local `apm.yml` file committed with the project.
- **Sync** means `instill sync` runs `apm install`, then `apm compile`, then copies OpenCode plugin files, then reports installed counts.
- **OpenCode plugin copying** is instill-owned because APM has no OpenCode plugin primitive: when `opencode` is a target (manifest `targets`, or a detected `.opencode/` directory when `targets` is empty), sync copies each `dependencies.apm` package's direct `opencode/plugins/*.ts` and `*.js` regular files into `.opencode/plugins/instill-<package>-<file>`, where `<package>` is the package directory's base name. Following ADR 0001, the `instill-` prefix is the ownership marker: sync overwrites changed copies, removes `instill-*.ts`/`instill-*.js` files no longer provided (all of them when `opencode` is not a target), and never touches files without the prefix. Git packages resolve under `apm_modules/<owner>/<repo>/<path>`.
- **Skill deployment** is APM-owned: `apm install` copies each skill's full directory (including supporting files such as `scripts/`) into the shared `.agents/skills/<name>/` path used by converged harnesses; Claude Code receives its copy under `.claude/skills/`. instill no longer passes `--legacy-skill-paths`; on the next `apm install` APM prunes lock-tracked per-harness copies such as `.codex/skills/` automatically.
- **Typed library entries** let one library manage skills, plugins, MCP servers, instructions, and prompts without overloading a skill-only manifest.

## Install

```bash
go install github.com/AvogadroSg1/instill@latest

# Or build locally
make install
```

APM MUST be available for commands that touch project APM state. If `apm` is missing, instill will try to install the configured APM formula with Homebrew.

## Configure The Library

```bash
export INSTILL_LIBRARY_PATH=~/path/to/agent-library
```

`INSTILL_LIBRARY_PATH` has highest precedence. `~/.config/instill/config.json` is used when the environment variable is absent. `SKILL_LIBRARY_PATH` remains a migration fallback only.

Expected library shape:

```text
~/path/to/agent-library/
  .instill.lock
  skills/catalog.csv
  skills/golang-testing/SKILL.md
  plugins/catalog.csv
  plugins/example/.claude-plugin/plugin.json
  mcp/catalog.csv
  mcp/local-db/config.json
  instructions/catalog.csv
  instructions/python-rules/INSTRUCTION.md
  prompts/catalog.csv
  prompts/debug/PROMPT.md
```

Run `instill library scan` to create or refresh catalog CSV files from library content.

### MCP Initial State and Pi

```bash
instill library add --type mcp --name local-db --transport stdio \
  --command sqlite-mcp --default-enabled=false
instill pick --type mcp local-db
instill sync
```

`--default-enabled` is MCP-only. Omission leaves the initial harness behavior unchanged; explicit `true` or `false` supplies an initial default. The MCP CSV schema is `name,transport,command,args,url,env,description,default_enabled`. The last cell accepts empty, `true`, or `false`; old seven-column catalogs remain readable without a migration write. Defaults are Instill metadata, never fields in `apm.yml`.

Defaults apply only to names absent from the destination's applicable local configuration before installation. Existing choices—including omitted flags—win independently in OpenCode, Codex, Claude Code, and Pi. Subsequent installs refresh connection definitions without reapplying defaults. A deleted Instill-owned destination entry can receive the current default when recreated. Change existing library defaults in the CSV; scan preserves a curated non-empty value and uses a marker default only for an unspecified row. MCP additions create missing `config.json` markers, but never overwrite existing markers.

OpenCode uses project `opencode.json` `enabled`; Codex uses `.codex/config.toml` `enabled`. Claude uses the selected project's `disabledMcpServers` in `~/.claude.json`, or `$CLAUDE_CONFIG_DIR/.claude.json`. Claude defaults affect `/mcp` on/off state, **not approval or trust**; enabled servers still receive normal project approval prompts. Other user/global harness configuration is read for collisions, not rewritten.

Pi support requires a real project `.pi/` directory and a separately installed, compatible [`pi-mcp-adapter`](https://www.npmjs.com/package/pi-mcp-adapter) extension (format verified against 4.0.0). Instill writes usable definitions to `.pi/mcp-adapter.json`; it does not install the extension, upgrade Pi, or grant trust. Existing provider/user/imported servers are not overridden or claimed. Only entries recorded in `_instillManagedServers` are reconciled or removed when selection changes. Unmatched registry dependencies are not synthesized into Pi definitions; their definitions remain provider/APM-owned.

Pi is not an APM target-picker selection. Legacy `targets: [pi]` selections migrate to supported targets; a Pi-only project uses `agent-skills`. Because APM 0.32.0's meta-target has no MCP adapter, managed Pi-only installs use `apm install --only apm` for package/skill deployment while Instill deploys the adapter definitions. Ordinary and mixed-harness installs keep the bare APM install command. `PI_MCP_CONFIG_MODE=exclusive` ignores the project adapter file: Instill leaves it untouched and errors if a desired server is missing from the provider's effective configuration.

### Concurrent Mutations

Instill serializes cooperating Library and Project mutations with an exclusive advisory lock on the persistent `<root>/.instill.lock` file. The file remains after a command exits and is not evidence that a process currently owns the lock. Scans and imported content ignore it.

Lock acquisition has one 10-second timeout for the complete ordered root set. Unrelated roots can mutate concurrently. Commands that update both the Library and a Project release the Library lock after publishing catalog-derived Project state, while retaining the Project lock through APM install, prune, or compile.

When the MCP catalog contains false defaults, Instill acquires a stable user-home guard and the Claude state directory's nearest existing parent lock alongside the Library and Project locks in canonical order. It retains these locks through APM and toggle repair, including when APM first creates `.claude/` or a state write creates a missing private config directory. This deliberately serializes more installs across the same user's projects. No Claude state file or private parent is created unless a real toggle addition is needed. Native MCP writes preserve existing modes and unrelated data, reject writable symlinks, and fail on intervening byte changes rather than replacing a newer file.

Local filesystems are the supported correctness baseline. Advisory locks do not prevent changes by editors, scripts, older Instill versions, or any other non-cooperating process. NFS, SMB, FUSE, container bind mounts, and other network or virtual filesystems vary by server, client, and mount configuration; successful acquisition confirms participation in the local advisory protocol but MUST NOT be treated as verified cross-host exclusion.

### Remote Skills

Register a GitHub skill with its repository alone:

```bash
instill library add --type skill --repository owner/repo
```

Instill derives the skill name from `repo`, verifies `skills/{repo}/SKILL.md`, and records the canonical clone URL (`https://github.com/owner/repo.git`), virtual package path (`skills/{repo}`), and the default branch's full immutable commit SHA in `skills/catalog.csv`. The expanded skill catalog schema is `name,category,path,source,repository,ref,description`; existing four-column local catalogs remain readable and are migrated on write.

Public repositories require no special setup. Private repositories use the user's normal Git credential helpers and SSH/HTTPS configuration when Git accesses GitHub. Instill never accepts, writes, or stores credentials.

The catalog SHA is the source pin. When APM installs it, its lockfile records the resolved package as a second pin. Instill MUST NOT update either pin automatically. To intentionally refresh a remote skill to its current default-branch commit, run:

```bash
instill library update --type skill --name repo
instill pick --type skill repo
instill sync
```

The explicit `pick` updates this project's manifest to the catalog SHA. `sync` alone MUST NOT change project dependency refs. Review and commit the catalog, manifest, and APM lockfile changes after an explicit upgrade.

### Remote Plugins

Register a plugin from a GitHub repository containing a root Claude marketplace:

```bash
instill library add --type plugin --repository pbakaus/impeccable --name impeccable
```

Instill resolves the repository's default branch to a full immutable commit SHA, reads `.claude-plugin/marketplace.json` at that commit, selects the named plugin, and verifies its package-local `.claude-plugin/plugin.json`. `--name` MAY be omitted when the marketplace advertises exactly one plugin; repositories advertising multiple plugins MUST use `--name`.

Marketplace plugin sources MUST be repository-local directories. Instill rejects absolute paths, URLs, traversal, symlinks, malformed metadata, duplicate names, and package manifests whose name does not match the marketplace entry. Registration failures MUST NOT change the catalog.

Repository-backed plugins use the expanded `plugins/catalog.csv` schema `name,category,path,source,repository,ref,description`. Existing four-column local plugin catalogs remain readable and migrate on their next write. For Impeccable, selecting the catalog entry writes this APM dependency:

```yaml
- git: https://github.com/pbakaus/impeccable.git
  path: plugin
  ref: <full-40-character-sha>
```

Refresh remains explicit and follows the same three-pin lifecycle as remote skills:

```bash
instill library update --type plugin --name impeccable
instill pick --type plugin impeccable
instill sync
```

`library update` refreshes the catalog source pin, `pick` replaces the project's manifest pin, and APM records installation state in its lockfile. `sync` MUST NOT discover or select a newer commit by itself.

## Project Workflow

```bash
cd your-project
instill init --skills golang-testing
instill pick --type instruction python-rules
instill pick --type prompt debug
instill sync
instill add-hooks
```

Project artifacts:

```text
your-project/
  .instill.lock            # persistent advisory lock file
  apm.yml                  # committed APM manifest
  apm.lock.yaml            # APM lockfile when produced by APM
  .apm/
    instructions/*.instructions.md
    prompts/*.prompt.md
  .claude/settings.json    # SessionStart hook from instill add-hooks
```

`instill add-hooks` registers `instill sync` as the Claude Code `SessionStart` hook so each new session refreshes APM-managed project content.

## Commands

| Command | Description |
|---------|-------------|
| `instill init` | Create `apm.yml` for the current project and optionally seed skills |
| `instill targets` | View or configure target agents for compilation |
| `instill pick [name...]` | Add or remove typed library entries from `apm.yml` or copied `.apm/` content |
| `instill sync` | Run `apm install`, then `apm compile`, copy OpenCode plugin files, and report synced counts |
| `instill status` | Compare project APM state with the Library catalog |
| `instill library scan` | Rebuild typed catalog CSV files from library content |
| `instill library add` | Add one typed catalog entry |
| `instill library update --type TYPE --name NAME` | Refresh a remote Skill or Plugin; `TYPE` MUST be `skill` or `plugin` |
| `instill library show` | List typed catalog entries |
| `instill import` | Import legacy instill, graft, Claude config, or generic directories |
| `instill bootstrap` | Ensure APM is installed and meets the minimum version |
| `instill add-hooks` | Register the `instill sync` SessionStart hook |

Legacy commands such as `instill check-skills` MUST NOT mutate project state. `check-skills` exits with migration guidance naming `instill sync`.

## Configuration

| Source | Precedence |
|--------|------------|
| `INSTILL_LIBRARY_PATH` environment variable | Highest |
| `SKILL_LIBRARY_PATH` environment variable | Migration fallback |
| `~/.config/instill/config.json` | Stored default |
| Interactive TTY prompt | Lowest |

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
| `1` | General error |
| `2` | Environment error |
| `3` | Filesystem error |

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

MIT — see [LICENSE](./LICENSE).

[build-badge]: https://img.shields.io/github/actions/workflow/status/AvogadroSg1/instill/test.yml?branch=main
[build-url]: https://github.com/AvogadroSg1/instill/actions

*Authored By Peter O'Connor with Assistance from OpenCode (openai/gpt-5.6-sol) · 2026-08-22 · Advisory locking and repository-backed package documentation*
