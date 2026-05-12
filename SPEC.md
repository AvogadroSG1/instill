# instill — Design Specification

> Deterministic CLI to curate and sync a project-specific skill library.

---

## 1. Problem Statement

A developer maintains 200+ personal Claude Code skills in a central library. Every project needs a curated subset — but manual copy-paste causes drift, and a global install gives every project every skill. `instill` provides a project-scoped, manifest-driven symlink layer so each project declares exactly which skills it uses, updates flow automatically from the library, and the setup is reproducible on any machine.

---

## 2. Data Model

### 2.1 Entities

#### Library
The source of truth for all available skills.

```
Location:   ~/ObsidianNotes/agent_config/skills/   (default)
Override:   SKILL_LIBRARY_PATH environment variable
Config:     ~/.config/instill/config.json
```

Each entry in the library is a directory:
```
<library_path>/
  golang-cli/
    SKILL.md
    [assets/]
    [evals/]
    [config.json]
  docker/
    SKILL.md
  ...
```

A skill exists in the library if and only if a subdirectory with a `SKILL.md` file is present at `<library_path>/<name>/SKILL.md`.

#### Skill
A named unit of Claude Code instruction.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Directory name under library path — the canonical identifier |
| `path` | path | `<library_path>/<name>/` |

Skills have no version numbers. The library is the live source of truth; the content at `<library_path>/<name>/` is always current.

#### Project
A directory containing a `.claude/skill-manifest.json`.

| Field | Type | Description |
|-------|------|-------------|
| `manifest_path` | path | `<project_root>/.claude/skill-manifest.json` |
| `symlink_dir` | path | `<project_root>/.claude/skills/` |
| `selected_skills` | string[] | Ordered list of skill names from manifest |

#### Global Config
```
~/.config/instill/config.json
```
```json
{
  "library_path": "/Users/you/ObsidianNotes/agent_config/skills"
}
```

### 2.2 Manifest Schema

**File:** `.claude/skill-manifest.json`

```json
{
  "skills": [
    "golang-cli",
    "golang-testing",
    "docker",
    "bash-defensive-patterns"
  ]
}
```

| Field | Type | Constraints |
|-------|------|-------------|
| `skills` | string[] | Unique, sorted alphabetically, each must be a valid library skill name at time of selection |

The manifest is the only `instill` artifact committed to the repository. Skill names are stable identifiers — they match library directory names exactly.

### 2.3 Symlink Structure

```
<project_root>/
  .claude/
    skill-manifest.json     ← committed
    skills/                 ← gitignored
      golang-cli            → ~/ObsidianNotes/agent_config/skills/golang-cli
      golang-testing        → ~/ObsidianNotes/agent_config/skills/golang-testing
      docker                → ~/ObsidianNotes/agent_config/skills/docker
  .gitignore                ← contains: .claude/skills/
```

Symlinks are absolute paths. They are recreated on any machine by `instill check-skills` using that machine's resolved `library_path`.

### 2.4 Relationships

```
Library (1) ──────────── (many) Skill
Project (1) ──────────── (1) Manifest
Manifest (1) ─────────── (many) SkillName
SkillName ──── resolves to ──── Skill in Library  [via symlink]
GlobalConfig (1) ─────── (1) library_path
```

---

## 3. Command Interface

### Global Behavior (all commands)

1. **Config resolution** (before any command executes):
   - If `SKILL_LIBRARY_PATH` is set: use it
   - Else if `~/.config/instill/config.json` exists: read `library_path`
   - Else if TTY available: prompt `Library path [<detected_default>]:`, write config, continue
   - Else: exit `2` — `error: no library path configured; set SKILL_LIBRARY_PATH`

2. **Implicit check-skills** (after config resolution, before command executes, only when manifest present):
   - Run reconciliation silently
   - Report any removals: `removed: golang-cli (no longer in library)`

3. **Exit codes**:
   - `0` Success
   - `1` General error (invalid args, unknown skill, malformed manifest, no manifest found)
   - `2` Environment error (library not found, config missing + no TTY)
   - `3` Filesystem error (cannot write manifest, cannot create/remove symlink)

---

### `instill init-project`

**Purpose:** Initialize a project with an empty manifest and launch skill selection.

**Flags:**
- `--skills <name,...>` — skip TUI, bootstrap with named skills (all-or-nothing validation)
- `--force` — overwrite existing manifest

