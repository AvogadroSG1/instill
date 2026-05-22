package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newPickSkillsCommand(cfg commandConfig) *cobra.Command {
	var removeCSV string

	command := &cobra.Command{
		Use:   "pick-skills [skill-name...]",
		Short: "Add or remove skills from the project manifest",
		RunE: func(_ *cobra.Command, args []string) error {
			cwd := cfg.cwd
			if cwd == "" {
				cwd = "."
			}

			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{
				Stdin:  cfg.stdin,
				Stderr: cfg.stderr,
			})
			if err != nil {
				return err
			}

			project, found, err := instill.FindProject(cwd)
			if err != nil {
				return err
			}
			if !found {
				return instill.NewExitError(instill.ExitGeneral, "error: no manifest found — run 'instill init' first")
			}

			if len(args) == 0 && len(parseCSV(removeCSV)) == 0 {
				runPicker := cfg.pickSkillsTUI
				if runPicker == nil {
					runPicker = instill.RunPickSkillsTUI
				}
				return runPicker(instill.PickSkillsTUIOptions{
					Project:     project,
					LibraryPath: libraryPath,
					Stdin:       cfg.stdin,
					Stdout:      cfg.stdout,
					Stderr:      cfg.stderr,
				})
			}

			return instill.PickSkills(instill.PickSkillsOptions{
				Project:     project,
				LibraryPath: libraryPath,
				Add:         args,
				Remove:      parseCSV(removeCSV),
				Stdout:      cfg.stdout,
			})
		},
	}

	command.Flags().StringVar(&removeCSV, "remove", "", "comma-separated skills to remove")
	return command
}
