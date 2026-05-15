package instill

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestReconcileSettingsLocalPermissionsCreatesMissingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), claudeDirName, settingsLocalFileName)

	changed, err := reconcileSettingsLocalPermissions(path, nil, []string{"golang-testing"})
	if err != nil {
		t.Fatalf("reconcileSettingsLocalPermissions() error = %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true")
	}

	settings := readSettingsLocalForTest(t, path)
	allow := settings["permissions"].(map[string]any)["allow"].([]any)
	if got := stringSliceForTest(t, allow); !slices.Equal(got, []string{"Skill(golang-testing)"}) {
		t.Fatalf("permissions.allow = %#v, want Skill(golang-testing)", got)
	}
}

func TestReconcileSettingsLocalPermissionsInitializesMissingAllow(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), claudeDirName, settingsLocalFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"model":"sonnet","permissions":{"deny":["Bash(rm *)"]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(settings.local) error = %v", err)
	}

	changed, err := reconcileSettingsLocalPermissions(path, nil, []string{"golang-testing"})
	if err != nil {
		t.Fatalf("reconcileSettingsLocalPermissions() error = %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true")
	}

	settings := readSettingsLocalForTest(t, path)
	if settings["model"] != "sonnet" {
		t.Fatalf("model = %#v, want preserved", settings["model"])
	}
	permissions := settings["permissions"].(map[string]any)
	deny := permissions["deny"].([]any)
	if got := stringSliceForTest(t, deny); !slices.Equal(got, []string{"Bash(rm *)"}) {
		t.Fatalf("permissions.deny = %#v, want preserved", got)
	}
	got := stringSliceForTest(t, permissions["allow"].([]any))
	if !slices.Equal(got, []string{"Skill(golang-testing)"}) {
		t.Fatalf("permissions.allow = %#v, want initialized", got)
	}
}

func TestReconcileSettingsLocalPermissionsCreatesMissingPermissionsObject(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), claudeDirName, settingsLocalFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"model":"sonnet"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(settings.local) error = %v", err)
	}

	changed, err := reconcileSettingsLocalPermissions(path, nil, []string{"golang-testing"})
	if err != nil {
		t.Fatalf("reconcileSettingsLocalPermissions() error = %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true")
	}

	settings := readSettingsLocalForTest(t, path)
	if settings["model"] != "sonnet" {
		t.Fatalf("model = %#v, want preserved", settings["model"])
	}
	permissions := settings["permissions"].(map[string]any)
	got := stringSliceForTest(t, permissions["allow"].([]any))
	if !slices.Equal(got, []string{"Skill(golang-testing)"}) {
		t.Fatalf("permissions.allow = %#v, want initialized", got)
	}
}

func TestReconcileSettingsLocalPermissionsPreservesManualEntriesAndRevokesOwnedEntries(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), claudeDirName, settingsLocalFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{
  "env": {"KEEP": "true"},
  "permissions": {
    "allow": ["Skill(old-owned)", "Bash(go test ./...)", "Skill(manual-private)", "Skill(golang-testing)"],
    "ask": ["Bash(git push)"]
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(settings.local) error = %v", err)
	}

	changed, err := reconcileSettingsLocalPermissions(
		path,
		[]string{"old-owned", "golang-testing"},
		[]string{"golang-testing", "golang-security"},
	)
	if err != nil {
		t.Fatalf("reconcileSettingsLocalPermissions() error = %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true")
	}

	settings := readSettingsLocalForTest(t, path)
	if got := settings["env"].(map[string]any)["KEEP"]; got != "true" {
		t.Fatalf("env.KEEP = %#v, want preserved", got)
	}
	permissions := settings["permissions"].(map[string]any)
	if got := stringSliceForTest(t, permissions["ask"].([]any)); !slices.Equal(got, []string{"Bash(git push)"}) {
		t.Fatalf("permissions.ask = %#v, want preserved", got)
	}
	wantAllow := []string{
		"Bash(go test ./...)",
		"Skill(manual-private)",
		"Skill(golang-testing)",
		"Skill(golang-security)",
	}
	if got := stringSliceForTest(t, permissions["allow"].([]any)); !slices.Equal(got, wantAllow) {
		t.Fatalf("permissions.allow = %#v, want reconciled allow", got)
	}
}