**Behavior:**
1. Check for existing `.claude/skill-manifest.json`
   - If found and `--force` not set: exit `1` — `error: manifest already exists; use --force to reinitialize`
2. Check for `.git` in project root
   - If absent: print `warning: not a git repository — manifest will not be committed`
3. Create `.claude/` directory if absent
4. Write `.claude/skill-manifest.json` with `{"skills": []}`
5. Create `.claude/skills/` directory if absent
6. Inject `.claude/skills/` into `.gitignore`:
   - If `.gitignore` absent: create it
   - If `.claude/skills/` not already present: append:
     ```
     # instill — managed symlinks, do not commit
     .claude/skills/
     ```
7. If `--skills` provided: validate all names against library (all-or-nothing), write manifest, reconcile symlinks, exit
8. Else: launch `pick-skills` TUI

**Output:**
```
initialized: .claude/skill-manifest.json
created:     .claude/skills/
updated:     .gitignore (+.claude/skills/)
warning:     not a git repository — manifest will not be committed  [if applicable]
```

---

### `instill show-library [--filter <substring>]`

**Purpose:** List available skills in the library, annotated with project selection state.

**Flags:**
- `--filter <substring>` — case-insensitive substring match on skill name

**Behavior:**
1. Read library directory — list all subdirectories containing `SKILL.md`
2. Sort alphabetically
3. Apply `--filter` if provided
4. If manifest present in current project: annotate each skill
5. Print

**Output (inside project):**
```
[✓] bash-defensive-patterns
[ ] bamboohr
[✓] docker
[ ] golang-benchmark
[✓] golang-cli
[✓] golang-testing
...
209 skills  (4 selected)
```

**Output (outside project):**
```
bash-defensive-patterns
bamboohr
docker
...
209 skills
```

**Out-of-project:** Proceeds normally — no manifest required.

---

### `instill pick-skills [skill-name...]`

**Purpose:** Add skills to the project manifest and reconcile symlinks. Additive by default.

**Flags:**
- `--remove <name,...>` — remove named skills from manifest

**Behavior (arg mode):**
1. Validate ALL provided names against library before any changes (all-or-nothing):
   - If any unknown: exit `1` — `error: unknown skill: typo-skill — run 'instill show-library' to see available skills`
2. Merge valid names into manifest (deduplicated, sorted)
3. If `--remove`: remove named skills from manifest
4. Write manifest atomically (write to `.claude/skill-manifest.json.tmp`, rename)
5. Run reconciliation

**Behavior (TUI mode, no args):**
1. Load library skill list
2. Load current manifest
3. Present interactive list: pre-check selected skills, space to toggle, `/` to fuzzy search
4. On confirm: compute diff (added, removed), write manifest atomically, reconcile

**Requires manifest.** If absent: exit `1`.

**Output (arg mode):**
```
added:   golang-context
added:   golang-error-handling
manifest: 6 skills
```

**Output (TUI confirm):**
```
added:   golang-context
added:   golang-error-handling
removed: bamboohr
manifest: 6 skills
```

---

### `instill check-skills`

**Purpose:** Full reconciler — make `.claude/skills/` symlinks exactly match the manifest.

**Three operations (in order):**

1. **Remove orphaned symlinks** — symlink exists in `.claude/skills/` but name not in manifest
2. **Remove broken symlinks** — name is in manifest but target no longer exists in library
   - Also removes the name from the manifest
   - Prints: `removed: <name> (no longer in library)`
3. **Create missing symlinks** — name is in manifest, target exists in library, but symlink absent
   - Creates: `.claude/skills/<name>` → `<library_path>/<name>`

**Always exits `0`** when reconciliation completes (removals are expected, not errors).
Exits `1` only if manifest is malformed (cannot parse JSON).
Exits `3` if symlink creation/deletion fails due to filesystem error.

**Out-of-project:** Exit `0` silently — nothing to reconcile.

**Output:**
```
removed: bamboohr (no longer in library)
created: golang-context → /Users/you/ObsidianNotes/agent_config/skills/golang-context
ok: 5 skills linked
```
*(silent when no changes needed)*

---

### `instill add-hooks`

**Purpose:** Inject `instill check-skills` as a `SessionStart` hook into `.claude/settings.json`.

