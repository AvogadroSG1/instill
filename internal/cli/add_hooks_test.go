package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

func TestAddHooksCLINoTTYIsSilentSuccess(t *testing.T) {
	root := createAPMProjectRoot(t, instill.APMManifest{})
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

func TestAddHooksCLIAPMProjectWritesSyncHookWhenTTY(t *testing.T) {
	root := createAPMProjectRoot(t, instill.APMManifest{})
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

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("ReadFile(settings.json) error = %v", err)
	}
	if !strings.Contains(string(data), "instill sync") {
		t.Fatalf("settings.json = %q, want instill sync hook", string(data))
	}
}

func TestAddHooksCLIMalformedManifestExitsOneWhenTTY(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "apm.yml"), []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile(apm.yml) error = %v", err)
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
