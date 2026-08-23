package instill

import (
	"os"
	"path/filepath"
	"testing"
)

// createLibrary creates a Library catalog root directory containing SKILL.md
// files for each of the given flat skill names, for use by tests across the
// package.
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

// createProject creates a legacy Project layout (manifest plus symlink dirs)
// seeded with the given skills, for use by tests across the package.
func createProject(t *testing.T, skills []string) Project {
	t.Helper()

	root := t.TempDir()
	project := Project{
		Root:             root,
		ManifestPath:     filepath.Join(root, claudeDirName, manifestFileName),
		SymlinkDir:       filepath.Join(root, claudeDirName, skillsDirName),
		AgentsSymlinkDir: filepath.Join(root, agentsDirName, skillsDirName),
	}
	if err := os.MkdirAll(project.SymlinkDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", project.SymlinkDir, err)
	}
	if err := WriteManifestAtomic(project.ManifestPath, Manifest{Skills: skills}); err != nil {
		t.Fatalf("WriteManifestAtomic() error = %v", err)
	}
	return project
}

// writeSettingsLocalForTest writes raw content to a project's
// .claude/settings.local.json file, for use by tests across the package.
func writeSettingsLocalForTest(t *testing.T, project Project, content string) {
	t.Helper()

	path := filepath.Join(project.Root, claudeDirName, settingsLocalFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(settings.local) error = %v", err)
	}
}
