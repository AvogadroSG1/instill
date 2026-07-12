# MCP Manifest Repair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make new and existing Library-owned MCP Server dependencies serialize as self-defined APM dependencies without altering unmatched registry dependencies.

**Architecture:** Extend the manifest model so it can preserve an omitted registry value and emit an explicit `false`. Centralize conversion from `CatalogEntry` to `MCPDependency`, use it for new picks, and reconcile name-matched dependencies from the Library catalog before APM install and compile.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, table-oriented Go tests, APM CLI 0.24.1 manifest contract

## Global Constraints

- Instill MUST treat an MCP Server selected from the configured Library as a self-defined APM dependency.
- New dependencies MUST include the catalog transport and `registry: false`.
- Sync MUST repair dependencies only when their names match Library catalog entries.
- Sync MUST preserve unmatched dependencies unchanged.
- Catalog connection fields MUST remain authoritative for matching dependencies.
- The implementation MUST NOT rename MCP Servers, infer ownership from spelling, modify the Library catalog, or change skill, instruction, prompt, install, or compile semantics.
- Every production change MUST begin with a failing BDD regression test and complete a red-green-refactor cycle.

---

### Task 1: Serialize self-defined MCP dependencies from Library picks

**Files:**
- Modify: `internal/instill/apm_manifest.go`
- Modify: `internal/instill/apm_manifest_test.go`
- Modify: `internal/instill/pick_skills.go`
- Modify: `internal/instill/pick_skills_test.go`

**Interfaces:**
- Consumes: `CatalogEntry{Type, Name, Transport, Command, Args, Env, URL}`.
- Produces: `MCPDependency{Transport string, Registry *bool}` and `mcpDependencyFromCatalog(CatalogEntry) MCPDependency` for Task 2.

- [ ] **Step 1: Write failing manifest serialization tests**

Update the manifest fixture to use an explicit local dependency:

```go
registry := false
manifest.Dependencies.MCP = []MCPDependency{{
	Name: "local-db", Transport: "stdio", Registry: &registry,
	Command: "sqlite-mcp", Args: []string{"--db", "dev.db"},
}}
```

Assert the serialized file contains:

```go
requireContains(t, data, "transport: stdio")
requireContains(t, data, "registry: false")
```

Add `TestReadAPMManifestPreservesOmittedAndFalseMCPRegistry` using one dependency with no `registry` key and one with `registry: false`. Assert the first `Registry == nil` and the second is a non-nil pointer to `false`.

- [ ] **Step 2: Run the manifest tests and verify RED**

Run:

```bash
go test ./internal/instill -run 'TestWriteAPMManifestAtomicWritesYAMLDependencies|TestReadAPMManifestPreservesOmittedAndFalseMCPRegistry' -count=1 -v
```

Expected: build FAIL because `MCPDependency` has no `Transport` or `Registry` fields.

- [ ] **Step 3: Add the minimal manifest fields**

Change `MCPDependency` to:

```go
type MCPDependency struct {
	Name      string   `yaml:"name,omitempty"`
	Transport string   `yaml:"transport,omitempty"`
	Registry  *bool    `yaml:"registry,omitempty"`
	Command   string   `yaml:"command,omitempty"`
	Args      []string `yaml:"args,omitempty"`
	Env       []string `yaml:"env,omitempty"`
	URL       string   `yaml:"url,omitempty"`
}
```

- [ ] **Step 4: Run the manifest tests and verify GREEN**

Run the command from Step 2. Expected: both tests PASS.

- [ ] **Step 5: Write failing BDD tests for stdio and HTTP picks**

Update `TestPickAddsMCPBlockAndRunsAPMInstall` to expect `Transport: "stdio"` and a `Registry` pointer to false.

Add `TestPickAddsSelfDefinedHTTPMCPDependency` with this catalog input and expected dependency:

```go
CatalogEntry{Type: LibraryTypeMCP, Name: "remote", Transport: "http", URL: "https://example.test/mcp"}

registry := false
MCPDependency{Name: "remote", Transport: "http", Registry: &registry, URL: "https://example.test/mcp"}
```

- [ ] **Step 6: Run the pick tests and verify RED**

Run:

```bash
go test ./internal/instill -run 'TestPickAddsMCPBlockAndRunsAPMInstall|TestPickAddsSelfDefinedHTTPMCPDependency' -count=1 -v
```

Expected: FAIL because picks omit transport and registry ownership.

- [ ] **Step 7: Centralize catalog conversion and use it for picks**

Add:

```go
func mcpDependencyFromCatalog(entry CatalogEntry) MCPDependency {
	registry := false
	return MCPDependency{
		Name: entry.Name, Transport: entry.Transport, Registry: &registry,
		Command: entry.Command, Args: entry.Args, Env: entry.Env, URL: entry.URL,
	}
}
```

In `applyMCPPick`, replace its MCP struct literal with:

```go
byName[name] = mcpDependencyFromCatalog(entry)
```

- [ ] **Step 8: Format and verify Task 1 GREEN**

Run:

```bash
gofmt -w internal/instill/apm_manifest.go internal/instill/apm_manifest_test.go internal/instill/pick_skills.go internal/instill/pick_skills_test.go
go test ./internal/instill -run 'TestWriteAPMManifestAtomicWritesYAMLDependencies|TestReadAPMManifestPreservesOmittedAndFalseMCPRegistry|TestPickAddsMCPBlockAndRunsAPMInstall|TestPickAddsSelfDefinedHTTPMCPDependency' -count=1 -v
```

