package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCatalogReadsSkillSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, "skills"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "skills", "catalog.csv"),
		[]byte("name,category,path,description\ncloud/azure/azure-cli,cloud/azure,cloud/azure/azure-cli/SKILL.md,Azure CLI helper\ndocker,,docker/SKILL.md,Container workflow\n"),
		0o644,
	))

	entries, err := LoadCatalog(root, LibraryTypeSkill)

	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{
		{
			Type:        LibraryTypeSkill,
			Name:        "cloud/azure/azure-cli",
			Category:    "cloud/azure",
			Path:        "cloud/azure/azure-cli/SKILL.md",
			Description: "Azure CLI helper",
		},
		{
			Type:        LibraryTypeSkill,
			Name:        "docker",
			Path:        "docker/SKILL.md",
			Description: "Container workflow",
		},
	}, entries)
}

func TestLoadCatalogReadsPluginSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, "plugins"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "plugins", "catalog.csv"),
		[]byte("name,category,path,description\nshortcuts-playground/claude,shortcuts-playground,shortcuts-playground/claude/.claude-plugin/plugin.json,Shortcuts plugin\ndocker,,docker/plugin.json,Container plugin\n"),
		0o644,
	))

	entries, err := LoadCatalog(root, LibraryTypePlugin)

	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{
		{
			Type:        LibraryTypePlugin,
			Name:        "docker",
			Path:        "docker/plugin.json",
			Description: "Container plugin",
		},
		{
			Type:        LibraryTypePlugin,
			Name:        "shortcuts-playground/claude",
			Category:    "shortcuts-playground",
			Path:        "shortcuts-playground/claude/.claude-plugin/plugin.json",
			Description: "Shortcuts plugin",
		},
	}, entries)
}

func TestLoadCatalogReadsMCPSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, "mcp"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "mcp", "catalog.csv"),
		[]byte("name,transport,command,args,url,env,description\nlocal-db,stdio,sqlite-mcp,\"--db,dev.db\",,\"DB_PATH=${DB_PATH},ENV=${ENV}\",SQLite server\nremote,http,,,https://example.test/mcp,,Remote server\n"),
		0o644,
	))

	entries, err := LoadCatalog(root, LibraryTypeMCP)

	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{
		{
			Type:        LibraryTypeMCP,
			Name:        "local-db",
			Transport:   "stdio",
			Command:     "sqlite-mcp",
			Args:        []string{"--db", "dev.db"},
			Env:         []string{"DB_PATH=${DB_PATH}", "ENV=${ENV}"},
			Description: "SQLite server",
		},
		{
			Type:        LibraryTypeMCP,
			Name:        "remote",
			Transport:   "http",
			URL:         "https://example.test/mcp",
			Description: "Remote server",
		},
	}, entries)
}

func TestLoadCatalogReadsInstructionSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, "instructions"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "instructions", "catalog.csv"),
		[]byte("name,apply_to,path,description\npython-rules,**/*.py,python-rules/INSTRUCTION.md,Python rules\n"),
		0o644,
	))

	entries, err := LoadCatalog(root, LibraryTypeInstruction)

	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type:        LibraryTypeInstruction,
		Name:        "python-rules",
		ApplyTo:     "**/*.py",
		Path:        "python-rules/INSTRUCTION.md",
		Description: "Python rules",
	}}, entries)
}

func TestLoadCatalogReadsPromptSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, "prompts"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "prompts", "catalog.csv"),
		[]byte("name,path,description\ndebug,debug/PROMPT.md,Debug helper\n"),
		0o644,
	))

	entries, err := LoadCatalog(root, LibraryTypePrompt)

	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type:        LibraryTypePrompt,
		Name:        "debug",
		Path:        "debug/PROMPT.md",
		Description: "Debug helper",
	}}, entries)
}

