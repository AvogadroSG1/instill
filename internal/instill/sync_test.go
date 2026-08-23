package instill

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestSyncProjectRunsInstallThenCompileAndReportsSummary(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: localDependencies(filepath.Join(library, "skills", "docker")),
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
		"apm install --root " + project.Root,
		"apm compile --root " + project.Root,
	})
	requireContains(t, stdout.String(), "ok: synced 1 skills, 0 plugins, 1 mcp servers, 1 instructions, 1 prompts")
}

func TestSyncRemovesLegacyLibrarySymlinksBeforeAPMInstall(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "docker", Path: "docker/SKILL.md"}},
	})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: localDependencies(filepath.Join(library, "skills", "docker")),
		},
	})
	librarySkill := filepath.Join(library, "skills", "docker")
	groupDir := filepath.Join(project.AgentsSymlinkDir, "workflow")
	requireNoError(t, os.MkdirAll(project.SymlinkDir, 0o755))
	requireNoError(t, os.MkdirAll(groupDir, 0o755))
	requireNoError(t, os.Symlink(librarySkill, filepath.Join(project.SymlinkDir, "docker")))
	requireNoError(t, os.Symlink(librarySkill, filepath.Join(project.AgentsSymlinkDir, "docker")))
	requireNoError(t, os.Symlink(librarySkill, filepath.Join(groupDir, "docker")))
	foreignTarget := filepath.Join(project.Root, "foreign")
	requireNoError(t, os.MkdirAll(foreignTarget, 0o755))
	requireNoError(t, os.Symlink(foreignTarget, filepath.Join(project.AgentsSymlinkDir, "foreign")))

	calls := []string{}
	err := SyncProject(SyncOptions{
		Project:     project,
		LibraryPath: library,
		Runner:      recordingRunner(&calls, nil),
		Stdout:      ioDiscard(),
	})

	requireNoError(t, err)
	for _, link := range []string{
		filepath.Join(project.SymlinkDir, "docker"),
		filepath.Join(project.AgentsSymlinkDir, "docker"),
		filepath.Join(groupDir, "docker"),
	} {
		if _, lerr := os.Lstat(link); !os.IsNotExist(lerr) {
			t.Fatalf("legacy library symlink %s should be removed before apm install, lstat err = %v", link, lerr)
		}
	}
	if _, lerr := os.Lstat(filepath.Join(project.AgentsSymlinkDir, "foreign")); lerr != nil {
		t.Fatalf("user-owned symlink should be preserved: %v", lerr)
	}
	if _, serr := os.Stat(filepath.Join(librarySkill, "SKILL.md")); serr != nil {
		t.Fatalf("library skill content must be untouched: %v", serr)
	}
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --root " + project.Root,
		"apm compile --root " + project.Root,
	})
}

func TestSyncProjectRepairsCatalogMCPAndPreservesRegistryDependency(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{
		mcp: []CatalogEntry{{
			Type: LibraryTypeMCP, Name: "local", Transport: "http", URL: "https://example.test/mcp",
		}},
	})
	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			MCP: []MCPDependency{
				{Name: "local"},
				{
					Name: "io.example/public-server", Registry: "https://registry.example.test",
					Extra: map[string]any{
						"headers": map[string]any{"Authorization": "${TOKEN}"},
						"version": "2.4.1", "package": "@example/server",
						"tools": []any{"search", "fetch"}, "x-owner": "platform",
					},
				},
				{Name: "Local"},
			},
		},
	})

	calls := []string{}
	err := SyncProject(SyncOptions{
		Project:     project,
		LibraryPath: library,
		Runner:      recordingRunner(&calls, nil),
		Stdout:      ioDiscard(),
	})

	requireNoError(t, err)
	manifest, readErr := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, readErr)
	requireEqual(t, []MCPDependency{
		{Name: "local", Transport: "http", Registry: false, URL: "https://example.test/mcp"},
		{
			Name: "io.example/public-server", Registry: "https://registry.example.test",
			Extra: map[string]any{
				"headers": map[string]any{"Authorization": "${TOKEN}"},
				"version": "2.4.1", "package": "@example/server",
				"tools": []any{"search", "fetch"}, "x-owner": "platform",
			},
		},
		{Name: "Local"},
	}, manifest.Dependencies.MCP)
	assertCommands(t, calls, []string{
		"apm --version",
		"apm install --root " + project.Root,
		"apm compile --root " + project.Root,
	})
}

