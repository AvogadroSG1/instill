package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitProjectCreatesManifestSkillsDirAndGitignore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v", err)
	}
	library := createLibrary(t, "docker")

	var stdout bytes.Buffer
	if err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: library,
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	manifest, err := ReadManifest(filepath.Join(root, ".claude", "skill-manifest.json"))
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if len(manifest.Skills) != 0 {
		t.Fatalf("manifest skills = %#v, want empty", manifest.Skills)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills")); err != nil {
		t.Fatalf("Stat(.claude/skills) error = %v", err)
	}
	gitignore, err := os.ReadFile(filepath.Join(root, ".gitignore")) //nolint:gosec // Test reads inside t.TempDir project root.
	if err != nil {
		t.Fatalf("ReadFile(.gitignore) error = %v", err)
	}
	if !strings.Contains(string(gitignore), ".claude/skills/") {
		t.Fatalf(".gitignore = %q, want skills entry", string(gitignore))
	}
	if !strings.Contains(string(gitignore), ".claude/settings.local.json") {
		t.Fatalf(".gitignore = %q, want settings.local.json entry", string(gitignore))
	}
	if !strings.Contains(stdout.String(), "updated:     .gitignore (+.claude/settings.local.json)") {
		t.Fatalf("stdout = %q, want settings.local.json gitignore update", stdout.String())
	}
}

func TestInitProjectExistingManifestRequiresForce(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".claude", "skill-manifest.json"),
		[]byte(`{"skills":[]}`),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}

	var stdout bytes.Buffer
	err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: createLibrary(t, "docker"),
		Stdout:      &stdout,
	})
	if err == nil {
		t.Fatal("InitProject() error = nil, want existing manifest error")
	}
	if ExitCode(err) != ExitGeneral {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitGeneral)
	}
}

func TestInitProjectForceOverwritesManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".claude", "skill-manifest.json"),
		[]byte(`{"skills":["docker"]}`),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: createLibrary(t, "docker"),
		Force:       true,
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	manifest, err := ReadManifest(filepath.Join(root, ".claude", "skill-manifest.json"))
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if len(manifest.Skills) != 0 {
		t.Fatalf("manifest skills = %#v, want empty", manifest.Skills)
	}
}

func TestInitProjectWithSkillsRejectsUnknownBeforeProjectWrites(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	library := createLibrary(t, "docker")

	var stdout bytes.Buffer
	err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: library,
		Skills:      []string{"docker", "missing"},
		Stdout:      &stdout,
	})
	if err == nil {
		t.Fatal("InitProject() error = nil, want unknown skill")
	}

	if _, statErr := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(statErr) {
		t.Fatalf(".claude exists after failed validation; err = %v", statErr)
	}
}

func TestInitProjectForceWithInvalidSkillsPreservesExistingManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".claude", "skill-manifest.json"),
		[]byte(`{"skills":["docker"]}`),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}

	var stdout bytes.Buffer
	err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: createLibrary(t, "docker"),
		Skills:      []string{"missing"},
		Force:       true,
		Stdout:      &stdout,
	})
	if err == nil {
		t.Fatal("InitProject() error = nil, want unknown skill")
	}

	manifest, readErr := ReadManifest(filepath.Join(root, ".claude", "skill-manifest.json"))
	if readErr != nil {
		t.Fatalf("ReadManifest() error = %v", readErr)
	}
	if got := strings.Join(manifest.Skills, ","); got != "docker" {
		t.Fatalf("manifest skills = %q, want docker", got)
	}
}

func TestInitProjectWithSkillsWritesManifestAndReconciles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	library := createLibrary(t, "docker", "golang-cli")

	var stdout bytes.Buffer
	if err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: library,
		Skills:      []string{"golang-cli", "docker"},
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	manifest, err := ReadManifest(filepath.Join(root, ".claude", "skill-manifest.json"))
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if got := strings.Join(manifest.Skills, ","); got != "docker,golang-cli" {
		t.Fatalf("manifest skills = %q, want sorted docker,golang-cli", got)
	}
	if _, err := os.Readlink(filepath.Join(root, ".claude", "skills", "docker")); err != nil {
		t.Fatalf("Readlink(docker) error = %v", err)
	}
}

