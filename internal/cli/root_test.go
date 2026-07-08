package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandHelpListsAPMCommandSurface(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"--help"},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}

	help := stdout.String()
	for _, command := range []string{"pick", "sync", "status", "library", "import", "bootstrap", "add-hooks"} {
		if !strings.Contains(help, command) {
			t.Fatalf("help = %q, want command %q", help, command)
		}
	}
	for _, legacy := range []string{"categorize", "check-skills", "pick-skills", "show-library"} {
		if strings.Contains(help, legacy) {
			t.Fatalf("help = %q, must not list legacy command %q as primary", help, legacy)
		}
	}
}
