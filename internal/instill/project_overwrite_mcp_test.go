package instill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectOverwriteMCPEntriesUsesRootFirstAndFreshPackageOrder(t *testing.T) {
	projectRoot := t.TempDir()
	library := t.TempDir()
	pluginSource := filepath.Join(library, "plugins", "plugin")
	skillSource := filepath.Join(library, "skills", "skill")
	pluginRoot := filepath.Join(projectRoot, "apm_modules", "_local", "plugin")
	skillRoot := filepath.Join(projectRoot, "apm_modules", "_local", "skill")
	writeOverwriteManifestForTest(t, pluginRoot, `dependencies:
  mcp:
    - name: collision
      transport: stdio
      command: plugin-collision
    - name: plugin-only
      transport: stdio
      command: '${CLAUDE_PLUGIN_ROOT}/bin/server'
      args: ['${OTHER_ENV}', '${CLAUDE_PLUGIN_ROOT}/arg']
      env:
        PATH: '${CLAUDE_PLUGIN_ROOT}/bin'
    - name: package-collision
      transport: stdio
      command: plugin-second
`)
	writeOverwriteManifestForTest(t, skillRoot, `dependencies:
  mcp:
    - name: collision
      transport: stdio
      command: skill-server
    - name: package-collision
      transport: stdio
      command: skill-first
`)

	defaultDisabled := false
	manifest := APMManifest{Dependencies: APMDependencies{
		// Manifest order deliberately differs from fresh-lock order.
		APM: []APMDependency{{Local: pluginSource}, {Local: skillSource}},
		MCP: []MCPDependency{{Name: "collision"}},
	}}
	entries, err := collectOverwriteMCPEntries(projectRoot, library, manifest,
		[]CatalogEntry{{Type: LibraryTypeSkill, Name: "skill", Path: "skill/SKILL.md"}},
		[]CatalogEntry{{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin/.claude-plugin/plugin.json"}},
		[]CatalogEntry{{Type: LibraryTypeMCP, Name: "collision", Transport: "stdio", Command: "root", DefaultEnabled: &defaultDisabled}},
		[]apmLockDependency{
			{Name: "skill", Source: "local", LocalPath: skillSource},
			{Name: "plugin", Source: "local", LocalPath: pluginSource},
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].Name != "collision" || entries[1].Name != "package-collision" || entries[2].Name != "plugin-only" {
		t.Fatalf("MCP entries = %#v, want root collision then package-collision and plugin-only", entries)
	}
	if entries[0].Command != "root" || entries[0].DefaultEnabled == nil || *entries[0].DefaultEnabled {
		t.Fatalf("root MCP entry lost explicit definition/default: %#v", entries[0])
	}
	if entries[1].Command != "skill-first" || entries[1].DefaultEnabled != nil {
		t.Fatalf("first package definition = %#v, want fresh-lock-order winner without default", entries[1])
	}
	if entries[2].Command != filepath.Join(pluginRoot, "bin", "server") || entries[2].DefaultEnabled != nil {
		t.Fatalf("plugin MCP entry = %#v, want installed-root expansion and no default", entries[2])
	}
	if len(entries[2].Args) != 2 || entries[2].Args[0] != "${OTHER_ENV}" || entries[2].Args[1] != filepath.Join(pluginRoot, "arg") {
		t.Fatalf("plugin args = %#v, want only literal plugin-root expansion", entries[2].Args)
	}
	if len(entries[2].Env) != 1 || entries[2].Env[0] != "PATH="+filepath.Join(pluginRoot, "bin") {
		t.Fatalf("plugin env = %#v", entries[2].Env)
	}
}

func TestCollectOverwriteMCPEntriesAllowsSkillWithoutPackageManifest(t *testing.T) {
	projectRoot := t.TempDir()
	library := t.TempDir()
	source := filepath.Join(library, "skills", "skill")
	installed := filepath.Join(projectRoot, "apm_modules", "_local", "skill")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := collectOverwriteMCPEntries(projectRoot, library,
		APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Local: source}}}},
		[]CatalogEntry{{Type: LibraryTypeSkill, Name: "skill", Path: "skill/SKILL.md"}}, nil, nil,
		[]apmLockDependency{{Name: "skill", Source: "local", LocalPath: source}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want none", entries)
	}
}

func TestCollectOverwriteMCPEntriesRequiresPluginManifest(t *testing.T) {
	projectRoot := t.TempDir()
	library := t.TempDir()
	source := filepath.Join(library, "plugins", "plugin")
	installed := filepath.Join(projectRoot, "apm_modules", "_local", "plugin")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := collectOverwriteMCPEntries(projectRoot, library,
		APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Local: source}}}},
		nil, []CatalogEntry{{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin/.claude-plugin/plugin.json"}}, nil,
		[]apmLockDependency{{Name: "plugin", Source: "local", LocalPath: source}})
	if err == nil || !strings.Contains(ErrorMessage(err), "requires installed manifest") {
		t.Fatalf("error = %v, want contextual missing Plugin manifest", err)
	}
}

