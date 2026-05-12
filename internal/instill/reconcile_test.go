package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReconcileCreatesMissingSymlinks(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli")
	project := createProject(t, []string{"docker", "golang-cli"})

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	for _, name := range []string{"docker", "golang-cli"} {
		got, err := os.Readlink(filepath.Join(project.SymlinkDir, name))
		if err != nil {
			t.Fatalf("Readlink(%s) error = %v", name, err)
		}
		want := filepath.Join(library, name)
		if got != want {
			t.Fatalf("Readlink(%s) = %q, want %q", name, got, want)
		}
	}

	output := stdout.String()
	if !strings.Contains(output, "created: docker -> ") ||
		!strings.Contains(output, "created: golang-cli -> ") ||
		!strings.Contains(output, "ok: 2 skills linked") {
		t.Fatalf("output = %q, want created and ok lines", output)
	}
}

func TestReconcileRemovesOrphanedSymlink(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker"})
	if err := os.Symlink(filepath.Join(library, "docker"), filepath.Join(project.SymlinkDir, "orphan")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if _, err := os.Lstat(filepath.Join(project.SymlinkDir, "orphan")); !os.IsNotExist(err) {
		t.Fatalf("orphan symlink still exists; err = %v", err)
	}
	if !strings.Contains(stdout.String(), "ok: 1 skills linked") {
		t.Fatalf("output = %q, want ok line for orphan cleanup", stdout.String())
	}
}

func TestReconcileRemovesBrokenSkillAndUpdatesManifest(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker", "missing"})
	if err := os.Symlink(filepath.Join(library, "missing"), filepath.Join(project.SymlinkDir, "missing")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	manifest, err := ReadManifest(project.ManifestPath)
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	if len(manifest.Skills) != 1 || manifest.Skills[0] != "docker" {
		t.Fatalf("manifest skills = %#v, want [docker]", manifest.Skills)
	}
	if !strings.Contains(stdout.String(), "removed: missing (no longer in library)") {
		t.Fatalf("output = %q, want removed line", stdout.String())
	}
	if _, err := os.Lstat(filepath.Join(project.SymlinkDir, "missing")); !os.IsNotExist(err) {
		t.Fatalf("broken symlink still exists; err = %v", err)
	}
}

func TestReconcileSilentWhenNoChangesNeeded(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker"})
	if err := os.Symlink(filepath.Join(library, "docker"), filepath.Join(project.SymlinkDir, "docker")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if stdout.String() != "" {
		t.Fatalf("output = %q, want silent", stdout.String())
	}
}

func TestReadManifestMalformedJSONUsesGeneralExitCode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "skill-manifest.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := ReadManifest(path)
	if err == nil {
		t.Fatal("ReadManifest() error = nil, want malformed JSON error")
	}
	if got := ExitCode(err); got != ExitGeneral {
		t.Fatalf("ExitCode(err) = %d, want %d", got, ExitGeneral)
	}
}

func TestReadManifestRejectsUnsafeSkillNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		skill string
	}{
		{name: "parent traversal", skill: "../escape"},
		{name: "nested path", skill: "go/docker"},
		{name: "absolute path", skill: "/tmp/docker"},
		{name: "backslash path", skill: `go\docker`},
		{name: "dot", skill: "."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "skill-manifest.json")
			data := []byte(`{"skills":["` + tt.skill + `"]}`)
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			_, err := ReadManifest(path)
			if err == nil {
				t.Fatal("ReadManifest() error = nil, want invalid skill name")
			}
			if got := ExitCode(err); got != ExitGeneral {
				t.Fatalf("ExitCode(err) = %d, want %d", got, ExitGeneral)
			}
		})
	}
}

func TestReconcileFilesystemErrorOnNonSymlinkCollision(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker"})
	if err := os.WriteFile(filepath.Join(project.SymlinkDir, "docker"), []byte("not a symlink"), 0o644); err != nil {
		t.Fatalf("WriteFile(collision) error = %v", err)
	}

	var stdout bytes.Buffer
	err := Reconcile(project, library, &stdout)
	if err == nil {
		t.Fatal("Reconcile() error = nil, want filesystem error")
	}
	if got := ExitCode(err); got != ExitFilesystem {
		t.Fatalf("ExitCode(err) = %d, want %d", got, ExitFilesystem)
	}
}

func createLibrary(t *testing.T, names ...string) string {
	t.Helper()

	root := t.TempDir()
	for _, name := range names {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(SKILL.md) error = %v", err)
		}
	}
	return root
}

func createProject(t *testing.T, skills []string) Project {
	t.Helper()

	root := t.TempDir()
	project := Project{
		Root:         root,
		ManifestPath: filepath.Join(root, claudeDirName, manifestFileName),
		SymlinkDir:   filepath.Join(root, claudeDirName, skillsDirName),
	}
	if err := os.MkdirAll(project.SymlinkDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", project.SymlinkDir, err)
	}
	if err := WriteManifestAtomic(project.ManifestPath, Manifest{Skills: skills}); err != nil {
		t.Fatalf("WriteManifestAtomic() error = %v", err)
	}
	return project
}
