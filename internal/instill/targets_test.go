package instill

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestSetProjectTargets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifestPath := filepath.Join(root, "apm.yml")
	writeAPMManifestForTest(t, manifestPath, APMManifest{
		Name:    "myproject",
		Version: "0.1.0",
		Targets: []string{"claude"},
	})

	project := Project{
		Root:         root,
		ManifestPath: manifestPath,
	}

	var stdout bytes.Buffer
	err := SetProjectTargets(SetTargetsOptions{
		Project: project,
		Targets: []string{"codex", "opencode"},
		Stdout:  &stdout,
	})
	if err != nil {
		t.Fatalf("SetProjectTargets() error = %v", err)
	}

	manifest, err := ReadAPMManifest(manifestPath)
	if err != nil {
		t.Fatalf("ReadAPMManifest() error = %v", err)
	}
	if len(manifest.Targets) != 2 || manifest.Targets[0] != "codex" || manifest.Targets[1] != "opencode" {
		t.Fatalf("manifest.Targets = %#v, want [codex, opencode]", manifest.Targets)
	}
	if got := stdout.String(); got != "ok: targets set to codex, opencode\n" {
		t.Fatalf("stdout = %q, want 'ok: targets set to codex, opencode\\n'", got)
	}

	targets, err := GetProjectTargets(project)
	if err != nil {
		t.Fatalf("GetProjectTargets() error = %v", err)
	}
	if len(targets) != 2 || targets[0] != "codex" || targets[1] != "opencode" {
		t.Fatalf("targets = %#v, want [codex, opencode]", targets)
	}
}