func TestReconcileSettingsLocalPermissionsNoopsWhenContentUnchanged(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), claudeDirName, settingsLocalFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	initial := []byte(`{
  "permissions": {
    "allow": [
      "Skill(golang-testing)"
    ]
  }
}
`)
	if err := os.WriteFile(path, initial, 0o644); err != nil {
		t.Fatalf("WriteFile(settings.local) error = %v", err)
	}
	oldTime := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes(settings.local) error = %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(settings.local) before error = %v", err)
	}

	changed, err := reconcileSettingsLocalPermissions(path, []string{"golang-testing"}, []string{"golang-testing"})
	if err != nil {
		t.Fatalf("reconcileSettingsLocalPermissions() error = %v", err)
	}
	if changed {
		t.Fatalf("changed = true, want false")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(settings.local) after error = %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("settings.local modtime = %v, want unchanged %v", after.ModTime(), before.ModTime())
	}
	data, err := os.ReadFile(path) //nolint:gosec // Test reads t.TempDir settings file.
	if err != nil {
		t.Fatalf("ReadFile(settings.local) error = %v", err)
	}
	if !bytes.Equal(data, initial) {
		t.Fatalf("settings.local changed unexpectedly:\n%s", data)
	}
}

func TestReconcileSettingsLocalPermissionsRejectsSymlinkedClaudeDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll(target) error = %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, claudeDirName)); err != nil {
		t.Fatalf("Symlink(.claude) error = %v", err)
	}

	path := filepath.Join(root, claudeDirName, settingsLocalFileName)
	_, err := reconcileSettingsLocalPermissions(path, nil, []string{"golang-testing"})
	if err == nil {
		t.Fatal("reconcileSettingsLocalPermissions() error = nil, want symlink rejection")
	}
	if ExitCode(err) != ExitFilesystem {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitFilesystem)
	}
	if _, err := os.Stat(filepath.Join(target, settingsLocalFileName)); !os.IsNotExist(err) {
		t.Fatalf("outside settings.local.json exists; err = %v", err)
	}
}

func TestReconcileSettingsLocalPermissionsRejectsSymlinkedSettingsLocal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	claudePath := filepath.Join(root, claudeDirName)
	if err := os.MkdirAll(claudePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	target := filepath.Join(t.TempDir(), "settings.local.json")
	if err := os.WriteFile(target, []byte(`{"permissions":{"allow":[]}}`), 0o644); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	path := filepath.Join(claudePath, settingsLocalFileName)
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("Symlink(settings.local.json) error = %v", err)
	}

	_, err := reconcileSettingsLocalPermissions(path, nil, []string{"golang-testing"})
	if err == nil {
		t.Fatal("reconcileSettingsLocalPermissions() error = nil, want symlink rejection")
	}
	if ExitCode(err) != ExitFilesystem {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitFilesystem)
	}

	data, err := os.ReadFile(target) //nolint:gosec // Test reads t.TempDir target file.
	if err != nil {
		t.Fatalf("ReadFile(target) error = %v", err)
	}
	if string(data) != `{"permissions":{"allow":[]}}` {
		t.Fatalf("target settings.local.json = %q, want unchanged", data)
	}
}

func TestReconcileSettingsLocalPermissionsRejectsMalformedPermissionsShape(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), claudeDirName, settingsLocalFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"permissions":[]}`), 0o644); err != nil {
		t.Fatalf("WriteFile(settings.local) error = %v", err)
	}

	_, err := reconcileSettingsLocalPermissions(path, nil, []string{"golang-testing"})
	if err == nil {
		t.Fatal("reconcileSettingsLocalPermissions() error = nil, want malformed permissions error")
	}
	if ExitCode(err) != ExitGeneral {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitGeneral)
	}
}

func TestReconcileSettingsLocalPermissionsRejectsMalformedAllowEntries(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), claudeDirName, settingsLocalFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(.claude) error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"permissions":{"allow":[42]}}`), 0o644); err != nil {
		t.Fatalf("WriteFile(settings.local) error = %v", err)
	}

	_, err := reconcileSettingsLocalPermissions(path, nil, []string{"golang-testing"})
	if err == nil {
		t.Fatal("reconcileSettingsLocalPermissions() error = nil, want malformed allow error")
	}
	if ExitCode(err) != ExitGeneral {
		t.Fatalf("ExitCode(err) = %d, want %d", ExitCode(err), ExitGeneral)
	}
}

func readSettingsLocalForTest(t *testing.T, path string) map[string]any {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // Test reads t.TempDir settings file.
	if err != nil {
		t.Fatalf("ReadFile(settings.local) error = %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("Unmarshal(settings.local) error = %v\n%s", err, data)
	}
	return settings
}

func stringSliceForTest(t *testing.T, values []any) []string {
	t.Helper()

	result := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("value = %#v, want string", value)
		}
		result = append(result, text)
	}
	return result
}
