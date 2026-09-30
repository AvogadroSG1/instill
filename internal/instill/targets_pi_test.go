package instill

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectHarnessTargetsPiDirectory(t *testing.T) {
	root := t.TempDir()
	requireNoError(t, os.Mkdir(filepath.Join(root, ".pi"), 0700))
	requireEqual(t, []string{"agent-skills"}, DetectHarnessTargets(root))
	requireEqual(t, []string{"agent-skills"}, normalizeAPMProjectTargets(root, nil))
	requireNoError(t, os.Mkdir(filepath.Join(root, ".claude"), 0700))
	requireEqual(t, []string{"claude"}, DetectHarnessTargets(root))
	requireEqual(t, []string{"claude"}, normalizeAPMProjectTargets(root, []string{"pi"}))
	requireEqual(t, []string{}, normalizeAPMProjectTargets(root, nil))
}

func TestTargetsRejectExplicitPi(t *testing.T) {
	root := t.TempDir()
	library := t.TempDir()
	for _, callback := range []bool{false, true} {
		opts := InitProjectOptions{Root: root, LibraryPath: library, Targets: []string{"pi"}, Runner: func(string, ...string) ([]byte, error) { t.Fatal("APM ran for invalid target"); return nil, nil }}
		if callback {
			opts.Targets = nil
			opts.SelectTargets = func([]string) ([]string, error) { return []string{"pi"}, nil }
		}
		err := InitProject(opts)
		if err == nil || !strings.Contains(err.Error(), "activated by the .pi directory") {
			t.Fatalf("init invalid pi: %v", err)
		}
		assertPathMissing(t, ProjectAPMPath(root))
	}
	err := SetProjectTargets(SetTargetsOptions{Project: Project{Root: root, ManifestPath: ProjectAPMPath(root)}, Targets: []string{"pi"}})
	if err == nil || !strings.Contains(err.Error(), "activated by the .pi directory") {
		t.Fatalf("set invalid pi: %v", err)
	}
}

func TestTargetsLegacyPiMigration(t *testing.T) {
	for _, operation := range []string{"install", "compile", "sync", "pick", "selection"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			library := t.TempDir()
			requireNoError(t, os.Mkdir(filepath.Join(root, ".pi"), 0700))
			manifest := []byte("name: project\nversion: 0.1.0\ntargets: [pi]\nx-user: {keep: yes}\ndependencies: {apm: [], mcp: []}\n")
			requireNoError(t, os.WriteFile(ProjectAPMPath(root), manifest, 0640))
			project := Project{Root: root, ManifestPath: ProjectAPMPath(root), SymlinkDir: filepath.Join(root, ".claude", "skills"), AgentsSymlinkDir: filepath.Join(root, ".agents", "skills")}
			targets, err := GetProjectTargets(project)
			requireNoError(t, err)
			requireEqual(t, []string{"agent-skills"}, targets)
			if !bytes.Equal(manifest, []byte(readFile(t, project.ManifestPath))) {
				t.Fatal("GetProjectTargets wrote manifest")
			}
			runner := func(name string, args ...string) ([]byte, error) {
				if len(args) > 0 && args[0] == "--version" {
					return []byte("0.32.0"), nil
				}
				current, err := ReadAPMManifest(project.ManifestPath)
				requireNoError(t, err)
				requireEqual(t, []string{"agent-skills"}, current.Targets)
				return nil, nil
			}
			switch operation {
			case "install":
				err = RunAPMInstall(runner, root)
			case "compile":
				err = RunAPMCompile(runner, root)
			case "sync":
				err = SyncProject(SyncOptions{Project: project, LibraryPath: library, Runner: runner, Stdout: &bytes.Buffer{}})
			case "pick":
				writeTypedLibraryMarker(t, filepath.Join(library, "instructions", "probe", "INSTRUCTION.md"), "content")
				requireNoError(t, ScanLibraryType(library, LibraryTypeInstruction, nil))
				err = Pick(PickOptions{Project: project, LibraryPath: library, Type: LibraryTypeInstruction, Add: []string{"probe"}, Runner: runner})
			case "selection":
				err = ApplySkillSelection(SkillSelectionOptions{Project: project, LibraryPath: library, Runner: runner})
			}
			requireNoError(t, err)
			if !strings.Contains(readFile(t, project.ManifestPath), "keep: yes") {
				t.Fatal("migration dropped unrelated YAML")
			}
			current, err := ReadAPMManifest(project.ManifestPath)
			requireNoError(t, err)
			requireEqual(t, []string{"agent-skills"}, current.Targets)
		})
	}
}
