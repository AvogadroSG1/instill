# APM-backed Library catalog

## Status

Accepted

## Context

instill originally managed a skill-only manifest, local symlinks, and Claude-specific permission entries. That made instill responsible for local ownership details that overlap with dependency resolution, lockfile state, security scanning, and agent-specific rendering.

The APM integration changes the boundary. instill still needs to provide a curated project UX, but the durable project contract SHOULD be an APM manifest that an external APM engine can install and compile.

## Decision Drivers

- instill MUST keep a simple curated Library catalog UX.
- APM MUST own dependency resolution, lockfile updates, security scanning, and harness targeting.
- The project manifest MUST be portable and committed.
- Instructions and prompts MUST be copied into project `.apm/` content instead of symlinked.
- Library-only commands MUST remain independent from external APM availability.

## Considered Options

### Option A: Keep instill as the full resolver and symlink reconciler

instill would continue to own skill manifests, symlinks, local permission reconciliation, and hook reconciliation.

This keeps the old model stable but keeps too much agent-specific state in instill and does not give projects a general APM dependency contract.

### Option B: Make instill a thin APM wrapper

instill would stop modeling the library and only forward commands to APM.

This removes duplication but loses the curated Library catalog and picker UX that makes project selection practical.

### Option C: instill curates the Library catalog; APM resolves and syncs

instill owns typed library entries, project selection, local content copies for instructions and prompts, and the `instill sync` hook. APM owns install, compile, lockfile state, security scanning, and downstream harness rendering.

## Decision Outcome

Chosen option: **Option C: instill curates the Library catalog; APM resolves and syncs**.

instill will write and update the project APM manifest (`apm.yml`) from the Library catalog. `instill sync` will call `apm install` and then `apm compile`. `instill add-hooks` will register `instill sync` as the Claude Code `SessionStart` hook.

## Consequences

- instill has reduced local symlink ownership. Supported commands MUST NOT depend on `.claude/skills` or `.agents/skills` reconciliation.
- Project state is centered on `apm.yml`, `.apm/` copied content, and APM-produced lock or rendered artifacts.
- Runtime project sync relies on external APM availability. If APM is missing or too old, APM-backed commands MUST fail with actionable environment guidance.
- Library-only commands such as `library scan`, `library add`, and `library show` can continue to work without bootstrapping APM.
- Legacy import code MAY retain small readers or cleanup helpers for old instill projects, but hidden legacy command names MUST NOT mutate project state.

## Confirmation

The decision is covered by unit tests for legacy command guidance and by smoke tests that use `apm.yml`, typed catalog CSV files, `instill sync`, and copied `.apm/` content.
