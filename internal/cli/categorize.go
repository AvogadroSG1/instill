package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newCategorizeCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "categorize",
		Short: "Legacy command replaced by typed library catalogs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return instill.NewExitError(
				instill.ExitGeneral,
				"error: categorize has been replaced by typed library catalogs; run 'instill library scan'",
			)
		},
	}
}
