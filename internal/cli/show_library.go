package cli

import (
	"bytes"
	"strings"

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

			var manifest *instill.Manifest
			if found {
				current, err := instill.ReadManifest(project.ManifestPath)
				if err != nil {
					return err
				}
				var reconcileOutput bytes.Buffer
				if err := instill.ReconcileManifest(project, current, libraryPath, &reconcileOutput); err != nil {
					return err
				}
				for line := range strings.Lines(reconcileOutput.String()) {
					if strings.HasPrefix(line, "removed: ") {
						if _, err := cfg.stdout.Write([]byte(line)); err != nil {
							return instill.NewExitError(instill.ExitFilesystem, "error: cannot write output: "+err.Error())
						}
					}
				}
				current, err = instill.ReadManifest(project.ManifestPath)
				if err != nil {
					return err
				}
				manifest = &current
			}

			return instill.ShowLibrary(instill.ShowLibraryOptions{
				LibraryPath: libraryPath,
				Manifest:    manifest,
				Filter:      filter,
				Category:    category,
				Stdout:      cfg.stdout,
			})
		},
	}

	command.Flags().StringVar(&filter, "filter", "", "case-insensitive skill name substring")
	command.Flags().StringVar(&category, "category", "", "category path prefix")
	return command
}
