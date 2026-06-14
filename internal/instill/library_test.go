package instill

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// createGroupLibrary creates a library with:
//   - flat skill "docker"
//   - group dir skills "superpowers/brainstorming" and "superpowers/writing-plans"
func createGroupLibrary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// flat skill
	mustMkdirAllLib(t, filepath.Join(root, "docker"))
	mustWriteFileLib(t, filepath.Join(root, "docker", "SKILL.md"), "# docker\n")

	// group dir — no SKILL.md at group root
	mustMkdirAllLib(t, filepath.Join(root, "superpowers", "brainstorming"))
	mustWriteFileLib(t, filepath.Join(root, "superpowers", "brainstorming", "SKILL.md"), "# brainstorming\n")
	mustMkdirAllLib(t, filepath.Join(root, "superpowers", "writing-plans"))
	mustWriteFileLib(t, filepath.Join(root, "superpowers", "writing-plans", "SKILL.md"), "# writing-plans\n")

	return root
}

func mustMkdirAllLib(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
}

func mustWriteFileLib(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func TestListLibrarySkillsDiscoversGroupDirSkills(t *testing.T) {
	t.Parallel()

	library := createGroupLibrary(t)

	skills, err := ListLibrarySkills(library)
	if err != nil {
		t.Fatalf("ListLibrarySkills() error = %v", err)
	}

	want := []string{"docker", "superpowers/brainstorming", "superpowers/writing-plans"}
	sort.Strings(skills)
	got := strings.Join(skills, ",")
	wantStr := strings.Join(want, ",")
	if got != wantStr {
		t.Fatalf("skills = %v, want %v", skills, want)
	}
}

func TestListLibrarySkillsExcludesGroupRootItself(t *testing.T) {
	t.Parallel()

	library := createGroupLibrary(t)

	skills, err := ListLibrarySkills(library)
	if err != nil {
		t.Fatalf("ListLibrarySkills() error = %v", err)
	}

	for _, s := range skills {
		if s == "superpowers" {
			t.Fatal("ListLibrarySkills() returned bare group root 'superpowers', want only qualified children")
		}
	}
}

func TestListLibrarySkillsExcludesGroupDirWithNoSkillChildren(t *testing.T) {
	t.Parallel()

	library := t.TempDir()
	mustMkdirAllLib(t, filepath.Join(library, "emptygroup"))

	skills, err := ListLibrarySkills(library)
	if err != nil {
		t.Fatalf("ListLibrarySkills() error = %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("skills = %v, want empty", skills)
	}
}

func TestListLibrarySkillsDiscoversDeepNesting(t *testing.T) {
	t.Parallel()

	library := t.TempDir()
	for _, name := range []string{"cloud/azure/azure-cli", "cloud/k8s-helm", "docker"} {
		mustMkdirAllLib(t, filepath.Join(library, filepath.FromSlash(name)))
		mustWriteFileLib(t, filepath.Join(library, filepath.FromSlash(name), "SKILL.md"), "# "+name+"\n")
	}

	skills, err := ListLibrarySkills(library)
	if err != nil {
		t.Fatalf("ListLibrarySkills() error = %v", err)
	}
	if got := strings.Join(skills, ","); got != "cloud/azure/azure-cli,cloud/k8s-helm,docker" {
		t.Fatalf("skills = %q, want cloud/azure/azure-cli,cloud/k8s-helm,docker", got)
	}
}

func TestListLibrarySkillsStopsAtSkillMarker(t *testing.T) {
	t.Parallel()

	library := t.TempDir()
	// cloud/azure is a skill; a nested dir beneath it must NOT be discovered.
	mustMkdirAllLib(t, filepath.Join(library, "cloud", "azure"))
	mustWriteFileLib(t, filepath.Join(library, "cloud", "azure", "SKILL.md"), "# azure\n")
	mustMkdirAllLib(t, filepath.Join(library, "cloud", "azure", "nested"))
	mustWriteFileLib(t, filepath.Join(library, "cloud", "azure", "nested", "SKILL.md"), "# nested\n")

	skills, err := ListLibrarySkills(library)
	if err != nil {
		t.Fatalf("ListLibrarySkills() error = %v", err)
	}
	if got := strings.Join(skills, ","); got != "cloud/azure" {
		t.Fatalf("skills = %q, want cloud/azure only (leaf stops recursion)", got)
	}
}

func TestSkillExistsReturnsTrueForFlatSkill(t *testing.T) {
	t.Parallel()

	library := createGroupLibrary(t)

	ok, err := SkillExists(library, "docker")
	if err != nil {
		t.Fatalf("SkillExists(docker) error = %v", err)
	}
	if !ok {
		t.Fatal("SkillExists(docker) = false, want true")
	}
}

func TestSkillExistsReturnsTrueForGroupSkill(t *testing.T) {
	t.Parallel()

	library := createGroupLibrary(t)

	ok, err := SkillExists(library, "superpowers/brainstorming")
	if err != nil {
		t.Fatalf("SkillExists(superpowers/brainstorming) error = %v", err)
	}
	if !ok {
		t.Fatal("SkillExists(superpowers/brainstorming) = false, want true")
	}
}

func TestSkillExistsReturnsFalseForGroupRootWithoutSKILLMd(t *testing.T) {
	t.Parallel()

	library := createGroupLibrary(t)

	ok, err := SkillExists(library, "superpowers")
	if err != nil {
		t.Fatalf("SkillExists(superpowers) error = %v", err)
	}
	if ok {
		t.Fatal("SkillExists(superpowers) = true, want false (group root is not a skill)")
	}
}

func TestSkillSourcePathForFlatSkill(t *testing.T) {
	t.Parallel()

	library := createGroupLibrary(t)

	got, err := SkillSourcePath(library, "docker")
	if err != nil {
		t.Fatalf("SkillSourcePath(docker) error = %v", err)
	}
	want := filepath.Join(library, "docker")
	if got != want {
		t.Fatalf("SkillSourcePath(docker) = %q, want %q", got, want)
	}
}

func TestSkillSourcePathForGroupSkill(t *testing.T) {
	t.Parallel()

	library := createGroupLibrary(t)

	got, err := SkillSourcePath(library, "superpowers/brainstorming")
	if err != nil {
		t.Fatalf("SkillSourcePath(superpowers/brainstorming) error = %v", err)
	}
	want := filepath.Join(library, "superpowers", "brainstorming")
	if got != want {
		t.Fatalf("SkillSourcePath(superpowers/brainstorming) = %q, want %q", got, want)
	}
}