**Behavior:**
1. If no TTY: exit `0` silently — hooks are dev-time, not CI/CD concern
2. Read `.claude/settings.json` (create if absent)
3. Check if a `SessionStart` hook with command `instill check-skills` already exists
4. If present: exit `0` — `already configured`
5. Else: merge new hook entry idempotently:
   ```json
   "SessionStart": [
     {
       "hooks": [{ "command": "instill check-skills", "type": "command" }],
       "matcher": ""
     }
   ]
   ```
6. Write `.claude/settings.json` atomically

**Requires manifest.** If absent: exit `1`.

**Output:**
```
added SessionStart hook: instill check-skills
```
or
```
already configured
```

---

## 4. State Machine

```
┌─────────────────────────────────────────────────────────┐
│                    GLOBAL CONFIG                         │
│                                                          │
│   [missing] ──(TTY prompt)──► [present]                 │
│   [missing] ──(no TTY)──────► EXIT 2                    │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                  PROJECT LIFECYCLE                        │
│                                                          │
│   [no manifest]                                          │
│       │                                                  │
│       ▼ instill init-project                             │
│   [manifest: empty]                                      │
│       │                                                  │
│       ▼ instill pick-skills (TUI/args)                   │
│   [manifest: N skills] ◄──────────────────┐             │
│       │                                    │             │
│       ▼ instill check-skills               │             │
│   [symlinks: reconciled]                   │             │
│       │                                    │             │
│       ├── skill removed from library ──► remove from     │
│       │                                   manifest +     │
│       │                                   symlink        │
│       ├── skill added to project ───────► create symlink │
│       │                                                  │
│       └── pick-skills again ───────────────────────────►┘│
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                SESSION HOOK FLOW                         │
│                                                          │
│   Claude session starts                                  │
│       │                                                  │
│       ▼ SessionStart hook fires                          │
│   instill check-skills                                   │
│       │                                                  │
│       ▼                                                  │
│   symlinks = manifest  ──► continue (silent)            │
│   drift detected       ──► reconcile + report           │
└─────────────────────────────────────────────────────────┘
```

---

## 5. Pseudocode

### `resolveLibraryPath() → string`
```
if SKILL_LIBRARY_PATH is set:
    path = SKILL_LIBRARY_PATH
else if ~/.config/instill/config.json exists:
    path = config["library_path"]
else if isTTY():
    path = prompt("Library path", default=detectDefault())
    writeConfig(path)
else:
    exit(2, "no library path configured; set SKILL_LIBRARY_PATH")

if not directoryExists(path):
    exit(2, "library not found: " + path)

return path
```

### `reconcile(manifestPath, symlinkDir, libraryPath)`
```
manifest = readManifest(manifestPath)   // exit 1 if malformed
selected = Set(manifest.skills)
existing = Set(listSymlinks(symlinkDir))

// 1. Remove orphaned symlinks
for name in existing - selected:
    removeSymlink(symlinkDir / name)    // exit 3 on failure

// 2. Remove broken symlinks (skill deleted from library)
for name in selected:
    if not directoryExists(libraryPath / name):
        removeSymlink(symlinkDir / name)
        selected.remove(name)
        print("removed: " + name + " (no longer in library)")

// 3. Create missing symlinks
for name in selected:
    target = symlinkDir / name
    if not symlinkExists(target):
        createSymlink(target, libraryPath / name)  // exit 3 on failure
        print("created: " + name)

if changed(manifest.skills, selected):
    writeManifest(manifestPath, sorted(selected))  // exit 3 on failure

print("ok: " + len(selected) + " skills linked")  // silent if no changes
exit(0)
```

### `pickSkillsArgs(names[], libraryPath, manifestPath)`
```
// Validate ALL before changing anything
for name in names:
    if not directoryExists(libraryPath / name):
        exit(1, "unknown skill: " + name + " — run 'instill show-library'")

manifest = readManifest(manifestPath)
newSkills = sorted(Set(manifest.skills) ∪ Set(names))
writeManifestAtomic(manifestPath, newSkills)  // write tmp, rename
reconcile(manifestPath, symlinkDir, libraryPath)
```

### `writeManifestAtomic(path, skills)`
```
tmp = path + ".tmp"
write(tmp, {"skills": sorted(skills)})
rename(tmp, path)   // atomic on POSIX
```

---

## 6. Example Session

