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

func TestAddLocalCatalogEntrySkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	library := filepath.Join(home, "library")
	requireNoError(t, os.MkdirAll(filepath.Join(library, "skills"), 0o755))

	externalDefault := filepath.Join(home, "repos", "go-skills")
	requireNoError(t, os.MkdirAll(externalDefault, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(externalDefault, "SKILL.md"), []byte("# go skills\n"), 0o644))

	externalCustom := filepath.Join(home, "repos", "python-skills")
	requireNoError(t, os.MkdirAll(externalCustom, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(externalCustom, "SKILL.md"), []byte("# python skills\n"), 0o644))

	// Default name derived from directory basename + ~/ contraction
	requireNoError(t, AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypeSkill,
	}, externalDefault))

	// Custom hierarchical name derives category
	requireNoError(t, AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type:        LibraryTypeSkill,
		Name:        "coding/python-skills",
		Description: "External Python skills",
	}, externalCustom))

	entries, err := LoadCatalog(library, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{
		{
			Type:        LibraryTypeSkill,
			Name:        "coding/python-skills",
			Category:    "coding",
			Path:        "~/repos/python-skills/SKILL.md",
			Source:      localExternalSource,
			Description: "External Python skills",
		},
		{
			Type:   LibraryTypeSkill,
			Name:   "go-skills",
			Path:   "~/repos/go-skills/SKILL.md",
			Source: localExternalSource,
		},
	}, entries)

	// Reject duplicate name
	otherDir := filepath.Join(home, "repos", "other-go")
	requireNoError(t, os.MkdirAll(otherDir, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(otherDir, "SKILL.md"), []byte("# other\n"), 0o644))
	err = AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypeSkill,
		Name: "go-skills",
	}, otherDir)
	if err == nil || !strings.Contains(err.Error(), "skill already exists: go-skills") {
		t.Fatalf("duplicate name error = %v, want skill already exists", err)
	}

	// Reject duplicate target path under different name
	err = AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypeSkill,
		Name: "coding/go-alias",
	}, externalDefault)
	if err == nil || !strings.Contains(err.Error(), "local skill already exists: go-skills") {
		t.Fatalf("duplicate path error = %v, want local skill already exists", err)
	}

	// Reject path inside library
	insideDir := filepath.Join(library, "skills", "in-lib")
	requireNoError(t, os.MkdirAll(insideDir, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(insideDir, "SKILL.md"), []byte("# in lib\n"), 0o644))
	err = AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypeSkill,
	}, insideDir)
	if err == nil || !strings.Contains(err.Error(), "inside the library") {
		t.Fatalf("inside-library error = %v, want inside the library rejection", err)
	}

	// Reject missing SKILL.md
	emptyDir := filepath.Join(home, "repos", "no-skill")
	requireNoError(t, os.MkdirAll(emptyDir, 0o755))
	err = AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypeSkill,
	}, emptyDir)
	if err == nil || !strings.Contains(err.Error(), "missing SKILL.md") {
		t.Fatalf("missing SKILL.md error = %v, want missing SKILL.md", err)
	}

	// Reject unsupported type
	err = AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypeMCP,
	}, externalDefault)
	if err == nil || !strings.Contains(err.Error(), "only supported for skills and plugins") {
		t.Fatalf("unsupported type error = %v, want skills and plugins error", err)
	}
}

func TestAddLocalCatalogEntryPluginLayouts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	library := filepath.Join(home, "library")
	requireNoError(t, os.MkdirAll(filepath.Join(library, "plugins"), 0o755))

	// 1. .claude-plugin/plugin.json with description loaded from metadata
	claudeDir := filepath.Join(home, "ext", "claude-ext")
	requireNoError(t, os.MkdirAll(filepath.Join(claudeDir, ".claude-plugin"), 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(claudeDir, ".claude-plugin", "plugin.json"), []byte(`{"name":"claude-ext","description":"Claude external plugin"}`), 0o644))
	requireNoError(t, AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypePlugin,
	}, claudeDir))

	// 2. .codex-plugin/plugin.json with explicit description preserved
	codexDir := filepath.Join(home, "ext", "codex-ext")
	requireNoError(t, os.MkdirAll(filepath.Join(codexDir, ".codex-plugin"), 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(codexDir, ".codex-plugin", "plugin.json"), []byte(`{"name":"codex-ext","description":"From JSON"}`), 0o644))
	requireNoError(t, AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type:        LibraryTypePlugin,
		Name:        "tools/codex-ext",
		Description: "Explicit override",
	}, codexDir))

	// 3. Flat plugin.json
	flatDir := filepath.Join(home, "ext", "flat-ext")
	requireNoError(t, os.MkdirAll(flatDir, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(flatDir, "plugin.json"), []byte(`{"name":"flat-ext","description":"Flat plugin"}`), 0o644))
	requireNoError(t, AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypePlugin,
	}, flatDir))

	entries, err := LoadCatalog(library, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{
		{
			Type:        LibraryTypePlugin,
			Name:        "claude-ext",
			Path:        "~/ext/claude-ext/.claude-plugin/plugin.json",
			Source:      localExternalSource,
			Description: "Claude external plugin",
		},
		{
			Type:        LibraryTypePlugin,
			Name:        "flat-ext",
			Path:        "~/ext/flat-ext/plugin.json",
			Source:      localExternalSource,
			Description: "Flat plugin",
		},
		{
			Type:        LibraryTypePlugin,
			Name:        "tools/codex-ext",
			Category:    "tools",
			Path:        "~/ext/codex-ext/.codex-plugin/plugin.json",
			Source:      localExternalSource,
			Description: "Explicit override",
		},
	}, entries)

	// Verify pluginDependencyPath resolves to the plugin root for all three layouts
	requireEqual(t, claudeDir, pluginDependencyPath(library, entries[0]))
	requireEqual(t, flatDir, pluginDependencyPath(library, entries[1]))
	requireEqual(t, codexDir, pluginDependencyPath(library, entries[2]))
}

