package instill

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const remoteSkillSHA = "0123456789abcdef0123456789abcdef01234567"
const refreshedRemoteSkillSHA = "fedcba9876543210fedcba9876543210fedcba98"

func TestAddRemoteSkillPinsVerifiedDefaultBranch(t *testing.T) {
	root := t.TempDir()
	calls := []string{}
	runner := func(name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		calls = append(calls, command)
		switch {
		case command == "git ls-remote --symref https://github.com/owner/example.git HEAD":
			return []byte("ref: refs/heads/main\tHEAD\n" + remoteSkillSHA + "\tHEAD\n"), nil
		case strings.HasPrefix(command, "git init "):
			return nil, nil
		case strings.Contains(command, " remote add origin https://github.com/owner/example.git"):
			return nil, nil
		case strings.Contains(command, "git -C ") && strings.HasSuffix(command, " fetch --depth 1 origin "+remoteSkillSHA):
			return nil, nil
		case strings.Contains(command, "git -C ") && strings.HasSuffix(command, " ls-tree "+remoteSkillSHA+" -- skills/example/SKILL.md"):
			return []byte("100644 blob abc\tskills/example/SKILL.md\n"), nil
		case strings.Contains(command, "git -C ") && strings.HasSuffix(command, " show "+remoteSkillSHA+":skills/example/SKILL.md"):
			return []byte("# example"), nil
		default:
			t.Fatalf("unexpected command: %s", command)
			return nil, nil
		}
	}

	err := AddRemoteSkill(root, "owner/example", runner)

	requireNoError(t, err)
	entries, err := LoadCatalog(root, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{{
		Type: LibraryTypeSkill, Name: "example", Path: "skills/example", Source: "git",
		Repository: "https://github.com/owner/example.git", Ref: remoteSkillSHA,
	}}, entries)
	requireEqual(t, 6, len(calls))
}

func TestAddRemoteSkillAcceptsGitSuffix(t *testing.T) {
	root := t.TempDir()

	err := AddRemoteSkill(root, "owner/example.git", remoteSkillRunner(t, remoteSkillSHA))

	requireNoError(t, err)
	entries, err := LoadCatalog(root, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, "example", entries[0].Name)
	requireEqual(t, "https://github.com/owner/example.git", entries[0].Repository)
}

func TestAddRemoteSkillFailureDoesNotMutateCatalog(t *testing.T) {
	root := t.TempDir()
	original := []CatalogEntry{{Type: LibraryTypeSkill, Name: "local", Path: "local/SKILL.md"}}
	requireNoError(t, WriteCatalog(root, LibraryTypeSkill, original))

	err := AddRemoteSkill(root, "owner/example", func(string, ...string) ([]byte, error) {
		return nil, errors.New("authentication failed")
	})

	if err == nil {
		t.Fatal("AddRemoteSkill() error = nil, want resolution failure")
	}
	entries, loadErr := LoadCatalog(root, LibraryTypeSkill)
	requireNoError(t, loadErr)
	requireEqual(t, original, entries)
}

func TestScanLibraryPreservesRemoteSkillWithoutLocalMarker(t *testing.T) {
	root := t.TempDir()
	remote := CatalogEntry{Type: LibraryTypeSkill, Name: "example", Path: "skills/example", Source: "git", Repository: "https://github.com/owner/example.git", Ref: remoteSkillSHA}
	requireNoError(t, WriteCatalog(root, LibraryTypeSkill, []CatalogEntry{remote}))

	var stdout bytes.Buffer
	requireNoError(t, ScanLibrary(root, &stdout))
	entries, err := LoadCatalog(root, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, []CatalogEntry{remote}, entries)
	requireEqual(t, "", stdout.String())
}

func TestRemoteSkillCatalogRejectsNonImmutableRef(t *testing.T) {
	err := WriteCatalog(t.TempDir(), LibraryTypeSkill, []CatalogEntry{{
		Type: LibraryTypeSkill, Name: "example", Path: "skills/example", Source: "git",
		Repository: "https://github.com/owner/example.git", Ref: "main",
	}})
	if err == nil {
		t.Fatal("WriteCatalog() error = nil, want immutable SHA validation failure")
	}
}

func TestPickAddsAndRemovesRemoteSkillWithoutChangingLocalDependencies(t *testing.T) {
	library := t.TempDir()
	remote := CatalogEntry{Type: LibraryTypeSkill, Name: "example", Path: "skills/example", Source: "git", Repository: "https://github.com/owner/example.git", Ref: remoteSkillSHA}
	requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{remote, {Type: LibraryTypeSkill, Name: "local", Path: "local/SKILL.md"}}))
	project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Local: filepath.Join(library, "skills", "local")}}}})

	requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeSkill, Add: []string{"example"}, Runner: recordingRunner(nil, nil)}))
	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, []APMDependency{
		{Git: &GitDependency{Repository: "https://github.com/owner/example.git", Path: "skills/example", Ref: remoteSkillSHA}},
		{Local: filepath.Join(library, "skills", "local")},
	}, manifest.Dependencies.APM)

	requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeSkill, Remove: []string{"example"}, Runner: recordingRunner(nil, nil)}))
	manifest, err = ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, []APMDependency{{Local: filepath.Join(library, "skills", "local")}}, manifest.Dependencies.APM)
}

