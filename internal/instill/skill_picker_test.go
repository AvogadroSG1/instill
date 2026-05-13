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

	model := newSkillPickerModel([]string{"docker", "golang-cli", "golang-testing"}, []string{"golang-cli"})
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

	model := newSkillPickerModel([]string{"docker"}, []string{})
	view := model.View()
	if !strings.Contains(view, "Categories") ||
		!strings.Contains(view, "(categories)") ||
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

func TestSkillPickerTogglesOffPrecheckedSkill(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel([]string{"docker", "golang-cli"}, []string{"docker"})
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

	model := newSkillPickerModel([]string{"docker"}, []string{"docker", "missing"})
	if got := strings.Join(model.selectedSkills(), ","); got != "docker" {
		t.Fatalf("selected skills = %q, want docker only", got)
	}
}

func TestSkillPickerCancelDoesNotApplySelection(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel([]string{"docker"}, []string{"docker"})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(skillPickerModel)
	if !model.cancelled || model.confirmed {
		t.Fatalf("cancelled = %v confirmed = %v, want cancelled only", model.cancelled, model.confirmed)
	}
}

func TestSkillPickerQCancelDoesNotConfirm(t *testing.T) {
	t.Parallel()

	model := newSkillPickerModel([]string{"docker"}, []string{"docker"})
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
	model := newSkillPickerModel(skills, []string{})
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
