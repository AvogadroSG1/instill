# instill APM Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace instill's legacy skill-manifest and symlink reconciliation workflow with an APM-backed CLI that curates a CSV library catalog, writes `apm.yml`, and shells out to `apm` for install and compile.

**Architecture:** Keep Cobra command wiring in `internal/cli` and move durable behavior into focused `internal/instill` files. The new core model reads per-type library catalogs, writes project-local APM manifest and copied local content, and delegates dependency resolution, lockfile updates, security scanning, and harness rendering to the external `apm` CLI.

**Tech Stack:** Go 1.24, Cobra, Bubbletea, standard-library CSV/YAML helpers where available, current `ExitError`, `writeFileAtomic`, and `commandConfig` injection patterns.

## Global Constraints

- The approved spec is `docs/superpowers/specs/2026-07-08-instill-apm-integration-design.md`.
- instill MUST own the library catalog and picker UX; APM MUST own resolution, lockfile management, security scanning, and harness targeting.
- Library path MUST be stored in `~/.config/instill/config.json` and MUST be overridable via `INSTILL_LIBRARY_PATH`.
- Library-only commands (`library scan`, `library add`, `library show`) MUST NOT bootstrap or invoke APM.
- Commands that interact with project APM state (`init`, `pick`, `sync`, `status`, `import`) MUST ensure APM is available before acting.
- Missing `brew` during APM bootstrap MUST return exit code 2 with `error: brew required to install apm; install from https://brew.sh`.
- `MIN_APM_VERSION` and `APM_BREW_FORMULA` MUST be source constants.
- `instill sync` MUST run `apm install`, then `apm compile`, then report `ok: synced N skills, M mcp servers, P instructions, Q prompts`.
- Skills MUST map to `dependencies.apm` local path entries.
- MCP servers MUST map to `dependencies.mcp` blocks with `name`, `command`, `args`, `env`, and `url`.
- Instructions MUST be copied, not symlinked, into `.apm/instructions/<name>.instructions.md`.
- Prompts MUST be copied, not symlinked, into `.apm/prompts/<name>.prompt.md`.
- `instill add-hooks` MUST register `instill sync` as the Claude Code `SessionStart` hook.
- Red-Green-Refactor is REQUIRED for every production-code change.
- Commits MUST be small and MUST include both required co-authors.

---

## File Structure

- Create `internal/instill/catalog.go`: catalog types, CSV load/write, scan helpers, and catalog row validation.
- Create `internal/instill/apm_manifest.go`: `apm.yml` read/write, dependency mutation, local content copy paths, and sync summary counts.
- Create `internal/instill/apm_runner.go`: APM and brew command runner abstraction, version check, bootstrap, install, compile, and prune.
- Create `internal/instill/sync.go`: project sync and status orchestration.
- Create `internal/instill/import.go`: importers for old instill, graft, Claude config, and generic directories.
- Modify `internal/instill/config.go`: rename env behavior to `INSTILL_LIBRARY_PATH` while preserving a compatibility fallback for `SKILL_LIBRARY_PATH` during migration.
- Modify `internal/instill/project.go`: project discovery now keys off `apm.yml`; legacy manifest discovery is only used by `import old-instill`.
- Modify `internal/instill/init_project.go`: initialize `apm.yml`, not `.claude/skill-manifest.json` or symlink directories.
- Modify `internal/instill/pick_skills.go` and `internal/instill/skill_picker.go`: generalize skill-only selection to typed catalog selection.
- Modify `internal/instill/show_library.go`: list typed library catalog contents.
- Modify `internal/instill/add_hooks.go`: change hook command from `instill check-skills` to `instill sync`.
- Modify `internal/cli/root.go`: command surface becomes `init`, `pick`, `sync`, `status`, `library scan|add|show`, `import`, `bootstrap`, and `add-hooks`.
- Modify or replace legacy CLI command files under `internal/cli/` so old command names either disappear or return migration-friendly guidance.
- Update tests under `internal/instill/*_test.go`, `internal/cli/*_test.go`, and `test/instill.bats`.
- Update `README.md`, `CLAUDE.md`, and add `docs/adr/0002-apm-backed-library-catalog.md`.

---

### Task 1: APM Bootstrap and Project Manifest Primitives