func TestScanLibraryPreservesAndPrunesExternalLocalEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	library := filepath.Join(home, "library")
	requireNoError(t, os.MkdirAll(filepath.Join(library, "skills"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(library, "plugins"), 0o755))

	extSkill := filepath.Join(home, "ext-skill")
	requireNoError(t, os.MkdirAll(extSkill, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(extSkill, "SKILL.md"), []byte("# ext skill\n"), 0o644))

	extPlugin := filepath.Join(home, "ext-plugin")
	requireNoError(t, os.MkdirAll(filepath.Join(extPlugin, ".claude-plugin"), 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(extPlugin, ".claude-plugin", "plugin.json"), []byte(`{"name":"ext-plugin"}`), 0o644))

	requireNoError(t, AddLocalCatalogEntry(context.Background(), library, CatalogEntry{Type: LibraryTypeSkill}, extSkill))
	requireNoError(t, AddLocalCatalogEntry(context.Background(), library, CatalogEntry{Type: LibraryTypePlugin}, extPlugin))

	var stdout bytes.Buffer
	requireNoError(t, ScanLibrary(library, &stdout))
	requireEqual(t, "", stdout.String())

	skills, err := LoadCatalog(library, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, 1, len(skills))
	requireEqual(t, "ext-skill", skills[0].Name)

	plugins, err := LoadCatalog(library, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, 1, len(plugins))
	requireEqual(t, "ext-plugin", plugins[0].Name)

	// Remove external skill content and rescan
	requireNoError(t, os.RemoveAll(extSkill))
	stdout.Reset()
	requireNoError(t, ScanLibrary(library, &stdout))
	requireContains(t, stdout.String(), "removed: ext-skill (content not found)")

	skills, err = LoadCatalog(library, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, 0, len(skills))

	// Plugin still exists and is preserved
	plugins, err = LoadCatalog(library, LibraryTypePlugin)
	requireNoError(t, err)
	requireEqual(t, 1, len(plugins))
}

func TestExternalLocalSkillPickStatusAndDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	library := filepath.Join(home, "library")
	requireNoError(t, os.MkdirAll(filepath.Join(library, "skills"), 0o755))

	extSkill := filepath.Join(home, "repos", "go-skills")
	requireNoError(t, os.MkdirAll(extSkill, 0o755))
	skillMD := filepath.Join(extSkill, "SKILL.md")
	requireNoError(t, os.WriteFile(skillMD, []byte("# go skills v1\n"), 0o644))

	unrelatedSameBasename := filepath.Join(home, "other", "go-skills")
	requireNoError(t, os.MkdirAll(unrelatedSameBasename, 0o755))

	requireNoError(t, AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypeSkill,
		Name: "coding/go-skills",
	}, extSkill))

	project := createAPMProject(t, APMManifest{
		Dependencies: APMDependencies{
			APM: []APMDependency{{Local: unrelatedSameBasename}},
		},
	})

	// Pick external local skill
	requireNoError(t, Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeSkill,
		Add:         []string{"coding/go-skills"},
		Runner:      recordingRunner(nil, nil),
	}))

	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, []APMDependency{
		{Local: unrelatedSameBasename},
		{Local: extSkill},
	}, manifest.Dependencies.APM)

	// Write lockfile with initial hash
	initialHash, err := fileSHA256(skillMD)
	requireNoError(t, err)
	lock := fmt.Sprintf(`lockfile_version: '1'
apm_version: 0.28.0
dependencies:
- repo_url: _local/go-skills
  name: go-skills
  version: 0.0.0
  package_type: claude_skill
  source: local
  local_path: %s
  deployed_files:
  - .agents/skills/go-skills
  - .agents/skills/go-skills/SKILL.md
  deployed_file_hashes:
    .agents/skills/go-skills/SKILL.md: sha256:%s
`, extSkill, initialHash)
	requireNoError(t, os.WriteFile(filepath.Join(project.Root, "apm.lock.yaml"), []byte(lock), 0o644))

	// Status before modification: no drift, no available/removed for coding/go-skills
	got := runProjectStatus(t, library, project)
	requireNotContains(t, got, "hash mismatch: skill coding/go-skills")
	requireNotContains(t, got, "available in library: skill coding/go-skills")
	requireNotContains(t, got, "removed from library: skill coding/go-skills")

	// Modify external skill SKILL.md -> status reports hash mismatch with catalog name
	requireNoError(t, os.WriteFile(skillMD, []byte("# go skills v2\n"), 0o644))
	got = runProjectStatus(t, library, project)
	requireContains(t, got, "hash mismatch: skill coding/go-skills")

	// Pick --remove removes only extSkill and leaves unrelatedSameBasename intact
	requireNoError(t, Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeSkill,
		Remove:      []string{"coding/go-skills"},
		Runner:      recordingRunner(nil, nil),
	}))
	manifest, err = ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, []APMDependency{{Local: unrelatedSameBasename}}, manifest.Dependencies.APM)
}

