package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncProjectRunsInstallThenCompileAndReportsSummary(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: []string{filepath.Join(library, "skills", "docker")},
			MCP: []MCPDependency{{Name: "local-db", Command: "sqlite-mcp"}},
		},
	})
	requireNoError(t, os.MkdirAll(filepath.Join(project.Root, ".apm", "instructions"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(project.Root, ".apm", "prompts"), 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(project.Root, ".apm", "instructions", "python.instructions.md"), []byte("x"), 0o644))
	requireNoError(t, os.WriteFile(filepath.Join(project.Root, ".apm", "prompts", "debug.prompt.md"), []byte("x"), 0o644))

	calls := []string{}
	var stdout bytes.Buffer
	err := SyncProject(SyncOptions{
		Project:     project,
		LibraryPath: library,
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &stdout,
	})

	requireNoError(t, err)
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --project " + project.Root,
		"apm compile --project " + project.Root,
	})
	requireContains(t, stdout.String(), "ok: synced 1 skills, 1 mcp servers, 1 instructions, 1 prompts")
}

func TestProjectStatusReportsRemovedAvailableAndHashMismatch(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{
			{Type: LibraryTypeSkill, Name: "docker", Path: "docker/SKILL.md"},
			{Type: LibraryTypeSkill, Name: "golang-cli", Path: "golang-cli/SKILL.md"},
		},
		instructions: []CatalogEntry{
			{Type: LibraryTypeInstruction, Name: "python-rules", Path: "python-rules/INSTRUCTION.md"},
		},
	})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: []string{
				filepath.Join(library, "skills", "docker"),
				filepath.Join(library, "skills", "missing"),
			},
		},
	})
	requireNoError(t, os.MkdirAll(filepath.Join(project.Root, ".apm", "instructions"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(project.Root, ".apm", "instructions", "python-rules.instructions.md"),
		[]byte("# stale instruction\n"),
		0o644,
	))
	requireNoError(t, os.WriteFile(
		filepath.Join(project.Root, "apm.lock.yaml"),
		[]byte(strings.Join([]string{
			"instructions:",
			"  - name: python-rules",
			"    sha256: deadbeef",
			"",
		}, "\n")),
		0o644,
	))

	calls := []string{}
	var stdout bytes.Buffer
	err := ProjectStatus(StatusOptions{
		Project:     project,
		LibraryPath: library,
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &stdout,
	})

	requireNoError(t, err)
	assertCommands(t, calls, []string{"apm --version"})
	got := stdout.String()
	requireContains(t, got, "removed from library: skill missing")
	requireContains(t, got, "available in library: skill golang-cli")
	requireContains(t, got, "hash mismatch: instruction python-rules")
}

func TestProjectStatusSupportsNestedSkillsAndMultiTypeDrift(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{
			{Type: LibraryTypeSkill, Name: "cloud/azure/azure-cli", Path: "cloud/azure/azure-cli/SKILL.md"},
			{Type: LibraryTypeSkill, Name: "golang-cli", Path: "golang-cli/SKILL.md"},
		},
		mcp: []CatalogEntry{
			{Type: LibraryTypeMCP, Name: "current-mcp", Transport: "stdio", Command: "current-mcp"},
			{Type: LibraryTypeMCP, Name: "available-mcp", Transport: "stdio", Command: "available-mcp"},
		},
		instructions: []CatalogEntry{
			{Type: LibraryTypeInstruction, Name: "python-rules", Path: "python-rules/INSTRUCTION.md"},
			{Type: LibraryTypeInstruction, Name: "available-instruction", Path: "available-instruction/INSTRUCTION.md"},
		},
		prompts: []CatalogEntry{
			{Type: LibraryTypePrompt, Name: "debug", Path: "debug/PROMPT.md"},
			{Type: LibraryTypePrompt, Name: "available-prompt", Path: "available-prompt/PROMPT.md"},
		},
	})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: []string{
				filepath.Join(library, "skills", "cloud", "azure", "azure-cli"),
				filepath.Join(library, "skills", "missing", "skill"),
			},
			MCP: []MCPDependency{
				{Name: "current-mcp", Command: "current-mcp"},
				{Name: "missing-mcp", Command: "missing-mcp"},
			},
		},
	})
	requireNoError(t, os.MkdirAll(filepath.Join(project.Root, ".apm", "instructions"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(project.Root, ".apm", "prompts"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(project.Root, ".apm", "instructions", "python-rules.instructions.md"),
		[]byte("# stale instruction\n"),
		0o644,
	))
	requireNoError(t, os.WriteFile(
		filepath.Join(project.Root, ".apm", "prompts", "debug.prompt.md"),
		[]byte("# stale prompt\n"),
		0o644,
	))
	requireNoError(t, os.WriteFile(
		filepath.Join(project.Root, "apm.lock.yaml"),
		[]byte(strings.Join([]string{
			"instructions:",
			"  - name: python-rules",
			"    sha256: deadbeef",
			"  - name: missing-instruction",
			"    sha256: cafebabe",
			"prompts:",
			"  - name: debug",
			"    sha256: deadbeef",
			"  - name: missing-prompt",
			"    sha256: cafebabe",
			"",
		}, "\n")),
		0o644,
	))

	calls := []string{}
	var stdout bytes.Buffer
	err := ProjectStatus(StatusOptions{
		Project:     project,
		LibraryPath: library,
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &stdout,
	})

	requireNoError(t, err)
	assertCommands(t, calls, []string{"apm --version"})
	got := stdout.String()
	requireNotContains(t, got, "available in library: skill cloud/azure/azure-cli")
	requireContains(t, got, "removed from library: skill missing/skill")
	requireContains(t, got, "available in library: skill golang-cli")
	requireContains(t, got, "removed from library: mcp missing-mcp")
	requireContains(t, got, "available in library: mcp available-mcp")
	requireContains(t, got, "available in library: instruction available-instruction")
	requireContains(t, got, "hash mismatch: instruction python-rules")
	requireContains(t, got, "available in library: prompt available-prompt")
	requireContains(t, got, "hash mismatch: prompt debug")
}

func TestProjectStatusStaleLockDoesNotSuppressAvailableCopiedContent(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		instructions: []CatalogEntry{
			{Type: LibraryTypeInstruction, Name: "python-rules", Path: "python-rules/INSTRUCTION.md"},
		},
	})
	project := createAPMProject(t, APMManifest{})
	requireNoError(t, os.WriteFile(
		filepath.Join(project.Root, "apm.lock.yaml"),
		[]byte(strings.Join([]string{
			"instructions:",
			"  - name: python-rules",
			"    sha256: deadbeef",
			"",
		}, "\n")),
		0o644,
	))

	calls := []string{}
	var stdout bytes.Buffer
	err := ProjectStatus(StatusOptions{
		Project:     project,
		LibraryPath: library,
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &stdout,
	})

	requireNoError(t, err)
	assertCommands(t, calls, []string{"apm --version"})
	got := stdout.String()
	requireContains(t, got, "available in library: instruction python-rules")
	requireNotContains(t, got, "hash mismatch: instruction python-rules")
}

