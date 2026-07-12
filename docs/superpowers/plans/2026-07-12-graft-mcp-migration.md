# Graft MCP Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `instill import graft` understand the live Graft lock format, include every Graft-managed MCP Server, and migrate Peter's three live definitions into the APM-backed Library.

**Architecture:** Extend the existing Graft import adapter rather than adding a second migration path. Parse both lock representations with `yaml.v3`, derive selected names from the lock plus `_graft_managed` markers, validate the complete selection before mutation, then reuse the existing catalog, manifest, and cleanup pipeline. Preserve the live source in `scratch_work` before invoking migration and verify through Instill's public catalog command.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3`, Go `testing`, Cobra CLI, Beads.

## Global Constraints

- The importer MUST support current `mcps` objects and legacy `servers` names.
- The selected set MUST be the normalized union of lock-selected and `_graft_managed: true` definitions.
- Unmanaged, unlocked entries MUST NOT be imported.
- Every selected name MUST be validated before any mutation.
- Empty selection MUST fail without removing migration inputs.
- A newly created APM manifest MUST contain the required project `name` and version `0.1.0`.
- Transport, command, arguments, URL, environment references, and existing descriptions MUST survive migration.
- The live source MUST be backed up before migration and MUST remain recoverable until verification succeeds.
- Go changes MUST follow BDD-style Red-Green-Refactor and MUST be formatted with `gofmt`.

---

### Task 1: Parse Current Locks and Select Graft-Managed MCP Servers

**Files:**
- Modify: `internal/instill/import_test.go`
- Modify: `internal/instill/import.go`

**Interfaces:**
- Consumes: `ImportGraft(ImportOptions) error`, `readGraftLock(path string) ([]string, error)`, and raw `.mcp.json` definitions.
- Produces: `selectedGraftServers(locked []string, servers map[string]json.RawMessage) ([]string, error)`.

- [ ] **Step 1: Write the failing current-format behavior test**

Add `TestImportGraftImportsCurrentLockAndManagedUnlockedServers`. Use a current JSON lock whose `mcps` array selects `Stack Internal` and `serena`. Use a `.mcp.json` containing those two, managed-but-unlocked `markitdown`, and unmanaged-unlocked `manual`:

```go
requireNoError(t, os.WriteFile(filepath.Join(root, "graft.lock"), []byte(`{
  "libraries": [{"name":"peter_mcps","url":"/cache/peter_mcps"}],
  "mcps": [
    {"name":"Stack Internal","library":"peter_mcps","version":"0.1.0","target":"both"},
    {"name":"serena","library":"peter_mcps","version":"0.1.0","target":"both"}
  ]
}`), 0o644))
requireNoError(t, os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
  "mcpServers": {
    "Stack Internal": {"_graft_managed":true,"command":"npx","args":["mcp-remote","https://stackinternal.stackenterprise.co/mcp"]},
    "markitdown": {"_graft_managed":true,"command":"uvx","args":["markitdown-mcp"]},
    "serena": {"_graft_managed":true,"command":"uvx","args":["serena","start-mcp-server"],"env":{"SSL_CERT_FILE":"${SSL_CERT_FILE}"}},
    "manual": {"command":"manual-mcp"}
  }
}`), 0o644))
```

After `ImportGraft`, load the catalog and manifest. Assert exactly the three expected names, preserved commands/arguments/environment reference, exclusion of `manual`, `manifest.Name == filepath.Base(root)`, and `manifest.Version == "0.1.0"`.

- [ ] **Step 2: Run the new test and verify RED**

Run: `go test ./internal/instill -run '^TestImportGraftImportsCurrentLockAndManagedUnlockedServers$' -count=1 -v`

Expected: FAIL because current `mcps` names and managed markers are ignored.

- [ ] **Step 3: Write the failing empty-selection safety test**

Add `TestImportGraftRejectsEmptySelectionWithoutMutation`. Write `graft.lock` as `{}` and `.mcp.json` with only unmanaged `manual`. Assert an error containing `no Graft-managed MCP servers found`, byte-identical source files, no catalog, and no manifest.

- [ ] **Step 4: Run the safety test and verify RED**

Run: `go test ./internal/instill -run '^TestImportGraftRejectsEmptySelectionWithoutMutation$' -count=1 -v`

Expected: FAIL because the importer currently reports success and removes `graft.lock`.

- [ ] **Step 5: Implement the minimal compatibility adapter**

Replace the lock model:

```go
type graftLockMCP struct {
	Name string `yaml:"name"`
}

