package instill

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchCatalogEntryForLocalDependency(t *testing.T) {
	t.Parallel()

	libraryPath := filepath.Join(t.TempDir(), "library")
	gmail := CatalogEntry{
		Type: LibraryTypeSkill,
		Name: "productivity/gws-skills/gws-gmail-read",
		Path: "productivity/gws-skills/gws-gmail-read/SKILL.md",
	}
	productVision := CatalogEntry{
		Type: LibraryTypeSkill,
		Name: "product-management/skills/product-vision",
		Path: "product-management/skills/product-vision/SKILL.md",
	}
	todoCLI := CatalogEntry{
		Type: LibraryTypeSkill,
		Name: "productivity/todo-cli",
		Path: "productivity/todo-cli/SKILL.md",
	}
	rootSkill := CatalogEntry{
		Type: LibraryTypeSkill,
		Name: "root-skill",
		Path: "SKILL.md",
	}
	rootRelocatedSkill := CatalogEntry{
		Type: LibraryTypeSkill,
		Name: "productivity/root-skill",
		Path: "productivity/root-skill/SKILL.md",
	}
	plugin := CatalogEntry{
		Type: LibraryTypePlugin,
		Name: "shortcuts-playground/claude",
		Path: "shortcuts-playground/claude/.claude-plugin/plugin.json",
	}
	relocatedPlugin := CatalogEntry{
		Type: LibraryTypePlugin,
		Name: "productivity/shortcuts-playground/claude",
		Path: "productivity/shortcuts-playground/claude/.claude-plugin/plugin.json",
	}

	tests := []struct {
		name      string
		typ       LibraryType
		localPath string
		catalog   []CatalogEntry
		want      CatalogEntry
		wantOK    bool
	}{
		{
			name:      "matches canonical skill path",
			typ:       LibraryTypeSkill,
			localPath: skillDependencyPath(libraryPath, gmail),
			catalog:   []CatalogEntry{gmail},
			want:      gmail,
			wantOK:    true,
		},
		{
			name:      "matches root path to sole catalog entry when its marker is absent",
			typ:       LibraryTypeSkill,
			localPath: filepath.Join(libraryPath, "skills"),
			catalog:   []CatalogEntry{rootRelocatedSkill},
			want:      rootRelocatedSkill,
			wantOK:    true,
		},
		{
			name:      "matches canonical root skill path",
			typ:       LibraryTypeSkill,
			localPath: skillDependencyPath(libraryPath, rootSkill),
			catalog:   []CatalogEntry{rootSkill},
			want:      rootSkill,
			wantOK:    true,
		},
		{
			name:      "matches canonical plugin path",
			typ:       LibraryTypePlugin,
			localPath: pluginDependencyPath(libraryPath, plugin),
			catalog:   []CatalogEntry{plugin},
			want:      plugin,
			wantOK:    true,
		},
		{
			name:      "matches relocated plugin path suffix",
			typ:       LibraryTypePlugin,
			localPath: filepath.Join(libraryPath, "plugins", "shortcuts-playground", "claude"),
			catalog:   []CatalogEntry{relocatedPlugin},
			want:      relocatedPlugin,
			wantOK:    true,
		},
		{
			name:      "matches unique path suffix after category relocation",
			typ:       LibraryTypeSkill,
			localPath: filepath.Join(libraryPath, "skills", "gws-skills", "gws-gmail-read"),
			catalog:   []CatalogEntry{gmail},
			want:      gmail,
			wantOK:    true,
		},
		{
			name:      "matches retained suffix after category reorganization",
			typ:       LibraryTypeSkill,
			localPath: filepath.Join(libraryPath, "skills", "pm-product-strategy", "skills", "product-vision"),
			catalog:   []CatalogEntry{productVision},
			want:      productVision,
			wantOK:    true,
		},
		{
			name:      "matches unique leaf name",
			typ:       LibraryTypeSkill,
			localPath: filepath.Join(libraryPath, "skills", "todo-cli"),
			catalog:   []CatalogEntry{todoCLI},
			want:      todoCLI,
			wantOK:    true,
		},
		{
			name:      "rejects ambiguous leaf name",
			typ:       LibraryTypeSkill,
			localPath: filepath.Join(libraryPath, "skills", "product-vision"),
			catalog: []CatalogEntry{
				productVision,
				{
					Type: LibraryTypeSkill,
					Name: "strategy/product-vision",
					Path: "strategy/product-vision/SKILL.md",
				},
			},
		},
		{
			name:      "rejects ambiguous path suffix",
			typ:       LibraryTypeSkill,
			localPath: filepath.Join(libraryPath, "skills", "old-category", "shared", "skill"),
			catalog: []CatalogEntry{
				{
					Type: LibraryTypeSkill,
					Name: "first-category/shared/skill",
					Path: "first-category/shared/skill/SKILL.md",
				},
				{
					Type: LibraryTypeSkill,
					Name: "second-category/shared/skill",
					Path: "second-category/shared/skill/SKILL.md",
				},
			},
		},
		{
			name:      "rejects wrong catalog type",
			typ:       LibraryTypeSkill,
			localPath: filepath.Join(libraryPath, "skills", "todo-cli"),
			catalog:   []CatalogEntry{plugin},
		},
		{
			name:      "rejects path outside library type root",
			typ:       LibraryTypeSkill,
			localPath: filepath.Join(t.TempDir(), "skills", "todo-cli"),
			catalog:   []CatalogEntry{todoCLI},
		},
		{
			name:      "rejects local path that escapes the library type root",
			typ:       LibraryTypeSkill,
			localPath: filepath.Join(libraryPath, "skills", "..", "external", "todo-cli"),
			catalog:   []CatalogEntry{todoCLI},
		},
		{
			name:      "rejects remote catalog entry",
			typ:       LibraryTypeSkill,
			localPath: filepath.Join(libraryPath, "skills", "todo-cli"),
			catalog: []CatalogEntry{{
				Type:   LibraryTypeSkill,
				Name:   "productivity/todo-cli",
				Path:   "productivity/todo-cli/SKILL.md",
				Source: "git",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOK := matchCatalogEntryForLocalDependency(libraryPath, tt.typ, tt.localPath, tt.catalog)

			requireEqual(t, tt.wantOK, gotOK)
			if gotOK {
				requireEqual(t, tt.want, got)
			}
		})
	}
}

func TestMatchCatalogEntryForLocalDependencyPreservesRootPackageWithMarker(t *testing.T) {
	t.Parallel()

	libraryPath := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(libraryPath, "skills"), 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(libraryPath, "skills", "SKILL.md"), []byte("custom"), 0o644))
	entry := CatalogEntry{
		Type: LibraryTypeSkill,
		Name: "productivity/root-skill",
		Path: "productivity/root-skill/SKILL.md",
	}

	_, matched := matchCatalogEntryForLocalDependency(libraryPath, LibraryTypeSkill, filepath.Join(libraryPath, "skills"), []CatalogEntry{entry})

	requireEqual(t, false, matched)
}