func TestSyncCommitsManifestChangesOnce(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{
		mcp: []CatalogEntry{{Type: LibraryTypeMCP, Name: "local", Transport: "stdio", Command: "new-command"}},
	})
	root := t.TempDir()
	requireNoError(t, os.MkdirAll(filepath.Join(root, ".codex"), 0o755))
	path := ProjectAPMPath(root)
	requireNoError(t, os.WriteFile(path, []byte(`x-user: {keep: true}
dependencies:
  lsp: [{name: gopls}]
  mcp:
    - {name: local, registry: true, command: old-command, x-owner: user}
    - io.example/opaque
`), 0o644))
	project := Project{Root: root, ManifestPath: path}
	metrics := &manifestIOMetrics{}

	requireNoError(t, SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: ioDiscard(), manifestMetrics: metrics}))
	requireEqual(t, 1, metrics.authoritativeLoads)
	requireEqual(t, 1, metrics.authoritativeParses)
	requireEqual(t, 1, metrics.rawDigestRereads)
	requireEqual(t, 1, metrics.atomicReplacements)
	requireEqual(t, []string{"authoritative-load", "authoritative-parse", "raw-digest-reread", "atomic-replacement"}, metrics.events)
	manifest, err := ReadAPMManifest(path)
	requireNoError(t, err)
	requireEqual(t, filepath.Base(root), manifest.Name)
	requireEqual(t, "0.1.0", manifest.Version)
	requireEqual(t, []string{"codex"}, manifest.Targets)
	requireEqual(t, "new-command", manifest.Dependencies.MCP[0].Command)
	data := readFile(t, path)
	for _, preserved := range []string{"x-user:", "lsp:", "x-owner: user", "io.example/opaque"} {
		requireContains(t, data, preserved)
	}
}

func TestSyncNoManifestChangePerformsNoWrite(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{})
	root := t.TempDir()
	path := ProjectAPMPath(root)
	original := "name: project\nversion: 1.0.0\ntargets: []\ndependencies: {apm: [], mcp: []}\n"
	requireNoError(t, os.WriteFile(path, []byte(original), 0o644))
	before, err := os.Stat(path)
	requireNoError(t, err)
	time.Sleep(10 * time.Millisecond)

	project := Project{Root: root, ManifestPath: path}
	metrics := &manifestIOMetrics{}
	requireNoError(t, SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: ioDiscard(), manifestMetrics: metrics}))
	requireEqual(t, 1, metrics.authoritativeLoads)
	requireEqual(t, 1, metrics.authoritativeParses)
	requireEqual(t, 0, metrics.rawDigestRereads)
	requireEqual(t, 0, metrics.atomicReplacements)
	requireEqual(t, []string{"authoritative-load", "authoritative-parse"}, metrics.events)
	after, err := os.Stat(path)
	requireNoError(t, err)
	requireEqual(t, original, readFile(t, path))
	requireEqual(t, before.ModTime(), after.ModTime())
}

func TestSyncAlreadyReconciledWithMissingIdentityWritesOneRepair(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{})
	root := t.TempDir()
	path := ProjectAPMPath(root)
	requireNoError(t, os.WriteFile(path, []byte("targets: []\ndependencies: {apm: [], mcp: []}\n"), 0o644))
	project := Project{Root: root, ManifestPath: path}
	metrics := &manifestIOMetrics{}
	assertSingleManifestRepairBy(t, path, metrics, func() error {
		return SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: ioDiscard(), manifestMetrics: metrics})
	})
	requireEqual(t, []string{"authoritative-load", "authoritative-parse", "raw-digest-reread", "atomic-replacement"}, metrics.events)
}