Expected: all four tests PASS.

- [ ] **Step 9: Request and address Task 1 review**

The reviewer MUST verify the YAML contract, pointer semantics, catalog conversion, and absence of unrelated refactoring. Address all actionable feedback and rerun Step 8 before Task 2.

- [ ] **Step 10: Commit Task 1**

Stage only the four Task 1 files. Commit with message `fix: serialize self-defined MCP dependencies` and both required co-author trailers.

### Task 2: Repair existing catalog-backed dependencies during sync

**Files:**
- Modify: `internal/instill/sync.go`
- Modify: `internal/instill/sync_test.go`

**Interfaces:**
- Consumes: `mcpDependencyFromCatalog(CatalogEntry) MCPDependency`, `LoadCatalog(string, LibraryType) ([]CatalogEntry, error)`, and the manifest loaded by `SyncProject`.
- Produces: `reconcileMCPDependencies([]MCPDependency, []CatalogEntry) ([]MCPDependency, bool)`.

- [ ] **Step 1: Write the failing sync reconciliation BDD test**

Add `TestSyncProjectRepairsCatalogMCPAndPreservesRegistryDependency`. Its catalog MUST contain:

```go
CatalogEntry{Type: LibraryTypeMCP, Name: "local", Transport: "http", URL: "https://example.test/mcp"}
```

Its existing manifest MUST contain:

```go
[]MCPDependency{{Name: "local"}, {Name: "io.example/public-server"}}
```

After sync, assert the first dependency equals:

```go
registry := false
MCPDependency{Name: "local", Transport: "http", Registry: &registry, URL: "https://example.test/mcp"}
```

Assert the unmatched dependency remains `MCPDependency{Name: "io.example/public-server"}` and runner calls remain version, install, compile in that order.

- [ ] **Step 2: Run the sync test and verify RED**

Run:

```bash
go test ./internal/instill -run TestSyncProjectRepairsCatalogMCPAndPreservesRegistryDependency -count=1 -v
```

Expected: FAIL because the matching dependency remains incomplete.

- [ ] **Step 3: Implement deterministic reconciliation**

Add `reflect` and:

```go
func reconcileMCPDependencies(current []MCPDependency, catalog []CatalogEntry) ([]MCPDependency, bool) {
	byName := make(map[string]CatalogEntry, len(catalog))
	for _, entry := range catalog {
		byName[entry.Name] = entry
	}
	next := append([]MCPDependency{}, current...)
	changed := false
	for i, dependency := range next {
		entry, ok := byName[dependency.Name]
		if !ok {
			continue
		}
		reconciled := mcpDependencyFromCatalog(entry)
		if reflect.DeepEqual(dependency, reconciled) {
			continue
		}
		next[i] = reconciled
		changed = true
	}
	return next, changed
}
```

After `ensureTargets` and before `RunAPMInstall`, load `LibraryTypeMCP`, reconcile, and call `WriteAPMManifestAtomic` only when changed.

- [ ] **Step 4: Format and verify focused sync GREEN**

Run:

```bash
gofmt -w internal/instill/sync.go internal/instill/sync_test.go
go test ./internal/instill -run TestSyncProjectRepairsCatalogMCPAndPreservesRegistryDependency -count=1 -v
```

Expected: PASS.

- [ ] **Step 5: Add a malformed-catalog boundary test**

Add `TestSyncProjectRejectsMalformedMCPCatalogBeforeAPMInstall`. Overwrite `mcp/catalog.csv` with `wrong,header\n`, call sync, assert `error: malformed catalog: invalid header`, and assert the runner recorded only `apm --version`.

- [ ] **Step 6: Run focused sync coverage**

Run:

```bash
go test ./internal/instill -run 'TestSyncProjectRepairsCatalogMCPAndPreservesRegistryDependency|TestSyncProjectRejectsMalformedMCPCatalogBeforeAPMInstall' -count=1 -v
```

Expected: both tests PASS.

- [ ] **Step 7: Run package and full quality gates**

Run:

```bash
go test ./internal/instill -count=1
go test ./... -count=1
go vet ./...
git diff --check
```

Expected: every command exits 0 with no failures or findings.

- [ ] **Step 8: Verify the real APM boundary from saved fixtures**

Create a Beads task named `Save work to scratch_work/instill_mcp_manifest_repair_code/` before creating fixture data. Save a minimal Library and project under `$HOME/peter_code/scratch_work/instill_mcp_manifest_repair_code/`; do not use `/tmp` and do not delete the files.

The fixture MUST contain one stdio and one HTTP Library MCP Server. Build Instill, run sync against the fixture, and confirm the resulting `apm.yml` contains transport and `registry: false` for both. APM MUST proceed past registry lookup.

- [ ] **Step 9: Request and address final code review**

The reviewer MUST compare the diff against every BDD criterion and confirm unmatched dependencies round-trip unchanged. Address all actionable feedback and rerun Steps 7 and 8.

- [ ] **Step 10: Commit Task 2**

Stage only `internal/instill/sync.go` and `internal/instill/sync_test.go`. Commit with message `fix: repair MCP dependencies during sync` and both required co-author trailers.

- [ ] **Step 11: Close and publish**

Close implementation Beads issues, run `bd dolt push`, push the feature branch, and verify `git status --short --branch` reports synchronization with its upstream. Unrelated untracked files MUST NOT be committed.

