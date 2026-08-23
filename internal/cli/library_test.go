package cli

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

const cliRemoteSHA = "3333333333333333333333333333333333333333"
const cliRefreshedSHA = "4444444444444444444444444444444444444444"

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

func TestLibraryAddRemotePlugin(t *testing.T) {
	for _, test := range []struct {
		name        string
		marketplace string
		args        []string
		wantName    string
	}{
		{name: "singleton", marketplace: `{"plugins":[{"name":"one","source":"one"}]}`, args: []string{"library", "add", "--type", "plugin", "--repository", "owner/repo"}, wantName: "one"},
		{name: "named multi", marketplace: `{"plugins":[{"name":"one","source":"one"},{"name":"two","source":"two"}]}`, args: []string{"library", "add", "--type", "plugin", "--repository", "owner/repo", "--name", "two"}, wantName: "two"},
	} {
		t.Run(test.name, func(t *testing.T) {
			library := t.TempDir()
			t.Setenv("INSTILL_LIBRARY_PATH", library)
			var stdout, stderr bytes.Buffer
			code := execute(commandConfig{stdout: &stdout, stderr: &stderr, args: test.args, runner: cliRemotePluginRunner(t, cliRemoteSHA, test.marketplace, test.wantName)})
			if code != 0 {
				t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
			}
			entries, err := instill.LoadCatalog(library, instill.LibraryTypePlugin)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name != test.wantName {
				t.Fatalf("entries = %#v, want plugin %q", entries, test.wantName)
			}
		})
	}
}

func TestLibraryUpdateRemotePlugin(t *testing.T) {
	library := t.TempDir()
	t.Setenv("INSTILL_LIBRARY_PATH", library)
	entry := instill.CatalogEntry{Type: instill.LibraryTypePlugin, Name: "plugin", Path: "plugin", Source: "git", Repository: "https://github.com/owner/repo.git", Ref: cliRemoteSHA}
	if err := instill.WriteCatalog(library, instill.LibraryTypePlugin, []instill.CatalogEntry{entry}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout, stderr: &stderr,
		args:   []string{"library", "update", "--type", "plugin", "--name", "plugin"},
		runner: cliRemotePluginRunner(t, cliRefreshedSHA, `{"plugins":[{"name":"plugin","source":"plugin"}]}`, "plugin"),
	})
	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	entries, err := instill.LoadCatalog(library, instill.LibraryTypePlugin)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Ref != cliRefreshedSHA {
		t.Fatalf("ref = %q, want %q", entries[0].Ref, cliRefreshedSHA)
	}

	stderr.Reset()
	code = execute(commandConfig{stdout: &stdout, stderr: &stderr, args: []string{"library", "update", "--type", "mcp", "--name", "plugin"}})
	if code == 0 || !strings.Contains(stderr.String(), "skills and plugins") {
		t.Fatalf("execute() = %d, stderr = %q, want unsupported type error", code, stderr.String())
	}

	stderr.Reset()
	code = execute(commandConfig{
		stdout: &stdout, stderr: &stderr,
		args:   []string{"library", "update", "--type", "plugin", "--name", "plugin"},
		runner: func(string, ...string) ([]byte, error) { return nil, errors.New("repository unavailable") },
	})
	if code == 0 || !strings.Contains(stderr.String(), "cannot access remote repository") {
		t.Fatalf("execute() = %d, stderr = %q, want actionable repository error", code, stderr.String())
	}
}

// TestLibraryAddRemoteSkillPropagatesCommandContext proves the ADR 0007 CLI
// wiring: the remote add command passes cmd.Context() into the domain, so an
// already-cancelled command context aborts the operation with
// context.Canceled instead of running to completion.
func TestLibraryAddRemoteSkillPropagatesCommandContext(t *testing.T) {
	library := t.TempDir()
	t.Setenv("INSTILL_LIBRARY_PATH", library)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout, stderr bytes.Buffer
	cfg := commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"library", "add", "--type", "skill", "--repository", "owner/example"},
		runner: func(name string, args ...string) ([]byte, error) {
			return []byte("0123456789abcdef0123456789abcdef01234567\tHEAD\n"), nil
		},
	}
	root := newRootCommand(cfg)
	err := root.ExecuteContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteContext() error = %v, want context.Canceled", err)
	}
	if instill.ExitCode(err) != instill.ExitGeneral {
		t.Fatalf("ExitCode(err) = %d, want %d", instill.ExitCode(err), instill.ExitGeneral)
	}
	entries, loadErr := instill.LoadCatalog(library, instill.LibraryTypeSkill)
	if loadErr != nil {
		t.Fatalf("LoadCatalog() error = %v", loadErr)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %#v, want no catalog mutation after cancellation", entries)
	}
}

func cliRemotePluginRunner(t *testing.T, sha, marketplace, pluginName string) instill.CommandRunner {
	t.Helper()
	return func(name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		switch {
		case command == "git ls-remote --symref https://github.com/owner/repo.git HEAD":
			return []byte(sha + "\tHEAD\n"), nil
		case strings.HasPrefix(command, "git init "):
			return nil, nil
		case strings.Contains(command, " remote add origin https://github.com/owner/repo.git"):
			return nil, nil
		case strings.Contains(command, " fetch --depth 1 origin "+sha):
			return nil, nil
		case strings.Contains(command, " ls-tree "+sha+" -- .claude-plugin/marketplace.json"):
			return []byte("100644 blob abc\t.claude-plugin/marketplace.json\n"), nil
		case strings.Contains(command, " cat-file -s "+sha+":.claude-plugin/marketplace.json"):
			return []byte(strconv.Itoa(len(marketplace))), nil
		case strings.Contains(command, " show "+sha+":.claude-plugin/marketplace.json"):
			return []byte(marketplace), nil
		case strings.Contains(command, " ls-tree "+sha+" -- ") && strings.HasSuffix(command, "/.claude-plugin/plugin.json"):
			path := command[strings.LastIndex(command, " -- ")+4:]
			return []byte("100644 blob def\t" + path + "\n"), nil
		case strings.Contains(command, " cat-file -s "+sha+":"):
			return []byte(strconv.Itoa(len(pluginName) + len(`{"name":""}`))), nil
		case strings.Contains(command, " ls-tree "+sha+" -- "):
			path := command[strings.LastIndex(command, " -- ")+4:]
			return []byte("040000 tree def\t" + path + "\n"), nil
		case strings.Contains(command, " show "+sha+":"):
			return []byte(`{"name":"` + pluginName + `"}`), nil
		default:
			return nil, errors.New("unexpected command: " + command)
		}
	}
}
