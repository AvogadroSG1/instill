package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestLibraryCommandHelpListsSubcommands(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"library", "--help"},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}

	help := stdout.String()
	for _, command := range []string{"scan", "add", "show"} {
		if !strings.Contains(help, command) {
			t.Fatalf("help = %q, want subcommand %q", help, command)
		}
	}
}

func TestLibraryCommandShowSkipsAPMBootstrap(t *testing.T) {
	library := createCatalogLibrary(t, cliCatalogLibrarySeed{
		skills: []catalogFixture{{
			typ:  "skill",
			name: "docker",
			path: "docker/SKILL.md",
		}},
	})
	t.Setenv("INSTILL_LIBRARY_PATH", library)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"library", "show", "--type", "skill"},
		cwd:    t.TempDir(),
		runner: func(name string, args ...string) ([]byte, error) {
			t.Fatalf("runner called unexpectedly: %s %v", name, args)
			return nil, nil
		},
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); got != "docker\n1 entries\n" {
		t.Fatalf("stdout = %q, want skill catalog listing", got)
	}
}
