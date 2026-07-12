package instill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestImportOldInstillWritesCatalogAndManifestAndRemovesLegacyArtifacts(t *testing.T) {
	library := createTypedLibrary(t)
	root := t.TempDir()
	legacy := Project{
		Root:             root,
		ManifestPath:     ProjectManifestPath(root),
		SymlinkDir:       filepath.Join(root, ".claude", "skills"),
		AgentsSymlinkDir: filepath.Join(root, ".agents", "skills"),
	}
	if err := os.MkdirAll(legacy.SymlinkDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude/skills) error = %v", err)
	}
	if err := WriteManifestAtomic(legacy.ManifestPath, Manifest{Skills: []string{"cloud/azure/azure-cli"}}); err != nil {
		t.Fatalf("WriteManifestAtomic() error = %v", err)
	}
	if err := os.Symlink(
		filepath.Join(library, "skills", "cloud", "azure", "azure-cli"),
		filepath.Join(legacy.SymlinkDir, "cloud:azure:azure-cli"),
	); err != nil {
		t.Fatalf("Symlink(skill) error = %v", err)
	}
	writeSettingsLocalForTest(t, legacy, `{"permissions":{"allow":["Skill(cloud:azure:azure-cli)"]}}`)

	err := ImportOldInstill(ImportOptions{
		Project: Project{
			Root:             root,
			ManifestPath:     ProjectAPMPath(root),
			SymlinkDir:       legacy.SymlinkDir,
			AgentsSymlinkDir: legacy.AgentsSymlinkDir,
		},
		LibraryPath: library,
	})

	requireNoError(t, err)
	entries, err := LoadCatalog(library, LibraryTypeSkill)
	requireNoError(t, err)
	if len(entries) != 1 || entries[0].Name != "cloud/azure/azure-cli" {
		t.Fatalf("skill catalog = %#v, want imported azure skill", entries)
	}

	manifest, err := ReadAPMManifest(ProjectAPMPath(root))
	requireNoError(t, err)
	wantDependency := filepath.Join(library, "skills", "cloud", "azure", "azure-cli")
	requireEqual(t, []string{wantDependency}, manifest.Dependencies.APM)

	if _, err := os.Lstat(legacy.ManifestPath); !os.IsNotExist(err) {
		t.Fatalf("legacy manifest remains; err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(legacy.SymlinkDir, "cloud:azure:azure-cli")); !os.IsNotExist(err) {
		t.Fatalf("legacy symlink remains; err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Fatalf("settings.local.json remains; err = %v", err)
	}
}

func TestImportOldInstillKeepsLegacyManifestWhenSymlinkRemovalFails(t *testing.T) {
	library := createTypedLibrary(t)
	root := t.TempDir()
	legacy := Project{
		Root:             root,
		ManifestPath:     ProjectManifestPath(root),
		SymlinkDir:       filepath.Join(root, ".claude", "skills"),
		AgentsSymlinkDir: filepath.Join(root, ".agents", "skills"),
	}
	requireNoError(t, os.MkdirAll(legacy.SymlinkDir, 0o755))
	requireNoError(t, WriteManifestAtomic(legacy.ManifestPath, Manifest{Skills: []string{"cloud/azure/azure-cli"}}))
	requireNoError(t, os.Symlink(
		filepath.Join(library, "skills", "cloud", "azure", "azure-cli"),
		filepath.Join(legacy.SymlinkDir, "cloud:azure:azure-cli"),
	))
	requireNoError(t, os.Chmod(legacy.SymlinkDir, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(legacy.SymlinkDir, 0o755)
	})

	err := ImportOldInstill(ImportOptions{
		Project: Project{
			Root:             root,
			ManifestPath:     filepath.Join(t.TempDir(), "apm.yml"),
			SymlinkDir:       legacy.SymlinkDir,
			AgentsSymlinkDir: legacy.AgentsSymlinkDir,
		},
		LibraryPath: library,
	})

	if err == nil {
		t.Fatal("ImportOldInstill() error = nil, want symlink removal error")
	}
	if _, statErr := os.Lstat(legacy.ManifestPath); statErr != nil {
		t.Fatalf("legacy manifest should remain; err = %v", statErr)
	}
}

func TestImportGraftWritesMCPCatalogAndManifestAndPreservesUnmanagedServers(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "graft.lock"), []byte("servers:\n  - local-db\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(graft.lock) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
  "mcpServers": {
    "local-db": {"command": "sqlite-mcp", "args": ["--db", "dev.db"]},
    "ignored": {"type": "http", "url": "https://example.test/mcp", "headers": {"Authorization": "Bearer token"}}
  }
}`), 0o600); err != nil {
		t.Fatalf("WriteFile(.mcp.json) error = %v", err)
	}

	err := ImportGraft(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: ProjectAPMPath(root),
		},
		LibraryPath: library,
	})

	requireNoError(t, err)
	entries, err := LoadCatalog(library, LibraryTypeMCP)
	requireNoError(t, err)
	if len(entries) != 1 || entries[0].Name != "local-db" || entries[0].Command != "sqlite-mcp" {
		t.Fatalf("mcp catalog = %#v, want local-db entry only", entries)
	}

	manifest, err := ReadAPMManifest(ProjectAPMPath(root))
	requireNoError(t, err)
	if len(manifest.Dependencies.MCP) != 1 || manifest.Dependencies.MCP[0].Name != "local-db" {
		t.Fatalf("manifest mcp = %#v, want local-db dependency", manifest.Dependencies.MCP)
	}

	if _, err := os.Lstat(filepath.Join(root, "graft.lock")); !os.IsNotExist(err) {
		t.Fatalf("graft.lock remains; err = %v", err)
	}
	mcpPath := filepath.Join(root, ".mcp.json")
	info, err := os.Stat(mcpPath)
	requireNoError(t, err)
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf(".mcp.json mode = %v, want 0600", got)
	}
	remaining, err := readMCPJSON(mcpPath)
	requireNoError(t, err)
	if len(remaining) != 1 {
		t.Fatalf(".mcp.json servers = %#v, want only ignored", remaining)
	}
	if _, ok := remaining["ignored"]; !ok {
		t.Fatalf(".mcp.json servers = %#v, want ignored preserved", remaining)
	}
	if _, ok := remaining["local-db"]; ok {
		t.Fatalf(".mcp.json servers = %#v, want local-db removed", remaining)
	}

	rawServers := readRawMCPServersForTest(t, mcpPath)
	var ignored map[string]any
	requireNoError(t, json.Unmarshal(rawServers["ignored"], &ignored))
	if ignored["type"] != "http" {
		t.Fatalf("ignored server type = %#v, want http", ignored["type"])
	}
	headers, ok := ignored["headers"].(map[string]any)
	if !ok || headers["Authorization"] != "Bearer token" {
		t.Fatalf("ignored server headers = %#v, want Authorization header preserved", ignored["headers"])
	}
}

func TestImportGraftImportsCurrentLockAndManagedUnlockedServers(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
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

	err := ImportGraft(ImportOptions{
		Project:     Project{Root: root, ManifestPath: ProjectAPMPath(root)},
		LibraryPath: library,
	})
	requireNoError(t, err)

	entries, err := LoadCatalog(library, LibraryTypeMCP)
	requireNoError(t, err)
	if len(entries) != 3 {
		t.Fatalf("mcp catalog = %#v, want exactly three Graft-managed entries", entries)
	}
	byName := catalogEntriesByNameForTest(entries)
	if byName["Stack Internal"].Command != "npx" || !slices.Equal(byName["Stack Internal"].Args, []string{"mcp-remote", "https://stackinternal.stackenterprise.co/mcp"}) {
		t.Fatalf("Stack Internal = %#v, want preserved command and args", byName["Stack Internal"])
	}
	if byName["markitdown"].Command != "uvx" || !slices.Equal(byName["markitdown"].Args, []string{"markitdown-mcp"}) {
		t.Fatalf("markitdown = %#v, want managed unlocked entry", byName["markitdown"])
	}
	if got := byName["serena"].Env; !slices.Equal(got, []string{"SSL_CERT_FILE=${SSL_CERT_FILE}"}) {
		t.Fatalf("serena env = %#v, want preserved environment reference", got)
	}
	if _, ok := byName["manual"]; ok {
		t.Fatalf("mcp catalog = %#v, want unmanaged unlocked manual excluded", entries)
	}

	manifest, err := ReadAPMManifest(ProjectAPMPath(root))
	requireNoError(t, err)
	if manifest.Name != filepath.Base(root) || manifest.Version != "0.1.0" {
		t.Fatalf("manifest identity = %q %q, want %q 0.1.0", manifest.Name, manifest.Version, filepath.Base(root))
	}
	if len(manifest.Dependencies.MCP) != 3 {
		t.Fatalf("manifest mcp = %#v, want exactly three dependencies", manifest.Dependencies.MCP)
	}
}

func TestImportGraftRejectsEmptySelectionWithoutMutation(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
	lockPath := filepath.Join(root, "graft.lock")
	mcpPath := filepath.Join(root, ".mcp.json")
	lockData := []byte("{}\n")
	mcpData := []byte(`{"mcpServers":{"manual":{"command":"manual-mcp"}}}`)
	requireNoError(t, os.WriteFile(lockPath, lockData, 0o644))
	requireNoError(t, os.WriteFile(mcpPath, mcpData, 0o644))

	err := ImportGraft(ImportOptions{
		Project:     Project{Root: root, ManifestPath: ProjectAPMPath(root)},
		LibraryPath: library,
	})
	if err == nil || !strings.Contains(ErrorMessage(err), "no Graft-managed MCP servers found") {
		t.Fatalf("ImportGraft() error = %v, want empty-selection error", err)
	}
	gotLock, readErr := os.ReadFile(lockPath)
	requireNoError(t, readErr)
	gotMCP, readErr := os.ReadFile(mcpPath)
	requireNoError(t, readErr)
	if !slices.Equal(gotLock, lockData) || !slices.Equal(gotMCP, mcpData) {
		t.Fatalf("migration inputs mutated: lock=%q mcp=%q", gotLock, gotMCP)
	}
	if _, statErr := os.Stat(filepath.Join(library, "mcp", "catalog.csv")); !os.IsNotExist(statErr) {
		t.Fatalf("catalog stat error = %v, want no catalog", statErr)
	}
	if _, statErr := os.Stat(ProjectAPMPath(root)); !os.IsNotExist(statErr) {
		t.Fatalf("manifest stat error = %v, want no manifest", statErr)
	}
}

func TestImportGraftKeepsLockWhenMCPJSONCleanupFails(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
	requireNoError(t, os.WriteFile(filepath.Join(root, "graft.lock"), []byte("servers:\n  - local-db\n"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
  "metadata": {"owner": "user"},
  "mcpServers": {
    "local-db": {"command": "sqlite-mcp", "args": ["--db", "dev.db"]}
  }
}`), 0o644))
	requireNoError(t, os.Chmod(root, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(root, 0o755)
	})

	err := ImportGraft(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: filepath.Join(t.TempDir(), "apm.yml"),
		},
		LibraryPath: library,
	})

	if err == nil {
		t.Fatal("ImportGraft() error = nil, want .mcp.json cleanup error")
	}
	if _, statErr := os.Lstat(filepath.Join(root, "graft.lock")); statErr != nil {
		t.Fatalf("graft.lock should remain; err = %v", statErr)
	}
}

