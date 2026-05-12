package instill

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const hookCommand = "instill check-skills"

// AddHooks adds the instill SessionStart hook to .claude/settings.json.
func AddHooks(project Project, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	path := filepath.Join(project.Root, claudeDirName, "settings.json")
	settings, mode, err := readSettingsTree(path)
	if err != nil {
		return err
	}
	if hasCommandHook(settings, hookCommand) {
		return writeLine(stdout, "already configured")
	}

	mergeCommandHook(settings)

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return NewExitError(ExitGeneral, "error: cannot encode settings: "+err.Error())
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data, mode); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot write settings.json: "+err.Error())
	}

	return writeLine(stdout, "added SessionStart hook: instill check-skills")
}

func readSettingsTree(path string) (map[string]any, os.FileMode, error) {
	mode := os.FileMode(0o644)
	info, err := os.Lstat(path) //nolint:gosec // Settings path is constrained to the selected project root.
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, 0, NewExitError(ExitFilesystem, "error: refusing to replace symlinked settings.json")
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return nil, 0, NewExitError(ExitFilesystem, "error: cannot inspect settings.json: "+err.Error())
	}

	data, err := os.ReadFile(path) //nolint:gosec // Settings path is constrained to the selected project root.
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, mode, nil
		}
		return nil, 0, NewExitError(ExitFilesystem, "error: cannot read settings.json: "+err.Error())
	}
	if len(data) == 0 {
		data = []byte("{}")
	}

	settings := map[string]any{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, 0, NewExitError(ExitGeneral, fmt.Sprintf("error: malformed settings.json: %v", err))
	}
	return settings, mode, nil
}

func hasCommandHook(settings map[string]any, command string) bool {
	for _, matcher := range sessionStartMatchers(settings) {
		hooks, _ := matcher["hooks"].([]any)
		for _, rawHook := range hooks {
			hook, ok := rawHook.(map[string]any)
			if !ok {
				continue
			}
			if hook["command"] == command {
				return true
			}
		}
	}
	return false
}

func mergeCommandHook(settings map[string]any) {
	hooks := hooksMap(settings)
	matchers := sessionStartMatchers(settings)

	for _, matcher := range matchers {
		if matcher["matcher"] == "" {
			matcher["hooks"] = append(hooksSlice(matcher), newCommandHook())
			hooks["SessionStart"] = matchers
			return
		}
	}

	hooks["SessionStart"] = append(matchers, map[string]any{
		"matcher": "",
		"hooks": []any{
			newCommandHook(),
		},
	})
}

func hooksMap(settings map[string]any) map[string]any {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}
	return hooks
}

func sessionStartMatchers(settings map[string]any) []map[string]any {
	rawMatchers, _ := hooksMap(settings)["SessionStart"].([]any)
	matchers := make([]map[string]any, 0, len(rawMatchers))
	for _, rawMatcher := range rawMatchers {
		matcher, ok := rawMatcher.(map[string]any)
		if ok {
			matchers = append(matchers, matcher)
		}
	}
	return matchers
}

func hooksSlice(matcher map[string]any) []any {
	hooks, _ := matcher["hooks"].([]any)
	if hooks == nil {
		hooks = []any{}
	}
	return hooks
}

func newCommandHook() map[string]any {
	return map[string]any{
		"command": hookCommand,
		"type":    "command",
	}
}