type graftLockFile struct {
	Servers []string       `yaml:"servers"`
	MCPs    []graftLockMCP `yaml:"mcps"`
}
```

Update `readGraftLock` to append each non-empty `MCPs[i].Name` to `Servers` and return `normalizeStringSlice(names)`.

Add:

```go
type graftManagedMarker struct {
	Managed bool `json:"_graft_managed"`
}

func selectedGraftServers(locked []string, servers map[string]json.RawMessage) ([]string, error) {
	names := append([]string{}, locked...)
	for name, raw := range servers {
		var marker graftManagedMarker
		if err := json.Unmarshal(raw, &marker); err != nil {
			return nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed .mcp.json server %q: %v", name, err))
		}
		if marker.Managed {
			names = append(names, name)
		}
	}
	names = normalizeStringSlice(names)
	sort.Strings(names)
	if len(names) == 0 {
		return nil, NewExitError(ExitGeneral, "error: no Graft-managed MCP servers found in graft.lock or .mcp.json")
	}
	return names, nil
}
```

In `ImportGraft`, call this after `rawMCPServers` and before `missingGraftServers`. This placement MUST keep validation ahead of catalog, marker, manifest, and cleanup writes.

Before writing the manifest, ensure the YAML document contains required identity fields when absent:

```go
func ensureAPMManifestIdentity(document *yaml.Node, root string) error {
	mapping, err := apmManifestMapping(document)
	if err != nil {
		return err
	}
	if mappingValue(mapping, "name") == nil {
		setMappingValue(mapping, "name", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: filepath.Base(root)})
	}
	if mappingValue(mapping, "version") == nil {
		setMappingValue(mapping, "version", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "0.1.0"})
	}
	return nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
