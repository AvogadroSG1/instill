// Package instill implements the core domain logic for managing project-specific
// AI coding skill libraries. It handles manifest read/write, skill library
// discovery, symlink reconciliation, and Claude Code hook injection. All
// functions accept explicit paths and writers — no direct os.Std* usage —
// so the package is fully testable without a real terminal or filesystem.
package instill

import (
	"os"
	"path/filepath"
	"sort"
)

const (
	claudeDirName         = ".claude"
	agentsDirName         = ".agents"
	manifestFileName      = "skill-manifest.json"
	settingsLocalFileName = "settings.local.json"
	skillsDirName         = "skills"
)

var harnessDetection = []struct {
	dir    string
	target string
}{
	{".claude", "claude"},
	{".codex", "codex"},
	{".cursor", "cursor"},
	{".gemini", "gemini"},
	{".kiro", "kiro"},
	{".opencode", "opencode"},
	{".windsurf", "windsurf"},
}

// DetectHarnessTargets scans root for harness directories and returns the
// corresponding APM target names. Returns nil when no harnesses are detected.
func DetectHarnessTargets(root string) []string {
	var targets []string
	for _, h := range harnessDetection {
		info, err := os.Stat(filepath.Join(root, h.dir))
		if err == nil && info.IsDir() {
			targets = append(targets, h.target)
		}
	}
	sort.Strings(targets)
	return targets
}

type Project struct {
	Root             string
	ManifestPath     string
	SymlinkDir       string // .claude/skills — Claude Code
	AgentsSymlinkDir string // .agents/skills — OpenAI Codex
}

// FindProject walks up from start until it finds a project manifest.
func FindProject(start string) (Project, bool, error) {
	return findProject(start, ProjectAPMPath)
}

// FindLegacyProject walks up from start until it finds a legacy JSON manifest.
func FindLegacyProject(start string) (Project, bool, error) {
	return findProject(start, ProjectManifestPath)
}

func ProjectManifestPath(root string) string {
	return filepath.Join(root, claudeDirName, manifestFileName)
}

func findProject(start string, manifestPathForRoot func(string) string) (Project, bool, error) {
	root, err := filepath.Abs(start)
	if err != nil {
		return Project{}, false, NewExitError(ExitGeneral, "error: cannot resolve project path: "+err.Error())
	}

	for {
		manifestPath := manifestPathForRoot(root)
		if _, err := os.Stat(manifestPath); err == nil {
			return Project{
				Root:             root,
				ManifestPath:     manifestPath,
				SymlinkDir:       filepath.Join(root, claudeDirName, skillsDirName),
				AgentsSymlinkDir: filepath.Join(root, agentsDirName, skillsDirName),
			}, true, nil
		} else if err != nil && !os.IsNotExist(err) {
			return Project{}, false, NewExitError(ExitFilesystem, "error: cannot read manifest: "+err.Error())
		}

		next := filepath.Dir(root)
		if next == root {
			return Project{}, false, nil
		}
		root = next
	}
}
