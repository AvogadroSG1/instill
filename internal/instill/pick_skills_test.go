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
	requireEqual(t, []string{filepath.Join(library, "skills", "docker")}, manifest.Dependencies.APM)
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --project " + project.Root,
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
		Name:    "local-db",
		Command: "sqlite-mcp",
		Args:    []string{"--db", "dev.db"},
		Env:     []string{"DB_PATH=${DB_PATH}"},
	}}, manifest.Dependencies.MCP)
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --project " + project.Root,
	})
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
		"apm install --project " + project.Root,
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
		"apm install --project " + project.Root,
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
			APM: []string{filepath.Join(library, "skills", "docker")},
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
		"apm prune --project " + project.Root,
	})
}

func TestPickMixedAddAndRemoveRunsPruneThenInstall(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{
			Type: LibraryTypeSkill,
			Name: "docker",
			Path: "docker/SKILL.md",
		}, {
			Type: LibraryTypeSkill,
			Name: "golang",
			Path: "golang/SKILL.md",
		}},
	})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: []string{filepath.Join(library, "skills", "docker")},
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
	requireEqual(t, []string{filepath.Join(library, "skills", "golang")}, manifest.Dependencies.APM)
	assertCommands(t, calls, []string{
		"apm --version",
		"apm prune --project " + project.Root,
		"apm install --project " + project.Root,
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
			APM: []string{filepath.Join(library, "skills", "cloud", "azure", "azure-cli")},
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
		"apm prune --project " + project.Root,
	})
}

func TestPickRemovalSupportsStaleSkillDependencyMissingFromCatalog(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: []string{filepath.Join(library, "skills", "docker")},
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
		"apm prune --project " + project.Root,
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
		"apm prune --project " + project.Root,
	})
}

func TestPickRemovalDeletesStaleMCPDependencyMissingFromCatalog(t *testing.T) {
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

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, 0, len(manifest.Dependencies.MCP))
	assertCommands(t, calls, []string{
		"apm --version",
		"apm prune --project " + project.Root,
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
		"apm prune --project " + project.Root,
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
		"apm prune --project " + project.Root,
	})
}
