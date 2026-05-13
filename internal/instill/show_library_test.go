package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestShowLibraryOutsideProjectPrintsPlainSortedList(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "golang-cli", "docker")
	if err := os.Mkdir(filepath.Join(library, "missing-skill-md"), 0o755); err != nil {
		t.Fatalf("Mkdir(missing-skill-md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(library, "top-level-file"), []byte("nope"), 0o600); err != nil {
		t.Fatalf("WriteFile(top-level-file) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(library, "bad-skill-md", "SKILL.md"), 0o755); err != nil {
		t.Fatalf("MkdirAll(bad-skill-md/SKILL.md) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := ShowLibrary(ShowLibraryOptions{
		LibraryPath: library,
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("ShowLibrary() error = %v", err)
	}

	want := "docker\ngolang-cli\n2 skills\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestShowLibraryInsideProjectAnnotatesSelectedSkills(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli")
	manifest := Manifest{Skills: []string{"golang-cli"}}

	var stdout bytes.Buffer
	if err := ShowLibrary(ShowLibraryOptions{
		LibraryPath: library,
		Manifest:    &manifest,
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("ShowLibrary() error = %v", err)
	}

	output := stdout.String()
	want := "[ ] docker\n[✓] golang-cli\n2 skills  (1 selected)\n"
	if output != want {
		t.Fatalf("stdout = %q, want %q", output, want)
	}
}

func TestShowLibraryFilterCountsOnlyVisibleSelectedSkills(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli", "golang-testing")
	manifest := Manifest{Skills: []string{"docker", "golang-testing"}}

	var stdout bytes.Buffer
	if err := ShowLibrary(ShowLibraryOptions{
		LibraryPath: library,
		Manifest:    &manifest,
		Filter:      "golang",
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("ShowLibrary() error = %v", err)
	}

	want := "[ ] golang-cli\n[✓] golang-testing\n2 skills  (1 selected)\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestShowLibraryFilterIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli", "golang-testing")

	var stdout bytes.Buffer
	if err := ShowLibrary(ShowLibraryOptions{
		LibraryPath: library,
		Filter:      "CLI",
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("ShowLibrary() error = %v", err)
	}

	if stdout.String() != "golang-cli\n1 skills\n" {
		t.Fatalf("stdout = %q, want filtered cli skill", stdout.String())
	}
}

func TestShowLibraryCategoryFiltersByPrefix(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "azure-blob-storage", "docker", "golang-cli")
	writeCategories(t, library, `{
		"cloud/azure": ["azure-blob-storage"],
		"cloud": ["docker"],
		"golang": ["golang-cli"]
	}`)

	var stdout bytes.Buffer
	if err := ShowLibrary(ShowLibraryOptions{
		LibraryPath: library,
		Category:    "cloud",
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("ShowLibrary() error = %v", err)
	}

	if stdout.String() != "azure-blob-storage\ndocker\n2 skills\n" {
		t.Fatalf("stdout = %q, want cloud skills", stdout.String())
	}
}

func TestShowLibraryCategoryMissingRegistryIsUnfiltered(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli")

	var stdout bytes.Buffer
	if err := ShowLibrary(ShowLibraryOptions{
		LibraryPath: library,
		Category:    "cloud",
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("ShowLibrary() error = %v", err)
	}

	if stdout.String() != "docker\ngolang-cli\n2 skills\n" {
		t.Fatalf("stdout = %q, want unfiltered skills", stdout.String())
	}
}

func TestShowLibraryEmptyLibrary(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	if err := ShowLibrary(ShowLibraryOptions{
		LibraryPath: t.TempDir(),
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("ShowLibrary() error = %v", err)
	}

	if stdout.String() != "0 skills\n" {
		t.Fatalf("stdout = %q, want empty footer", stdout.String())
	}
}