**Files:**
- Create: `internal/instill/apm_runner.go`
- Create: `internal/instill/apm_manifest.go`
- Modify: `internal/instill/config.go`
- Modify: `internal/instill/project.go`
- Test: `internal/instill/apm_runner_test.go`
- Test: `internal/instill/apm_manifest_test.go`
- Test: `internal/instill/config_test.go`
- Test: `internal/instill/project_test.go`

**Interfaces:**
- Consumes: existing `ExitError`, `writeFileAtomic`, and `expandHome`.
- Produces:
  - `const APMBrewFormula = "apm"`
  - `const MinAPMVersion = "0.1.0"`
  - `type CommandRunner func(name string, args ...string) ([]byte, error)`
  - `func EnsureAPM(runner CommandRunner) error`
  - `func RunAPMInstall(runner CommandRunner, root string) error`
  - `func RunAPMCompile(runner CommandRunner, root string) error`
  - `type APMManifest struct { Dependencies APMDependencies `yaml:"dependencies"` }`
  - `type APMDependencies struct { APM []string `yaml:"apm,omitempty"`; MCP []MCPDependency `yaml:"mcp,omitempty"` }`
  - `func ReadAPMManifest(path string) (APMManifest, error)`
  - `func WriteAPMManifestAtomic(path string, manifest APMManifest) error`
  - `func ProjectAPMPath(root string) string`

- [ ] **Step 1: Write failing APM bootstrap tests**

Add tests that prove:

```go
func TestEnsureAPMInstallsWithBrewWhenMissing(t *testing.T) {
	calls := []string{}
	runner := func(name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch name + " " + strings.Join(args, " ") {
		case "apm --version":
			if len(calls) == 1 {
				return nil, exec.ErrNotFound
			}
			return []byte("apm 0.1.0\n"), nil
		case "brew --version", "brew install apm":
			return []byte("ok\n"), nil
		default:
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		return nil, nil
	}

	err := EnsureAPM(runner)

	requireNoError(t, err)
	requireEqual(t, []string{"apm --version", "brew --version", "brew install apm", "apm --version"}, calls)
}
```

Also test missing brew returns `ExitEnvironment` and the exact message from the spec.

- [ ] **Step 2: Run the APM bootstrap tests to verify RED**

Run: `go test ./internal/instill -run 'TestEnsureAPM' -v`

Expected: FAIL because `EnsureAPM`, `APMBrewFormula`, and `MinAPMVersion` do not exist.

- [ ] **Step 3: Implement minimal APM runner**

Implement `internal/instill/apm_runner.go` with command injection. Use `exec.Command` only in a default runner; keep `EnsureAPM` pure over `CommandRunner` for tests.

- [ ] **Step 4: Write failing APM manifest tests**

Add tests that prove:

```go
func TestWriteAPMManifestAtomicWritesYAMLDependencies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apm.yml")
	manifest := APMManifest{Dependencies: APMDependencies{
		APM: []string{"/library/skills/golang-testing"},
		MCP: []MCPDependency{{Name: "local-db", Command: "sqlite-mcp", Args: []string{"--db", "dev.db"}}},
	}}

	err := WriteAPMManifestAtomic(path, manifest)

	requireNoError(t, err)
	data := readFile(t, path)
	requireContains(t, data, "dependencies:")
	requireContains(t, data, "- /library/skills/golang-testing")
	requireContains(t, data, "name: local-db")
}
```

Also test read normalizes missing `dependencies` to empty slices and malformed YAML returns `ExitGeneral`.

- [ ] **Step 5: Run the manifest tests to verify RED**

Run: `go test ./internal/instill -run 'Test.*APMManifest|TestWriteAPMManifest' -v`

Expected: FAIL because APM manifest types and functions do not exist.

- [ ] **Step 6: Implement minimal manifest read/write**

Use `gopkg.in/yaml.v3` for YAML. If the module is missing, add it with `go get gopkg.in/yaml.v3`, then commit `go.mod` and `go.sum` with this task.

- [ ] **Step 7: Write failing config and project discovery tests**

Update tests so `INSTILL_LIBRARY_PATH` has precedence, `SKILL_LIBRARY_PATH` remains a compatibility fallback, config JSON still works, and `FindProject` finds `apm.yml` at the project root.

- [ ] **Step 8: Run config/project tests to verify RED**

Run: `go test ./internal/instill -run 'TestResolveLibraryPath|TestFindProject' -v`

Expected: FAIL on `INSTILL_LIBRARY_PATH` and `apm.yml` discovery until code changes land.

- [ ] **Step 9: Implement config and project changes**

