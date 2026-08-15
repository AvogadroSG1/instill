package cli

import (
	"fmt"

	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newTargetsCommand(cfg commandConfig) *cobra.Command {
	var setFlag string

	command := &cobra.Command{
		Use:   "targets [agents...]",
		Short: "View or configure project target agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd := cfg.cwd
			if cwd == "" {
				cwd = "."
			}

			project, found, err := instill.FindProject(cwd)
			if err != nil {
				return err
			}
			if !found {
				return instill.NewExitError(instill.ExitGeneral, "error: no instill project found; run 'instill init' first")
			}

			var targets []string
			hasExplicitTargets := false

			if cmd.Flags().Changed("set") {
				targets = parseCSV(setFlag)
				hasExplicitTargets = true
			} else if len(args) > 0 {
				targets = args
				hasExplicitTargets = true
			}

			if hasExplicitTargets {
				return instill.SetProjectTargets(instill.SetTargetsOptions{
					Project: project,
					Targets: targets,
					Stdout:  cfg.stdout,
				})
			}

			isTTY := cfg.isTTY
			if isTTY == nil {
				isTTY = instill.IsTerminal
			}

			if isTTY(cfg.stdin) {
				currentTargets, err := instill.GetProjectTargets(project)
				if err != nil {
					return err
				}

				runTargetPicker := cfg.targetPicker
				if runTargetPicker == nil {
					runTargetPicker = instill.RunTargetPickerTUI
				}

				selected, confirmed, err := runTargetPicker(instill.TargetPickerOptions{
					Available: instill.DefaultAvailableTargets,
					Selected:  currentTargets,
					Stdin:     cfg.stdin,
					Stdout:    cfg.stdout,
					Stderr:    cfg.stderr,
					IsTTY:     isTTY,
				})
				if err != nil {
					return err
				}
				if !confirmed {
					return nil
				}

				return instill.SetProjectTargets(instill.SetTargetsOptions{
					Project: project,
					Targets: selected,
					Stdout:  cfg.stdout,
				})
			}

			currentTargets, err := instill.GetProjectTargets(project)
			if err != nil {
				return err
			}

			if len(currentTargets) == 0 {
				_, _ = fmt.Fprintln(cfg.stdout, "no targets configured")
				return nil
			}

			for _, t := range currentTargets {
				_, _ = fmt.Fprintln(cfg.stdout, t)
			}
			return nil
		},
	}

	command.Flags().StringVar(&setFlag, "set", "", "comma-separated targets to configure")
	return command
}
