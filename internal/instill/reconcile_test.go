package instill

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

func TestReconcileGrantsFinalSkillPermissions(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker"})
	createSkillSymlink(t, project, library, "docker")
	writeSettingsLocalForTest(t, project, `{
  "permissions": {
    "allow": [
      "Bash(go test ./...)"
    ]
  }
}
`)

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if stdout.String() != "" {
		t.Fatalf("output = %q, want silent permission-only change", stdout.String())
	}
	assertSettingsAllow(t, project, []string{"Bash(go test ./...)", "Skill(docker)"})
}

func TestReconcileRevokesRemovedManifestSkillPermissions(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker", "missing"})
	createSkillSymlink(t, project, library, "docker")
	writeSettingsLocalForTest(t, project, `{
  "permissions": {
    "allow": [
      "Skill(missing)",
      "Skill(docker)"
    ]
  }
}
`)

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertSettingsAllow(t, project, []string{"Skill(docker)"})
}

func TestReconcilePreservesManualSkillPermissions(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker", "missing"})
	createSkillSymlink(t, project, library, "docker")
	writeSettingsLocalForTest(t, project, `{
  "permissions": {
    "allow": [
      "Skill(manual-private)",
      "Skill(missing)",
      "Skill(docker)"
    ]
  }
}
`)

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	assertSettingsAllow(t, project, []string{"Skill(manual-private)", "Skill(docker)"})
}

func TestReconcileCreatesSettingsLocalForFinalSkillPermissions(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker"})
	createSkillSymlink(t, project, library, "docker")

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if stdout.String() != "" {
		t.Fatalf("output = %q, want silent permission-only change", stdout.String())
	}
	assertSettingsAllow(t, project, []string{"Skill(docker)"})
}

func TestReconcileDoesNotRewriteSettingsLocalWhenPermissionsUnchanged(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	project := createProject(t, []string{"docker"})
	createSkillSymlink(t, project, library, "docker")
	writeSettingsLocalForTest(t, project, `{
  "permissions": {
    "allow": [
      "Skill(docker)"
    ]
  }
}
`)
	settingsPath := filepath.Join(project.Root, claudeDirName, settingsLocalFileName)
	oldTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(settingsPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes(settings.local) error = %v", err)
	}
	before, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatalf("Stat(settings.local) before error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if stdout.String() != "" {
		t.Fatalf("output = %q, want silent no-op", stdout.String())
	}
	after, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatalf("Stat(settings.local) after error = %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("settings.local modtime = %v, want unchanged %v", after.ModTime(), before.ModTime())
	}
}

func TestReconcileRejectsSymlinkedClaudeDirectory(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll(target) error = %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, claudeDirName)); err != nil {
		t.Fatalf("Symlink(.claude) error = %v", err)
	}
	project := Project{
		Root:         root,
		ManifestPath: filepath.Join(root, claudeDirName, manifestFileName),
		SymlinkDir:   filepath.Join(root, claudeDirName, skillsDirName),
	}

	var stdout bytes.Buffer
	err := ReconcileManifest(project, Manifest{Skills: []string{"docker"}}, library, &stdout)
	if err == nil {
		t.Fatal("ReconcileManifest() error = nil, want symlink rejection")
	}
	if ExitCode(err) != ExitFilesystem {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitFilesystem)
	}
	if _, err := os.Stat(filepath.Join(target, skillsDirName, "docker")); !os.IsNotExist(err) {
		t.Fatalf("outside docker symlink exists; err = %v", err)
	}
}

