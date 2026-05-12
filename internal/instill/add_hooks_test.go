package instill

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddHooksCreatesSettings(t *testing.T) {
	t.Parallel()

	project := createProject(t, []string{"docker"})

	var stdout bytes.Buffer
	if err := AddHooks(project, &stdout); err != nil {
		t.Fatalf("AddHooks() error = %v", err)
	}
	if stdout.String() != "added SessionStart hook: instill check-skills\n" {
		t.Fatalf("stdout = %q, want added line", stdout.String())
	}

	settings := readSettingsForTest(t, filepath.Join(project.Root, ".claude", "settings.json"))
	hooks := settings["hooks"].(map[string]any)
	sessionStart := hooks["SessionStart"].([]any)
	if len(sessionStart) != 1 {
		t.Fatalf("SessionStart hooks = %#v, want one matcher", sessionStart)
	}
	matcher := sessionStart[0].(map[string]any)
	if matcher["matcher"] != "" {
		t.Fatalf("matcher = %#v, want empty matcher", matcher["matcher"])
	}
	entries := matcher["hooks"].([]any)
	if len(entries) != 1 {
		t.Fatalf("hooks = %#v, want one hook entry", entries)
	}
	hook := entries[0].(map[string]any)
	if hook["command"] != "instill check-skills" || hook["type"] != "command" {
		t.Fatalf("hook = %#v, want command hook", hook)
	}
}

func TestAddHooksIsIdempotent(t *testing.T) {
	t.Parallel()

	project := createProject(t, []string{"docker"})
	var stdout bytes.Buffer
	if err := AddHooks(project, &stdout); err != nil {
		t.Fatalf("AddHooks() first error = %v", err)
	}
	stdout.Reset()
	if err := AddHooks(project, &stdout); err != nil {
		t.Fatalf("AddHooks() second error = %v", err)
	}
	if stdout.String() != "already configured\n" {
		t.Fatalf("stdout = %q, want already configured", stdout.String())
	}

	data, err := os.ReadFile(filepath.Join(project.Root, ".claude", "settings.json")) //nolint:gosec // Test reads t.TempDir project file.
	if err != nil {
		t.Fatalf("ReadFile(settings) error = %v", err)
	}
	if strings.Count(string(data), hookCommand) != 1 {
		t.Fatalf("settings = %s, want one hook command", data)
	}
}

func TestAddHooksPreservesExistingSettings(t *testing.T) {
	t.Parallel()

	project := createProject(t, []string{"docker"})
	settingsPath := filepath.Join(project.Root, ".claude", "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{
  "permissions": {"allow": ["Bash(go test ./...)"]},
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo pre", "extra": true}]}],
    "SessionStart": [{"matcher": "", "extraMatcher": "keep", "hooks": [{"type": "command", "command": "echo existing", "extraHook": "keep"}]}]
  }
}`), 0o600); err != nil {
		t.Fatalf("WriteFile(settings) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := AddHooks(project, &stdout); err != nil {
		t.Fatalf("AddHooks() error = %v", err)
	}

	settings := readSettingsForTest(t, settingsPath)
	if _, ok := settings["permissions"]; !ok {
		t.Fatalf("settings = %#v, want permissions preserved", settings)
	}
	hooks := settings["hooks"].(map[string]any)
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Fatalf("settings hooks = %#v, want PreToolUse preserved", hooks)
	}
	sessionStart := hooks["SessionStart"].([]any)
	if len(sessionStart) != 1 {
		t.Fatalf("SessionStart = %#v, want merge into existing matcher", sessionStart)
	}
	matcher := sessionStart[0].(map[string]any)
	if matcher["extraMatcher"] != "keep" {
		t.Fatalf("matcher = %#v, want extra matcher field preserved", matcher)
	}
	entries := matcher["hooks"].([]any)
	if len(entries) != 2 {
		t.Fatalf("hooks = %#v, want existing plus instill hook", entries)
	}
	if entries[0].(map[string]any)["extraHook"] != "keep" {
		t.Fatalf("first hook = %#v, want extra hook field preserved", entries[0])
	}
}

func TestAddHooksPreservesExistingMode(t *testing.T) {
	t.Parallel()

	project := createProject(t, []string{"docker"})
	settingsPath := filepath.Join(project.Root, ".claude", "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("WriteFile(settings) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := AddHooks(project, &stdout); err != nil {
		t.Fatalf("AddHooks() error = %v", err)
	}

	info, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatalf("Stat(settings) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("settings mode = %o, want 0600", got)
	}
}

func TestAddHooksTreatsCommandOnlyAsAlreadyConfigured(t *testing.T) {
	t.Parallel()

	project := createProject(t, []string{"docker"})
	settingsPath := filepath.Join(project.Root, ".claude", "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"hooks":{"SessionStart":[{"matcher":"","hooks":[{"command":"instill check-skills"}]}]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(settings) error = %v", err)
	}

	var stdout bytes.Buffer
	if err := AddHooks(project, &stdout); err != nil {
		t.Fatalf("AddHooks() error = %v", err)
	}
	if stdout.String() != "already configured\n" {
		t.Fatalf("stdout = %q, want already configured", stdout.String())
	}
}

func TestAddHooksRejectsSymlinkedSettings(t *testing.T) {
	t.Parallel()

	project := createProject(t, []string{"docker"})
	settingsPath := filepath.Join(project.Root, ".claude", "settings.json")
	targetPath := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(targetPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	if err := os.Symlink(targetPath, settingsPath); err != nil {
		t.Fatalf("Symlink(settings) error = %v", err)
	}

	err := AddHooks(project, ioDiscard())
	if err == nil {
		t.Fatal("AddHooks() error = nil, want symlink rejection")
	}
	if ExitCode(err) != ExitFilesystem {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitFilesystem)
	}
	if info, statErr := os.Lstat(settingsPath); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("settings symlink changed; info = %v err = %v", info, statErr)
	}
}

func TestAddHooksMalformedSettingsExitsOne(t *testing.T) {
	t.Parallel()

	project := createProject(t, []string{"docker"})
	if err := os.WriteFile(filepath.Join(project.Root, ".claude", "settings.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile(settings) error = %v", err)
	}

	err := AddHooks(project, ioDiscard())
	if err == nil {
		t.Fatal("AddHooks() error = nil, want malformed settings")
	}
	if ExitCode(err) != ExitGeneral {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitGeneral)
	}
}

func readSettingsForTest(t *testing.T, path string) map[string]any {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // Test reads t.TempDir project file.
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("Unmarshal(settings) error = %v", err)
	}
	return settings
}
