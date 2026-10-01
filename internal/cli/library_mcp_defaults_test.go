package cli

import (
	"bytes"
	"github.com/AvogadroSG1/instill/internal/instill"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibraryCommandMCPDefaultEnabled(t *testing.T) {
	for _, flag := range []string{"", "--default-enabled=true", "--default-enabled=false"} {
		t.Run(flag, func(t *testing.T) {
			library := t.TempDir()
			t.Setenv("INSTILL_LIBRARY_PATH", library)
			args := []string{"library", "add", "--type", "mcp", "--name", "probe", "--transport", "stdio", "--command", "probe"}
			if flag != "" {
				args = append(args, flag)
			}
			var out, errs bytes.Buffer
			code := execute(commandConfig{args: args, stdout: &out, stderr: &errs, cwd: t.TempDir()})
			if code != 0 {
				t.Fatalf("add: %d %s", code, errs.String())
			}
			entries, err := instill.LoadCatalog(library, instill.LibraryTypeMCP)
			if err != nil {
				t.Fatal(err)
			}
			value := entries[0].DefaultEnabled
			if flag == "" {
				if value != nil {
					t.Fatal("omitted flag set default")
				}
			} else if value == nil || *value != (flag == "--default-enabled=true") {
				t.Fatalf("default %v for %q", value, flag)
			}
			if err := instill.ScanLibraryType(library, instill.LibraryTypeMCP, nil); err != nil {
				t.Fatal(err)
			}
			entries, err = instill.LoadCatalog(library, instill.LibraryTypeMCP)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatal("markerless addition did not survive scan")
			}
		})
	}
	library := t.TempDir()
	t.Setenv("INSTILL_LIBRARY_PATH", library)
	var out, errs bytes.Buffer
	code := execute(commandConfig{args: []string{"library", "add", "--type", "skill", "--name", "probe", "--path", "probe/SKILL.md", "--default-enabled=false"}, stdout: &out, stderr: &errs, cwd: t.TempDir()})
	if code == 0 || !strings.Contains(errs.String(), "--default-enabled is only supported for mcp") {
		t.Fatalf("non MCP: %d %s", code, errs.String())
	}
	if _, err := os.Stat(filepath.Join(library, "skills", "catalog.csv")); !os.IsNotExist(err) {
		t.Fatalf("invalid flag wrote catalog: %v", err)
	}
}
