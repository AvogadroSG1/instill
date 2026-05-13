package cli

import (
	"fmt"
	"io"

	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newCheckSkillsCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "check-skills",
		Short: "Reconcile project skill symlinks with the manifest",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd := cfg.cwd
			if cwd == "" {
				cwd = "."
			}

			project, found, err := instill.FindProject(cwd)
			if err != nil {
				return err
			}
			if !found {
				return nil
			}

			manifest, err := instill.ReadManifest(project.ManifestPath)
			if err != nil {
				return err
			}

			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{
				Stdin:  cfg.stdin,
				Stderr: cfg.stderr,
			})
			if err != nil {
				return err
			}

			if err := instill.ReconcileManifest(project, manifest, libraryPath, cfg.stdout); err != nil {
				return err
			}
			return warnUncategorizedSkills(libraryPath, cfg.stdout)
		},
	}
}

func warnUncategorizedSkills(libraryPath string, stdout io.Writer) error {
	if !instill.CategoryRegistryExists(libraryPath) {
		return nil
	}

	skills, err := instill.ListLibrarySkills(libraryPath)
	if err != nil {
		return err
	}
	categories := instill.LoadCategoriesWithWarnings(libraryPath, nil)
	for _, skill := range skills {
		if instill.CategoryForSkill(categories, skill) != "" {
			continue
		}
		if _, err := fmt.Fprintf(stdout, "uncategorized: %s\n", skill); err != nil {
			return instill.NewExitError(instill.ExitFilesystem, "error: cannot write output: "+err.Error())
		}
	}
	return nil
}