func TestLoadCatalogRejectsMCPStdioWithoutCommand(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, "mcp"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "mcp", "catalog.csv"),
		[]byte("name,transport,command,args,url,env,description\nlocal-db,stdio,,,,,\n"),
		0o644,
	))

	_, err := LoadCatalog(root, LibraryTypeMCP)

	if err == nil {
		t.Fatal("LoadCatalog() error = nil, want stdio validation failure")
	}
	requireEqual(t, ExitGeneral, ExitCode(err))
}

func TestLoadCatalogRejectsMCPHTTPWithoutURL(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, "mcp"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "mcp", "catalog.csv"),
		[]byte("name,transport,command,args,url,env,description\nremote,http,,,,,\n"),
		0o644,
	))

	_, err := LoadCatalog(root, LibraryTypeMCP)

	if err == nil {
		t.Fatal("LoadCatalog() error = nil, want http validation failure")
	}
	requireEqual(t, ExitGeneral, ExitCode(err))
}

func TestWriteCatalogWritesSkillSchemaSortedByName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	entries := []CatalogEntry{
		{
			Type:        LibraryTypeSkill,
			Name:        "docker",
			Path:        "docker/SKILL.md",
			Description: "Container workflow",
		},
		{
			Type:        LibraryTypeSkill,
			Name:        "cloud/azure/azure-cli",
			Category:    "cloud/azure",
			Path:        "cloud/azure/azure-cli/SKILL.md",
			Description: "Azure CLI helper",
		},
	}

	err := WriteCatalog(root, LibraryTypeSkill, entries)

	requireNoError(t, err)
	got := readFile(t, filepath.Join(root, "skills", "catalog.csv"))
	want := strings.Join([]string{
		"name,category,path,source,repository,ref,description",
		"cloud/azure/azure-cli,cloud/azure,cloud/azure/azure-cli/SKILL.md,,,,Azure CLI helper",
		"docker,,docker/SKILL.md,,,,Container workflow",
		"",
	}, "\n")
	requireEqual(t, want, got)
}

func TestWriteCatalogWritesPluginSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	entries := []CatalogEntry{
		{
			Type:        LibraryTypePlugin,
			Name:        "docker",
			Path:        "docker/plugin.json",
			Description: "Container plugin",
		},
		{
			Type:        LibraryTypePlugin,
			Name:        "shortcuts-playground/claude",
			Category:    "shortcuts-playground",
			Path:        "shortcuts-playground/claude/.claude-plugin/plugin.json",
			Description: "Shortcuts plugin",
		},
	}

	err := WriteCatalog(root, LibraryTypePlugin, entries)

	requireNoError(t, err)
	got := readFile(t, filepath.Join(root, "plugins", "catalog.csv"))
	want := strings.Join([]string{
		"name,category,path,source,repository,ref,description",
		"docker,,docker/plugin.json,,,,Container plugin",
		"shortcuts-playground/claude,shortcuts-playground,shortcuts-playground/claude/.claude-plugin/plugin.json,,,,Shortcuts plugin",
		"",
	}, "\n")
	requireEqual(t, want, got)
}

func TestWriteCatalogWritesMCPSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	entries := []CatalogEntry{
		{
			Type:        LibraryTypeMCP,
			Name:        "local-db",
			Transport:   "stdio",
			Command:     "sqlite-mcp",
			Args:        []string{"--db", "dev.db"},
			Env:         []string{"DB_PATH=${DB_PATH}", "ENV=${ENV}"},
			Description: "SQLite server",
		},
	}

	err := WriteCatalog(root, LibraryTypeMCP, entries)

	requireNoError(t, err)
	got := readFile(t, filepath.Join(root, "mcp", "catalog.csv"))
	want := strings.Join([]string{
		"name,transport,command,args,url,env,description",
		"local-db,stdio,sqlite-mcp,\"--db,dev.db\",,\"DB_PATH=${DB_PATH},ENV=${ENV}\",SQLite server",
		"",
	}, "\n")
	requireEqual(t, want, got)
}

func TestWriteCatalogWritesInstructionSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	entries := []CatalogEntry{{
		Type:        LibraryTypeInstruction,
		Name:        "python-rules",
		ApplyTo:     "**/*.py",
		Path:        "python-rules/INSTRUCTION.md",
		Description: "Python rules",
	}}

	err := WriteCatalog(root, LibraryTypeInstruction, entries)

	requireNoError(t, err)
	got := readFile(t, filepath.Join(root, "instructions", "catalog.csv"))
	want := strings.Join([]string{
		"name,apply_to,path,description",
		"python-rules,**/*.py,python-rules/INSTRUCTION.md,Python rules",
		"",
	}, "\n")
	requireEqual(t, want, got)
}

func TestWriteCatalogWritesPromptSchema(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	entries := []CatalogEntry{{
		Type:        LibraryTypePrompt,
		Name:        "debug",
		Path:        "debug/PROMPT.md",
		Description: "Debug helper",
	}}

	err := WriteCatalog(root, LibraryTypePrompt, entries)

	requireNoError(t, err)
	got := readFile(t, filepath.Join(root, "prompts", "catalog.csv"))
	want := strings.Join([]string{
		"name,path,description",
		"debug,debug/PROMPT.md,Debug helper",
		"",
	}, "\n")
	requireEqual(t, want, got)
}

func TestScanLibraryWritesCatalogsAndPreservesManualMetadata(t *testing.T) {
	t.Parallel()

	root := createTypedLibrary(t)
	requireNoError(t, WriteCatalog(root, LibraryTypeSkill, []CatalogEntry{{
		Type:        LibraryTypeSkill,
		Name:        "cloud/azure/azure-cli",
		Category:    "custom/cloud",
		Path:        "cloud/azure/azure-cli/SKILL.md",
		Description: "Azure CLI helper",
	}}))
	requireNoError(t, WriteCatalog(root, LibraryTypeMCP, []CatalogEntry{{
		Type:        LibraryTypeMCP,
		Name:        "local-db",
		Transport:   "stdio",
		Command:     "sqlite-mcp",
		Args:        []string{"--db", "dev.db"},
		Env:         []string{"DB_PATH=${DB_PATH}"},
		Description: "SQLite server",
	}}))

	var stdout bytes.Buffer
	err := ScanLibrary(root, &stdout)

	requireNoError(t, err)
	requireEqual(t, "", stdout.String())

	skills, err := LoadCatalog(root, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type:        LibraryTypeSkill,
		Name:        "cloud/azure/azure-cli",
		Category:    "custom/cloud",
		Path:        "cloud/azure/azure-cli/SKILL.md",
		Description: "Azure CLI helper",
	}}, skills)

	mcpEntries, err := LoadCatalog(root, LibraryTypeMCP)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type:        LibraryTypeMCP,
		Name:        "local-db",
		Transport:   "stdio",
		Command:     "sqlite-mcp",
		Args:        []string{"--db", "dev.db"},
		Env:         []string{"DB_PATH=${DB_PATH}"},
		Description: "SQLite server",
	}}, mcpEntries)

	instructions, err := LoadCatalog(root, LibraryTypeInstruction)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type: LibraryTypeInstruction,
		Name: "python-rules",
		Path: "python-rules/INSTRUCTION.md",
	}}, instructions)

	plugins, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type:        LibraryTypePlugin,
		Name:        "shortcuts-playground/claude",
		Category:    "shortcuts-playground",
		Path:        "shortcuts-playground/claude/.claude-plugin/plugin.json",
		Description: "Shortcuts description",
	}}, plugins)

	prompts, err := LoadCatalog(root, LibraryTypePrompt)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type: LibraryTypePrompt,
		Name: "debug",
		Path: "debug/PROMPT.md",
	}}, prompts)
}

