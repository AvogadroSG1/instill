// Package instill implements the core domain logic for managing project-specific
// AI coding skill libraries. It handles manifest read/write, skill library
// discovery, symlink reconciliation, and Claude Code hook injection. All
// functions accept explicit paths and writers — no direct os.Std* usage —
// so the package is fully testable without a real terminal or filesystem.
package instill

import (
	"os"
	"path/filepath"
)

const (
	claudeDirName         = ".claude"
	agentsDirName         = ".agents"
	manifestFileName      = "skill-manifest.json"
	settingsLocalFileName = "settings.local.json"
	skillsDirName         = "skills"
)

type Project struct {
	Root             string
	ManifestPath     string
	SymlinkDir       string // .claude/skills — Claude Code
	AgentsSymlinkDir string // .agents/skills — OpenAI Codex
}

// FindProject walks up from start until it finds a project manifest.
func FindProject(start string) (Project, bool, error) {
	root, err := filepath.Abs(start)
	if err != nil {
		return Project{}, false, NewExitError(ExitGeneral, "error: cannot resolve project path: "+err.Error())
	}

	for {
		manifestPath := filepath.Join(root, claudeDirName, manifestFileName)
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
