package instill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

func reconcileSettingsLocalPermissions(path string, previousSkills, finalSkills []string) (bool, error) {
	projectRoot := filepath.Dir(filepath.Dir(path))
	var changed bool
	err := withRootLocks(context.Background(), []string{projectRoot}, func(ctx context.Context, held *heldLocks) error {
		var err error
		changed, err = reconcileSettingsLocalPermissionsLocked(ctx, held, projectRoot, path, previousSkills, finalSkills)
		return err
	})
	return changed, err
}

func reconcileSettingsLocalPermissionsLocked(
	ctx context.Context,
	held *heldLocks,
	projectRoot string,
	path string,
	previousSkills []string,
	finalSkills []string,
) (bool, error) {
	if err := held.requireContext(ctx, projectRoot); err != nil {
		return false, err
	}
	emitMutationTestEvent("dependent-read:settings")
	settings, mode, existing, err := readSettingsLocalTree(path)
	if err != nil {
		return false, err
	}

	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		if _, exists := settings["permissions"]; exists {
			return false, NewExitError(ExitGeneral, "error: malformed settings.local.json: permissions must be an object")
		}
		permissions = map[string]any{}
		settings["permissions"] = permissions
	}
	allow, err := reconcileAllowPermissions(permissions["allow"], previousSkills, finalSkills)
	if err != nil {
		return false, err
	}
	// beforeSkills is read from the still-unmodified raw value; the call above
	// already proved it well-formed, so re-reading it here cannot fail.
	beforeSkills := skillPermissionEntries(rawAllowStrings(permissions["allow"]))
	afterSkills := skillPermissionEntries(allow)
	permissions["allow"] = allow

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, NewExitError(ExitGeneral, "error: cannot encode settings.local.json: "+err.Error())
	}
	data = append(data, '\n')
	if bytes.Equal(data, existing) {
		return false, nil
	}

	if err := ensureSettingsLocalDir(filepath.Dir(path)); err != nil {
		return false, err
	}
	if err := writeFileAtomic(path, data, mode); err != nil {
		return false, NewExitError(ExitFilesystem, "error: cannot write settings.local.json: "+err.Error())
	}
	// The file is normalized whenever its bytes differ, but the returned bool
	// reflects only a genuine Skill permission set change: a formatting-only
	// rewrite (different indent or key order from another tool) stays silent.
	return !maps.Equal(beforeSkills, afterSkills), nil
}

func readSettingsLocalTree(path string) (map[string]any, os.FileMode, []byte, error) {
	mode := os.FileMode(0o644)
	info, err := os.Lstat(path) //nolint:gosec // Settings path is constrained to the selected project root.
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, 0, nil, NewExitError(ExitFilesystem, "error: refusing to replace symlinked settings.local.json")
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return nil, 0, nil, NewExitError(ExitFilesystem, "error: cannot inspect settings.local.json: "+err.Error())
	}

	data, err := os.ReadFile(path) //nolint:gosec // Settings path is constrained to the selected project root.
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, mode, nil, nil
		}
		return nil, 0, nil, NewExitError(ExitFilesystem, "error: cannot read settings.local.json: "+err.Error())
	}
	if len(data) == 0 {
		data = []byte("{}")
	}

	settings := map[string]any{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, 0, nil, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed settings.local.json: %v", err))
	}
	if settings == nil {
		return nil, 0, nil, NewExitError(ExitGeneral, "error: malformed settings.local.json: expected object")
	}
	return settings, mode, data, nil
}

func ensureSettingsLocalDir(path string) error {
	//nolint:gosec // Project metadata directory must be user-accessible in the repository.
	if err := os.MkdirAll(path, 0o755); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot create .claude directory: "+err.Error())
	}

	info, err := os.Lstat(path)
	if err != nil {
		return NewExitError(ExitFilesystem, "error: cannot inspect .claude directory: "+err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return NewExitError(ExitFilesystem, "error: refusing to write through symlinked .claude directory")
	}
	if !info.IsDir() {
		return NewExitError(ExitFilesystem, "error: .claude path is not a directory")
	}
	return nil
}

func reconcileAllowPermissions(existing any, previousSkills, finalSkills []string) ([]string, error) {
	previousOwned := skillPermissionSet(previousSkills)
	finalOwned := skillPermissionSet(finalSkills)

	allow := []string{}
	seen := map[string]struct{}{}
	if existing != nil {
		values, ok := existing.([]any)
		if !ok {
			return nil, NewExitError(ExitGeneral, "error: malformed settings.local.json: permissions.allow must be an array")
		}
		for _, value := range values {
			entry, ok := value.(string)
			if !ok {
				return nil, NewExitError(
					ExitGeneral,
					"error: malformed settings.local.json: permissions.allow entries must be strings",
				)
			}
			if _, remove := previousOwned[entry]; remove {
				if _, keep := finalOwned[entry]; !keep {
					continue
				}
			}
			if _, ok := seen[entry]; ok {
				continue
			}
			seen[entry] = struct{}{}
			allow = append(allow, entry)
		}
	}

	for _, skill := range normalizeSkills(finalSkills) {
		entry := skillPermission(skill)
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		allow = append(allow, entry)
	}

	return allow, nil
}

// rawAllowStrings extracts permissions.allow entries as strings from the raw
// decoded JSON value. Callers MUST only use it after reconcileAllowPermissions
// has already validated the same value, so a malformed entry cannot occur
// here; anything unexpected is silently dropped rather than erroring twice.
func rawAllowStrings(existing any) []string {
	values, ok := existing.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if entry, ok := value.(string); ok {
			result = append(result, entry)
		}
	}
	return result
}

// skillPermissionEntries filters values down to the Skill(...) permission
// entries, as a set, so semantic-change comparison ignores formatting and the
// order or presence of unrelated manually-managed entries such as Bash(...).
func skillPermissionEntries(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		if strings.HasPrefix(v, "Skill(") {
			set[v] = struct{}{}
		}
	}
	return set
}

func skillPermissionSet(skills []string) map[string]struct{} {
	set := make(map[string]struct{}, len(skills)*2)
	for _, skill := range normalizeSkills(skills) {
		set[skillPermission(skill)] = struct{}{}
		// Also recognize the legacy slash format so it can be cleaned up on migration.
		if strings.Contains(skill, "/") {
			set["Skill("+skill+")"] = struct{}{}
		}
	}
	return set
}

func skillPermission(skill string) string {
	return "Skill(" + skillLinkName(skill) + ")"
}