func TestScanLibraryRemovesEntriesWhoseContentIsMissing(t *testing.T) {
	t.Parallel()

	root := createTypedLibrary(t)
	requireNoError(t, WriteCatalog(root, LibraryTypePrompt, []CatalogEntry{
		{
			Type:        LibraryTypePrompt,
			Name:        "debug",
			Path:        "debug/PROMPT.md",
			Description: "Debug helper",
		},
		{
			Type:        LibraryTypePrompt,
			Name:        "stale",
			Path:        "stale/PROMPT.md",
			Description: "Old helper",
		},
	}))

	var stdout bytes.Buffer
	err := ScanLibrary(root, &stdout)

	requireNoError(t, err)
	requireEqual(t, "removed: stale (content not found)\n", stdout.String())

	prompts, err := LoadCatalog(root, LibraryTypePrompt)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type:        LibraryTypePrompt,
		Name:        "debug",
		Path:        "debug/PROMPT.md",
		Description: "Debug helper",
	}}, prompts)
}

func TestScanLibraryPreservesExistingEntryWhenPathMatchesDiscoveredContent(t *testing.T) {
	t.Parallel()

	root := createTypedLibrary(t)
	requireNoError(t, WriteCatalog(root, LibraryTypeSkill, []CatalogEntry{{
		Type:        LibraryTypeSkill,
		Name:        "custom/azure-helper",
		Category:    "custom",
		Path:        "cloud/azure/azure-cli/SKILL.md",
		Description: "Manual alias",
	}}))

	var stdout bytes.Buffer
	err := ScanLibrary(root, &stdout)

	requireNoError(t, err)
	requireEqual(t, "", stdout.String())

	skills, err := LoadCatalog(root, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type:        LibraryTypeSkill,
		Name:        "custom/azure-helper",
		Category:    "custom",
		Path:        "cloud/azure/azure-cli/SKILL.md",
		Description: "Manual alias",
	}}, skills)
}

func TestScanLibraryTypeOnlyWritesRequestedCatalog(t *testing.T) {
	t.Parallel()

	root := createTypedLibrary(t)
	pluginCatalog := filepath.Join(root, "plugins", "catalog.csv")
	requireNoError(t, os.WriteFile(pluginCatalog, []byte("preserve this catalog\n"), 0o644))

	requireNoError(t, ScanLibraryType(root, LibraryTypeSkill, &bytes.Buffer{}))

	requireEqual(t, "preserve this catalog\n", readFile(t, pluginCatalog))
	entries, err := LoadCatalog(root, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, 1, len(entries))
}

func TestScanLibraryReturnsClearErrorForIncompleteDiscoveredMCPConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteCatalogMarker(t, filepath.Join(root, "mcp", "local-db", "config.json"))

	var stdout bytes.Buffer
	err := ScanLibrary(root, &stdout)

	if err == nil {
		t.Fatal("ScanLibrary() error = nil, want incomplete discovered MCP config failure")
	}
	requireEqual(t, ExitGeneral, ExitCode(err))
	if !strings.Contains(err.Error(), "local-db/config.json") {
		t.Fatalf("error = %q, want config path context", err)
	}
}

func createTypedLibrary(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	mustWriteCatalogMarker(t, filepath.Join(root, "skills", "cloud", "azure", "azure-cli", "SKILL.md"))
	requireNoError(t, os.MkdirAll(filepath.Join(root, "plugins", "shortcuts-playground", "claude", ".claude-plugin"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "plugins", "shortcuts-playground", "claude", ".claude-plugin", "plugin.json"),
		[]byte("{\"name\":\"shortcuts-playground\",\"description\":\"Shortcuts description\"}\n"),
		0o644,
	))
	requireNoError(t, os.MkdirAll(filepath.Join(root, "mcp", "local-db"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "mcp", "local-db", "config.json"),
		[]byte("{\"transport\":\"stdio\",\"command\":\"sqlite-mcp\"}\n"),
		0o644,
	))
	mustWriteCatalogMarker(t, filepath.Join(root, "instructions", "python-rules", "INSTRUCTION.md"))
	mustWriteCatalogMarker(t, filepath.Join(root, "prompts", "debug", "PROMPT.md"))
	return root
}

func mustWriteCatalogMarker(t *testing.T, path string) {
	t.Helper()
	requireNoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	content := []byte("marker\n")
	if filepath.Base(path) == "config.json" {
		content = []byte("{}\n")
	}
	requireNoError(t, os.WriteFile(path, content, 0o644))
}
