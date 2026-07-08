package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newBootstrapCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Ensure the required APM CLI is installed",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return instill.EnsureAPM(cfg.runner)
		},
	}
}
