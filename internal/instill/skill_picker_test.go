package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSkillPickerPrechecksTogglesFiltersAndConfirms(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel([]string{"docker", "golang-cli", "golang-testing"}, []string{"golang-cli"}, nil, nil)
	if !model.selected["golang-cli"] {
		t.Fatal("golang-cli not preselected")
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gt")})
	model = updated.(skillPickerModel)
	if got := strings.Join(model.visibleSkills(), ","); got != "golang-testing" {
		t.Fatalf("visible skills = %q, want golang-testing", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = updated.(skillPickerModel)
	if !model.selected["golang-testing"] {
		t.Fatal("golang-testing not selected after toggle")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(skillPickerModel)
	if !model.confirmed {
		t.Fatal("model not confirmed after enter")
	}
}

func TestSkillPickerScaffoldShowsTwoPanesAndSwitchesFocus(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel([]string{"docker"}, []string{}, nil, nil)
	view := model.View()
	if !strings.Contains(view, "Categories") ||
		!strings.Contains(view, "All") ||
		!strings.Contains(view, "Skills") ||
		!strings.Contains(view, "> [ ] docker") {
		t.Fatalf("view = %q, want category scaffold and focused skill row", view)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(skillPickerModel)
	if model.focusedPane != skillPickerCategoriesPane {
		t.Fatalf("focusedPane = %v, want categories pane", model.focusedPane)
	}
	model.toggleCurrent()
	if model.selected["docker"] {
		t.Fatal("docker selected while category pane focused")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(skillPickerModel)
	if model.focusedPane != skillPickerSkillsPane {
		t.Fatalf("focusedPane = %v, want skills pane", model.focusedPane)
	}
	model.toggleCurrent()
	if !model.selected["docker"] {
		t.Fatal("docker not selected after returning to skills pane")
	}
}

func TestSkillPickerShowsTopLevelCategoriesAlphabetically(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel(
		[]string{"azure-blob-storage", "docker", "golang-cli"},
		[]string{},
		[]string{"cloud", "golang"},
		nil,
	)

	view := model.View()
	allIndex := strings.Index(view, "  All")
	cloudIndex := strings.Index(view, "  cloud")
	golangIndex := strings.Index(view, "  golang")
	if allIndex == -1 || cloudIndex == -1 || golangIndex == -1 || allIndex >= cloudIndex || cloudIndex >= golangIndex {
		t.Fatalf("view = %q, want All then alphabetic categories", view)
	}
	if got := strings.Join(model.visibleSkills(), ","); got != "azure-blob-storage,docker,golang-cli" {
		t.Fatalf("visible skills = %q, want unfiltered flat list", got)
	}
}

func TestSkillPickerCategoryNavigationFiltersSkillsAndResetsCursor(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel(
		[]string{"azure-blob-storage", "docker", "golang-cli"},
		[]string{},
		[]string{"cloud", "golang"},
		map[string][]string{
			"cloud/azure": {"azure-blob-storage"},
			"cloud":       {"docker"},
			"golang":      {"golang-cli"},
		},
	)
	model.skillCursor = 2

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(skillPickerModel)

	if model.categoryCursor != 1 {
		t.Fatalf("categoryCursor = %d, want cloud category", model.categoryCursor)
	}
	if model.skillCursor != 0 {
		t.Fatalf("skillCursor = %d, want reset to 0", model.skillCursor)
	}
	if got := strings.Join(model.visibleSkills(), ","); got != "azure-blob-storage,docker" {
		t.Fatalf("visible skills = %q, want selected category skills", got)
	}
}

func TestSkillPickerAllCategoryShowsAllSkillsBeforeFiltering(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel(
		[]string{"azure-blob-storage", "docker", "golang-cli"},
		[]string{},
		[]string{"cloud", "golang"},
		map[string][]string{
			"cloud/azure": {"azure-blob-storage"},
			"cloud":       {"docker"},
			"golang":      {"golang-cli"},
		},
	)

	if model.selectedCategory() != "All" {
		t.Fatalf("selectedCategory() = %q, want All", model.selectedCategory())
	}
	if got := strings.Join(model.visibleSkills(), ","); got != "azure-blob-storage,docker,golang-cli" {
		t.Fatalf("visible skills = %q, want all skills", got)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(skillPickerModel)
	if got := strings.Join(model.visibleSkills(), ","); got != "azure-blob-storage,docker" {
		t.Fatalf("visible skills = %q, want cloud and cloud subcategory skills", got)
	}
}

func TestSkillPickerDrillsDownAndNavigatesBack(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel(
		[]string{"azure-blob-storage", "azure-functions", "docker", "golang-cli"},
		[]string{},
		[]string{"cloud", "golang"},
		map[string][]string{
			"cloud/azure":  {"azure-blob-storage"},
			"cloud/server": {"azure-functions"},
			"cloud":        {"docker"},
			"golang":       {"golang-cli"},
		},
	)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(skillPickerModel)

	if got := strings.Join(model.categoryPath, "/"); got != "cloud" {
		t.Fatalf("categoryPath = %q, want cloud", got)
	}
	if got := strings.Join(model.categories, ","); got != "azure,server" {
		t.Fatalf("categories = %q, want cloud children", got)
	}
	if got := model.categoryBreadcrumb(); got != "cloud" {
		t.Fatalf("breadcrumb = %q, want cloud", got)
	}
	if got := strings.Join(model.visibleSkills(), ","); got != "azure-blob-storage" {
		t.Fatalf("visible skills = %q, want azure skills", got)
	}
	view := model.View()
	if !strings.Contains(view, "cloud\n") {
		t.Fatalf("view = %q, want cloud breadcrumb", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(skillPickerModel)
	if got := strings.Join(model.categoryPath, "/"); got != "" {
		t.Fatalf("categoryPath = %q, want top level", got)
	}
	if got := strings.Join(model.categories, ","); got != "All,cloud,golang" {
		t.Fatalf("categories = %q, want top-level categories", got)
	}
	if model.categoryCursor != 0 || model.skillCursor != 0 {
		t.Fatalf("categoryCursor = %d skillCursor = %d, want reset", model.categoryCursor, model.skillCursor)
	}
}

func TestSkillPickerGlobalSearchIgnoresDrilledCategoryScope(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel(
		[]string{"azure-blob-storage", "azure-functions", "golang-azure-helper"},
		[]string{},
		[]string{"cloud", "golang"},
		map[string][]string{
			"cloud/azure":  {"azure-blob-storage"},
			"cloud/server": {"azure-functions"},
			"golang":       {"golang-azure-helper"},
		},
	)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("azure")})
	model = updated.(skillPickerModel)

	if got := strings.Join(model.visibleSkills(), ","); got != "azure-blob-storage,azure-functions,golang-azure-helper" {
		t.Fatalf("visible skills = %q, want global matches outside drilled category", got)
	}
	view := model.View()
	if strings.Contains(view, "Categories                 Skills") {
		t.Fatalf("view = %q, want flat search mode without category panes", view)
	}
	if !strings.Contains(view, "/azure\n") {
		t.Fatalf("view = %q, want search prompt", view)
	}
}

func TestSkillPickerEscapeLeavesGlobalSearchWithoutCancelling(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel(
		[]string{"azure-blob-storage", "azure-functions", "golang-azure-helper"},
		[]string{},
		[]string{"cloud", "golang"},
		map[string][]string{
			"cloud/azure":  {"azure-blob-storage"},
			"cloud/server": {"azure-functions"},
			"golang":       {"golang-azure-helper"},
		},
	)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("azure")})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(skillPickerModel)

	if model.cancelled || model.confirmed {
		t.Fatalf("cancelled = %v confirmed = %v, want neither after leaving search", model.cancelled, model.confirmed)
	}
	if got := strings.Join(model.categoryPath, "/"); got != "cloud" {
		t.Fatalf("categoryPath = %q, want previous browse path", got)
	}
	if got := strings.Join(model.categories, ","); got != "azure,server" {
		t.Fatalf("categories = %q, want previous subcategory list", got)
	}
	if got := strings.Join(model.visibleSkills(), ","); got != "azure-blob-storage" {
		t.Fatalf("visible skills = %q, want browsed category after leaving search", got)
	}
}

func TestSkillPickerRightArrowOnLeafFocusesSkillsPane(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel(
		[]string{"docker"},
		[]string{},
		[]string{"cloud"},
		map[string][]string{"cloud": {"docker"}},
	)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(skillPickerModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(skillPickerModel)

	if model.focusedPane != skillPickerSkillsPane {
		t.Fatalf("focusedPane = %v, want skills pane", model.focusedPane)
	}
	if got := strings.Join(model.categoryPath, "/"); got != "" {
		t.Fatalf("categoryPath = %q, want no drilldown on leaf", got)
	}
}

func TestSkillPickerCategoriesForLibraryUsesRegistryTopLevels(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "azure-blob-storage", "docker", "golang-cli")
	writeCategories(t, library, `{
		"golang": ["golang-cli"],
		"cloud/azure": ["azure-blob-storage"],
		"cloud": ["docker"]
	}`)

	model := newSkillPickerModel(
		[]string{"azure-blob-storage", "docker", "golang-cli"},
		[]string{},
		skillPickerCategoriesForLibrary(library),
		LoadCategoriesWithWarnings(library, nil),
	)

	if got := strings.Join(model.categories, ","); got != "All,cloud,golang" {
		t.Fatalf("categories = %q, want All,cloud,golang", got)
	}
	if got := strings.Join(model.visibleSkills(), ","); got != "azure-blob-storage,docker,golang-cli" {
		t.Fatalf("visible skills = %q, want All to show every skill", got)
	}
}

func TestSkillPickerCategoriesForLibraryFallsBackToAll(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")

	model := newSkillPickerModel([]string{"docker"}, []string{}, skillPickerCategoriesForLibrary(library), nil)
	if got := strings.Join(model.categories, ","); got != "All" {
		t.Fatalf("categories = %q, want All fallback", got)
	}
}

func TestSkillPickerCategoriesForLibraryEmptyRegistryFallsBackToAll(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	writeCategories(t, library, `{}`)

	model := newSkillPickerModel([]string{"docker"}, []string{}, skillPickerCategoriesForLibrary(library), nil)
	if got := strings.Join(model.categories, ","); got != "All" {
		t.Fatalf("categories = %q, want All fallback", got)
	}
}

func TestSkillPickerTogglesOffPrecheckedSkill(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel([]string{"docker", "golang-cli"}, []string{"docker"}, nil, nil)
	model.toggleCurrent()
	if model.selected["docker"] {
		t.Fatal("docker remains selected after toggle")
	}
	if got := strings.Join(model.selectedSkills(), ","); got != "" {
		t.Fatalf("selected skills = %q, want none", got)
	}
}

func TestSkillPickerDropsStaleManifestSkillsFromSelection(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel([]string{"docker"}, []string{"docker", "missing"}, nil, nil)
	if got := strings.Join(model.selectedSkills(), ","); got != "docker" {
		t.Fatalf("selected skills = %q, want docker only", got)
	}
}

func TestSkillPickerCancelDoesNotApplySelection(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel([]string{"docker"}, []string{"docker"}, nil, nil)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(skillPickerModel)
	if !model.cancelled || model.confirmed {
		t.Fatalf("cancelled = %v confirmed = %v, want cancelled only", model.cancelled, model.confirmed)
	}
}

func TestSkillPickerQCancelDoesNotConfirm(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel([]string{"docker"}, []string{"docker"}, nil, nil)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	model = updated.(skillPickerModel)
	if !model.cancelled || model.confirmed {
		t.Fatalf("cancelled = %v confirmed = %v, want q cancel only", model.cancelled, model.confirmed)
	}
}

func TestSkillPickerHandlesLargeLibrary(t *testing.T) {
	t.Parallel()

	skills := make([]string, 0, 220)
	for i := range 220 {
		skills = append(skills, "skill-"+strconv.Itoa(i))
	}
	model := newSkillPickerModel(skills, []string{}, nil, nil)
	for range 219 {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(skillPickerModel)
	}
	if model.skillCursor != 219 {
		t.Fatalf("skillCursor = %d, want 219", model.skillCursor)
	}
	model.toggleCurrent()
	if got := strings.Join(model.selectedSkills(), ","); got != "skill-219" {
		t.Fatalf("selected = %q, want skill-219", got)
	}
}

func TestApplySkillSelectionWritesDiffAndReconciles(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli", "golang-testing")
	project := createProject(t, []string{"docker", "golang-cli"})
	if err := os.Symlink(filepath.Join(library, "docker"), filepath.Join(project.SymlinkDir, "docker")); err != nil {
		t.Fatalf("Symlink(docker) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := ApplySkillSelection(SkillSelectionOptions{
		Project:     project,
		LibraryPath: library,
		Skills:      []string{"golang-testing", "golang-cli"},
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("ApplySkillSelection() error = %v", err)
	}

	manifest, err := ReadManifest(project.ManifestPath)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if got := strings.Join(manifest.Skills, ","); got != "golang-cli,golang-testing" {
		t.Fatalf("manifest skills = %q, want golang-cli,golang-testing", got)
	}
	if _, err := os.Lstat(filepath.Join(project.SymlinkDir, "docker")); !os.IsNotExist(err) {
		t.Fatalf("docker symlink remains; err = %v", err)
	}
	if !strings.Contains(stdout.String(), "added:   golang-testing") ||
		!strings.Contains(stdout.String(), "removed: docker") ||
		!strings.Contains(stdout.String(), "manifest: 2 skills") {
		t.Fatalf("stdout = %q, want diff and manifest lines", stdout.String())
	}
}

func TestApplySkillSelectionRemovesStaleManifestSkillOnConfirm(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker", "missing"})

	var stdout bytes.Buffer
	if err := ApplySkillSelection(SkillSelectionOptions{
		Project:     project,
		LibraryPath: library,
		Skills:      []string{"docker"},
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("ApplySkillSelection() error = %v", err)
	}

	manifest, err := ReadManifest(project.ManifestPath)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if got := strings.Join(manifest.Skills, ","); got != "docker" {
		t.Fatalf("manifest skills = %q, want docker", got)
	}
	if !strings.Contains(stdout.String(), "removed: missing") {
		t.Fatalf("stdout = %q, want stale skill removal", stdout.String())
	}
}

func TestRunPickSkillsTUINonTTYExitsTwoWithoutManifestChanges(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli")
	project := createProject(t, []string{"docker"})
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("Open(os.DevNull) error = %v", err)
	}
	t.Cleanup(func() {
		if err := stdin.Close(); err != nil {
			t.Fatalf("Close(stdin) error = %v", err)
		}
	})

	err = RunPickSkillsTUI(PickSkillsTUIOptions{
		Project:     project,
		LibraryPath: library,
		Stdin:       stdin,
		Stdout:      ioDiscard(),
	})
	if err == nil {
		t.Fatal("RunPickSkillsTUI() error = nil, want non-TTY error")
	}
	if ExitCode(err) != ExitEnvironment {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitEnvironment)
	}

	manifest, readErr := ReadManifest(project.ManifestPath)
	if readErr != nil {
		t.Fatalf("ReadManifest() error = %v", readErr)
	}
	if got := strings.Join(manifest.Skills, ","); got != "docker" {
		t.Fatalf("manifest skills = %q, want unchanged docker", got)
	}
}
