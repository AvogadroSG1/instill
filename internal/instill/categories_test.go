package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCategoriesReadsRegistry(t *testing.T) {
	t.Parallel()

	library := t.TempDir()
	writeCategories(t, library, `{
		"golang": ["golang-testing", "golang-cli", "golang-cli"],
		"cloud/azure": ["azure-blob-storage"]
	}`)

	categories := LoadCategories(library)

	if got := strings.Join(categories["golang"], ","); got != "golang-cli,golang-testing" {
		t.Fatalf("golang category = %q, want normalized skills", got)
	}
	if got := CategoryForSkill(categories, "azure-blob-storage"); got != "cloud/azure" {
		t.Fatalf("CategoryForSkill() = %q, want cloud/azure", got)
	}
}

func TestLoadCategoriesMissingFileReturnsEmptyAndWarns(t *testing.T) {
	var stderr bytes.Buffer
	categories := LoadCategoriesWithWarnings(t.TempDir(), &stderr)
	if len(categories) != 0 {
		t.Fatalf("len(categories) = %d, want empty", len(categories))
	}

	if !strings.Contains(stderr.String(), "warning: category registry not found") {
		t.Fatalf("stderr = %q, want missing registry warning", stderr.String())
	}
}

func TestLoadCategoriesMalformedFileReturnsEmptyAndWarns(t *testing.T) {
	library := t.TempDir()
	writeCategories(t, library, `{nope`)

	var stderr bytes.Buffer
	categories := LoadCategoriesWithWarnings(library, &stderr)
	if len(categories) != 0 {
		t.Fatalf("len(categories) = %d, want empty", len(categories))
	}

	if !strings.Contains(stderr.String(), "warning: cannot parse category registry") {
		t.Fatalf("stderr = %q, want malformed registry warning", stderr.String())
	}
}

func TestLoadCategoriesRejectsMalformedRegistryShapes(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"null registry":       `null`,
		"null category array": `{"golang": null}`,
		"trimmed duplicate":   `{"golang": ["golang-cli"], " golang ": ["golang-testing"]}`,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			library := t.TempDir()
			writeCategories(t, library, contents)

			var stderr bytes.Buffer
			categories := LoadCategoriesWithWarnings(library, &stderr)
			if len(categories) != 0 {
				t.Fatalf("len(categories) = %d, want empty", len(categories))
			}
			if !strings.Contains(stderr.String(), "warning: cannot parse category registry") {
				t.Fatalf("stderr = %q, want malformed registry warning", stderr.String())
			}
		})
	}
}

func TestCategoryForSkillReturnsAlphabeticalCategoryWhenDuplicated(t *testing.T) {
	t.Parallel()

	categories := map[string][]string{
		"golang/testing": {"golang-cli"},
		"golang":         {"golang-cli"},
	}

	if got := CategoryForSkill(categories, "golang-cli"); got != "golang" {
		t.Fatalf("CategoryForSkill() = %q, want deterministic first category", got)
	}
}

func writeCategories(t *testing.T, library string, contents string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(library, categoriesFileName), []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", categoriesFileName, err)
	}
}
