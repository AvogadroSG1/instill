package cli

import (
	"strings"

	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newInitProjectCommand(cfg commandConfig) *cobra.Command {
	var force bool
	var skillsCSV string

	command := &cobra.Command{
		Use:   "init-project",
		Short: "Initialize an instill manifest in the current project",
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

			skills := parseCSV(skillsCSV)
			if len(skills) == 0 && (force || !instill.HasManifest(cwd)) {
				isTTY := cfg.isTTY
				if isTTY == nil {
					isTTY = instill.IsTerminal
				}
				if !isTTY(cfg.stdin) {
					return instill.NewExitError(instill.ExitEnvironment, "error: pick-skills TUI requires a terminal")
				}
			}

			runPicker := cfg.pickSkillsTUI
			if runPicker == nil {
				runPicker = instill.RunPickSkillsTUI
			}

			return instill.InitProject(instill.InitProjectOptions{
				Root:        cwd,
				LibraryPath: libraryPath,
				Skills:      skills,
				Force:       force,
				Stdout:      cfg.stdout,
				SelectSkills: func(project instill.Project) error {
					if len(skills) > 0 {
						return nil
					}
					return runPicker(instill.PickSkillsTUIOptions{
						Project:     project,
						LibraryPath: libraryPath,
						Stdin:       cfg.stdin,
						Stdout:      cfg.stdout,
						Stderr:      cfg.stderr,
					})
				},
			})
		},
	}

	command.Flags().BoolVar(&force, "force", false, "overwrite an existing manifest")
	command.Flags().StringVar(&skillsCSV, "skills", "", "comma-separated skills to add without launching the TUI")
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