func TestInitProjectWarnsOutsideGitRepository(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	if err := InitProject(InitProjectOptions{
		Root:        t.TempDir(),
		LibraryPath: createLibrary(t, "docker"),
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "warning:     not a git repository — manifest will not be committed") {
		t.Fatalf("stdout = %q, want exact git warning", stdout.String())
	}
}

func TestInitProjectDoesNotDuplicateExistingGitignoreEntry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/.claude/skills/\r\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: createLibrary(t, "docker"),
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("ReadFile(.gitignore) error = %v", err)
	}
	if strings.Count(string(data), ".claude/skills/") != 1 {
		t.Fatalf(".gitignore = %q, want no duplicate skills entry", string(data))
	}
}

func TestInitProjectAddsSettingsLocalGitignoreEntryWhenSkillsEntryExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".claude/skills/\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: createLibrary(t, "docker"),
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("ReadFile(.gitignore) error = %v", err)
	}
	if !strings.Contains(string(data), ".claude/settings.local.json") {
		t.Fatalf(".gitignore = %q, want settings.local.json entry", string(data))
	}
	if strings.Contains(stdout.String(), "updated:     .gitignore (+.claude/skills/)") {
		t.Fatalf("stdout = %q, want no skills gitignore update", stdout.String())
	}
	if !strings.Contains(stdout.String(), "updated:     .gitignore (+.claude/settings.local.json)") {
		t.Fatalf("stdout = %q, want settings.local.json gitignore update", stdout.String())
	}
}

func TestEnsureGitignoreEntryNoopsWhenEntryExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gitignorePath := filepath.Join(root, ".gitignore")
	initial := []byte(".claude/skills/\n.claude/settings.local.json\n")
	if err := os.WriteFile(gitignorePath, initial, 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore) error = %v", err)
	}

	changed, err := ensureGitignoreEntry(root, ".claude/settings.local.json")
	if err != nil {
		t.Fatalf("ensureGitignoreEntry() error = %v", err)
	}
	if changed {
		t.Fatal("ensureGitignoreEntry() changed = true, want false")
	}
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("ReadFile(.gitignore) error = %v", err)
	}
	if string(data) != string(initial) {
		t.Fatalf(".gitignore = %q, want unchanged %q", string(data), string(initial))
	}
}

func TestInitProjectDoesNotReportGitignoreUpdateWhenEntriesExist(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	initial := []byte(".claude/skills/\n.claude/settings.local.json\n")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), initial, 0o644); err != nil {
		t.Fatalf("WriteFile(.gitignore) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: createLibrary(t, "docker"),
		Stdout:      &stdout,
	}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}

	if strings.Contains(stdout.String(), "updated:     .gitignore") {
		t.Fatalf("stdout = %q, want no gitignore update", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("ReadFile(.gitignore) error = %v", err)
	}
	if string(data) != string(initial) {
		t.Fatalf(".gitignore = %q, want unchanged %q", string(data), string(initial))
	}
}

func TestInitProjectRejectsSymlinkedGitignore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "gitignore")
	if err := os.WriteFile(target, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".gitignore")); err != nil {
		t.Fatalf("Symlink(.gitignore) error = %v", err)
	}

	var stdout bytes.Buffer
	err := InitProject(InitProjectOptions{
		Root:        root,
		LibraryPath: createLibrary(t, "docker"),
		Stdout:      &stdout,
	})
	if err == nil {
		t.Fatal("InitProject() error = nil, want symlink rejection")
	}
	if ExitCode(err) != ExitFilesystem {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitFilesystem)
	}
}
