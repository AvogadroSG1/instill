package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newShowLibraryCommand(cfg commandConfig) *cobra.Command {
	var filter string
	var category string

	command := &cobra.Command{
		Use:   "show-library",
		Short: "List available skills in the configured library",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return instill.NewExitError(
				instill.ExitGeneral,
				"error: show-library has been replaced by 'instill library show'; run 'instill sync' to update project state",
			)
		},
	}

	command.Flags().StringVar(&filter, "filter", "", "case-insensitive skill name substring")
	command.Flags().StringVar(&category, "category", "", "category path prefix")
	return command
}
