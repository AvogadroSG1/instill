package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddHooksCLINoTTYIsSilentSuccess(t *testing.T) {
	root := createProject(t, []string{"docker"})
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("Open(os.DevNull) error = %v", err)
	}
	t.Cleanup(func() {
		if err := stdin.Close(); err != nil {
			t.Fatalf("Close(stdin) error = %v", err)
		}
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdin:  stdin,
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"add-hooks"},
		cwd:    root,
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Fatalf("stdout = %q stderr = %q, want silence", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("settings.json exists after no-tty no-op; err = %v", err)
	}
}

func TestAddHooksCLINoManifestExitsOneWhenTTY(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdin:  nil,
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"add-hooks"},
		cwd:    t.TempDir(),
		isTTY: func(*os.File) bool {
			return true
		},
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "error: no manifest found — run 'instill init' first") {
		t.Fatalf("stderr = %q, want no manifest message", stderr.String())
	}
}

func TestAddHooksCLIMalformedManifestExitsOneWhenTTY(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "skill-manifest.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"add-hooks"},
		cwd:    root,
		isTTY: func(*os.File) bool {
			return true
		},
	})

	if code != 1 {
		t.Fatalf("execute() = %d, want 1; stderr = %q", code, stderr.String())
	}
}
