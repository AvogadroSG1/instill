package instill

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

const (
	envLibraryPath = "SKILL_LIBRARY_PATH"
	configDirName  = "instill"
	configFileName = "config.json"
)

type ConfigResolverOptions struct {
	Stdin  *os.File
	Stderr io.Writer
}

type configFile struct {
	LibraryPath string `json:"library_path"`
}

// ResolveLibraryPath resolves and validates the configured skill library path.
func ResolveLibraryPath(opts ConfigResolverOptions) (string, error) {
	if value := strings.TrimSpace(os.Getenv(envLibraryPath)); value != "" {
		return requireLibrary(value)
	}

	configPath, err := userConfigPath()
	if err != nil {
		return "", NewExitError(ExitEnvironment, fmt.Sprintf("error: cannot resolve config path: %v", err))
	}

	if path, ok, err := readConfigLibraryPath(configPath); err != nil {
		return "", err
	} else if ok {
		return requireLibrary(path)
	}

	if IsTerminal(opts.Stdin) {
		path, err := promptForLibraryPath(opts)
		if err != nil {
			return "", err
		}
		if err := writeConfig(configPath, path); err != nil {
			return "", NewExitError(ExitFilesystem, fmt.Sprintf("error: cannot write config: %v", err))
		}
		if _, err := fmt.Fprintf(opts.Stderr, "Config written: %s\n", configPath); err != nil {
			return "", NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
		}
		return requireLibrary(path)
	}

	return "", NewExitError(ExitEnvironment, "error: no library path configured; set SKILL_LIBRARY_PATH")
}

func readConfigLibraryPath(path string) (string, bool, error) {
	//nolint:gosec // Config path is resolved from the user's home directory by userConfigPath.
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, NewExitError(ExitEnvironment, fmt.Sprintf("error: cannot read config: %v", err))
	}

	var config configFile
	if err := json.Unmarshal(data, &config); err != nil {
		return "", false, NewExitError(ExitEnvironment, fmt.Sprintf("error: malformed config: %v", err))
	}
	if strings.TrimSpace(config.LibraryPath) == "" {
		return "", false, NewExitError(ExitEnvironment, "error: no library path configured; set SKILL_LIBRARY_PATH")
	}

	return config.LibraryPath, true, nil
}

func writeConfig(path string, libraryPath string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(configFile{LibraryPath: libraryPath}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return writeFileAtomic(path, data, 0o600)
}

func promptForLibraryPath(opts ConfigResolverOptions) (string, error) {
	defaultPath := defaultLibraryPath()
	if _, err := fmt.Fprintf(opts.Stderr, "Library path [%s]: ", defaultPath); err != nil {
		return "", NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
	}

	reader := bufio.NewReader(opts.Stdin)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", NewExitError(ExitEnvironment, fmt.Sprintf("error: cannot read library path: %v", err))
	}

	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultPath
	}

	return value, nil
}

func requireLibrary(path string) (string, error) {
	expanded, err := expandHome(path)
	if err != nil {
		return "", NewExitError(ExitEnvironment, fmt.Sprintf("error: invalid library path: %v", err))
	}

	info, err := os.Stat(expanded) //nolint:gosec // Library path is user-controlled configuration by design.
	if err != nil {
		if os.IsNotExist(err) {
			return "", NewExitError(ExitEnvironment, "error: library not found: "+expanded)
		}
		return "", NewExitError(ExitEnvironment, fmt.Sprintf("error: cannot read library: %v", err))
	}
	if !info.IsDir() {
		return "", NewExitError(ExitEnvironment, "error: library not found: "+expanded)
	}

	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", NewExitError(ExitEnvironment, fmt.Sprintf("error: invalid library path: %v", err))
	}

	return absolute, nil
}

func userConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".config", configDirName, configFileName), nil
}

func defaultLibraryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/ObsidianNotes/agent_config/skills"
	}

	return filepath.Join(home, "ObsidianNotes", "agent_config", "skills")
}

func expandHome(path string) (string, error) {
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}

	return path, nil
}

// IsTerminal reports whether file is an interactive terminal.
func IsTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}