func TestReconcileRejectsSymlinkedSkillsDirectory(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	root := t.TempDir()
	claudePath := filepath.Join(root, claudeDirName)
	if err := os.MkdirAll(claudePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	target := filepath.Join(t.TempDir(), "outside-skills")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll(target) error = %v", err)
	}
	if err := os.Symlink(target, filepath.Join(claudePath, skillsDirName)); err != nil {
		t.Fatalf("Symlink(.claude/skills) error = %v", err)
	}
	project := Project{
		Root:         root,
		ManifestPath: filepath.Join(root, claudeDirName, manifestFileName),
		SymlinkDir:   filepath.Join(root, claudeDirName, skillsDirName),
	}

	var stdout bytes.Buffer
	err := ReconcileManifest(project, Manifest{Skills: []string{"docker"}}, library, &stdout)
	if err == nil {
		t.Fatal("ReconcileManifest() error = nil, want symlink rejection")
	}
	if ExitCode(err) != ExitFilesystem {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitFilesystem)
	}
	if _, err := os.Stat(filepath.Join(target, "docker")); !os.IsNotExist(err) {
		t.Fatalf("outside docker symlink exists; err = %v", err)
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
		{name: "absolute path", skill: "/tmp/docker"},
		{name: "backslash path", skill: `go\docker`},
		{name: "dot", skill: "."},
		{name: "double slash", skill: "go//docker"},
		{name: "slash at end", skill: "docker/"},
		{name: "dotdot second segment", skill: "superpowers/.."},
		{name: "dot second segment", skill: "superpowers/."},
		{name: "empty second segment", skill: "superpowers/"},
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

func TestReadManifestAcceptsQualifiedSkillNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		skill string
	}{
		{name: "flat", skill: "docker"},
		{name: "qualified", skill: "superpowers/brainstorming"},
		{name: "qualified with dash", skill: "obsidian/json-canvas"},
		{name: "three segments", skill: "cloud/azure/azure-cli"},
		{name: "deep nesting", skill: "cloud/azure/compute/azure-vm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !IsValidSkillName(tt.skill) {
				t.Fatalf("IsValidSkillName(%q) = false, want true", tt.skill)
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

func createSkillSymlink(t *testing.T, project Project, library string, name string) {
	t.Helper()

	if err := os.Symlink(filepath.Join(library, name), filepath.Join(project.SymlinkDir, name)); err != nil {
		t.Fatalf("Symlink(%s) error = %v", name, err)
	}
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

func assertSettingsAllow(t *testing.T, project Project, want []string) {
	t.Helper()

	path := filepath.Join(project.Root, claudeDirName, settingsLocalFileName)
	data, err := os.ReadFile(path) //nolint:gosec // Test reads t.TempDir settings file.
	if err != nil {
		t.Fatalf("ReadFile(settings.local) error = %v", err)
	}

	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("Unmarshal(settings.local) error = %v\n%s", err, data)
	}
	if !slices.Equal(settings.Permissions.Allow, want) {
		t.Fatalf("permissions.allow = %#v, want %#v", settings.Permissions.Allow, want)
	}
}

// createGroupSkill creates library/<group>/<leaf>/SKILL.md and returns the qualified name.
func createGroupSkill(t *testing.T, library, group, leaf string) string {
	t.Helper()
	path := filepath.Join(library, group, leaf)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("# "+group+"/"+leaf+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	return group + "/" + leaf
}

func TestReconcileCreatesFlatSymlinkForGroupSkill(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	createGroupSkill(t, library, "superpowers", "brainstorming")
	project := createProject(t, []string{"docker", "superpowers/brainstorming"})

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	// flat docker symlink still works
	flatTarget, err := os.Readlink(filepath.Join(project.SymlinkDir, "docker"))
	if err != nil {
		t.Fatalf("Readlink(docker) error = %v", err)
	}
	if flatTarget != filepath.Join(library, "docker") {
		t.Fatalf("docker -> %q, want %q", flatTarget, filepath.Join(library, "docker"))
	}

	// no nested superpowers directory
	if _, err := os.Lstat(filepath.Join(project.SymlinkDir, "superpowers")); !os.IsNotExist(err) {
		t.Fatalf("nested superpowers dir should not exist; err = %v", err)
	}

	// group skill is a flat colon-separated symlink pointing to library source
	groupTarget, err := os.Readlink(filepath.Join(project.SymlinkDir, "superpowers:brainstorming"))
	if err != nil {
		t.Fatalf("Readlink(superpowers:brainstorming) error = %v", err)
	}
	wantTarget := filepath.Join(library, "superpowers", "brainstorming")
	if groupTarget != wantTarget {
		t.Fatalf("superpowers:brainstorming -> %q, want %q", groupTarget, wantTarget)
	}

	if !strings.Contains(stdout.String(), "created: superpowers/brainstorming ->") {
		t.Fatalf("output = %q, missing created line", stdout.String())
	}
}

func TestReconcileRemovesFlatGroupSymlink(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	createGroupSkill(t, library, "superpowers", "brainstorming")
	project := createProject(t, []string{"docker", "superpowers/brainstorming"})

	// pre-create state (as if reconcile already ran once with flat links)
	if err := os.Symlink(filepath.Join(library, "superpowers", "brainstorming"), filepath.Join(project.SymlinkDir, "superpowers:brainstorming")); err != nil {
		t.Fatalf("Symlink(superpowers:brainstorming) error = %v", err)
	}
	if err := os.Symlink(filepath.Join(library, "docker"), filepath.Join(project.SymlinkDir, "docker")); err != nil {
		t.Fatalf("Symlink(docker) error = %v", err)
	}

	// reconcile with brainstorming removed
	if err := WriteManifestAtomic(project.ManifestPath, Manifest{Skills: []string{"docker"}}); err != nil {
		t.Fatalf("WriteManifestAtomic() error = %v", err)
	}
	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if _, err := os.Lstat(filepath.Join(project.SymlinkDir, "superpowers:brainstorming")); !os.IsNotExist(err) {
		t.Fatalf("superpowers:brainstorming symlink still exists; err = %v", err)
	}
}

func TestReconcileFlatGroupSymlinksAreIndependent(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	createGroupSkill(t, library, "superpowers", "brainstorming")
	createGroupSkill(t, library, "superpowers", "writing-plans")
	project := createProject(t, []string{"docker", "superpowers/brainstorming", "superpowers/writing-plans"})

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	// remove only brainstorming
	if err := WriteManifestAtomic(project.ManifestPath, Manifest{Skills: []string{"docker", "superpowers/writing-plans"}}); err != nil {
		t.Fatalf("WriteManifestAtomic() error = %v", err)
	}
	var stdout2 bytes.Buffer
	if err := Reconcile(project, library, &stdout2); err != nil {
		t.Fatalf("Reconcile(updated) error = %v", err)
	}

	// writing-plans flat symlink must survive
	if _, err := os.Readlink(filepath.Join(project.SymlinkDir, "superpowers:writing-plans")); err != nil {
		t.Fatalf("superpowers:writing-plans symlink missing; err = %v", err)
	}
	// brainstorming flat symlink must be gone
	if _, err := os.Lstat(filepath.Join(project.SymlinkDir, "superpowers:brainstorming")); !os.IsNotExist(err) {
		t.Fatalf("superpowers:brainstorming still exists; err = %v", err)
	}
}

func TestReconcileMigratesLegacyNestedToFlatSymlink(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker")
	createGroupSkill(t, library, "superpowers", "brainstorming")
	project := createProject(t, []string{"docker", "superpowers/brainstorming"})

	// Pre-create legacy nested structure (old behavior: real dir + nested symlink).
	superpowersDir := filepath.Join(project.SymlinkDir, "superpowers")
	if err := os.MkdirAll(superpowersDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(superpowers) error = %v", err)
	}
	if err := os.Symlink(filepath.Join(library, "superpowers", "brainstorming"), filepath.Join(superpowersDir, "brainstorming")); err != nil {
		t.Fatalf("Symlink(brainstorming) error = %v", err)
	}
	if err := os.Symlink(filepath.Join(library, "docker"), filepath.Join(project.SymlinkDir, "docker")); err != nil {
		t.Fatalf("Symlink(docker) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	// Legacy nested symlink and its parent dir must be gone.
	if _, err := os.Lstat(filepath.Join(superpowersDir, "brainstorming")); !os.IsNotExist(err) {
		t.Fatalf("legacy nested brainstorming symlink still exists; err = %v", err)
	}
	if _, err := os.Lstat(superpowersDir); !os.IsNotExist(err) {
		t.Fatalf("legacy superpowers dir still exists; err = %v", err)
	}

	// New flat colon symlink must exist and point to the correct library source.
	flatTarget, err := os.Readlink(filepath.Join(project.SymlinkDir, "superpowers:brainstorming"))
	if err != nil {
		t.Fatalf("Readlink(superpowers:brainstorming) error = %v", err)
	}
	if flatTarget != filepath.Join(library, "superpowers", "brainstorming") {
		t.Fatalf("superpowers:brainstorming -> %q, want %q", flatTarget, filepath.Join(library, "superpowers", "brainstorming"))
	}
}

func TestReconcilePopulatesAgentsSkillsDir(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli")
	project := createProject(t, []string{"docker", "golang-cli"})
	project.AgentsSymlinkDir = filepath.Join(project.Root, agentsDirName, skillsDirName)

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	for _, name := range []string{"docker", "golang-cli"} {
		claudeTarget, err := os.Readlink(filepath.Join(project.SymlinkDir, name))
		if err != nil {
			t.Fatalf("Readlink(.claude/skills/%s) error = %v", name, err)
		}
		agentsTarget, err := os.Readlink(filepath.Join(project.AgentsSymlinkDir, name))
		if err != nil {
			t.Fatalf("Readlink(.agents/skills/%s) error = %v", name, err)
		}
		if claudeTarget != agentsTarget {
			t.Fatalf("%s: .claude/skills -> %q, .agents/skills -> %q, want identical targets", name, claudeTarget, agentsTarget)
		}
	}
}

func TestReconcileRemovesOrphansFromAgentsSkillsDir(t *testing.T) {
	t.Parallel()

	library := createLibrary(t, "docker", "golang-cli")
	project := createProject(t, []string{"docker", "golang-cli"})
	project.AgentsSymlinkDir = filepath.Join(project.Root, agentsDirName, skillsDirName)
	if err := os.MkdirAll(project.AgentsSymlinkDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(.agents/skills) error = %v", err)
	}

	// Pre-create symlinks as if previously reconciled.
	for _, name := range []string{"docker", "golang-cli"} {
		if err := os.Symlink(filepath.Join(library, name), filepath.Join(project.SymlinkDir, name)); err != nil {
			t.Fatalf("Symlink(.claude/skills/%s) error = %v", name, err)
		}
		if err := os.Symlink(filepath.Join(library, name), filepath.Join(project.AgentsSymlinkDir, name)); err != nil {
			t.Fatalf("Symlink(.agents/skills/%s) error = %v", name, err)
		}
	}

	// Remove golang-cli from manifest.
	if err := WriteManifestAtomic(project.ManifestPath, Manifest{Skills: []string{"docker"}}); err != nil {
		t.Fatalf("WriteManifestAtomic() error = %v", err)
	}

	var stdout bytes.Buffer
	if err := Reconcile(project, library, &stdout); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if _, err := os.Lstat(filepath.Join(project.SymlinkDir, "golang-cli")); !os.IsNotExist(err) {
		t.Fatalf(".claude/skills/golang-cli still exists after removal; err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(project.AgentsSymlinkDir, "golang-cli")); !os.IsNotExist(err) {
		t.Fatalf(".agents/skills/golang-cli still exists after removal; err = %v", err)
	}
}