Modify `config.go` and `project.go` so new behavior passes while retaining old-manifest helpers only for import code.

- [ ] **Step 10: Run task verification**

Run: `go test ./internal/instill -run 'TestEnsureAPM|Test.*APMManifest|TestWriteAPMManifest|TestResolveLibraryPath|TestFindProject' -v`

Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add go.mod go.sum internal/instill/apm_runner.go internal/instill/apm_runner_test.go internal/instill/apm_manifest.go internal/instill/apm_manifest_test.go internal/instill/config.go internal/instill/config_test.go internal/instill/project.go internal/instill/project_test.go
git commit -m "feat: add APM project primitives" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```

---

### Task 2: CSV Library Catalog Model

**Files:**
- Create: `internal/instill/catalog.go`
- Test: `internal/instill/catalog_test.go`
- Modify: `internal/instill/library.go`
- Modify: `internal/instill/show_library.go`
- Test: `internal/instill/library_test.go`
- Test: `internal/instill/show_library_test.go`

**Interfaces:**
- Consumes: `IsValidSkillName`, `writeFileAtomic`, and `ConfigResolverOptions`.
- Produces:
  - `type LibraryType string`
  - `const LibraryTypeSkill`, `LibraryTypeMCP`, `LibraryTypeInstruction`, `LibraryTypePrompt`
  - `type CatalogEntry struct { Type LibraryType; Name string; Category string; Path string; Transport string; Command string; Args []string; URL string; Env []string; ApplyTo string; Description string }`
  - `func LoadCatalog(root string, typ LibraryType) ([]CatalogEntry, error)`
  - `func WriteCatalog(root string, typ LibraryType, entries []CatalogEntry) error`
  - `func ScanLibrary(root string, stdout io.Writer) error`
  - `func AddCatalogEntry(root string, entry CatalogEntry) error`
  - `func ShowCatalog(root string, typ LibraryType, filter string, stdout io.Writer) error`

- [ ] **Step 1: Write failing catalog CSV load/write tests**

Tests MUST cover the exact spec schemas for skills, mcp, instructions, and prompts. Include one test where MCP `transport=stdio` without `command` fails, and one where `transport=http` without `url` fails.

- [ ] **Step 2: Run catalog CSV tests to verify RED**

Run: `go test ./internal/instill -run 'TestLoadCatalog|TestWriteCatalog' -v`

Expected: FAIL because catalog APIs do not exist.

- [ ] **Step 3: Implement catalog CSV load/write**

Use `encoding/csv`, strict required-column validation, stable name sorting, and RFC 2119 style error strings through `ExitError`.

- [ ] **Step 4: Write failing scan tests**

Tests MUST create a temp library with:

```text
skills/cloud/azure/azure-cli/SKILL.md
mcp/local-db/config.json
instructions/python-rules/INSTRUCTION.md
prompts/debug/PROMPT.md
```

Then assert `ScanLibrary` writes four `catalog.csv` files and prints removed rows when an existing CSV row points to missing content.

- [ ] **Step 5: Run scan tests to verify RED**

Run: `go test ./internal/instill -run 'TestScanLibrary' -v`

Expected: FAIL until scan is implemented.

- [ ] **Step 6: Implement scan, add, and show behavior**

Preserve valid manual CSV metadata when marker content still exists. Remove rows whose marker file is gone and print `removed: <name> (content not found)`.

- [ ] **Step 7: Run task verification**

Run: `go test ./internal/instill -run 'Test.*Catalog|TestScanLibrary|TestShow' -v`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/instill/catalog.go internal/instill/catalog_test.go internal/instill/library.go internal/instill/library_test.go internal/instill/show_library.go internal/instill/show_library_test.go
git commit -m "feat: add typed library catalogs" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```

---

### Task 3: Project Init, Pick, Sync, and Status

**Files:**
- Create: `internal/instill/sync.go`
- Modify: `internal/instill/init_project.go`
- Modify: `internal/instill/pick_skills.go`
- Modify: `internal/cli/init_project.go`
- Modify: `internal/cli/pick_skills.go`
- Create: `internal/cli/sync.go`
- Create: `internal/cli/status.go`
- Test: `internal/instill/init_project_test.go`
- Test: `internal/instill/pick_skills_test.go`
- Test: `internal/instill/sync_test.go`
- Test: `internal/cli/init_project_test.go`
- Test: `internal/cli/pick_skills_test.go`
- Test: `internal/cli/sync_test.go`
- Test: `internal/cli/status_test.go`

