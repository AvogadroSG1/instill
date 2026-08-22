package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPickAddsSkillPathAndRunsAPMInstall(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{
			Type: LibraryTypeSkill,
			Name: "docker",
			Path: "docker/SKILL.md",
		}},
	})
	project := createAPMProject(t, APMManifest{})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeSkill,
		Add:         []string{"docker"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, localDependencies(filepath.Join(library, "skills", "docker")), manifest.Dependencies.APM)
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --legacy-skill-paths --root " + project.Root,
	})
}

func TestPickAddsPluginPathAndRunsAPMInstall(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		plugins: []CatalogEntry{{
			Type: LibraryTypePlugin,
			Name: "shortcuts-playground/claude",
			Path: "shortcuts-playground/claude/.claude-plugin/plugin.json",
		}},
	})
	project := createAPMProject(t, APMManifest{})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypePlugin,
		Add:         []string{"shortcuts-playground/claude"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, localDependencies(filepath.Join(library, "plugins", "shortcuts-playground", "claude")), manifest.Dependencies.APM)
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --legacy-skill-paths --root " + project.Root,
	})
}

func TestPickUnknownPluginNamesTypedLibraryShowGuidance(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{})

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypePlugin,
		Add:         []string{"missing-plugin"},
		Runner:      recordingRunner(nil, nil),
		Stdout:      &bytes.Buffer{},
	})

	if err == nil {
		t.Fatal("Pick() error = nil, want unknown plugin error")
	}
	if !strings.Contains(ErrorMessage(err), "instill library show --type plugin") {
		t.Fatalf("error = %q, want typed library show guidance for plugin", ErrorMessage(err))
	}
}

func TestPickPluginRemovalUpdatesManifestAndCallsAPMPrune(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		plugins: []CatalogEntry{{
			Type: LibraryTypePlugin,
			Name: "shortcuts-playground/claude",
			Path: "shortcuts-playground/claude/.claude-plugin/plugin.json",
		}},
	})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: localDependencies(filepath.Join(library, "plugins", "shortcuts-playground", "claude")),
		},
	})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypePlugin,
		Remove:      []string{"shortcuts-playground/claude"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, 0, len(manifest.Dependencies.APM))
	assertCommands(t, calls, []string{
		"apm --version",
		"apm prune",
	})
}

func TestPickUnknownSkillNamesTypedLibraryShowGuidance(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{})

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeSkill,
		Add:         []string{"missing"},
		Runner:      recordingRunner(nil, nil),
		Stdout:      &bytes.Buffer{},
	})

	if err == nil {
		t.Fatal("Pick() error = nil, want unknown skill error")
	}
	if !strings.Contains(ErrorMessage(err), "instill library show --type skill") {
		t.Fatalf("error = %q, want typed library show guidance", ErrorMessage(err))
	}
}

func TestPickAddsMCPBlockAndRunsAPMInstall(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		mcp: []CatalogEntry{{
			Type:      LibraryTypeMCP,
			Name:      "local-db",
			Transport: "stdio",
			Command:   "sqlite-mcp",
			Args:      []string{"--db", "dev.db"},
			Env:       []string{"DB_PATH=${DB_PATH}"},
		}},
	})
	project := createAPMProject(t, APMManifest{})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeMCP,
		Add:         []string{"local-db"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, []MCPDependency{{
		Name:      "local-db",
		Transport: "stdio",
		Registry:  false,
		Command:   "sqlite-mcp",
		Args:      []string{"--db", "dev.db"},
		Env:       map[string]string{"DB_PATH": "${DB_PATH}"},
	}}, manifest.Dependencies.MCP)
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --legacy-skill-paths --root " + project.Root,
	})
}

func TestPickSerializesMCPEnvironmentAsMappingAndSplitsFirstEquals(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		mcp: []CatalogEntry{{
			Type: LibraryTypeMCP, Name: "local-db", Transport: "stdio", Command: "sqlite-mcp",
			Env: []string{"TOKEN=${TOKEN}", "DSN=scheme://host?a=b"},
		}},
	})
	project := createAPMProject(t, APMManifest{})

	err := Pick(PickOptions{
		Project: project, LibraryPath: library, Type: LibraryTypeMCP, Add: []string{"local-db"},
		Runner: recordingRunner(nil, nil), Stdout: &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, map[string]string{"DSN": "scheme://host?a=b", "TOKEN": "${TOKEN}"}, manifest.Dependencies.MCP[0].Env)
}

func TestPickAddsSelfDefinedHTTPMCPDependency(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		mcp: []CatalogEntry{{Type: LibraryTypeMCP, Name: "remote", Transport: "http", URL: "https://example.test/mcp"}},
	})
	project := createAPMProject(t, APMManifest{})

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeMCP,
		Add:         []string{"remote"},
		Runner:      recordingRunner(nil, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, []MCPDependency{{Name: "remote", Transport: "http", Registry: false, URL: "https://example.test/mcp"}}, manifest.Dependencies.MCP)
}

func TestPickCopiesInstructionAndRunsAPMInstall(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		instructions: []CatalogEntry{{
			Type:    LibraryTypeInstruction,
			Name:    "python-rules",
			Path:    "python-rules/INSTRUCTION.md",
			ApplyTo: "**/*.py",
		}},
	})
	project := createAPMProject(t, APMManifest{})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeInstruction,
		Add:         []string{"python-rules"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	got := readFile(t, filepath.Join(project.Root, ".apm", "instructions", "python-rules.instructions.md"))
	requireContains(t, got, "# instruction python-rules")
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --legacy-skill-paths --root " + project.Root,
	})
}

