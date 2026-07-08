package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newPickSkillsCommand(cfg commandConfig) *cobra.Command {
	var libraryType string
	var removeMode bool

	command := &cobra.Command{
		Use:   "pick [name...]",
		Short: "Add or remove library entries from the project manifest",
	}

	command.RunE = func(_ *cobra.Command, args []string) error {
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

		typ := instill.LibraryType(libraryType)
		if typ == "" {
			typ = instill.LibraryTypeSkill
		}
		if len(args) == 0 && !removeMode {
			runPicker := cfg.pickTUI
			if runPicker == nil {
				runPicker = instill.RunPickTUI
			}
			return runPicker(instill.PickTUIOptions{
				Project:     project,
				LibraryPath: libraryPath,
				InitialType: typ,
				Stdin:       cfg.stdin,
				Stdout:      cfg.stdout,
				Stderr:      cfg.stderr,
				Runner:      cfg.runner,
			})
		}

		add := args
		remove := []string{}
		if removeMode {
			add = nil
			remove = args
		}
		return instill.Pick(instill.PickOptions{
			Project:     project,
			LibraryPath: libraryPath,
			Add:         add,
			Remove:      remove,
			Type:        typ,
			Runner:      cfg.runner,
			Stdout:      cfg.stdout,
		})
	}

	command.Flags().StringVar(&libraryType, "type", string(instill.LibraryTypeSkill), "library type to modify")
	command.Flags().BoolVar(&removeMode, "remove", false, "remove the named entries")
	return command
}