func TestValidateCatalogEntryLocalExternalRejectsMalformed(t *testing.T) {
	cases := []struct {
		name  string
		entry CatalogEntry
	}{
		{
			name: "relative skill path",
			entry: CatalogEntry{
				Type:   LibraryTypeSkill,
				Name:   "ext",
				Path:   "ext/SKILL.md",
				Source: localExternalSource,
			},
		},
		{
			name: "traversal skill path",
			entry: CatalogEntry{
				Type:   LibraryTypeSkill,
				Name:   "ext",
				Path:   "~/../etc/SKILL.md",
				Source: localExternalSource,
			},
		},
		{
			name: "wrong skill marker",
			entry: CatalogEntry{
				Type:   LibraryTypeSkill,
				Name:   "ext",
				Path:   "~/ext/README.md",
				Source: localExternalSource,
			},
		},
		{
			name: "skill with repository",
			entry: CatalogEntry{
				Type:       LibraryTypeSkill,
				Name:       "ext",
				Path:       "~/ext/SKILL.md",
				Source:     localExternalSource,
				Repository: "https://github.com/owner/repo.git",
			},
		},
		{
			name: "plugin with marketplace format",
			entry: CatalogEntry{
				Type:              LibraryTypePlugin,
				Name:              "ext",
				Path:              "~/ext/plugin.json",
				Source:            localExternalSource,
				MarketplaceFormat: PluginMarketplaceFormatClaude,
			},
		},
		{
			name: "wrong plugin marker",
			entry: CatalogEntry{
				Type:   LibraryTypePlugin,
				Name:   "ext",
				Path:   "~/ext/SKILL.md",
				Source: localExternalSource,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateCatalogEntry(tc.entry); err == nil {
				t.Fatalf("validateCatalogEntry(%+v) = nil, want error", tc.entry)
			}
		})
	}
}

func TestExternalLocalSkillAndPluginStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	library := filepath.Join(home, "library")
	requireNoError(t, os.MkdirAll(filepath.Join(library, "skills"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(library, "plugins"), 0o755))

	extSkill := filepath.Join(home, "repos", "go-skills")
	requireNoError(t, os.MkdirAll(extSkill, 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(extSkill, "SKILL.md"), []byte("# go skills\n"), 0o644))

	extPlugin := filepath.Join(home, "repos", "my-plugin")
	requireNoError(t, os.MkdirAll(filepath.Join(extPlugin, ".claude-plugin"), 0o755))
	requireNoError(t, os.WriteFile(filepath.Join(extPlugin, ".claude-plugin", "plugin.json"), []byte(`{"name":"my-plugin"}`), 0o644))

	requireNoError(t, AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypeSkill,
		Name: "coding/go-skills",
	}, extSkill))
	requireNoError(t, AddLocalCatalogEntry(context.Background(), library, CatalogEntry{
		Type: LibraryTypePlugin,
		Name: "tools/my-plugin",
	}, extPlugin))

	project := createAPMProject(t, APMManifest{})
	requireNoError(t, Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypeSkill,
		Add:         []string{"coding/go-skills"},
		Runner:      recordingRunner(nil, nil),
	}))
	requireNoError(t, Pick(PickOptions{
		Project:     project,
		LibraryPath: library,
		Type:        LibraryTypePlugin,
		Add:         []string{"tools/my-plugin"},
		Runner:      recordingRunner(nil, nil),
	}))

	got := runProjectStatus(t, library, project)
	requireNotContains(t, got, "removed from library:")
	requireNotContains(t, got, "available in library:")
}
