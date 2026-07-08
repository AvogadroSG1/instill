package cli

import (
	"path/filepath"

	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newImportCommand(cfg commandConfig) *cobra.Command {
	command := &cobra.Command{
		Use:   "import",
		Short: "Import existing skill and MCP state into instill catalogs",
	}
	command.AddCommand(newImportOldInstillCommand(cfg))
	command.AddCommand(newImportGraftCommand(cfg))
	command.AddCommand(newImportClaudeCommand(cfg))
	command.AddCommand(newImportDirectoryCommand(cfg))
	return command
}

func newImportOldInstillCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "old-instill",
		Short: "Import a legacy instill project manifest into apm.yml",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			libraryPath, project, err := importContext(cfg)
			if err != nil {
				return err
			}
			if err := instill.EnsureAPM(cfg.runner); err != nil {
				return err
			}
			return instill.ImportOldInstill(instill.ImportOptions{
				Project:     project,
				LibraryPath: libraryPath,
			})
		},
	}
}

func newImportGraftCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "graft",
		Short: "Import graft MCP state into apm.yml and the library catalog",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			libraryPath, project, err := importContext(cfg)
			if err != nil {
				return err
			}
			if err := instill.EnsureAPM(cfg.runner); err != nil {
				return err
			}
			return instill.ImportGraft(instill.ImportOptions{
				Project:     project,
				LibraryPath: libraryPath,
			})
		},
	}
}

func newImportClaudeCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "claude",
		Short: "Import Claude MCP configuration into the library catalog",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			libraryPath, _, err := importContext(cfg)
			if err != nil {
				return err
			}
			if err := instill.EnsureAPM(cfg.runner); err != nil {
				return err
			}
			return instill.ImportClaude(instill.ImportOptions{LibraryPath: libraryPath})
		},
	}
}

func newImportDirectoryCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "directory <path>",
		Short: "Scan an existing content directory and write instill catalogs",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			libraryPath, _, err := importContext(cfg)
			if err != nil {
				return err
			}
			if err := instill.EnsureAPM(cfg.runner); err != nil {
				return err
			}
			return instill.ImportDirectory(instill.ImportDirectoryOptions{
				LibraryPath: libraryPath,
				Path:        args[0],
			})
		},
	}
}

func importContext(cfg commandConfig) (string, instill.Project, error) {
	libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{
		Stdin:  cfg.stdin,
		Stderr: cfg.stderr,
	})
	if err != nil {
		return "", instill.Project{}, err
	}

	cwd := cfg.cwd
	if cwd == "" {
		cwd = "."
	}
	root, err := filepath.Abs(cwd)
	if err != nil {
		return "", instill.Project{}, instill.NewExitError(instill.ExitGeneral, "error: cannot resolve project path: "+err.Error())
	}
	return libraryPath, instill.Project{
		Root:             root,
		ManifestPath:     instill.ProjectAPMPath(root),
		SymlinkDir:       filepath.Join(root, ".claude", "skills"),
		AgentsSymlinkDir: filepath.Join(root, ".agents", "skills"),
	}, nil
}