func TestMatchCatalogEntryForLocalDependencyPreservesRootPluginWithNestedMarker(t *testing.T) {
	t.Parallel()

	libraryPath := t.TempDir()
	marker := filepath.Join(libraryPath, "plugins", ".claude-plugin", "plugin.json")
	requireNoError(t, os.MkdirAll(filepath.Dir(marker), 0o755))
	requireNoError(t, os.WriteFile(marker, []byte("{}"), 0o644))
	entry := CatalogEntry{
		Type: LibraryTypePlugin,
		Name: "productivity/root-plugin",
		Path: "productivity/root-plugin/.claude-plugin/plugin.json",
	}

	_, matched := matchCatalogEntryForLocalDependency(libraryPath, LibraryTypePlugin, filepath.Join(libraryPath, "plugins"), []CatalogEntry{entry})

	requireEqual(t, false, matched)
}

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

func TestPublicCatalogWritesPreserveCrossCatalogGitIdentity(t *testing.T) {
	root := t.TempDir()
	skill := CatalogEntry{
		Type:       LibraryTypeSkill,
		Name:       "repo",
		Path:       "skills/repo",
		Source:     "git",
		Repository: "https://github.com/owner/repo.git",
		Ref:        remotePluginSHA,
	}
	plugin := skill
	plugin.Type = LibraryTypePlugin
	plugin.Name = "plugin"
	requireNoError(t, WriteCatalog(root, LibraryTypeSkill, []CatalogEntry{skill}))

	err := WriteCatalog(root, LibraryTypePlugin, []CatalogEntry{plugin})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("WriteCatalog() error = %v, want cross-catalog ambiguity", err)
	}
	assertPathMissing(t, filepath.Join(root, "plugins", "catalog.csv"))

	err = AddCatalogEntry(root, plugin)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("AddCatalogEntry() error = %v, want cross-catalog ambiguity", err)
	}
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