func TestImportGraftDeletesMCPJSONWhenAllServersAreManaged(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "graft.lock"), []byte("servers:\n  - local-db\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(graft.lock) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
  "mcpServers": {
    "local-db": {"command": "sqlite-mcp", "args": ["--db", "dev.db"]}
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(.mcp.json) error = %v", err)
	}

	err := ImportGraft(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: ProjectAPMPath(root),
		},
		LibraryPath: library,
	})

	requireNoError(t, err)
	if _, err := os.Lstat(filepath.Join(root, ".mcp.json")); !os.IsNotExist(err) {
		t.Fatalf(".mcp.json remains; err = %v", err)
	}
}

func TestImportGraftPreservesMCPJSONTopLevelFieldsWhenAllServersAreManaged(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
	requireNoError(t, os.WriteFile(filepath.Join(root, "graft.lock"), []byte("servers:\n  - local-db\n"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
  "metadata": {"owner": "user"},
  "mcpServers": {
    "local-db": {"command": "sqlite-mcp", "args": ["--db", "dev.db"]}
  }
}`), 0o600))

	err := ImportGraft(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: ProjectAPMPath(root),
		},
		LibraryPath: library,
	})

	requireNoError(t, err)
	mcpPath := filepath.Join(root, ".mcp.json")
	info, err := os.Stat(mcpPath)
	requireNoError(t, err)
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf(".mcp.json mode = %v, want 0600", got)
	}
	var remaining map[string]any
	data, err := os.ReadFile(mcpPath)
	requireNoError(t, err)
	requireNoError(t, json.Unmarshal(data, &remaining))
	metadata, ok := remaining["metadata"].(map[string]any)
	if !ok || metadata["owner"] != "user" {
		t.Fatalf(".mcp.json metadata = %#v, want owner preserved", remaining["metadata"])
	}
	if servers, ok := remaining["mcpServers"].(map[string]any); ok && len(servers) != 0 {
		t.Fatalf(".mcp.json mcpServers = %#v, want imported server removed", servers)
	}
}

func TestImportGraftPreservesSSETransport(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
	requireNoError(t, os.WriteFile(filepath.Join(root, "graft.lock"), []byte("servers:\n  - events\n"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
  "mcpServers": {
    "events": {"type": "sse", "url": "https://example.test/events"}
  }
}`), 0o644))

	err := ImportGraft(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: ProjectAPMPath(root),
		},
		LibraryPath: library,
	})

	requireNoError(t, err)
	entries, err := LoadCatalog(library, LibraryTypeMCP)
	requireNoError(t, err)
	if len(entries) != 1 || entries[0].Name != "events" || entries[0].Transport != "sse" {
		t.Fatalf("mcp catalog = %#v, want events with sse transport", entries)
	}
}

