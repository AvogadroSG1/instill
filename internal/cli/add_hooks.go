package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newAddHooksCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "add-hooks",
		Short: "Add the instill SessionStart hook to Claude Code settings",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			isTTY := cfg.isTTY
			if isTTY == nil {
				isTTY = instill.IsTerminal
			}
			if !isTTY(cfg.stdin) {
				return nil
			}

			cwd := cfg.cwd
			if cwd == "" {
				cwd = "."
			}
			project, found, err := instill.FindProject(cwd)
			if err != nil {
				return err
			}
			if !found {
				return instill.NewExitError(instill.ExitGeneral, "error: no manifest found — run 'instill init' first")
			}
			if _, err := instill.ReadManifest(project.ManifestPath); err != nil {
				return err
			}

			return instill.AddHooks(project, cfg.stdout)
		},
	}
}