func TestSyncProjectRejectsMalformedMCPCatalogBeforeAPMInstall(t *testing.T) {
	t.Parallel()

	library := createCatalogLibrary(t, catalogLibrarySeed{})
	requireNoError(t, os.WriteFile(filepath.Join(library, "mcp", "catalog.csv"), []byte("wrong,header\n"), 0o644))
	project := createAPMProject(t, APMManifest{})

	calls := []string{}
	err := SyncProject(SyncOptions{
		Project:     project,
		LibraryPath: library,
		Runner:      recordingRunner(&calls, nil),
		Stdout:      ioDiscard(),
	})

	if err == nil {
		t.Fatal("SyncProject() error = nil, want malformed catalog error")
	}
	requireEqual(t, "error: malformed catalog: invalid header", err.Error())
	assertCommands(t, calls, []string{"apm --version"})
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
			APM: localDependencies(
				filepath.Join(library, "skills", "docker"),
				filepath.Join(library, "skills", "missing"),
			),
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
		plugins: []CatalogEntry{
			{Type: LibraryTypePlugin, Name: "shortcuts-playground/claude", Path: "shortcuts-playground/claude/.claude-plugin/plugin.json"},
			{Type: LibraryTypePlugin, Name: "available-plugin", Path: "available-plugin/plugin.json"},
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
			APM: localDependencies(
				filepath.Join(library, "skills", "cloud", "azure", "azure-cli"),
				filepath.Join(library, "skills", "missing", "skill"),
				filepath.Join(library, "plugins", "shortcuts-playground", "claude"),
				filepath.Join(library, "plugins", "missing-plugin"),
			),
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
	requireNotContains(t, got, "available in library: plugin shortcuts-playground/claude")
	requireContains(t, got, "removed from library: plugin missing-plugin")
	requireContains(t, got, "available in library: plugin available-plugin")
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

func TestProjectStatusDoesNotReportOpaqueAPMScalarsAsRemovedSkills(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{})
	root := t.TempDir()
	path := ProjectAPMPath(root)
	requireNoError(t, os.WriteFile(path, []byte(`name: project
version: 1.0.0
dependencies:
  apm:
    - owner/repository#main
    - !custom /library/skills/tagged
    - /library/skills/local
`), 0o644))
	project := Project{Root: root, ManifestPath: path}

	manifest, err := ReadAPMManifest(path)
	requireNoError(t, err)
	if len(manifest.Dependencies.APM) != 1 || manifest.Dependencies.APM[0].Local != "/library/skills/local" {
		t.Fatalf("typed APM projection = %#v, want only ordinary local path", manifest.Dependencies.APM)
	}
	var stdout bytes.Buffer
	requireNoError(t, ProjectStatus(StatusOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: &stdout}))
	requireContains(t, stdout.String(), "removed from library: skill local")
	requireNotContains(t, stdout.String(), "owner/repository")
	requireNotContains(t, stdout.String(), "tagged")
}

func createDriftFixture(t *testing.T) (library string, project Project, scriptPath string) {
	t.Helper()

	library = createCatalogLibrary(t, catalogLibrarySeed{
		skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "git-pushing", Path: "git-pushing/SKILL.md"}},
	})
	scriptPath = filepath.Join(library, "skills", "git-pushing", "scripts", "smart_commit.sh")
	requireNoError(t, os.MkdirAll(filepath.Dir(scriptPath), 0o755))
	requireNoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\necho push\n"), 0o644))
	project = createAPMProject(t, APMManifest{Dependencies: APMDependencies{
		APM: localDependencies(filepath.Join(library, "skills", "git-pushing")),
	}})
	return library, project, scriptPath
}

func writeDriftLock(t *testing.T, library string, project Project, includeScriptHash bool) {
	t.Helper()

	skillHash, err := fileSHA256(filepath.Join(library, "skills", "git-pushing", "SKILL.md"))
	requireNoError(t, err)
	scriptHash, err := fileSHA256(filepath.Join(library, "skills", "git-pushing", "scripts", "smart_commit.sh"))
	requireNoError(t, err)
	lock := fmt.Sprintf(`lockfile_version: '1'
apm_version: 0.28.0
dependencies:
- repo_url: _local/git-pushing
  name: git-pushing
  version: 0.0.0
  package_type: claude_skill
  source: local
  local_path: %s
  deployed_files:
  - .agents/skills/git-pushing
  - .agents/skills/git-pushing/SKILL.md
  deployed_file_hashes:
    .agents/skills/git-pushing/SKILL.md: sha256:%s
    .claude/skills/git-pushing/SKILL.md: sha256:%s
`, filepath.Join(library, "skills", "git-pushing"), skillHash, skillHash)
	if includeScriptHash {
		lock += fmt.Sprintf(`    .agents/skills/git-pushing/scripts/smart_commit.sh: sha256:%s
    .claude/skills/git-pushing/scripts/smart_commit.sh: sha256:%s
`, scriptHash, scriptHash)
	}
	lock += `- repo_url: github.com/example/remote-skill
  name: remote-skill
  version: 1.0.0
  package_type: claude_skill
  source: git
  deployed_file_hashes:
    .agents/skills/remote-skill/SKILL.md: sha256:0000000000000000000000000000000000000000000000000000000000000000
`
	requireNoError(t, os.WriteFile(filepath.Join(project.Root, "apm.lock.yaml"), []byte(lock), 0o644))
}