func TestPickCopiesPromptAndRunsAPMInstall(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		prompts: []CatalogEntry{{
			Type: LibraryTypePrompt,
			Name: "debug",
			Path: "debug/PROMPT.md",
		}},
	})
	project := createAPMProject(t, APMManifest{})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypePrompt,
		Add:         []string{"debug"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	got := readFile(t, filepath.Join(project.Root, ".apm", "prompts", "debug.prompt.md"))
	requireContains(t, got, "# prompt debug")
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --legacy-skill-paths --root " + project.Root,
	})
}

func TestPickRemovalUpdatesManifestAndCallsAPMPrune(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{
			Type: LibraryTypeSkill,
			Name: "docker",
			Path: "docker/SKILL.md",
		}},
	})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: localDependencies(filepath.Join(library, "skills", "docker")),
		},
	})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeSkill,
		Remove:      []string{"docker"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, 0, len(manifest.Dependencies.APM))
	assertCommands(t, calls, []string{
		"apm --version",
		"apm prune",
	})
}

func TestPickMixedAddAndRemoveRunsInstallThenPrune(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{
			Type: LibraryTypeSkill,
			Name: "golang",
			Path: "golang/SKILL.md",
		}},
	})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: localDependencies(filepath.Join(library, "skills", "docker")),
		},
	})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeSkill,
		Add:         []string{"golang"},
		Remove:      []string{"docker"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, localDependencies(filepath.Join(library, "skills", "golang")), manifest.Dependencies.APM)
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --legacy-skill-paths --root " + project.Root,
		"apm prune",
	})
}

func TestPickRemovalSupportsNestedSkillNames(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{
			Type: LibraryTypeSkill,
			Name: "cloud/azure/azure-cli",
			Path: "cloud/azure/azure-cli/SKILL.md",
		}},
	})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: localDependencies(filepath.Join(library, "skills", "cloud", "azure", "azure-cli")),
		},
	})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeSkill,
		Remove:      []string{"cloud/azure/azure-cli"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, 0, len(manifest.Dependencies.APM))
	assertCommands(t, calls, []string{
		"apm --version",
		"apm prune",
	})
}

func TestPickRemovalSupportsStaleSkillDependencyMissingFromCatalog(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: localDependencies(filepath.Join(library, "skills", "docker")),
		},
	})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeSkill,
		Remove:      []string{"docker"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, 0, len(manifest.Dependencies.APM))
	assertCommands(t, calls, []string{
		"apm --version",
		"apm prune",
	})
}

func TestPickRemovalSupportsStalePluginDependencyMissingFromCatalog(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: localDependencies(filepath.Join(library, "plugins", "plugin")),
		},
	})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypePlugin,
		Remove:      []string{"plugin"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, 0, len(manifest.Dependencies.APM))
	assertCommands(t, calls, []string{
		"apm --version",
		"apm prune",
	})
}

func TestPickRemovalDeletesCopiedInstruction(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		instructions: []CatalogEntry{{
			Type: LibraryTypeInstruction,
			Name: "python-rules",
			Path: "python-rules/INSTRUCTION.md",
		}},
	})
	project := createAPMProject(t, APMManifest{})
	requireNoError(t, os.MkdirAll(filepath.Join(project.Root, ".apm", "instructions"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(project.Root, ".apm", "instructions", "python-rules.instructions.md"),
		[]byte("old"),
		0o644,
	))
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeInstruction,
		Remove:      []string{"python-rules"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	assertPathMissing(t, filepath.Join(project.Root, ".apm", "instructions", "python-rules.instructions.md"))
	assertCommands(t, calls, []string{
		"apm --version",
		"apm prune",
	})
}

func TestPickRemovalRejectsUserOwnedMCPDependencyMissingFromCatalog(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			MCP: []MCPDependency{{
				Name:    "local-db",
				Command: "sqlite-mcp",
				Args:    []string{"--db", "dev.db"},
			}},
		},
	})
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeMCP,
		Remove:      []string{"local-db"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	if err == nil {
		t.Fatal("Pick() error = nil, want unknown MCP rejection")
	}
	requireContains(t, ErrorMessage(err), "unknown mcp: local-db")
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, 1, len(manifest.Dependencies.MCP))
	requireEqual(t, "local-db", manifest.Dependencies.MCP[0].Name)
	assertCommands(t, calls, []string{
		"apm --version",
	})
}

func TestPickRemovalDeletesCopiedInstructionMissingFromCatalog(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{})
	requireNoError(t, os.MkdirAll(filepath.Join(project.Root, ".apm", "instructions"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(project.Root, ".apm", "instructions", "python-rules.instructions.md"),
		[]byte("old"),
		0o644,
	))
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeInstruction,
		Remove:      []string{"python-rules"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	assertPathMissing(t, filepath.Join(project.Root, ".apm", "instructions", "python-rules.instructions.md"))
	assertCommands(t, calls, []string{
		"apm --version",
		"apm prune",
	})
}

func TestPickRemovalDeletesCopiedPromptMissingFromCatalog(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{})
	requireNoError(t, os.MkdirAll(filepath.Join(project.Root, ".apm", "prompts"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(project.Root, ".apm", "prompts", "debug.prompt.md"),
		[]byte("old"),
		0o644,
	))
	calls := []string{}

	err := Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypePrompt,
		Remove:      []string{"debug"},
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &bytes.Buffer{},
	})

	requireNoError(t, err)
	assertPathMissing(t, filepath.Join(project.Root, ".apm", "prompts", "debug.prompt.md"))
	assertCommands(t, calls, []string{
		"apm --version",
		"apm prune",
	})
}
