package instill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildProjectSelectionPlanResolvesCompleteCatalogSelection(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{
		skills:       []CatalogEntry{{Type: LibraryTypeSkill, Name: "one", Path: "one/SKILL.md"}},
		plugins:      []CatalogEntry{{Type: LibraryTypePlugin, Name: "plug", Path: "plug/.claude-plugin/plugin.json"}},
		mcp:          []CatalogEntry{{Type: LibraryTypeMCP, Name: "zeta", Transport: "stdio", Command: "zeta"}, {Type: LibraryTypeMCP, Name: "alpha", Transport: "stdio", Command: "alpha"}},
		instructions: []CatalogEntry{{Type: LibraryTypeInstruction, Name: "rules/api", Path: "rules/api/INSTRUCTION.md"}},
		prompts:      []CatalogEntry{{Type: LibraryTypePrompt, Name: "review", Path: "review/PROMPT.md"}},
	})
	project := createAPMProject(t, APMManifest{})
	instructionDest := projectContentPath(project.Root, LibraryTypeInstruction, "rules/api")
	if err := os.MkdirAll(filepath.Dir(instructionDest), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(project.Root, ".apm", "instructions", "stale.instructions.md")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.Root, ".apm", "instructions", "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	selection := ProjectSelection{Skills: []string{"one"}, Plugins: []string{"plug"}, MCPs: []string{"zeta", "alpha"}, Instructions: []string{"rules/api"}, Prompts: []string{"review"}}

	plan, err := buildProjectSelectionPlan(ProjectSelectionOptions{Project: project, LibraryPath: library, Selection: selection})
	requireNoError(t, err)
	if len(plan.APM) != 2 || len(plan.MCP) != 2 {
		t.Fatalf("dependency counts = APM %d, MCP %d; want 2 each", len(plan.APM), len(plan.MCP))
	}
	requireEqual(t, "alpha", plan.MCP[0].Name)
	requireEqual(t, "zeta", plan.MCP[1].Name)
	if len(plan.Content) != 2 {
		t.Fatalf("content snapshots = %d, want 2", len(plan.Content))
	}

	// Mutations to the Library after capture do not change the bytes eventually published.
	instructionSource := filepath.Join(library, "instructions", "rules/api/INSTRUCTION.md")
	if err := os.WriteFile(instructionSource, []byte("later"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := plan.Content[0].publish(); err != nil {
		t.Fatal(err)
	}
	requireEqual(t, "# instruction rules/api\n", readFile(t, instructionDest))
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale copied file remains: %v", err)
	}
	if got := readFile(t, filepath.Join(project.Root, ".apm", "instructions", "keep.txt")); got != "keep" {
		t.Fatalf("unrelated .apm file = %q", got)
	}
}

func TestBuildProjectSelectionPlanRejectsUnknownSelectionAcrossTypes(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{skills: []CatalogEntry{{Type: LibraryTypeSkill, Name: "known", Path: "known/SKILL.md"}}})
	project := createAPMProject(t, APMManifest{})
	_, err := buildProjectSelectionPlan(ProjectSelectionOptions{Project: project, LibraryPath: library, Selection: ProjectSelection{Skills: []string{"known"}, Prompts: []string{"missing"}}})
	if err == nil || ErrorMessage(err) == "" {
		t.Fatalf("expected unknown selected prompt error, got %v", err)
	}
}

func TestCaptureProjectContentRejectsSanitizedDestinationCollision(t *testing.T) {
	library := createCatalogLibrary(t, catalogLibrarySeed{instructions: []CatalogEntry{
		{Type: LibraryTypeInstruction, Name: "a/b", Path: "a-b/one.md"},
		{Type: LibraryTypeInstruction, Name: "a-b", Path: "a-b/two.md"},
	}})
	project := createAPMProject(t, APMManifest{})
	_, err := captureProjectContent(project.Root, library, LibraryTypeInstruction, []string{"a/b", "a-b"}, []CatalogEntry{
		{Type: LibraryTypeInstruction, Name: "a/b", Path: "a-b/one.md"},
		{Type: LibraryTypeInstruction, Name: "a-b", Path: "a-b/two.md"},
	})
	if err == nil {
		t.Fatal("expected duplicate sanitized destination error")
	}
}

func TestEmptyProjectContentSnapshotRemovesOnlyMatchingFiles(t *testing.T) {
	project := createAPMProject(t, APMManifest{})
	dir := filepath.Join(project.Root, ".apm", "prompts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old.prompt.md", "manual.prompt.md", "keep.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before, err := snapshotProjectContentOutputs(project.Root, LibraryTypePrompt)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := projectContentSnapshot{typ: LibraryTypePrompt, root: project.Root, destinations: map[string][]byte{}, before: before}
	if err := snapshot.publish(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"old.prompt.md", "manual.prompt.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remains: %v", name, err)
		}
	}
	if got := readFile(t, filepath.Join(dir, "keep.json")); got != "keep.json" {
		t.Fatalf("unrelated file = %q", got)
	}
}

func TestProjectContentPublishRejectsConcurrentOutputChanges(t *testing.T) {
	project := createAPMProject(t, APMManifest{})
	dir := filepath.Join(project.Root, ".apm", "instructions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "manual.instructions.md")
	if err := os.WriteFile(output, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := snapshotProjectContentOutputs(project.Root, LibraryTypeInstruction)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := projectContentSnapshot{
		typ: LibraryTypeInstruction, root: project.Root,
		destinations: map[string][]byte{filepath.Join(dir, "selected.instructions.md"): []byte("selected")},
		before:       before,
	}
	if err := os.WriteFile(output, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.publish(); err == nil {
		t.Fatal("publish succeeded after matching output changed")
	}
	if got := readFile(t, output); got != "changed" {
		t.Fatalf("concurrent output = %q, want preserved changed bytes", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "selected.instructions.md")); !os.IsNotExist(err) {
		t.Fatalf("selected output was written after conflict: %v", err)
	}
}