**Interfaces:**
- Consumes: Task 1 APM manifest and runner APIs; Task 2 catalog APIs.
- Produces:
  - `type SyncOptions struct { Project Project; LibraryPath string; Runner CommandRunner; Stdout io.Writer }`
  - `func SyncProject(opts SyncOptions) error`
  - `func ProjectStatus(opts StatusOptions) error`
  - `type PickOptions struct { Project Project; LibraryPath string; Add []string; Remove []string; Type LibraryType; Runner CommandRunner; Stdout io.Writer }`
  - `func Pick(opts PickOptions) error`

- [ ] **Step 1: Write failing init tests**

Tests MUST assert `InitProject` creates `apm.yml`, does not create `.claude/skill-manifest.json`, `.claude/skills`, or `.agents/skills`, and invokes `apm install` when initialized with selected skills.

- [ ] **Step 2: Run init tests to verify RED**

Run: `go test ./internal/instill -run 'TestInitProject' -v`

Expected: FAIL because legacy manifest/symlink behavior still exists.

- [ ] **Step 3: Implement APM-backed init**

Write empty `apm.yml` when no selections exist. For selected skills, resolve catalog rows and write `dependencies.apm` local paths before `apm install`.

- [ ] **Step 4: Write failing pick tests**

Tests MUST assert:
- `Pick` adds a skill path to `dependencies.apm` and runs `apm install`.
- `Pick` adds an MCP block to `dependencies.mcp` and runs `apm install`.
- `Pick` copies an instruction into `.apm/instructions/<name>.instructions.md` and runs `apm install`.
- `Pick` copies a prompt into `.apm/prompts/<name>.prompt.md` and runs `apm install`.
- `Pick` removal updates `apm.yml` and calls `apm prune`.

- [ ] **Step 5: Run pick tests to verify RED**

Run: `go test ./internal/instill -run 'TestPick' -v`

Expected: FAIL until pick behavior is generalized.

- [ ] **Step 6: Implement generalized pick**

Keep old `PickSkills` wrappers only if needed by tests, but make CLI route through `Pick`. Copies MUST use file contents, not symlinks.

- [ ] **Step 7: Write failing sync/status tests**

`SyncProject` MUST call `apm install`, then `apm compile`, then write the exact summary format. `ProjectStatus` MUST report project items removed from library, available library items not yet in project, and content hash mismatches when lock data differs.

- [ ] **Step 8: Run sync/status tests to verify RED**

Run: `go test ./internal/instill -run 'TestSyncProject|TestProjectStatus' -v`

Expected: FAIL until sync/status exist.

- [ ] **Step 9: Implement sync and status**

Hash local catalog content with SHA-256. Treat lock hash parsing defensively: missing lock data is informational, not fatal.

- [ ] **Step 10: Wire CLI commands**

Update Cobra commands so:
- `instill init`
- `instill pick`
- `instill sync`
- `instill status`

all bootstrap APM through injected runners for tests.

- [ ] **Step 11: Run task verification**

Run: `go test ./internal/instill ./internal/cli -run 'TestInitProject|TestPick|TestSync|TestStatus' -v`

Expected: PASS.

- [ ] **Step 12: Commit**

```bash
git add internal/instill/sync.go internal/instill/sync_test.go internal/instill/init_project.go internal/instill/init_project_test.go internal/instill/pick_skills.go internal/instill/pick_skills_test.go internal/cli/init_project.go internal/cli/init_project_test.go internal/cli/pick_skills.go internal/cli/pick_skills_test.go internal/cli/sync.go internal/cli/sync_test.go internal/cli/status.go internal/cli/status_test.go
git commit -m "feat: sync projects through APM" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```

---

### Task 4: Library, Import, Bootstrap, and Hook CLI Surface

**Files:**
- Create: `internal/cli/library.go`
- Create: `internal/cli/import.go`
- Create: `internal/cli/bootstrap.go`
- Create: `internal/instill/import.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/add_hooks.go`
- Modify: `internal/instill/add_hooks.go`
- Test: `internal/cli/library_test.go`
- Test: `internal/cli/import_test.go`
- Test: `internal/cli/bootstrap_test.go`
- Test: `internal/cli/add_hooks_test.go`
- Test: `internal/instill/import_test.go`
- Test: `internal/instill/add_hooks_test.go`

