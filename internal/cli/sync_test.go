package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AvogadroSG1/instill/internal/instill"
)

func TestSyncCLIReportsSummary(t *testing.T) {
	root := createAPMProjectRoot(t, instill.APMManifest{
		Dependencies: instill.APMDependencies{
			APM: []string{"/library/skills/docker"},
			MCP: []instill.MCPDependency{{Name: "local-db", Command: "sqlite-mcp"}},
		},
	})
	if err := os.MkdirAll(filepath.Join(root, ".apm", "instructions"), 0o755); err != nil {
		t.Fatalf("MkdirAll(instructions) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".apm", "prompts"), 0o755); err != nil {
		t.Fatalf("MkdirAll(prompts) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".apm", "instructions", "python.instructions.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(instruction) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".apm", "prompts", "debug.prompt.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(prompt) error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := execute(commandConfig{
		stdout: &stdout,
		stderr: &stderr,
		args:   []string{"sync"},
		cwd:    root,
		runner: recordingRunner(nil),
	})

	if code != 0 {
		t.Fatalf("execute() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ok: synced 1 skills, 1 mcp servers, 1 instructions, 1 prompts") {
		t.Fatalf("stdout = %q, want sync summary", stdout.String())
	}
}
