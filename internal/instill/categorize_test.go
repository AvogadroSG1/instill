package instill

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCategorizeLibraryClassifiesKnownPrefixes(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "azure-blob-storage", "dd-trace", "docker", "golang-cli", "k8s-deploy")

	var stdout bytes.Buffer
	if err := CategorizeLibrary(CategorizeOptions{
		LibraryPath: library,
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("CategorizeLibrary() error = %v", err)
	}

	categories := readCategoriesFile(t, library)
	assertCategorySkills(t, categories, "cloud", "docker", "k8s-deploy")
	assertCategorySkills(t, categories, "cloud/azure", "azure-blob-storage")
	assertCategorySkills(t, categories, "datadog", "dd-trace")
	assertCategorySkills(t, categories, "golang", "golang-cli")
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want no uncategorized skills", stdout.String())
	}
}

func TestCategorizeLibraryPreservesExistingAssignmentsAndReportsUnknown(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli", "mystery")
	writeCategories(t, library, `{"custom": ["docker"]}`)

	var stdout bytes.Buffer
	if err := CategorizeLibrary(CategorizeOptions{
		LibraryPath: library,
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("CategorizeLibrary() error = %v", err)
	}

	categories := readCategoriesFile(t, library)
	assertCategorySkills(t, categories, "custom", "docker")
	assertCategorySkills(t, categories, "golang", "golang-cli")
	if _, ok := categories["cloud"]; ok {
		t.Fatalf("cloud category created for already-assigned docker: %#v", categories)
	}
	if !strings.Contains(stdout.String(), "uncategorized: mystery\n") {
		t.Fatalf("stdout = %q, want uncategorized mystery", stdout.String())
	}
}

func TestCategorizeLibraryIsIdempotent(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli")

	if err := CategorizeLibrary(CategorizeOptions{LibraryPath: library, Stdout: ioDiscard()}); err != nil {
		t.Fatalf("CategorizeLibrary(first) error = %v", err)
	}
	first, err := os.ReadFile(filepath.Join(library, categoriesFileName))
	if err != nil {
		t.Fatalf("ReadFile(first) error = %v", err)
	}

	if err := CategorizeLibrary(CategorizeOptions{LibraryPath: library, Stdout: ioDiscard()}); err != nil {
		t.Fatalf("CategorizeLibrary(second) error = %v", err)
	}
	second, err := os.ReadFile(filepath.Join(library, categoriesFileName))
	if err != nil {
		t.Fatalf("ReadFile(second) error = %v", err)
	}
	if string(second) != string(first) {
		t.Fatalf("second registry = %q, want unchanged %q", string(second), string(first))
	}
}

func TestCategorizeLibraryRejectsMalformedExistingRegistry(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	writeCategories(t, library, `{`)

	err := CategorizeLibrary(CategorizeOptions{
		LibraryPath: library,
		Stdout:      ioDiscard(),
	})
	if err == nil {
		t.Fatal("CategorizeLibrary() error = nil, want malformed registry error")
	}
	if ExitCode(err) != ExitGeneral {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitGeneral)
	}
	contents, readErr := os.ReadFile(filepath.Join(library, categoriesFileName))
	if readErr != nil {
		t.Fatalf("ReadFile(%s) error = %v", categoriesFileName, readErr)
	}
	if string(contents) != `{` {
		t.Fatalf("registry = %q, want malformed contents preserved", string(contents))
	}
}

func readCategoriesFile(t *testing.T, library string) map[string][]string {
	t.Helper()

	contents, err := os.ReadFile(filepath.Join(library, categoriesFileName))
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", categoriesFileName, err)
	}
	var categories map[string][]string
	if err := json.Unmarshal(contents, &categories); err != nil {
		t.Fatalf("Unmarshal(categories) error = %v; contents = %q", err, string(contents))
	}
	return categories
}

func assertCategorySkills(t *testing.T, categories map[string][]string, category string, want ...string) {
	t.Helper()

	if got := strings.Join(categories[category], ","); got != strings.Join(want, ",") {
		t.Fatalf("categories[%q] = %q, want %q", category, got, strings.Join(want, ","))
	}
}
