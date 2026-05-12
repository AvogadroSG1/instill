package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPickSkillsAddsDeduplicatedSortedSkills(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli")
	project := createProject(t, []string{"docker"})

	var stdout bytes.Buffer
	if err := PickSkills(PickSkillsOptions{
		Project:     project,
		LibraryPath: library,
		Add:         []string{"golang-cli", "docker"},
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("PickSkills() error = %v", err)
	}

	manifest, err := ReadManifest(project.ManifestPath)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if strings.Join(manifest.Skills, ",") != "docker,golang-cli" {
		t.Fatalf("manifest skills = %#v, want sorted docker,golang-cli", manifest.Skills)
	}
	if !strings.Contains(stdout.String(), "added:   golang-cli") ||
		!strings.Contains(stdout.String(), "manifest: 2 skills") {
		t.Fatalf("stdout = %q, want added and manifest lines", stdout.String())
	}
}

func TestPickSkillsRemoveDeletesManifestEntryAndSymlink(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli")
	project := createProject(t, []string{"docker", "golang-cli"})
	if err := os.Symlink(filepath.Join(library, "docker"), filepath.Join(project.SymlinkDir, "docker")); err != nil {
		t.Fatalf("Symlink(docker) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := PickSkills(PickSkillsOptions{
		Project:     project,
		LibraryPath: library,
		Remove:      []string{"docker"},
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("PickSkills() error = %v", err)
	}

	manifest, err := ReadManifest(project.ManifestPath)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if strings.Join(manifest.Skills, ",") != "golang-cli" {
		t.Fatalf("manifest skills = %#v, want golang-cli", manifest.Skills)
	}
	if _, err := os.Lstat(filepath.Join(project.SymlinkDir, "docker")); !os.IsNotExist(err) {
		t.Fatalf("docker symlink remains; err = %v", err)
	}
	if !strings.Contains(stdout.String(), "removed: docker") {
		t.Fatalf("stdout = %q, want removed line", stdout.String())
	}
}

func TestPickSkillsUnknownAddMakesNoChanges(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker"})

	var stdout bytes.Buffer
	err := PickSkills(PickSkillsOptions{
		Project:     project,
		LibraryPath: library,
		Add:         []string{"missing"},
		Stdout:      &stdout,
	})
	if err == nil {
		t.Fatal("PickSkills() error = nil, want unknown skill")
	}

	manifest, readErr := ReadManifest(project.ManifestPath)
	if readErr != nil {
		t.Fatalf("ReadManifest() error = %v", readErr)
	}
	if strings.Join(manifest.Skills, ",") != "docker" {
		t.Fatalf("manifest skills = %#v, want unchanged docker", manifest.Skills)
	}
}

func TestPickSkillsUnknownRemoveMakesNoChanges(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker"})
	if err := os.Symlink(filepath.Join(library, "docker"), filepath.Join(project.SymlinkDir, "docker")); err != nil {
		t.Fatalf("Symlink(docker) error = %v", err)
	}

	var stdout bytes.Buffer
	err := PickSkills(PickSkillsOptions{
		Project:     project,
		LibraryPath: library,
		Remove:      []string{"docker", "missing"},
		Stdout:      &stdout,
	})
	if err == nil {
		t.Fatal("PickSkills() error = nil, want unknown remove skill")
	}

	manifest, readErr := ReadManifest(project.ManifestPath)
	if readErr != nil {
		t.Fatalf("ReadManifest() error = %v", readErr)
	}
	if strings.Join(manifest.Skills, ",") != "docker" {
		t.Fatalf("manifest skills = %#v, want unchanged docker", manifest.Skills)
	}
	if _, statErr := os.Lstat(filepath.Join(project.SymlinkDir, "docker")); statErr != nil {
		t.Fatalf("docker symlink changed after failed validation; err = %v", statErr)
	}
}

func TestPickSkillsMixedUnknownRemoveBlocksAdd(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli")
	project := createProject(t, []string{"docker"})

	var stdout bytes.Buffer
	err := PickSkills(PickSkillsOptions{
		Project:     project,
		LibraryPath: library,
		Add:         []string{"golang-cli"},
		Remove:      []string{"missing"},
		Stdout:      &stdout,
	})
	if err == nil {
		t.Fatal("PickSkills() error = nil, want unknown remove skill")
	}

	manifest, readErr := ReadManifest(project.ManifestPath)
	if readErr != nil {
		t.Fatalf("ReadManifest() error = %v", readErr)
	}
	if strings.Join(manifest.Skills, ",") != "docker" {
		t.Fatalf("manifest skills = %#v, want unchanged docker", manifest.Skills)
	}
}

func TestPickSkillsNoArgsExitsOne(t *testing.T) {
	t.Parallel()

	err := PickSkills(PickSkillsOptions{
		Project:     createProject(t, []string{}),
		LibraryPath: createLibrary(t, "docker"),
		Stdout:      ioDiscard(),
	})
	if err == nil {
		t.Fatal("PickSkills() error = nil, want no skills error")
	}
	if ExitCode(err) != ExitGeneral {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitGeneral)
	}
}

func ioDiscard() *bytes.Buffer {
	return &bytes.Buffer{}
}
