---
name: instill
description: Consume and curate an instill-managed AI-agent library. Use when picking or syncing agent skills, plugins, or MCP servers, editing or reviewing `apm.yml`, running `instill` commands, curating the local library, or adding plugin marketplaces.
---

# Instill

`instill` curates a local library of agent content (skills, plugins, MCP servers, instructions, prompts) and selects items into a project. The project's `apm.yml` is the committed selection contract; `instill sync` installs what it lists.

This skill covers concepts and workflows. For flags, run `instill <cmd> --help`. For config and exit codes, read `/Users/avogadro/peter_code/instill/llms.txt`.

## Concepts

- **Library**: a directory of typed content. Resolved from `INSTILL_LIBRARY_PATH` first, then `library_path` in `~/.config/instill/config.json`.
- **Layout**: `skills/<name>/SKILL.md`, `plugins/`, `mcp/<name>/config.json`, `instructions/<name>/INSTRUCTION.md`, `prompts/<name>/PROMPT.md`. Each typed folder has a `catalog.csv`.
- **Project**: a directory containing `apm.yml`. instill finds it by walking up from the current directory.
- **Sync**: `apm install` + `apm compile` + copy of OpenCode plugin files to `.opencode/plugins/instill-<package>-<file>`.
- **Ownership**: the `instill-` prefix marks files instill owns. instill only touches prefixed files. Entries in `dependencies.apm` that the typed catalog does not own MUST be left alone.
- **Nested skills**: skills may live in group folders (e.g. `cloud/azure/azure-cli`). Always pick them by full path.

## Consuming (in a project)

1. Choose items: `instill pick <name...>` (add `--type <type>` for non-skill types such as `plugin`).
2. Remove items: `instill pick --remove <name...>`.
3. Install: `instill sync`.
4. Check drift: `instill status` lists selected entries missing from the library, library entries not selected, and skill git pins with updates. It also reports skill `hash mismatch` (see Failure checks) and instruction or prompt copies that differ from the library.
5. New project: `instill init` creates the manifest. Use `--force` only with explicit user intent to overwrite.
6. Session hook (optional): `instill add-hooks` installs `instill sync` as the Claude Code SessionStart hook.

## Curating (the library)

1. Add content: `instill library add --type <type> --name <name> ...`, or `instill library add plugin-marketplace <owner/repo>` (see Remote sources).
2. Import existing state: `instill import <subcommand>` (see `instill import --help`).
3. Inspect: `instill library show [--type <type>] [--filter <text>]`.
4. Rebuild catalogs after any content change: `instill library scan`.
5. Refresh a remote pin: `instill library update --type skill --name <name>`, or `instill library update plugin-marketplace <name>`.

## Rules

- You MUST NOT hand-edit the project's `apm.yml`. You MUST change selections with `instill pick` (or `instill pick --remove`).
- You MUST NOT hand-add a catalog row without matching content. `instill library scan` deletes it (`removed: <name> (content not found)`).
- For skills and plugins, `scan` regenerates `name` and `path` from the folder and keeps the existing `description` and any customized `category`. Change the content, then scan.
- For MCP, `scan` keeps the existing `transport`, `command`, `args`, `url`, `env`, `headers`, and `default_enabled` values. It reads `config.json` only for new entries: editing `command` and rescanning left the old value in the row. To change an existing MCP row, edit `mcp/catalog.csv` directly.
- You MUST use `instill library add --type mcp --default-enabled=true|false` only to set the initial state for a name new to a harness. Existing choices win; do not expect the flag to change them.
- You MUST NOT assume `instill sync` updates Git source pins. It never does. See Failure checks.

## Failure checks

**Legacy `source=git` plugin rows break plugin catalog reads.**
Symptom: plugin catalog reads fail. Cause: `plugins/catalog.csv` contains rows with `source=git`. Fix: delete those rows, then re-add the plugin through a marketplace with `instill library add plugin-marketplace <owner/repo>`. `library add --type plugin --repository` and `library update --type plugin` were removed; MUST NOT recommend or use them.

**`instill sync` does not advance Git source pins.**
To pick up a newer commit, in order:
1. `instill library update --type skill --name <name>` (or `instill library update plugin-marketplace <name>` for a marketplace) to refresh the library pin.
2. Review the changed library content.
3. `instill pick <name>` to update the project manifest.
4. `instill sync` to install it.

**Nested skill.** Pick it by its full group path (e.g. `cloud/azure/azure-cli`), not the leaf name.

**Drift.** `instill status` prints `hash mismatch: skill <name>` when a skill's library content changed since the last `apm install`. Run `instill sync`, which runs `apm install` again. Do not edit copied `.apm` content by hand.

## Remote sources (plugin marketplaces)

`instill library add plugin-marketplace <owner/repo>` downloads that GitHub repository's default-branch commit **into the library, locally**, at `plugins/marketplaces/<marketplace>/`. It catalogs each installable plugin so `instill pick --type plugin <name>` can select it.

- The content is copied into the library once. `instill sync` does NOT fetch it again on each run.
- To refresh the local copy, run `instill library update plugin-marketplace <name>`, review the changes, then `instill pick` and `instill sync` as needed.
- Do not treat a marketplace as a live link to GitHub. Its contents are whatever the library last downloaded.
