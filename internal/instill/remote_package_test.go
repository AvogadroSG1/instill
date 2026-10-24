package instill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPickRefreshesRemotePackageWithoutDuplicateDependency(t *testing.T) {
	library := t.TempDir()
	entry := CatalogEntry{Type: LibraryTypeSkill, Name: "repo", Path: "skills/repo", Source: "git", Repository: "https://github.com/owner/repo.git", Ref: otherRefreshedRemoteSHA}
	requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{entry}))
	project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Git: &GitDependency{Repository: entry.Repository, Path: entry.Path, Ref: otherRemoteSHA}}}}})
	requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeSkill, Add: []string{entry.Name}, Runner: recordingRunner(nil, nil)}))
	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, 1, len(manifest.Dependencies.APM))
	requireEqual(t, otherRefreshedRemoteSHA, manifest.Dependencies.APM[0].Git.Ref)
}

func TestTypedGitDependencyOwnershipIsPreserved(t *testing.T) {
	for _, removeType := range []LibraryType{LibraryTypeSkill, LibraryTypePlugin} {
		t.Run(string(removeType), func(t *testing.T) {
			library := t.TempDir()
			skill := CatalogEntry{Type: LibraryTypeSkill, Name: "skill-repo", Path: "skills/skill-repo", Source: "git", Repository: "https://github.com/owner/skill-repo.git", Ref: otherRemoteSHA}
			plugin := CatalogEntry{Type: LibraryTypePlugin, Name: "mkt/plugin", Category: "mkt", Path: "marketplaces/mkt/plugin/.claude-plugin/plugin.json", Source: "marketplace", Repository: "https://github.com/owner/repo.git", Ref: otherRemoteSHA}
			requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{skill}))
			requireNoError(t, WriteCatalog(library, LibraryTypePlugin, []CatalogEntry{plugin}))
			requireNoError(t, os.MkdirAll(filepath.Join(library, "plugins", "marketplaces", "mkt", "plugin", ".claude-plugin"), 0o755))
			requireNoError(t, os.WriteFile(filepath.Join(library, "plugins", "marketplaces", "mkt", "plugin", ".claude-plugin", "plugin.json"), []byte(`{"name":"plugin"}`), 0o644))
			unknown := APMDependency{Git: &GitDependency{Repository: "https://github.com/other/unknown.git", Path: "package", Ref: otherRemoteSHA}}
			skillDependency := APMDependency{Git: &GitDependency{Repository: skill.Repository, Path: skill.Path, Ref: skill.Ref}}
			pluginDependency := APMDependency{Local: filepath.Join(library, "plugins", "marketplaces", "mkt", "plugin")}
			project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{skillDependency, pluginDependency, unknown}}})
			name := skill.Name
			want := []APMDependency{pluginDependency, unknown}
			if removeType == LibraryTypePlugin {
				name = plugin.Name
				want = []APMDependency{skillDependency, unknown}
			}

			requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: removeType, Remove: []string{name}, Runner: recordingRunner(nil, nil)}))
			manifest, err := ReadAPMManifest(project.ManifestPath)
			requireNoError(t, err)
			requireEqual(t, want, manifest.Dependencies.APM)
		})
	}
}

func TestTypedLocalDependencyOwnershipIsPreserved(t *testing.T) {
	for _, removeType := range []LibraryType{LibraryTypeSkill, LibraryTypePlugin} {
		t.Run(string(removeType), func(t *testing.T) {
			library := t.TempDir()
			skill := CatalogEntry{Type: LibraryTypeSkill, Name: "shared-name", Path: "shared-name/SKILL.md"}
			plugin := CatalogEntry{Type: LibraryTypePlugin, Name: "shared-name", Path: "shared-name/.claude-plugin/plugin.json"}
			requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{skill}))
			requireNoError(t, WriteCatalog(library, LibraryTypePlugin, []CatalogEntry{plugin}))
			skillDependency := APMDependency{Local: filepath.Join(library, "skills", "shared-name")}
			pluginDependency := APMDependency{Local: filepath.Join(library, "plugins", "shared-name")}
			unknown := APMDependency{Local: filepath.Join(t.TempDir(), "shared-name")}
			project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{skillDependency, pluginDependency, unknown}}})
			want := []APMDependency{pluginDependency, unknown}
			if removeType == LibraryTypePlugin {
				want = []APMDependency{skillDependency, unknown}
			}

			requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: removeType, Remove: []string{"shared-name"}, Runner: recordingRunner(nil, nil)}))
			manifest, err := ReadAPMManifest(project.ManifestPath)
			requireNoError(t, err)
			requireEqual(t, want, manifest.Dependencies.APM)
		})
	}
}
