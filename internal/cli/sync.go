package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newSyncCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Sync the project through APM",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
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
			return instill.SyncProject(instill.SyncOptions{
				Project:     project,
				LibraryPath: libraryPath,
				Runner:      cfg.runner,
				Stdout:      cfg.stdout,
			})
		},
	}
}