func TestCollectOverwriteMCPEntriesRejectsMalformedPluginManifest(t *testing.T) {
	projectRoot := t.TempDir()
	library := t.TempDir()
	source := filepath.Join(library, "plugins", "plugin")
	installed := filepath.Join(projectRoot, "apm_modules", "_local", "plugin")
	writeOverwriteManifestForTest(t, installed, "dependencies: [\n")
	_, err := collectOverwriteMCPEntries(projectRoot, library,
		APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Local: source}}}},
		nil, []CatalogEntry{{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin/.claude-plugin/plugin.json"}}, nil,
		[]apmLockDependency{{Name: "plugin", Source: "local", LocalPath: source}})
	if err == nil || !strings.Contains(ErrorMessage(err), "cannot read selected plugin") {
		t.Fatalf("error = %v, want contextual malformed Plugin manifest failure", err)
	}
}

func TestCollectOverwriteMCPEntriesKeepsGenericPackageTransport(t *testing.T) {
	projectRoot := t.TempDir()
	library := t.TempDir()
	source := filepath.Join(library, "plugins", "plugin")
	installed := filepath.Join(projectRoot, "apm_modules", "_local", "plugin")
	writeOverwriteManifestForTest(t, installed, `dependencies:
  mcp:
    - name: generic
      transport: websocket
      url: wss://example.test
`)
	entries, err := collectOverwriteMCPEntries(projectRoot, library,
		APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Local: source}}}},
		nil, []CatalogEntry{{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin/.claude-plugin/plugin.json"}}, nil,
		[]apmLockDependency{{Name: "plugin", Source: "local", LocalPath: source}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "generic" || entries[0].Transport != "websocket" || entries[0].URL != "wss://example.test" {
		t.Fatalf("entries = %#v, want generic websocket definition preserved", entries)
	}
}

func TestValidateOverwritePiMCPEntryRejectsUnrenderableTransport(t *testing.T) {
	err := validateOverwritePiMCPEntry(CatalogEntry{Type: LibraryTypeMCP, Name: "generic", Transport: "websocket", URL: "wss://example.test"})
	if err == nil || !strings.Contains(err.Error(), `cannot represent transport "websocket" in Pi`) {
		t.Fatalf("error = %v, want Pi transport rejection", err)
	}
}

func TestCollectOverwriteMCPEntriesRejectsSelectedPackageMissingFromFreshLock(t *testing.T) {
	projectRoot := t.TempDir()
	library := t.TempDir()
	source := filepath.Join(library, "skills", "skill")
	_, err := collectOverwriteMCPEntries(projectRoot, library,
		APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Local: source}}}},
		[]CatalogEntry{{Type: LibraryTypeSkill, Name: "skill", Path: "skill/SKILL.md"}}, nil, nil, nil)
	if err == nil || !strings.Contains(ErrorMessage(err), "absent from the fresh APM lock") {
		t.Fatalf("error = %v, want missing fresh-lock dependency failure", err)
	}
}

func TestCollectOverwriteMCPEntriesUsesCanonicalGitMaterialization(t *testing.T) {
	projectRoot := t.TempDir()
	library := t.TempDir()
	dependency := APMDependency{Git: &GitDependency{Repository: "https://github.com/acme/plugin.git", Path: "packages/plugin", Ref: "v1.2.3"}}
	materialized := filepath.Join(projectRoot, "apm_modules", "acme", "plugin", "packages", "plugin")
	writeOverwriteManifestForTest(t, materialized, `dependencies:
  mcp:
    - name: from-git
      transport: stdio
      command: git-server
`)
	manifest := APMManifest{Dependencies: APMDependencies{APM: []APMDependency{dependency}}}
	entry := CatalogEntry{Type: LibraryTypeSkill, Name: "remote-skill", Source: "git", Repository: dependency.Git.Repository, Path: dependency.Git.Path, Ref: dependency.Git.Ref}
	entries, err := collectOverwriteMCPEntries(projectRoot, library, manifest, []CatalogEntry{entry}, nil, nil,
		[]apmLockDependency{{Name: "remote-skill", RepoURL: "acme/plugin", Host: "github.com", VirtualPath: "packages/plugin", IsVirtual: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "from-git" || entries[0].Command != "git-server" {
		t.Fatalf("entries = %#v, want MCP from canonical Git materialization", entries)
	}
}

func TestDependencyMatchesSelectedMarketplacePluginIdentity(t *testing.T) {
	library := t.TempDir()
	entry := CatalogEntry{
		Type: LibraryTypePlugin, Name: "market/a",
		Path: "marketplaces/market/plugins/a/.claude-plugin/plugin.json",
	}
	dependency := pluginDependencyFromCatalog(library, entry)
	if !dependencyMatchesSelectedPackage(library, dependency, entry) {
		t.Fatal("marketplace Plugin dependency did not match its catalog identity")
	}
	wrong := dependency
	wrong.Local = filepath.Join(library, "plugins", "marketplaces", "other", "plugins", "a")
	if dependencyMatchesSelectedPackage(library, wrong, entry) {
		t.Fatal("different marketplace source matched the selected Plugin")
	}
}

func TestCollectOverwriteMCPEntriesRejectsStringPackageMCPDeclaration(t *testing.T) {
	projectRoot := t.TempDir()
	library := t.TempDir()
	source := filepath.Join(library, "plugins", "plugin")
	installed := filepath.Join(projectRoot, "apm_modules", "_local", "plugin")
	writeOverwriteManifestForTest(t, installed, "dependencies:\n  mcp:\n    - io.example/server\n")
	_, err := collectOverwriteMCPEntries(projectRoot, library,
		APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Local: source}}}},
		nil, []CatalogEntry{{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin/.claude-plugin/plugin.json"}}, nil,
		[]apmLockDependency{{Name: "plugin", Source: "local", LocalPath: source}})
	if err == nil || !strings.Contains(err.Error(), "unsupported dependencies.mcp declaration") {
		t.Fatalf("error = %v, want unresolved string MCP declaration error", err)
	}
}

func TestCollectOverwriteMCPEntriesRejectsPiUnrepresentablePackageOptions(t *testing.T) {
	t.Setenv("PI_MCP_CONFIG_MODE", "")
	projectRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectRoot, ".pi"), 0700); err != nil {
		t.Fatal(err)
	}
	library := t.TempDir()
	source := filepath.Join(library, "plugins", "plugin")
	installed := filepath.Join(projectRoot, "apm_modules", "_local", "plugin")
	writeOverwriteManifestForTest(t, installed, "dependencies:\n  mcp:\n    - name: restricted\n      transport: stdio\n      command: server\n      tools: [read_only]\n")
	_, err := collectOverwriteMCPEntries(projectRoot, library,
		APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Local: source}}}},
		nil, []CatalogEntry{{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin/.claude-plugin/plugin.json"}}, nil,
		[]apmLockDependency{{Name: "plugin", Source: "local", LocalPath: source}})
	if err == nil || !strings.Contains(err.Error(), `package option "tools" is not representable`) {
		t.Fatalf("error = %v, want Pi package option rejection", err)
	}
}