```

Use the existing mapping helpers and add the shown `mappingValue` lookup. Call `ensureAPMManifestIdentity(document, opts.Project.Root)` after reading the manifest document and before `writeAPMManifestDocumentAtomic`. Existing non-empty `name` and `version` values MUST remain unchanged.

- [ ] **Step 6: Format and verify GREEN**

Run:
```bash
gofmt -w internal/instill/import.go internal/instill/import_test.go
go test ./internal/instill -run 'TestImportGraft' -count=1 -v
```

Expected: every `TestImportGraft*` test PASS, including legacy `servers` coverage.

- [ ] **Step 7: Run package regression tests**

Run: `go test ./internal/instill -count=1`

Expected: PASS.

- [ ] **Step 8: Review the diff against the design**

Run:
```bash
git diff --check
git diff -- internal/instill/import.go internal/instill/import_test.go
```

Confirm current and legacy support, managed-unlocked inclusion, unmanaged-unlocked exclusion, pre-mutation validation, valid manifest identity, and no unrelated refactor.

- [ ] **Step 9: Commit the code slice**

```bash
git add internal/instill/import.go internal/instill/import_test.go
git commit -m "fix: migrate current Graft MCP definitions" -m "Co-Authored-By: Peter O'Connor <poconnor@stackoverflow.com>
Co-Authored-By: Codex <noreply@anthropic.com> - GPT-5"
```

---

### Task 2: Verify the Full CLI and Migrate the Live Library

**Files:**
- Create: `~/peter_code/scratch_work/instill_graft_migration_code/{graft.lock,.mcp.json}`
- Modify: `~/peter_code/ai_support/mcp/catalog.csv`
- Create: `~/peter_code/ai_support/mcp/{Stack Internal,markitdown,serena}/config.json`
- Modify: `~/peter_code/graft/apm.yml`

**Interfaces:**
- Consumes: fixed `instill import graft` and live `~/peter_code/graft/{graft.lock,.mcp.json}`.
- Produces: chooser-visible Library entries and APM dependencies for all three servers.

- [ ] **Step 1: Run full quality gates before live mutation**

Run:
```bash
go test ./...
go vet ./...
```

Expected: both exit 0. Any failure MUST stop live migration.

- [ ] **Step 2: Build the verified CLI**

Run: `go build -o ./bin/instill ./`

Expected: exit 0 and `./bin/instill` exists.

- [ ] **Step 3: Back up live migration inputs**

```bash
mkdir -p "$HOME/peter_code/scratch_work/instill_graft_migration_code"
cp -f "$HOME/peter_code/graft/graft.lock" "$HOME/peter_code/scratch_work/instill_graft_migration_code/graft.lock"
cp -f "$HOME/peter_code/graft/.mcp.json" "$HOME/peter_code/scratch_work/instill_graft_migration_code/.mcp.json"
cmp "$HOME/peter_code/graft/graft.lock" "$HOME/peter_code/scratch_work/instill_graft_migration_code/graft.lock"
cmp "$HOME/peter_code/graft/.mcp.json" "$HOME/peter_code/scratch_work/instill_graft_migration_code/.mcp.json"
```

Expected: all exit 0. Only then MAY `instill-vpl` be closed.

- [ ] **Step 4: Run live migration**

From `~/peter_code/graft` run:
```bash
INSTILL_LIBRARY_PATH="$HOME/peter_code/ai_support" /absolute/path/to/worktree/bin/instill import graft
```

Expected: exit 0. On failure, stop and restore from verified backup before another attempt.

- [ ] **Step 5: Verify the public catalog**

Run:
```bash
INSTILL_LIBRARY_PATH="$HOME/peter_code/ai_support" /absolute/path/to/worktree/bin/instill library show --type mcp
```

Expected: exactly `Stack Internal`, `markitdown`, and `serena`.

- [ ] **Step 6: Verify durability and APM state**

Inspect catalog, three markers, and `~/peter_code/graft/apm.yml`. They MUST contain all names and preserve commands, arguments, and Serena environment references. Run `instill library scan`, then show the catalog again; the same three entries MUST survive.

- [ ] **Step 7: Verify chooser loading without interactive mutation**

Run:
```bash
go test ./internal/instill -run 'Test.*Pick.*MCP|TestLoadPickTypeStates' -count=1 -v
INSTILL_LIBRARY_PATH="$HOME/peter_code/ai_support" /absolute/path/to/worktree/bin/instill library show --type mcp
```

Expected: tests PASS and the command lists the same three entries. Both use the catalog loaded by `loadPickTypeStates`.

- [ ] **Step 8: Record evidence and close Beads**

Update `instill-vg5` with red/green commands, full gates, backup path, and live catalog evidence. Close only after every acceptance criterion is verified.

---

### Task 3: Final Review and Branch Completion

**Files:**
- Verify only: committed diff, Beads state, repository status, and remote state.

**Interfaces:**
- Consumes: verified code and live migration evidence.
- Produces: reviewed, pushed branch state ready for integration.

- [ ] **Step 1: Run fresh completion verification**

```bash
go test ./...
go vet ./...
git diff --check
git status --short --branch
```

Expected: Go and diff commands exit 0; status contains no unintended changes from this work.

- [ ] **Step 2: Complete required reviews**

Review the committed diff against the design and acceptance criteria. Every actionable finding MUST be addressed and full verification rerun.

- [ ] **Step 3: Invoke branch-finishing workflow**

Use `superpowers:finishing-a-development-branch`. Execute the user's chosen integration option. Before session end, `bd dolt push` and `git push` MUST succeed, and `git status` MUST report the branch is up to date with its remote.

*Authored By Peter O'Connor with Assistance from Codex (GPT-5) · 2026-07-12 · Instill Graft MCP migration implementation plan*