func TestScanLibraryIgnoresNestedSkillMarkerInsideSkillDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteCatalogMarker(t, filepath.Join(root, "skills", "docker", "SKILL.md"))
	mustWriteCatalogMarker(t, filepath.Join(root, "skills", "docker", "examples", "nested", "SKILL.md"))

	var stdout bytes.Buffer
	requireNoError(t, ScanLibrary(root, &stdout))

	skills, err := LoadCatalog(root, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type: LibraryTypeSkill,
		Name: "docker",
		Path: "docker/SKILL.md",
	}}, skills)
}

func TestScanLibraryIgnoresNestedPluginMarkerInsidePluginDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePluginMarker := func(rel string, name string) {
		path := filepath.Join(root, "plugins", filepath.FromSlash(rel))
		requireNoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		requireNoError(t, os.WriteFile(path, []byte(fmt.Sprintf("{\"name\":%q,\"description\":\"d\"}\n", name)), 0o644))
	}
	writePluginMarker("foo/.claude-plugin/plugin.json", "foo")
	writePluginMarker("foo/examples/bar/.claude-plugin/plugin.json", "bar")
	writePluginMarker("flat/plugin.json", "flat")
	writePluginMarker("flat/examples/plugin.json", "flat-example")

	var stdout bytes.Buffer
	requireNoError(t, ScanLibrary(root, &stdout))

	plugins, err := LoadCatalog(root, LibraryTypePlugin)
	requireNoError(t, err)
	names := make([]string, 0, len(plugins))
	for _, entry := range plugins {
		names = append(names, entry.Name)
	}
	requireEqual(t, []string{"flat", "foo"}, names)
}

func TestScanLibraryIgnoresNestedInstructionAndPromptMarkers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteCatalogMarker(t, filepath.Join(root, "instructions", "x", "INSTRUCTION.md"))
	mustWriteCatalogMarker(t, filepath.Join(root, "instructions", "x", "sub", "INSTRUCTION.md"))
	mustWriteCatalogMarker(t, filepath.Join(root, "prompts", "y", "PROMPT.md"))
	mustWriteCatalogMarker(t, filepath.Join(root, "prompts", "y", "sub", "PROMPT.md"))

	var stdout bytes.Buffer
	requireNoError(t, ScanLibrary(root, &stdout))

	instructions, err := LoadCatalog(root, LibraryTypeInstruction)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{Type: LibraryTypeInstruction, Name: "x", Path: "x/INSTRUCTION.md"}}, instructions)

	prompts, err := LoadCatalog(root, LibraryTypePrompt)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{Type: LibraryTypePrompt, Name: "y", Path: "y/PROMPT.md"}}, prompts)
}

func TestScanLibraryIgnoresNestedMCPConfigInsideServerDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, "mcp", "db", "fixtures"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "mcp", "db", "config.json"),
		[]byte("{\"transport\":\"stdio\",\"command\":\"db-mcp\"}\n"),
		0o644,
	))
	requireNoError(t, os.WriteFile(
		filepath.Join(root, "mcp", "db", "fixtures", "config.json"),
		[]byte("{\"not\":\"an mcp config\"}\n"),
		0o644,
	))

	var stdout bytes.Buffer
	requireNoError(t, ScanLibrary(root, &stdout))

	mcpEntries, err := LoadCatalog(root, LibraryTypeMCP)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type:      LibraryTypeMCP,
		Name:      "db",
		Transport: "stdio",
		Command:   "db-mcp",
	}}, mcpEntries)
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

func TestScanLibraryIgnoresPersistentInstillLock(t *testing.T) {
	root := t.TempDir()
	requireNoError(t, os.WriteFile(filepath.Join(root, lockFileName), []byte("persistent"), 0o600))
	requireNoError(t, ScanLibrary(root, nil))
	for _, typ := range []LibraryType{
		LibraryTypeSkill,
		LibraryTypePlugin,
		LibraryTypeMCP,
		LibraryTypeInstruction,
		LibraryTypePrompt,
	} {
		entries, err := LoadCatalog(root, typ)
		requireNoError(t, err)
		if len(entries) != 0 {
			t.Fatalf("LoadCatalog(%s) = %v, want no lock-file entry", typ, entries)
		}
	}
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

func writeCatalogFixtureRaw(t *testing.T, root string, typ LibraryType, entries []CatalogEntry) {
	t.Helper()
	err := withRootLocks(context.Background(), []string{root}, func(ctx context.Context, held *heldLocks) error {
		return writeCatalogRawLocked(ctx, held, root, typ, entries)
	})
	requireNoError(t, err)
}