func TestProjectStatusReportsOrphanedCopiedContentWithoutLockData(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{})
	requireNoError(t, os.MkdirAll(filepath.Join(project.Root, ".apm", "instructions"), 0o755))
	requireNoError(t, os.WriteFile(
		filepath.Join(project.Root, ".apm", "instructions", "orphaned.instructions.md"),
		[]byte("# orphaned instruction\n"),
		0o644,
	))

	calls := []string{}
	var stdout bytes.Buffer
	err := ProjectStatus(StatusOptions{
		Project:     project,
		LibraryPath: library,
		Runner:      recordingRunner(&calls, nil),
		Stdout:      &stdout,
	})

	requireNoError(t, err)
	assertCommands(t, calls, []string{"apm --version"})
	requireContains(t, stdout.String(), "removed from library: instruction orphaned")
}

type catalogLibrarySeed struct {
	skills       []CatalogEntry
	mcp          []CatalogEntry
	instructions []CatalogEntry
	prompts      []CatalogEntry
}

func createCatalogLibrary(t *testing.T, seed catalogLibrarySeed) string {
	t.Helper()

	root := t.TempDir()
	writeCatalogFixtures(t, root, LibraryTypeSkill, seed.skills)
	writeCatalogFixtures(t, root, LibraryTypeMCP, seed.mcp)
	writeCatalogFixtures(t, root, LibraryTypeInstruction, seed.instructions)
	writeCatalogFixtures(t, root, LibraryTypePrompt, seed.prompts)
	return root
}

func writeCatalogFixtures(t *testing.T, root string, typ LibraryType, entries []CatalogEntry) {
	t.Helper()

	requireNoError(t, WriteCatalog(root, typ, entries))
	for _, entry := range entries {
		relative := catalogContentRelativePath(entry)
		if relative == "" {
			continue
		}
		target := filepath.Join(root, projectLibraryTypeDir(typ), relative)
		requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		requireNoError(t, os.WriteFile(target, []byte(catalogContent(entry)), 0o644))
	}
}

func createAPMProject(t *testing.T, manifest APMManifest) Project {
	t.Helper()

	root := t.TempDir()
	path := filepath.Join(root, "apm.yml")
	requireNoError(t, WriteAPMManifestAtomic(path, manifest))
	return Project{
		Root:             root,
		ManifestPath:     path,
		SymlinkDir:       filepath.Join(root, ".claude", "skills"),
		AgentsSymlinkDir: filepath.Join(root, ".agents", "skills"),
	}
}

func recordingRunner(calls *[]string, responses map[string][]byte) CommandRunner {
	return func(name string, args ...string) ([]byte, error) {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		if calls != nil {
			*calls = append(*calls, command)
		}
		if responses != nil {
			if output, ok := responses[command]; ok {
				return output, nil
			}
		}
		if command == "apm --version" {
			return []byte("apm 0.1.0\n"), nil
		}
		return []byte("ok\n"), nil
	}
}

func assertCommands(t *testing.T, got []string, want []string) {
	t.Helper()
	requireEqual(t, want, got)
}

func requireNotContains(t *testing.T, got string, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Fatalf("got %q, did not expect to contain %q", got, want)
	}
}

func ioDiscard() *bytes.Buffer {
	return &bytes.Buffer{}
}

func projectLibraryTypeDir(typ LibraryType) string {
	switch typ {
	case LibraryTypeSkill:
		return "skills"
	case LibraryTypeMCP:
		return "mcp"
	case LibraryTypeInstruction:
		return "instructions"
	case LibraryTypePrompt:
		return "prompts"
	default:
		return ""
	}
}

func catalogContentRelativePath(entry CatalogEntry) string {
	switch entry.Type {
	case LibraryTypeSkill, LibraryTypeInstruction, LibraryTypePrompt:
		return entry.Path
	default:
		return ""
	}
}

func catalogContent(entry CatalogEntry) string {
	switch entry.Type {
	case LibraryTypeSkill:
		return "# skill " + entry.Name + "\n"
	case LibraryTypeInstruction:
		return "# instruction " + entry.Name + "\n"
	case LibraryTypePrompt:
		return "# prompt " + entry.Name + "\n"
	default:
		return ""
	}
}
