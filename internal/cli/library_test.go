package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
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

func TestLibraryCommandShowPlugin(t *testing.T) {
	root := t.TempDir()
	requireNoError := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	requireNoError(instill.WriteCatalog(root, instill.LibraryTypePlugin, []instill.CatalogEntry{
		{
			Type:        instill.LibraryTypePlugin,
			Name:        "shortcuts-playground/claude",
			Path:        "shortcuts-playground/claude/.claude-plugin/plugin.json",
			Description: "Shortcuts playground",
		},
	}))
	t.Setenv("INSTILL_LIBRARY_PATH", root)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"library", "show", "--type", "plugin"},
		cwd:    t.TempDir(),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); got != "shortcuts-playground/claude\n1 entries\n" {
		t.Fatalf("stdout = %q, want plugin catalog listing", got)
	}
}
