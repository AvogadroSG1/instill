package instill

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestPickAddsPinnedRemotePluginDependency(t *testing.T) {
	library := t.TempDir()
	plugin := remotePluginCatalogEntry(remotePluginSHA)
	requireNoError(t, WriteCatalog(library, LibraryTypePlugin, []CatalogEntry{plugin}))
	project := createAPMProject(t, APMManifest{})

	requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypePlugin, Add: []string{"plugin"}, Runner: recordingRunner(nil, nil)}))
	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, []APMDependency{{Git: &GitDependency{Repository: plugin.Repository, Path: plugin.Path, Ref: plugin.Ref}}}, manifest.Dependencies.APM)
}

func TestPickRefreshesRemotePackageWithoutDuplicateDependency(t *testing.T) {
	for _, typ := range []LibraryType{LibraryTypeSkill, LibraryTypePlugin} {
		t.Run(string(typ), func(t *testing.T) {
			library := t.TempDir()
			entry := remotePluginCatalogEntry(refreshedRemotePluginSHA)
			if typ == LibraryTypeSkill {
				entry = CatalogEntry{Type: typ, Name: "repo", Path: "skills/repo", Source: "git", Repository: "https://github.com/owner/repo.git", Ref: refreshedRemotePluginSHA}
			}
			requireNoError(t, WriteCatalog(library, typ, []CatalogEntry{entry}))
			project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Git: &GitDependency{Repository: entry.Repository, Path: entry.Path, Ref: remotePluginSHA}}}}})
			requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: typ, Add: []string{entry.Name}, Runner: recordingRunner(nil, nil)}))
			manifest, err := ReadAPMManifest(project.ManifestPath)
			requireNoError(t, err)
			requireEqual(t, 1, len(manifest.Dependencies.APM))
			requireEqual(t, refreshedRemotePluginSHA, manifest.Dependencies.APM[0].Git.Ref)
		})
	}
}

func TestTypedGitDependencyOwnershipIsPreserved(t *testing.T) {
	for _, removeType := range []LibraryType{LibraryTypeSkill, LibraryTypePlugin} {
		t.Run(string(removeType), func(t *testing.T) {
			library := t.TempDir()
			skill := CatalogEntry{Type: LibraryTypeSkill, Name: "skill-repo", Path: "skills/skill-repo", Source: "git", Repository: "https://github.com/owner/skill-repo.git", Ref: remotePluginSHA}
			plugin := remotePluginCatalogEntry(remotePluginSHA)
			requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{skill}))
			requireNoError(t, WriteCatalog(library, LibraryTypePlugin, []CatalogEntry{plugin}))
			unknown := APMDependency{Git: &GitDependency{Repository: "https://github.com/other/unknown.git", Path: "package", Ref: remotePluginSHA}}
			skillDependency := APMDependency{Git: &GitDependency{Repository: skill.Repository, Path: skill.Path, Ref: skill.Ref}}
			pluginDependency := APMDependency{Git: &GitDependency{Repository: plugin.Repository, Path: plugin.Path, Ref: plugin.Ref}}
			project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{skillDependency, pluginDependency, unknown}}})
			name := skill.Name
			want := []APMDependency{unknown, pluginDependency}
			if removeType == LibraryTypePlugin {
				name = plugin.Name
				want = []APMDependency{unknown, skillDependency}
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

func TestRemoteRegistrationRejectsCrossCatalogIdentityCollisionWithoutMutation(t *testing.T) {
	root := t.TempDir()
	skill := CatalogEntry{Type: LibraryTypeSkill, Name: "repo", Path: "skills/repo", Source: "git", Repository: "https://github.com/owner/repo.git", Ref: remotePluginSHA}
	requireNoError(t, WriteCatalog(root, LibraryTypeSkill, []CatalogEntry{skill}))
	err := AddRemotePlugin(root, "owner/repo", "", remotePluginRunner(t, remotePluginSHA, `{"plugins":[{"name":"plugin","source":"skills/repo"}]}`, `{"name":"plugin"}`))
	if err == nil {
		t.Fatal("AddRemotePlugin() error = nil, want cross-catalog collision")
	}
	assertPathMissing(t, filepath.Join(root, "plugins", "catalog.csv"))
}

func TestAmbiguousTypedGitIdentityFailsBeforeProjectMutation(t *testing.T) {
	library := t.TempDir()
	skill := CatalogEntry{Type: LibraryTypeSkill, Name: "repo", Path: "skills/repo", Source: "git", Repository: "https://github.com/owner/repo.git", Ref: remotePluginSHA}
	plugin := CatalogEntry{Type: LibraryTypePlugin, Name: "plugin", Path: "skills/repo", Source: "git", Repository: "https://github.com/owner/repo.git", Ref: refreshedRemotePluginSHA}
	requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{skill}))
	requireNoError(t, WriteCatalog(library, LibraryTypePlugin, []CatalogEntry{plugin}))
	project := createAPMProject(t, APMManifest{})
	original := readFile(t, project.ManifestPath)

	err := Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeSkill, Add: []string{"repo"}, Runner: recordingRunner(nil, nil)})
	if err == nil || !strings.Contains(ErrorMessage(err), "ambiguous") {
		t.Fatalf("Pick() error = %v, want ambiguity", err)
	}
	requireEqual(t, original, readFile(t, project.ManifestPath))
	if _, err := loadPickTypeStates(project, library); err == nil {
		t.Fatal("loadPickTypeStates() error = nil, want ambiguity")
	}
	if err := ProjectStatus(StatusOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: &bytes.Buffer{}}); err == nil {
		t.Fatal("ProjectStatus() error = nil, want ambiguity")
	}
}

func TestRemotePluginPickerAndStatusUseStableIdentity(t *testing.T) {
	library := t.TempDir()
	plugin := remotePluginCatalogEntry(refreshedRemotePluginSHA)
	requireNoError(t, WriteCatalog(library, LibraryTypePlugin, []CatalogEntry{plugin}))
	project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Git: &GitDependency{Repository: plugin.Repository, Path: plugin.Path, Ref: remotePluginSHA}}}}})
	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	selected, err := currentProjectTypeSelection(project, library, LibraryTypePlugin, manifest, []CatalogEntry{plugin})
	requireNoError(t, err)
	requireEqual(t, []string{"plugin"}, selected)
	var stdout bytes.Buffer
	requireNoError(t, ProjectStatus(StatusOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: &stdout}))
	requireContains(t, stdout.String(), "update available: plugin plugin")
}

func TestSyncDoesNotRefreshRemotePluginManifestPin(t *testing.T) {
	library := t.TempDir()
	plugin := remotePluginCatalogEntry(refreshedRemotePluginSHA)
	requireNoError(t, WriteCatalog(library, LibraryTypePlugin, []CatalogEntry{plugin}))
	project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Git: &GitDependency{Repository: plugin.Repository, Path: plugin.Path, Ref: remotePluginSHA}}}}})
	var stdout bytes.Buffer
	requireNoError(t, SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: &stdout}))
	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, remotePluginSHA, manifest.Dependencies.APM[0].Git.Ref)
	requireContains(t, stdout.String(), "ok: synced 0 skills, 1 plugins")
}

func remotePluginCatalogEntry(ref string) CatalogEntry {
	return CatalogEntry{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin", Source: "git", Repository: "https://github.com/owner/repo.git", Ref: ref}
}