func TestAPMManifestRoundTripPreservesUnknownAPMDependencyObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apm.yml")
	requireNoError(t, os.WriteFile(path, []byte("dependencies:\n  apm:\n    - /library/skills/local\n    - git: https://github.com/owner/example.git\n      path: skills/example\n      ref: "+remoteSkillSHA+"\n      x-owner: platform\n"), 0o644))

	manifest, err := ReadAPMManifest(path)
	requireNoError(t, err)
	requireNoError(t, WriteAPMManifestAtomic(path, manifest))
	data := readFile(t, path)
	requireContains(t, data, "x-owner: platform")
}

func TestPickPluginOperationsPreserveRemoteSkillDependency(t *testing.T) {
	library := t.TempDir()
	remote := CatalogEntry{Type: LibraryTypeSkill, Name: "example", Path: "skills/example", Source: "git", Repository: "https://github.com/owner/example.git", Ref: remoteSkillSHA}
	plugin := CatalogEntry{Type: LibraryTypePlugin, Name: "plugin", Path: "plugin/plugin.json"}
	requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{remote}))
	requireNoError(t, WriteCatalog(library, LibraryTypePlugin, []CatalogEntry{plugin}))
	remoteDependency := APMDependency{Git: &GitDependency{Repository: remote.Repository, Path: remote.Path, Ref: remote.Ref}}
	project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{remoteDependency}}})

	requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypePlugin, Add: []string{"plugin"}, Runner: recordingRunner(nil, nil)}))
	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, []APMDependency{remoteDependency, {Local: filepath.Join(library, "plugins", "plugin")}}, manifest.Dependencies.APM)

	requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypePlugin, Remove: []string{"plugin"}, Runner: recordingRunner(nil, nil)}))
	manifest, err = ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, []APMDependency{remoteDependency}, manifest.Dependencies.APM)
}

func TestPickExplicitlyRefreshesRemoteSkillManifestPin(t *testing.T) {
	library := t.TempDir()
	remote := CatalogEntry{Type: LibraryTypeSkill, Name: "example", Path: "skills/example", Source: "git", Repository: "https://github.com/owner/example.git", Ref: refreshedRemoteSkillSHA}
	requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{remote}))
	project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Git: &GitDependency{Repository: remote.Repository, Path: remote.Path, Ref: remoteSkillSHA}}}}})

	requireNoError(t, Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeSkill, Add: []string{"example"}, Runner: recordingRunner(nil, nil)}))
	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, refreshedRemoteSkillSHA, manifest.Dependencies.APM[0].Git.Ref)
}

func TestSyncDoesNotRefreshRemoteSkillManifestPin(t *testing.T) {
	library := t.TempDir()
	remote := CatalogEntry{Type: LibraryTypeSkill, Name: "example", Path: "skills/example", Source: "git", Repository: "https://github.com/owner/example.git", Ref: refreshedRemoteSkillSHA}
	requireNoError(t, WriteCatalog(library, LibraryTypeSkill, []CatalogEntry{remote}))
	project := createAPMProject(t, APMManifest{Dependencies: APMDependencies{APM: []APMDependency{{Git: &GitDependency{Repository: remote.Repository, Path: remote.Path, Ref: remoteSkillSHA}}}}})

	requireNoError(t, SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: recordingRunner(nil, nil), Stdout: &bytes.Buffer{}}))
	manifest, err := ReadAPMManifest(project.ManifestPath)
	requireNoError(t, err)
	requireEqual(t, remoteSkillSHA, manifest.Dependencies.APM[0].Git.Ref)
}

func TestUpdateRemoteSkillPreservesUserMaintainedMetadata(t *testing.T) {
	root := t.TempDir()
	entry := CatalogEntry{Type: LibraryTypeSkill, Name: "example", Category: "custom", Description: "curated description", Path: "skills/example", Source: "git", Repository: "https://github.com/owner/example.git", Ref: remoteSkillSHA}
	requireNoError(t, WriteCatalog(root, LibraryTypeSkill, []CatalogEntry{entry}))

	requireNoError(t, UpdateRemoteSkill(root, "example", remoteSkillRunner(t, refreshedRemoteSkillSHA)))
	entries, err := LoadCatalog(root, LibraryTypeSkill)
	requireNoError(t, err)
	requireEqual(t, "custom", entries[0].Category)
	requireEqual(t, "curated description", entries[0].Description)
	requireEqual(t, refreshedRemoteSkillSHA, entries[0].Ref)
}

func remoteSkillRunner(t *testing.T, sha string) CommandRunner {
	t.Helper()
	return func(name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		switch {
		case command == "git ls-remote --symref https://github.com/owner/example.git HEAD":
			return []byte("ref: refs/heads/main\tHEAD\n" + sha + "\tHEAD\n"), nil
		case strings.HasPrefix(command, "git init "):
			return nil, nil
		case strings.Contains(command, " remote add origin https://github.com/owner/example.git"):
			return nil, nil
		case strings.Contains(command, "git -C ") && strings.HasSuffix(command, " fetch --depth 1 origin "+sha):
			return nil, nil
		case strings.Contains(command, "git -C ") && strings.HasSuffix(command, " ls-tree "+sha+" -- skills/example/SKILL.md"):
			return []byte("100644 blob abc\tskills/example/SKILL.md\n"), nil
		case strings.Contains(command, "git -C ") && strings.HasSuffix(command, " show "+sha+":skills/example/SKILL.md"):
			return []byte("# example"), nil
		default:
			t.Fatalf("unexpected command: %s", command)
			return nil, nil
		}
	}
}
