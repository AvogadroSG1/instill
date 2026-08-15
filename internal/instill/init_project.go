package instill

import (
	"io"
	"path/filepath"
)

// InitProjectOptions configures project initialization.
type InitProjectOptions struct {
	Root          string
	LibraryPath   string
	Skills        []string
	Targets       []string
	TargetsSet    bool
	Force         bool
	Runner        CommandRunner
	Stdout        io.Writer
	SelectTargets func(detected []string) ([]string, error)
	SelectSkills  func(Project) error
}

// InitProject initializes apm.yml for the selected project.
func InitProject(opts InitProjectOptions) error {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return NewExitError(ExitGeneral, "error: cannot resolve project path: "+err.Error())
	}
	project := Project{
		Root:             root,
		ManifestPath:     ProjectAPMPath(root),
		SymlinkDir:       filepath.Join(root, claudeDirName, skillsDirName),
		AgentsSymlinkDir: filepath.Join(root, agentsDirName, skillsDirName),
	}

	if HasAPMManifest(root) && !opts.Force {
		return NewExitError(ExitGeneral, "error: manifest already exists; use --force to reinitialize")
	}

	manifest := APMManifest{Name: filepath.Base(root), Version: "0.1.0"}
	if opts.TargetsSet || len(opts.Targets) > 0 {
		manifest.Targets = normalizeStringSlice(opts.Targets)
	} else if opts.SelectTargets != nil {
		selected, err := opts.SelectTargets(DetectHarnessTargets(root))
		if err != nil {
			return err
		}
		manifest.Targets = normalizeStringSlice(selected)
	} else {
		manifest.Targets = DetectHarnessTargets(root)
	}
	if len(opts.Skills) > 0 {
		dependencies, err := resolveSkillDependencies(opts.LibraryPath, opts.Skills)
		if err != nil {
			return err
		}
		manifest.Dependencies.APM = dependencies
	}

	if err := EnsureAPM(opts.Runner); err != nil {
		return err
	}
	if err := WriteAPMManifestAtomic(project.ManifestPath, manifest); err != nil {
		return err
	}

	if len(opts.Skills) == 0 {
		if opts.SelectSkills != nil {
			return opts.SelectSkills(project)
		}
		return nil
	}
	return RunAPMInstall(opts.Runner, root)
}

func HasAPMManifest(root string) bool {
	_, err := ReadAPMManifest(ProjectAPMPath(root))
	return err == nil
}