### First-time setup on a new machine
```bash
# No config yet — first run auto-prompts
$ instill init-project
Library path [~/ObsidianNotes/agent_config/skills/]: ↵
Config written: ~/.config/instill/config.json

initialized: .claude/skill-manifest.json
created:     .claude/skills/
updated:     .gitignore (+.claude/skills/)

# TUI launches automatically
# [ ] bash-defensive-patterns
# [ ] docker
# [✓] golang-cli          ← space to toggle
# [ ] golang-context
# [✓] golang-testing
# /golang                  ← fuzzy filter active
# Confirm: 3 skills selected

added:   golang-cli
added:   golang-testing
added:   golang-error-handling
manifest: 3 skills
```

### Adding skills later
```bash
$ instill pick-skills golang-context golang-concurrency
added:   golang-concurrency
added:   golang-context
manifest: 5 skills
```

### Removing a skill via args
```bash
$ instill pick-skills --remove golang-concurrency
removed: golang-concurrency
manifest: 4 skills
```

### Viewing library with filter
```bash
$ instill show-library --filter golang
[✓] golang-cli
[✓] golang-context
[ ] golang-benchmark
[ ] golang-concurrency
[✓] golang-error-handling
[✓] golang-testing
[ ] golang-grpc
...
12 skills  (4 selected)
```

### Installing hooks
```bash
$ instill add-hooks
added SessionStart hook: instill check-skills
```

### Cloning the repo on a second machine
```bash
$ git clone git@github.com:you/my-project.git
$ cd my-project

# .claude/skills/ is empty — gitignored, not committed
# .claude/skill-manifest.json is present — committed

$ SKILL_LIBRARY_PATH=~/my-skills instill check-skills
created: golang-cli → ~/my-skills/golang-cli
created: golang-context → ~/my-skills/golang-context
created: golang-error-handling → ~/my-skills/golang-error-handling
created: golang-testing → ~/my-skills/golang-testing
ok: 4 skills linked
```

### A skill removed from library — auto-healed on next session
```bash
# Developer deletes ~/ObsidianNotes/agent_config/skills/golang-context/
# Next Claude session starts, SessionStart hook fires:

$ instill check-skills
removed: golang-context (no longer in library)
ok: 3 skills linked
```

### CI/CD usage
```yaml
# .github/workflows/check.yml
- name: Validate skill manifest
  env:
    SKILL_LIBRARY_PATH: /opt/shared-skills
  run: instill check-skills
# exits 0 if symlinks match manifest, 1 if manifest malformed, 3 on fs error
```

---

## 7. Error Handling Reference

| Scenario | Command | Behavior | Exit |
|----------|---------|----------|------|
| No global config, no TTY | any | `error: no library path configured; set SKILL_LIBRARY_PATH` | 2 |
| Library path not found | any | `error: library not found: <path>` | 2 |
| Manifest already exists | `init-project` | `error: manifest already exists; use --force to reinitialize` | 1 |
| Not a git repo | `init-project` | `warning: not a git repository — manifest will not be committed` (continues) | — |
| Unknown skill name | `pick-skills` (args) | `error: unknown skill: <name> — run 'instill show-library'` — nothing applied | 1 |
| Malformed manifest JSON | any | `error: cannot parse .claude/skill-manifest.json` | 1 |
| No manifest in directory | `pick-skills`, `add-hooks` | `error: no manifest found — run 'instill init-project' first` | 1 |
| Skill removed from library | `check-skills` (implicit) | `removed: <name> (no longer in library)` — auto-removes | 0 |
| Filesystem write failure | any | `error: cannot write <path>: <os error>` | 3 |
| Hook already present | `add-hooks` | `already configured` | 0 |
| No TTY | `add-hooks` | silent no-op | 0 |
| Out-of-project | `check-skills` | silent no-op | 0 |
| Out-of-project | `show-library` | plain list (no annotations) | 0 |

---

## 8. File Inventory

| Path | Committed | Owner |
|------|-----------|-------|
| `.claude/skill-manifest.json` | Yes | `instill` |
| `.claude/skills/` | No (gitignored) | `instill` |
| `.claude/skills/<name>` | No | `instill` |
| `.claude/settings.json` | Yes | `instill add-hooks` (merges) |
| `.gitignore` | Yes | `instill init-project` (appends) |
| `~/.config/instill/config.json` | N/A (user-global) | `instill` |

---

*Authored By Peter O'Connor with Assistance from Claude Code (claude-sonnet-4-6) · 2026-05-12 · instill design specification*
