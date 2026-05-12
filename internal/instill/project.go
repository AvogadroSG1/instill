package instill

import (
	"os"
	"path/filepath"
)

const (
	claudeDirName    = ".claude"
	manifestFileName = "skill-manifest.json"
	skillsDirName    = "skills"
)

type Project struct {
	Root         string
	ManifestPath string
	SymlinkDir   string
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
				Root:         root,
				ManifestPath: manifestPath,
				SymlinkDir:   filepath.Join(root, claudeDirName, skillsDirName),
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