func runProjectStatus(t *testing.T, library string, project Project) string {
	t.Helper()

	var stdout bytes.Buffer
	requireNoError(t, ProjectStatus(StatusOptions{
		Project:     project,
		LibraryPath: library,
		Runner:      recordingRunner(nil, nil),
		Stdout:      &stdout,
	}))
	return stdout.String()
}

func TestProjectStatusReportsSkillSupportingFileDrift(t *testing.T) {
	t.Parallel()

	library, project, scriptPath := createDriftFixture(t)
	writeDriftLock(t, library, project, true)
	requireNoError(t, os.WriteFile(scriptPath, []byte("#!/bin/sh\necho changed\n"), 0o644))

	got := runProjectStatus(t, library, project)

	requireContains(t, got, "hash mismatch: skill git-pushing")
	if count := strings.Count(got, "hash mismatch: skill git-pushing"); count != 1 {
		t.Fatalf("expected exactly one drift line, got %d in output:\n%s", count, got)
	}
}

func TestProjectStatusReportsSkillDriftForDeletedLibraryFile(t *testing.T) {
	t.Parallel()

	library, project, scriptPath := createDriftFixture(t)
	writeDriftLock(t, library, project, true)
	requireNoError(t, os.Remove(scriptPath))

	got := runProjectStatus(t, library, project)

	requireContains(t, got, "hash mismatch: skill git-pushing")
}

func TestProjectStatusReportsSkillDriftForAddedLibraryFile(t *testing.T) {
	t.Parallel()

	library, project, _ := createDriftFixture(t)
	writeDriftLock(t, library, project, false)

	got := runProjectStatus(t, library, project)

	requireContains(t, got, "hash mismatch: skill git-pushing")
}

func TestProjectStatusNoSkillDriftWhenLockMatchesLibrary(t *testing.T) {
	t.Parallel()

	library, project, _ := createDriftFixture(t)
	writeDriftLock(t, library, project, true)

	got := runProjectStatus(t, library, project)

	requireNotContains(t, got, "hash mismatch: skill")
}

func TestProjectStatusSkillDriftToleratesMalformedOrLegacyLock(t *testing.T) {
	t.Parallel()

	for name, lock := range map[string]string{
		"empty":     "",
		"malformed": "dependencies: [oops",
		"legacy":    "instructions:\n  - name: python-rules\n    sha256: deadbeef\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			library, project, _ := createDriftFixture(t)
			requireNoError(t, os.WriteFile(filepath.Join(project.Root, "apm.lock.yaml"), []byte(lock), 0o644))

			got := runProjectStatus(t, library, project)

			requireNotContains(t, got, "hash mismatch: skill")
		})
	}
}

type catalogLibrarySeed struct {
	skills       []CatalogEntry
	plugins      []CatalogEntry
	mcp          []CatalogEntry
	instructions []CatalogEntry
	prompts      []CatalogEntry
}

func createCatalogLibrary(t *testing.T, seed catalogLibrarySeed) string {
	t.Helper()

	root := t.TempDir()
	writeCatalogFixtures(t, root, LibraryTypeSkill, seed.skills)
	writeCatalogFixtures(t, root, LibraryTypePlugin, seed.plugins)
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
	writeAPMManifestForTest(t, path, manifest)
	return Project{
		Root:             root,
		ManifestPath:     path,
		SymlinkDir:       filepath.Join(root, ".claude", "skills"),
		AgentsSymlinkDir: filepath.Join(root, ".agents", "skills"),
	}
}

func writeAPMManifestForTest(t *testing.T, path string, manifest APMManifest) {
	t.Helper()
	normalizeAPMManifest(&manifest)
	data, err := yaml.Marshal(manifest)
	requireNoError(t, err)
	requireNoError(t, os.WriteFile(path, data, 0o644))
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
			return []byte("apm 0.28.0\n"), nil
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
	case LibraryTypePlugin:
		return "plugins"
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
	case LibraryTypeSkill, LibraryTypePlugin, LibraryTypeInstruction, LibraryTypePrompt:
		return entry.Path
	default:
		return ""
	}
}

func catalogContent(entry CatalogEntry) string {
	switch entry.Type {
	case LibraryTypeSkill:
		return "# skill " + entry.Name + "\n"
	case LibraryTypePlugin:
		return "{\"name\":\"" + entry.Name + "\"}\n"
	case LibraryTypeInstruction:
		return "# instruction " + entry.Name + "\n"
	case LibraryTypePrompt:
		return "# prompt " + entry.Name + "\n"
	default:
		return ""
	}
}