func TestImportGraftWritesDurableMCPMarkersThatSurviveScan(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
	requireNoError(t, WriteCatalog(library, LibraryTypeMCP, []CatalogEntry{{
		Type:        LibraryTypeMCP,
		Name:        "local-db",
		Transport:   "stdio",
		Command:     "stale-mcp",
		Args:        []string{"--old"},
		Env:         []string{"OLD=value"},
		Description: "keep this note",
	}}))
	requireNoError(t, os.WriteFile(filepath.Join(root, "graft.lock"), []byte("servers:\n  - local-db\n  - events\n"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
  "mcpServers": {
    "local-db": {"command": "sqlite-mcp", "args": ["--db", "dev.db"], "env": {"DB_PATH": "dev.db"}},
    "events": {"type": "sse", "url": "https://example.test/events"}
  }
}`), 0o644))

	err := ImportGraft(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: ProjectAPMPath(root),
		},
		LibraryPath: library,
	})
	requireNoError(t, err)

	requireNoError(t, ScanLibrary(library, nil))
	entries, err := LoadCatalog(library, LibraryTypeMCP)
	requireNoError(t, err)
	byName := catalogEntriesByNameForTest(entries)

	local := byName["local-db"]
	if local.Name == "" || local.Transport != "stdio" || local.Command != "sqlite-mcp" {
		t.Fatalf("local-db catalog entry = %#v, want stdio sqlite server after scan", local)
	}
	if local.Description != "keep this note" {
		t.Fatalf("local-db description = %q, want preserved note", local.Description)
	}
	if got := local.Args; !slices.Equal(got, []string{"--db", "dev.db"}) {
		t.Fatalf("local-db args = %#v, want sqlite args after scan", got)
	}
	if got := local.Env; !slices.Equal(got, []string{"DB_PATH=dev.db"}) {
		t.Fatalf("local-db env = %#v, want unredacted graft env after scan", got)
	}

	events := byName["events"]
	if events.Name == "" || events.Transport != "sse" || events.URL != "https://example.test/events" {
		t.Fatalf("events catalog entry = %#v, want sse server after scan", events)
	}
}

func TestImportOldInstillMergesExistingAPMDependencies(t *testing.T) {
	library := createTypedLibrary(t)
	root := t.TempDir()
	legacyManifestPath := ProjectManifestPath(root)
	requireNoError(t, os.MkdirAll(filepath.Dir(legacyManifestPath), 0o755))
	requireNoError(t, WriteManifestAtomic(legacyManifestPath, Manifest{Skills: []string{"cloud/azure/azure-cli"}}))

	existingDependency := filepath.ToSlash(filepath.Join(root, "external", "SKILL.md"))
	apmPath := ProjectAPMPath(root)
	requireNoError(t, WriteAPMManifestAtomic(apmPath, APMManifest{
		Dependencies: APMDependencies{
			APM: []string{existingDependency},
			MCP: []MCPDependency{{Name: "existing-mcp", Command: "existing-command"}},
		},
	}))

	err := ImportOldInstill(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: apmPath,
		},
		LibraryPath: library,
	})

	requireNoError(t, err)
	manifest, err := ReadAPMManifest(apmPath)
	requireNoError(t, err)
	importedDependency := filepath.Join(library, "skills", "cloud", "azure", "azure-cli")
	requireEqual(t, []string{existingDependency, importedDependency}, manifest.Dependencies.APM)
	if len(manifest.Dependencies.MCP) != 1 || manifest.Dependencies.MCP[0].Name != "existing-mcp" {
		t.Fatalf("manifest mcp = %#v, want existing mcp dependency preserved", manifest.Dependencies.MCP)
	}
}

func TestImportOldInstillPreservesUnknownAPMManifestFields(t *testing.T) {
	library := createTypedLibrary(t)
	root := t.TempDir()
	legacyManifestPath := ProjectManifestPath(root)
	requireNoError(t, os.MkdirAll(filepath.Dir(legacyManifestPath), 0o755))
	requireNoError(t, WriteManifestAtomic(legacyManifestPath, Manifest{Skills: []string{"cloud/azure/azure-cli"}}))
	apmPath := ProjectAPMPath(root)
	requireNoError(t, os.WriteFile(apmPath, []byte(`lockfileVersion: 2
profiles:
  dev:
    enabled: true
custom:
  owner: platform
dependencies:
  apm:
    - ../existing/SKILL.md
  mcp:
    - name: existing-mcp
      command: existing-command
`), 0o644))

	err := ImportOldInstill(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: apmPath,
		},
		LibraryPath: library,
	})

	requireNoError(t, err)
	data, err := os.ReadFile(apmPath)
	requireNoError(t, err)
	text := string(data)
	for _, want := range []string{"lockfileVersion: 2", "profiles:", "dev:", "enabled: true", "custom:", "owner: platform"} {
		if !strings.Contains(text, want) {
			t.Fatalf("apm.yml = %q, want preserved field %q", text, want)
		}
	}
	manifest, err := ReadAPMManifest(apmPath)
	requireNoError(t, err)
	importedDependency := filepath.Join(library, "skills", "cloud", "azure", "azure-cli")
	requireEqual(t, []string{"../existing/SKILL.md", importedDependency}, manifest.Dependencies.APM)
}

func TestImportOldInstillReturnsErrorForUnreadableAPMManifest(t *testing.T) {
	library := createTypedLibrary(t)
	root := t.TempDir()
	legacyManifestPath := ProjectManifestPath(root)
	requireNoError(t, os.MkdirAll(filepath.Dir(legacyManifestPath), 0o755))
	requireNoError(t, WriteManifestAtomic(legacyManifestPath, Manifest{Skills: []string{"cloud/azure/azure-cli"}}))
	apmPath := ProjectAPMPath(root)
	requireNoError(t, os.Mkdir(apmPath, 0o755))

	err := ImportOldInstill(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: apmPath,
		},
		LibraryPath: library,
	})

	if err == nil {
		t.Fatal("ImportOldInstill() error = nil, want unreadable apm.yml error")
	}
	if !strings.Contains(ErrorMessage(err), "cannot read manifest") {
		t.Fatalf("ImportOldInstill() error = %q, want cannot read manifest", ErrorMessage(err))
	}
	info, statErr := os.Stat(apmPath)
	requireNoError(t, statErr)
	if !info.IsDir() {
		t.Fatalf("apm.yml was overwritten; mode = %v, want directory preserved", info.Mode())
	}
}

func TestImportOldInstillReturnsErrorForUnreadableSettingsLocal(t *testing.T) {
	library := createTypedLibrary(t)
	root := t.TempDir()
	legacyManifestPath := ProjectManifestPath(root)
	requireNoError(t, os.MkdirAll(filepath.Dir(legacyManifestPath), 0o755))
	requireNoError(t, WriteManifestAtomic(legacyManifestPath, Manifest{Skills: []string{"cloud/azure/azure-cli"}}))
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".claude", "settings.local.json"), 0o755))

	err := ImportOldInstill(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: ProjectAPMPath(root),
		},
		LibraryPath: library,
	})

	if err == nil {
		t.Fatal("ImportOldInstill() error = nil, want unreadable settings.local.json error")
	}
	if !strings.Contains(ErrorMessage(err), "cannot read settings.local.json") {
		t.Fatalf("ImportOldInstill() error = %q, want settings.local.json read error", ErrorMessage(err))
	}
	if _, statErr := os.Stat(legacyManifestPath); statErr != nil {
		t.Fatalf("legacy manifest stat error = %v, want manifest preserved", statErr)
	}
}

func TestImportGraftMergesExistingMCPDependencies(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
	requireNoError(t, os.WriteFile(filepath.Join(root, "graft.lock"), []byte("servers:\n  - local-db\n"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
  "mcpServers": {
    "local-db": {"command": "sqlite-mcp", "args": ["--db", "dev.db"]}
  }
}`), 0o644))
	apmPath := ProjectAPMPath(root)
	requireNoError(t, WriteAPMManifestAtomic(apmPath, APMManifest{
		Dependencies: APMDependencies{
			APM: []string{"../existing/SKILL.md"},
			MCP: []MCPDependency{{Name: "existing-mcp", Command: "existing-command"}},
		},
	}))

	err := ImportGraft(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: apmPath,
		},
		LibraryPath: library,
	})

	requireNoError(t, err)
	manifest, err := ReadAPMManifest(apmPath)
	requireNoError(t, err)
	requireEqual(t, []string{"../existing/SKILL.md"}, manifest.Dependencies.APM)
	if len(manifest.Dependencies.MCP) != 2 {
		t.Fatalf("manifest mcp = %#v, want existing and imported dependencies", manifest.Dependencies.MCP)
	}
	if manifest.Dependencies.MCP[0].Name != "existing-mcp" || manifest.Dependencies.MCP[1].Name != "local-db" {
		t.Fatalf("manifest mcp = %#v, want dependencies keyed by stable names", manifest.Dependencies.MCP)
	}
}