**Interfaces:**
- Consumes: Tasks 1-3.
- Produces:
  - `func ImportOldInstill(opts ImportOptions) error`
  - `func ImportGraft(opts ImportOptions) error`
  - `func ImportClaude(opts ImportOptions) error`
  - `func ImportDirectory(opts ImportDirectoryOptions) error`

- [ ] **Step 1: Write failing CLI command surface tests**

Assert root help includes `pick`, `sync`, `status`, `library`, `import`, `bootstrap`, and `add-hooks`; assert legacy `check-skills`, `pick-skills`, and `show-library` are no longer primary commands.

- [ ] **Step 2: Run CLI surface tests to verify RED**

Run: `go test ./internal/cli -run 'TestRootCommand|TestLibraryCommand|TestBootstrapCommand' -v`

Expected: FAIL until commands are rewired.

- [ ] **Step 3: Implement `library` and `bootstrap` commands**

`library scan`, `library add`, and `library show` MUST skip APM bootstrap. `bootstrap` MUST only call `EnsureAPM`.

- [ ] **Step 4: Write failing import tests**

Tests MUST cover:
- `import old-instill` reads `.claude/skill-manifest.json`, writes catalog entries and `apm.yml`, removes legacy symlink artifacts.
- `import graft` reads `graft.lock`, writes MCP catalog rows, updates `apm.yml`, and removes `graft.lock` plus graft-managed `.mcp.json`.
- `import claude` reads `~/.claude.json` or `$CLAUDE_CONFIG_DIR/claude.json`, redacts secrets into `${VAR}` placeholders, and writes MCP catalog rows.
- `import directory <path>` scans markers and writes catalogs.

- [ ] **Step 5: Run import tests to verify RED**

Run: `go test ./internal/instill ./internal/cli -run 'TestImport' -v`

Expected: FAIL until import APIs and commands exist.

- [ ] **Step 6: Implement import commands**

Keep import formats conservative and fixture-backed. Remove only files that the importer proves are managed legacy artifacts.

- [ ] **Step 7: Write failing hook tests**

Assert `AddHooks` inserts `instill sync` and treats an existing `instill check-skills` hook as replaceable legacy state.

- [ ] **Step 8: Run hook tests to verify RED**

Run: `go test ./internal/instill ./internal/cli -run 'TestAddHooks' -v`

Expected: FAIL until hook command changes.

- [ ] **Step 9: Implement hook update**

Change `hookCommand` to `instill sync`. Replace old managed hook entries where present while preserving unrelated hooks.

- [ ] **Step 10: Run task verification**

Run: `go test ./internal/instill ./internal/cli -run 'TestRootCommand|TestLibraryCommand|TestBootstrapCommand|TestImport|TestAddHooks' -v`

Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/cli/root.go internal/cli/library.go internal/cli/library_test.go internal/cli/import.go internal/cli/import_test.go internal/cli/bootstrap.go internal/cli/bootstrap_test.go internal/cli/add_hooks.go internal/cli/add_hooks_test.go internal/instill/import.go internal/instill/import_test.go internal/instill/add_hooks.go internal/instill/add_hooks_test.go
git commit -m "feat: add APM command surface" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```

---

### Task 5: Unified Picker TUI

**Files:**
- Modify: `internal/instill/skill_picker.go`
- Modify: `internal/instill/skill_picker_test.go`
- Modify: `internal/cli/pick_skills.go`
- Modify: `internal/cli/pick_skills_test.go`

**Interfaces:**
- Consumes: `CatalogEntry`, `Pick`, and `LibraryType`.
- Produces:
  - `type PickTUIOptions struct { Project Project; LibraryPath string; InitialType LibraryType; Stdin *os.File; Stdout io.Writer; Stderr io.Writer; Runner CommandRunner }`
  - `func RunPickTUI(opts PickTUIOptions) error`

- [ ] **Step 1: Write failing TUI model tests**

Tests MUST assert the top-level model renders four primitive rows with available and installed counts:

```text
▶ Skills (47 available, 12 installed)
  MCP Servers (8 available, 3 installed)
  Instructions (5 available, 2 installed)
  Prompts (9 available, 4 installed)
