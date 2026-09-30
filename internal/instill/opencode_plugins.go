package instill

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// openCodeManagedPrefix marks files instill owns in .opencode/plugins/. Following
// ADR 0001, ownership is by naming convention: instill writes only
// instill-<package>-<file> and reconciles only files carrying this prefix.
const openCodeManagedPrefix = "instill-"

// openCodePluginSource is an APM package whose opencode/plugins/ directory may
// hold OpenCode plugin files.
type openCodePluginSource struct {
	name string // package name used in the destination file name
	dir  string // package root on disk
}

// apmPackageDirs resolves each dependency to its on-disk package root:
//   - Local: "~/" expands to $HOME, a relative path joins projectRoot, and the
//     name is the directory base name.
//   - Git: only canonical GitHub repositories, materialized by APM at
//     projectRoot/apm_modules/<owner>/<repo>/<path>; the name is the directory
//     base name.
//
// Anything else is skipped.
func apmPackageDirs(projectRoot string, deps []APMDependency) []openCodePluginSource {
	sources := make([]openCodePluginSource, 0, len(deps))
	for _, dep := range deps {
		var dir string
		switch {
		case dep.Git != nil:
			if !canonicalGitHubRepository(dep.Git.Repository) {
				continue
			}
			ownerRepo := strings.TrimSuffix(strings.TrimPrefix(dep.Git.Repository, "https://github.com/"), ".git")
			dir = filepath.Join(projectRoot, "apm_modules", filepath.FromSlash(ownerRepo), filepath.FromSlash(normalizedGitPath(dep.Git.Path)))
		case dep.Local != "":
			dir = dep.Local
			if rest, ok := strings.CutPrefix(dir, "~/"); ok {
				home, err := os.UserHomeDir()
				if err != nil {
					continue
				}
				dir = filepath.Join(home, rest)
			} else if !filepath.IsAbs(dir) {
				dir = filepath.Join(projectRoot, dir)
			}
			dir = filepath.Clean(dir)
		default:
			continue
		}
		sources = append(sources, openCodePluginSource{name: filepath.Base(dir), dir: dir})
	}
	return sources
}

// openCodeTargetEnabled reports whether opencode is an APM target: the manifest
// targets when non-empty, else the harnesses detected under projectRoot.
func openCodeTargetEnabled(projectRoot string, targets []string) bool {
	if len(targets) == 0 {
		targets = DetectHarnessTargets(projectRoot)
	}
	return slices.Contains(targets, "opencode")
}

// reconcileOpenCodePlugins makes <projectRoot>/.opencode/plugins/instill-* exactly
// match the direct .ts/.js regular files under each source's opencode/plugins/
// directory, and returns the number of desired files. Files without the
// instill- prefix are never touched, and .opencode/plugins/ is created only when
// there is a file to write.
func reconcileOpenCodePlugins(projectRoot string, sources []openCodePluginSource) (int, error) {
	pluginsDir := filepath.Join(projectRoot, ".opencode", "plugins")
	desired := make(map[string]string) // destination -> source file
	for _, src := range sources {
		entries, err := os.ReadDir(filepath.Join(src.dir, "opencode", "plugins"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, NewExitError(ExitFilesystem, "error: cannot read opencode plugins: "+err.Error())
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() || !isOpenCodePluginFile(entry.Name()) {
				continue
			}
			dst := filepath.Join(pluginsDir, openCodeManagedPrefix+sanitizeContentName(src.name)+"-"+entry.Name())
			if _, exists := desired[dst]; exists {
				return 0, NewExitError(ExitGeneral, "error: opencode plugin name collision: "+dst)
			}
			desired[dst] = filepath.Join(src.dir, "opencode", "plugins", entry.Name())
		}
	}

	for dst, srcFile := range desired {
		same, err := sameFileContent(srcFile, dst)
		if err != nil {
			return 0, err
		}
		if same {
			continue
		}
		if err := copyFile(srcFile, dst); err != nil {
			return 0, err
		}
	}

	existing, err := os.ReadDir(pluginsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return len(desired), nil
	}
	if err != nil {
		return 0, NewExitError(ExitFilesystem, "error: cannot read opencode plugins: "+err.Error())
	}
	for _, entry := range existing {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, openCodeManagedPrefix) || !isOpenCodePluginFile(name) {
			continue
		}
		path := filepath.Join(pluginsDir, name)
		if _, keep := desired[path]; keep {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return 0, NewExitError(ExitFilesystem, "error: cannot remove opencode plugin: "+err.Error())
		}
	}
	return len(desired), nil
}

func isOpenCodePluginFile(name string) bool {
	ext := filepath.Ext(name)
	return ext == ".ts" || ext == ".js"
}

// sameFileContent reports whether dst already holds exactly the bytes of src.
// A missing or unreadable dst reports false so the caller rewrites it.
func sameFileContent(src, dst string) (bool, error) {
	want, err := os.ReadFile(src) //nolint:gosec // Source path comes from the project's APM dependencies.
	if err != nil {
		return false, NewExitError(ExitFilesystem, "error: cannot read opencode plugins: "+err.Error())
	}
	got, err := os.ReadFile(dst) //nolint:gosec // Destination is inside the project's .opencode/plugins.
	if err != nil {
		return false, nil
	}
	return bytes.Equal(want, got), nil
}