func TestImportGraftPreservesUnknownAPMManifestFields(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
	requireNoError(t, os.WriteFile(filepath.Join(root, "graft.lock"), []byte("servers:\n  - local-db\n"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
  "mcpServers": {
    "local-db": {"command": "sqlite-mcp", "args": ["--db", "dev.db"]}
  }
}`), 0o644))
	apmPath := ProjectAPMPath(root)
	requireNoError(t, os.WriteFile(apmPath, []byte(`lockfileVersion: 2
profiles:
  dev:
    enabled: true
custom:
  owner: platform
dependencies:
  apm:
    - ../existing/SKILL.md
  mcp:
    - name: existing-mcp
      command: existing-command
`), 0o644))

	err := ImportGraft(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: apmPath,
		},
		LibraryPath: library,
	})

	requireNoError(t, err)
	data, err := os.ReadFile(apmPath)
	requireNoError(t, err)
	text := string(data)
	for _, want := range []string{"lockfileVersion: 2", "profiles:", "dev:", "enabled: true", "custom:", "owner: platform"} {
		if !strings.Contains(text, want) {
			t.Fatalf("apm.yml = %q, want preserved field %q", text, want)
		}
	}
	manifest, err := ReadAPMManifest(apmPath)
	requireNoError(t, err)
	requireEqual(t, []string{"../existing/SKILL.md"}, manifest.Dependencies.APM)
	if len(manifest.Dependencies.MCP) != 2 {
		t.Fatalf("manifest mcp = %#v, want existing and imported dependencies", manifest.Dependencies.MCP)
	}
	if manifest.Dependencies.MCP[0].Name != "existing-mcp" || manifest.Dependencies.MCP[1].Name != "local-db" {
		t.Fatalf("manifest mcp = %#v, want dependencies keyed by stable names", manifest.Dependencies.MCP)
	}
}

func TestImportGraftFailsWhenLockedServerIsMissing(t *testing.T) {
	library := t.TempDir()
	root := t.TempDir()
	lockPath := filepath.Join(root, "graft.lock")
	requireNoError(t, os.WriteFile(lockPath, []byte("servers:\n  - missing-server\n"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers":{}}`), 0o644))

	err := ImportGraft(ImportOptions{
		Project: Project{
			Root:         root,
			ManifestPath: ProjectAPMPath(root),
		},
		LibraryPath: library,
	})

	if err == nil {
		t.Fatal("ImportGraft() error = nil, want missing server error")
	}
	if !strings.Contains(ErrorMessage(err), "missing-server") {
		t.Fatalf("ImportGraft() error = %q, want missing server name", ErrorMessage(err))
	}
	if _, statErr := os.Lstat(lockPath); statErr != nil {
		t.Fatalf("graft.lock stat error = %v, want lock preserved", statErr)
	}
}