func TestNormalizedGitRepositoryMatchesAPMLockRepresentations(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/acme/plugin.git", "github.com/acme/plugin"},
		{"http://github.com/acme/plugin", "github.com/acme/plugin"},
		{"ssh://git@github.com/acme/plugin.git", "github.com/acme/plugin"},
		{"git@github.com:acme/plugin.git", "github.com/acme/plugin"},
		{"github.com/acme/plugin", "github.com/acme/plugin"},
	}
	for _, test := range tests {
		if got := normalizedGitRepository(test.input); got != test.want {
			t.Errorf("normalizedGitRepository(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestCollectOverwriteMCPEntriesRejectsRegistryPackageMCP(t *testing.T) {
	projectRoot := t.TempDir()
	library := t.TempDir()
	source := filepath.Join(library, "plugins", "plugin")
	installed := filepath.Join(projectRoot, "apm_modules", "_local", "plugin")
	writeOverwriteManifestForTest(t, installed, "dependencies:\n  mcp:\n    - name: registry-server\n      registry: true\n")
	_, err := collectOverwriteMCPEntries(projectRoot, library,
		APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Local: source}}}},
		nil, []CatalogEntry{{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin/.claude-plugin/plugin.json"}}, nil,
		[]apmLockDependency{{Name: "plugin", Source: "local", LocalPath: source}})
	if err == nil || !strings.Contains(err.Error(), "registry-backed package MCP definitions") {
		t.Fatalf("error = %v, want unresolved registry MCP failure", err)
	}
}

func TestGitLockMatchesAPMRowWithoutVirtualPath(t *testing.T) {
	dependency := APMDependency{Git: &GitDependency{
		Repository: "https://github.com/example/remote-skill",
		Path:       "skills/remote-skill",
	}}
	lockDependency := apmLockDependency{
		Name:    "remote-skill",
		RepoURL: "example/remote-skill",
		Host:    "github.com",
	}
	if !gitLockMatches(dependency, lockDependency) {
		t.Fatal("legacy APM Git lock row without virtual_path did not match the selected remote Skill")
	}
}

func TestGitLockMatchesWholeRepoRowWithoutVirtualPath(t *testing.T) {
	dependency := APMDependency{Git: &GitDependency{Repository: "https://github.com/example/whole-repo.git"}}
	lockDependency := apmLockDependency{
		Name:    "whole-repo",
		RepoURL: "example/whole-repo",
		Host:    "github.com",
	}
	if !gitLockMatches(dependency, lockDependency) {
		t.Fatal("whole-repository Git dependency did not match APM row without virtual_path")
	}
}

func TestGitLockMatchesHostSeparatedRepositoryIdentity(t *testing.T) {
	dependency := APMDependency{Git: &GitDependency{Repository: "https://gitlab.example:7999/group/repo.git"}}
	lockDependency := apmLockDependency{
		Name: "repo", RepoURL: "group/repo", Host: "gitlab.example", Port: 7999, Source: "git",
	}
	if !gitLockMatches(dependency, lockDependency) {
		t.Fatal("host-separated APM Git lock identity did not match the selected repository")
	}
}

func writeOverwriteManifestForTest(t *testing.T, root, contents string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, apmManifestFileName), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
