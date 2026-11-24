package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newPickSkillsCommand(cfg commandConfig) *cobra.Command {
	var libraryType string
	var removeMode bool
	var overwriteMode bool

	command := &cobra.Command{
		Use:   "pick [name...]",
		Short: "Add or remove library entries from the project manifest",
	}

	command.RunE = func(_ *cobra.Command, args []string) error {
		if overwriteMode && len(args) > 0 {
			return instill.NewExitError(instill.ExitGeneral, "error: --overwrite requires the complete interactive picker; omit entry names")
		}

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
				Overwrite:   overwriteMode,
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
	command.Flags().BoolVar(&overwriteMode, "overwrite", false, "Replace the complete project selection and reset project targets, including manual entries")
	command.MarkFlagsMutuallyExclusive("remove", "overwrite")
	return command
}
