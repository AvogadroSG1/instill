package cli

import (
	"strings"

	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newInitProjectCommand(cfg commandConfig) *cobra.Command {
	var force bool
	var skillsCSV string
	var targetsCSV string

	command := &cobra.Command{
		Use:   "init",
		Short: "Initialize an instill manifest in the current project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd := cfg.cwd
			if cwd == "" {
				cwd = "."
			}
			if instill.HasAPMManifest(cwd) && !force {
				return instill.NewExitError(instill.ExitGeneral, "error: manifest already exists; use --force to reinitialize")
			}

			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{
				Stdin:  cfg.stdin,
				Stderr: cfg.stderr,
			})
			if err != nil {
				return err
			}

			isTTY := cfg.isTTY
			if isTTY == nil {
				isTTY = instill.IsTerminal
			}

			targetsFlagProvided := cmd.Flags().Changed("targets")
			targets := parseCSV(targetsCSV)

			skills := parseCSV(skillsCSV)
			if len(skills) == 0 && (force || !instill.HasAPMManifest(cwd)) {
				if !isTTY(cfg.stdin) {
					return instill.NewExitError(instill.ExitEnvironment, "error: pick TUI requires a terminal")
				}
			}

			runTargetPicker := cfg.targetPicker
			if runTargetPicker == nil {
				runTargetPicker = instill.RunTargetPickerTUI
			}

			var selectTargets func([]string) ([]string, error)
			if !targetsFlagProvided && isTTY(cfg.stdin) {
				selectTargets = func(detected []string) ([]string, error) {
					selected, confirmed, err := runTargetPicker(instill.TargetPickerOptions{
						Available: instill.DefaultAvailableTargets,
						Selected:  detected,
						Stdin:     cfg.stdin,
						Stdout:    cfg.stdout,
						Stderr:    cfg.stderr,
						IsTTY:     isTTY,
					})
					if err != nil {
						return nil, err
					}
					if !confirmed {
						return nil, instill.NewExitError(instill.ExitGeneral, "initialization cancelled")
					}
					return selected, nil
				}
			}

			return instill.InitProject(instill.InitProjectOptions{
				Root:          cwd,
				LibraryPath:   libraryPath,
				Skills:        skills,
				Targets:       targets,
				TargetsSet:    targetsFlagProvided,
				Force:         force,
				Runner:        cfg.runner,
				Stdout:        cfg.stdout,
				SelectTargets: selectTargets,
				SelectSkills: func() (instill.InitialSkillSelectionPlan, bool, error) {
					if len(skills) > 0 {
						return instill.InitialSkillSelectionPlan{Skills: skills}, true, nil
					}
					runPicker := cfg.initPicker
					if runPicker == nil {
						runPicker = instill.SelectInitialSkillsTUI
					}
					return runPicker(instill.PickSkillsTUIOptions{
						LibraryPath: libraryPath,
						Stdin:       cfg.stdin,
						Stdout:      cfg.stdout,
						Stderr:      cfg.stderr,
						Runner:      cfg.runner,
						IsTTY:       isTTY,
					})
				},
			})
		},
	}

	command.Flags().BoolVar(&force, "force", false, "overwrite an existing manifest")
	command.Flags().StringVar(&skillsCSV, "skills", "", "comma-separated skills to add without launching the TUI")
	command.Flags().StringVar(&targetsCSV, "targets", "", "comma-separated target agents to configure without prompting")
	return command
}

func parseCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}

	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}
