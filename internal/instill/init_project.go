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
	SelectSkills  func() (InitialSkillSelectionPlan, bool, error)
}

// InitialSkillSelectionPlan describes the manifest-owned result of init selection.
type InitialSkillSelectionPlan struct {
	Skills []string
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
	} else if opts.SelectSkills != nil {
		plan, confirmed, selectErr := opts.SelectSkills()
		if selectErr != nil {
			return selectErr
		}
		if !confirmed {
			return NewExitError(ExitGeneral, "initialization cancelled")
		}
		dependencies, resolveErr := resolveSkillDependencies(opts.LibraryPath, plan.Skills)
		if resolveErr != nil {
			return resolveErr
		}
		manifest.Dependencies.APM = dependencies
	}

	if err := EnsureAPM(opts.Runner); err != nil {
		return err
	}
	document, err := loadManifestDocument(project.ManifestPath)
	if err != nil {
		return err
	}
	skills, plugins, err := loadTypedPackageCatalogs(opts.LibraryPath)
	if err != nil {
		return err
	}
	catalogDependencies := make([]APMDependency, 0, len(skills)+len(plugins))
	for _, entry := range skills {
		catalogDependencies = append(catalogDependencies, skillDependencyFromCatalog(opts.LibraryPath, entry))
	}
	for _, entry := range plugins {
		catalogDependencies = append(catalogDependencies, pluginDependencyFromCatalog(opts.LibraryPath, entry))
	}
	ownership := ownershipForDependencies(catalogDependencies, []string{
		filepath.Join(opts.LibraryPath, "skills"),
		filepath.Join(opts.LibraryPath, "plugins"),
	})
	if _, err := document.dependencySequence("apm", true); err != nil {
		return err
	}
	if err := document.mutateAPM(manifest.Dependencies.APM, ownership); err != nil {
		return err
	}
	mcpCatalog, err := LoadCatalog(opts.LibraryPath, LibraryTypeMCP)
	if err != nil {
		return err
	}
	if _, err := document.dependencySequence("mcp", true); err != nil {
		return err
	}
	if err := document.mutateMCP([]MCPDependency{}, catalogEntryNamesSet(mcpCatalog)); err != nil {
		return err
	}
	if err := document.setTargets(manifest.Targets, false); err != nil {
		return err
	}
	if err := document.repairIdentity(root, opts.Force); err != nil {
		return err
	}
	if err := document.write(); err != nil {
		return err
	}

	if len(manifest.Dependencies.APM) == 0 {
		return nil
	}
	return RunAPMInstall(opts.Runner, root)
}

func HasAPMManifest(root string) bool {
	_, err := ReadAPMManifest(ProjectAPMPath(root))
	return err == nil
}
