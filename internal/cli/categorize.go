package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newCategorizeCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "categorize",
		Short: "Create or update the skill category registry",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{
				Stdin:  cfg.stdin,
				Stderr: cfg.stderr,
			})
			if err != nil {
				return err
			}

			return instill.CategorizeLibrary(instill.CategorizeOptions{
				LibraryPath: libraryPath,
				Stdout:      cfg.stdout,
			})
		},
	}
}