```

Use small fixtures instead of exact counts above; the format MUST match.

- [ ] **Step 2: Run TUI model tests to verify RED**

Run: `go test ./internal/instill -run 'TestPick.*TUI|Test.*Picker' -v`

Expected: FAIL while picker is skill-only.

- [ ] **Step 3: Implement four-type picker model**

Keep Bubbletea dependencies unchanged. Folders are navigable, entries are selectable, type-to-filter narrows by name, and `a` applies via `Pick`.

- [ ] **Step 4: Write failing CLI jump/add/remove tests**

Assert:
- `instill pick --type=mcp` starts at MCP.
- `instill pick <name>...` non-interactively adds entries.
- `instill pick --remove <name>...` removes entries.

- [ ] **Step 5: Run CLI pick tests to verify RED**

Run: `go test ./internal/cli -run 'TestPick' -v`

Expected: FAIL until command flags and routing are updated.

- [ ] **Step 6: Implement CLI picker routes**

Update `commandConfig` TUI injection from skill-specific options to typed picker options. Preserve testability without a real terminal.

- [ ] **Step 7: Run task verification**

Run: `go test ./internal/instill ./internal/cli -run 'TestPick|Test.*Picker' -v`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/instill/skill_picker.go internal/instill/skill_picker_test.go internal/cli/pick_skills.go internal/cli/pick_skills_test.go
git commit -m "feat: generalize picker across library types" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```

---

### Task 6: Legacy Cleanup, Docs, ADR, and End-to-End Verification

**Files:**
- Modify: `internal/cli/check_skills.go`
- Modify: `internal/cli/check_skills_test.go`
- Modify: `internal/instill/reconcile.go`
- Modify: `internal/instill/reconcile_test.go`
- Modify: `internal/instill/settings_local.go`
- Modify: `internal/instill/settings_local_test.go`
- Modify: `test/instill.bats`
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Create: `docs/adr/0002-apm-backed-library-catalog.md`

**Interfaces:**
- Consumes: all previous tasks.
- Produces: updated contributor-facing docs and final removal or deprecation of legacy symlink/reconcile code.

- [ ] **Step 1: Write failing legacy-removal tests**

Tests MUST assert legacy symlink reconciliation is not used by supported commands and legacy command names do not mutate project state. If a legacy command remains for migration guidance, it MUST exit 1 with a message naming the replacement command.

- [ ] **Step 2: Run legacy-removal tests to verify RED**

Run: `go test ./internal/instill ./internal/cli -run 'TestCheckSkills|TestReconcile|TestSettingsLocal|TestLegacy' -v`

Expected: FAIL while legacy behavior remains active.

- [ ] **Step 3: Remove or deprecate legacy code paths**

Delete dead symlink/settings-local paths only after tests prove no supported command calls them. Keep tiny migration helpers if import tests require them.

- [ ] **Step 4: Update bats smoke tests**

Change smoke tests from `skill-manifest.json` and symlink expectations to `apm.yml`, catalog, `instill sync`, and copied `.apm/` content expectations.

- [ ] **Step 5: Update docs**

README MUST describe the APM-backed model, new commands, `INSTILL_LIBRARY_PATH`, and the sync hook. CLAUDE.md MUST use the new ubiquitous language: Library catalog, APM manifest, Sync, and typed library entries.

- [ ] **Step 6: Add ADR**

Create `docs/adr/0002-apm-backed-library-catalog.md` in MADR style. Decision: instill becomes the curated library UX and APM becomes the resolution/sync engine. Consequences MUST include reduced local symlink ownership and reliance on external APM availability.

- [ ] **Step 7: Run full verification**

Run:

```bash
go test ./...
bats test/instill.bats
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/check_skills.go internal/cli/check_skills_test.go internal/instill/reconcile.go internal/instill/reconcile_test.go internal/instill/settings_local.go internal/instill/settings_local_test.go test/instill.bats README.md CLAUDE.md docs/adr/0002-apm-backed-library-catalog.md
git commit -m "docs: document APM-backed instill workflow" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```

---

## Self-Review

- **Spec coverage:** The plan maps every command in the approved command surface to a task. It covers CSV catalogs, APM bootstrap, picker mapping, sync/status, hooks, import paths, legacy cleanup, docs, and ADR.
- **Scope check:** This is a large migration, but the tasks are reviewable independently: primitives, catalog, project commands, command surface/imports, TUI, cleanup/docs.
- **Placeholder scan:** No `TBD`, `TODO`, or unspecified command names remain.
- **Type consistency:** Later tasks consume names produced by earlier tasks: `APMManifest`, `CatalogEntry`, `LibraryType`, `CommandRunner`, `Pick`, `SyncProject`, and `RunPickTUI`.

*Authored By Peter O'Connor with Assistance from Codex (GPT-5) · 2026-07-08 · instill APM integration implementation plan*
