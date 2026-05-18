# instill

A CLI that curates a project-specific skill library for AI coding agents — managing which skills a project uses, keeping symlinks reconciled, and granting agent permissions automatically.

## Language

**Skill**:
A named directory in the Library containing a `SKILL.md` file. Identified by a single path-safe name (e.g. `golang-testing`, `superpowers:tdd`).
_Avoid_: plugin, tool, module

**Library**:
A developer-local directory of Skills, configured via `SKILL_LIBRARY_PATH` or `~/.config/instill/config.json`. Not committed to the project.
_Avoid_: skill store, repository

**Manifest**:
The committed JSON file (`.claude/skill-manifest.json`) listing the skill names a project uses. The source of truth for what instill manages.
_Avoid_: config, lockfile

**Project**:
A directory containing an instill Manifest. Discovered by walking up from the current working directory.
_Avoid_: workspace, repo

**Reconcile**:
The process of bringing a developer's local state — symlinks and agent permissions — into agreement with the Manifest. Creates skill symlinks in both `.claude/skills/` (Claude Code) and `.agents/skills/` (OpenAI Codex). Agent permissions are written only to `.claude/settings.local.json`; Codex has no equivalent. Runs automatically on `SessionStart` via a hook.
_Avoid_: sync, update, apply

**Agent permission**:
An entry in an agent's local settings file that auto-approves a Skill invocation without prompting. For Claude: a `"Skill(name)"` string in `.claude/settings.local.json` under `permissions.allow`. Always developer-local (never committed).
_Avoid_: whitelist entry, allowlist, tool permission

## Relationships

- A **Manifest** lists one or more **Skills** by name
- A **Library** contains the actual **Skill** directories that a Manifest references
- **Reconcile** reads the **Manifest**, creates symlinks from `.claude/skills/` and `.agents/skills/` → **Library**, and writes **agent permissions** to `.claude/settings.local.json` (Claude-only)
- The **Manifest** is the ownership boundary for agent permissions: instill only adds or removes a permission for a Skill it also manages as a symlink

## Example dialogue

> **Dev:** "I cloned my friend's repo — will I get the skill permissions automatically?"
> **Domain expert:** "Yes. On the next session start, Reconcile reads the Manifest, links the Skills from your local Library, and writes the agent permissions to your `settings.local.json`."

> **Dev:** "I manually added `Skill(my-tool)` to `settings.local.json`. Will instill remove it?"
> **Domain expert:** "Only if `my-tool` is in the Manifest. If instill didn't put it in the Manifest, it won't touch the permission."

## Flagged ambiguities

- "whitelist" was used during design — resolved: use **agent permission** for the settings.local.json entry; the action is "granting" or "revoking" a permission.
