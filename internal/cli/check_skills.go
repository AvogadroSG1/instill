package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newCheckSkillsCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "check-skills",
		Short: "Deprecated legacy command; use instill sync",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return instill.NewExitError(
				instill.ExitGeneral,
				"error: check-skills has been replaced by 'instill sync'; run 'instill sync' to update APM-managed project content",
			)
		},
	}
}