func TestImportClaudeWritesRedactedMCPCatalog(t *testing.T) {
	library := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	if err := os.WriteFile(filepath.Join(configDir, "claude.json"), []byte(`{
  "mcpServers": {
    "docs-search": {
      "command": "docs-mcp",
      "args": ["serve"],
      "env": {"API_KEY": "secret", "REGION": "us-east-1"}
    }
  },
  "projects": {
    "/tmp/example": {
      "mcpServers": {
        "local-db": {"command": "sqlite-mcp", "args": ["--db", "dev.db"]}
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(claude.json) error = %v", err)
	}

	err := ImportClaude(ImportOptions{LibraryPath: library})

	requireNoError(t, err)
	entries, err := LoadCatalog(library, LibraryTypeMCP)
	requireNoError(t, err)
	if len(entries) != 2 {
		t.Fatalf("mcp catalog = %#v, want two imported entries", entries)
	}

	byName := map[string]CatalogEntry{}
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	if got := byName["docs-search"].Env; !slices.Equal(got, []string{"API_KEY=${API_KEY}", "REGION=${REGION}"}) {
		t.Fatalf("docs-search env = %#v, want redacted placeholders", got)
	}
	if got := byName["local-db"].Args; !slices.Equal(got, []string{"--db", "dev.db"}) {
		t.Fatalf("local-db args = %#v, want sqlite args", got)
	}
}

func TestImportClaudePreservesSSETransport(t *testing.T) {
	library := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	requireNoError(t, os.WriteFile(filepath.Join(configDir, "claude.json"), []byte(`{
  "mcpServers": {
    "events": {"transport": "sse", "url": "https://example.test/events"}
  }
}`), 0o644))

	err := ImportClaude(ImportOptions{LibraryPath: library})

	requireNoError(t, err)
	entries, err := LoadCatalog(library, LibraryTypeMCP)
	requireNoError(t, err)
	if len(entries) != 1 || entries[0].Name != "events" || entries[0].Transport != "sse" {
		t.Fatalf("mcp catalog = %#v, want events with sse transport", entries)
	}
}

func TestImportClaudeWritesDurableRedactedMCPMarkersThatSurviveScan(t *testing.T) {
	library := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	requireNoError(t, WriteCatalog(library, LibraryTypeMCP, []CatalogEntry{{
		Type:        LibraryTypeMCP,
		Name:        "docs-search",
		Transport:   "stdio",
		Command:     "old-docs-mcp",
		Description: "Docs search server",
	}}))
	requireNoError(t, os.WriteFile(filepath.Join(configDir, "claude.json"), []byte(`{
  "mcpServers": {
    "docs-search": {
      "command": "docs-mcp",
      "args": ["serve"],
      "env": {"API_KEY": "secret", "REGION": "us-east-1"}
    },
    "events": {"transport": "sse", "url": "https://example.test/events"}
  }
}`), 0o644))

	err := ImportClaude(ImportOptions{LibraryPath: library})
	requireNoError(t, err)

	marker, err := loadMCPConfig(filepath.Join(library, "mcp", "docs-search", "config.json"))
	requireNoError(t, err)
	if got := marker.Env; !slices.Equal(got, []string{"API_KEY=${API_KEY}", "REGION=${REGION}"}) {
		t.Fatalf("docs-search marker env = %#v, want redacted placeholders", got)
	}
	if marker.Description != "Docs search server" {
		t.Fatalf("docs-search marker description = %q, want existing description", marker.Description)
	}

	requireNoError(t, ScanLibrary(library, nil))
	entries, err := LoadCatalog(library, LibraryTypeMCP)
	requireNoError(t, err)
	byName := catalogEntriesByNameForTest(entries)

	docs := byName["docs-search"]
	if docs.Name == "" || docs.Transport != "stdio" || docs.Command != "docs-mcp" {
		t.Fatalf("docs-search catalog entry = %#v, want imported stdio server after scan", docs)
	}
	if got := docs.Env; !slices.Equal(got, []string{"API_KEY=${API_KEY}", "REGION=${REGION}"}) {
		t.Fatalf("docs-search env = %#v, want redacted placeholders after scan", got)
	}
	if docs.Description != "Docs search server" {
		t.Fatalf("docs-search description = %q, want existing description after scan", docs.Description)
	}

	events := byName["events"]
	if events.Name == "" || events.Transport != "sse" || events.URL != "https://example.test/events" {
		t.Fatalf("events catalog entry = %#v, want sse server after scan", events)
	}
}

func TestImportDirectoryScansMarkersAndWritesCatalogs(t *testing.T) {
	source := t.TempDir()
	library := t.TempDir()
	writeTypedLibraryMarker(t, filepath.Join(source, "vendor", "azure", "SKILL.md"), "# azure\n")
	writeTypedLibraryMarker(t, filepath.Join(source, "tools", "local-db", "config.json"), `{"transport":"stdio","command":"sqlite-mcp","args":["--db","dev.db"]}`)
	writeTypedLibraryMarker(t, filepath.Join(source, "guidance", "python-rules", "INSTRUCTION.md"), "Use typing\n")
	writeTypedLibraryMarker(t, filepath.Join(source, "templates", "debug", "PROMPT.md"), "/debug\n")
	writeTypedLibraryMarker(t, filepath.Join(source, "notes", "scratch.md"), "ignore me\n")

	err := ImportDirectory(ImportDirectoryOptions{
		LibraryPath: library,
		Path:        source,
	})

	requireNoError(t, err)
	wantNames := map[LibraryType]string{
		LibraryTypeSkill:       "vendor/azure",
		LibraryTypeMCP:         "tools/local-db",
		LibraryTypeInstruction: "guidance/python-rules",
		LibraryTypePrompt:      "templates/debug",
	}
	for _, typ := range []LibraryType{LibraryTypeSkill, LibraryTypeMCP, LibraryTypeInstruction, LibraryTypePrompt} {
		entries, loadErr := LoadCatalog(library, typ)
		requireNoError(t, loadErr)
		if len(entries) != 1 || entries[0].Name != wantNames[typ] {
			t.Fatalf("%s catalog = %#v, want one %q entry", typ, entries, wantNames[typ])
		}
	}
	if _, err := os.Stat(filepath.Join(library, "notes", "scratch.md")); !os.IsNotExist(err) {
		t.Fatalf("unrecognized content copied; err = %v", err)
	}
}

func readRawMCPServersForTest(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()

	data, err := os.ReadFile(path)
	requireNoError(t, err)
	var config struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	requireNoError(t, json.Unmarshal(data, &config))
	return config.MCPServers
}

func writeTypedLibraryMarker(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func catalogEntriesByNameForTest(entries []CatalogEntry) map[string]CatalogEntry {
	byName := make(map[string]CatalogEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}
	return byName
}

func TestImportClaudePrefersExplicitConfigDir(t *testing.T) {
	library := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	data, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"explicit": map[string]any{"command": "explicit-mcp"},
		},
	})
	requireNoError(t, err)
	requireNoError(t, os.WriteFile(filepath.Join(configDir, "claude.json"), data, 0o644))

	err = ImportClaude(ImportOptions{LibraryPath: library})
	requireNoError(t, err)

	entries, err := LoadCatalog(library, LibraryTypeMCP)
	requireNoError(t, err)
	if len(entries) != 1 || entries[0].Name != "explicit" {
		t.Fatalf("entries = %#v, want explicit config dir server", entries)
	}
}
