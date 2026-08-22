package cli

import (
	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

func newLibraryCommand(cfg commandConfig) *cobra.Command {
	command := &cobra.Command{
		Use:   "library",
		Short: "Manage the typed instill library catalogs",
	}
	command.AddCommand(newLibraryScanCommand(cfg))
	command.AddCommand(newLibraryAddCommand(cfg))
	command.AddCommand(newLibraryShowCommand(cfg))
	command.AddCommand(newLibraryUpdateCommand(cfg))
	return command
}

func newLibraryScanCommand(cfg commandConfig) *cobra.Command {
	var typ instill.LibraryType
	command := &cobra.Command{
		Use:   "scan",
		Short: "Scan the configured library and rebuild catalog CSVs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{
				Stdin:  cfg.stdin,
				Stderr: cfg.stderr,
			})
			if err != nil {
				return err
			}

			if typ != "" {
				return instill.ScanLibraryType(libraryPath, typ, cfg.stdout)
			}
			return instill.ScanLibrary(libraryPath, cfg.stdout)
		},
	}
	command.Flags().Var(newLibraryTypeValue(&typ), "type", "catalog type to scan")
	return command
}

func newLibraryAddCommand(cfg commandConfig) *cobra.Command {
	var entry instill.CatalogEntry
	var repository string

	command := &cobra.Command{
		Use:   "add",
		Short: "Add a catalog entry to the configured library",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if repository == "" && entry.Name == "" {
				return instill.NewExitError(instill.ExitGeneral, "error: required flag(s) \"name\" not set")
			}
			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{
				Stdin:  cfg.stdin,
				Stderr: cfg.stderr,
			})
			if err != nil {
				return err
			}

			if repository != "" {
				switch entry.Type {
				case instill.LibraryTypeSkill:
					if entry.Name != "" {
						return instill.NewExitError(instill.ExitGeneral, "error: --name is only used to select a remote plugin")
					}
					return instill.AddRemoteSkill(libraryPath, repository, cfg.runner)
				case instill.LibraryTypePlugin:
					return instill.AddRemotePlugin(libraryPath, repository, entry.Name, cfg.runner)
				default:
					return instill.NewExitError(instill.ExitGeneral, "error: --repository is only supported for skills and plugins")
				}
			}
			return instill.AddCatalogEntry(libraryPath, entry)
		},
	}

	command.Flags().Var(newLibraryTypeValue(&entry.Type), "type", "catalog type: skill, plugin, mcp, instruction, or prompt")
	command.Flags().StringVar(&entry.Name, "name", "", "entry name")
	command.Flags().StringVar(&entry.Category, "category", "", "skill or plugin category")
	command.Flags().StringVar(&entry.Path, "path", "", "relative content path")
	command.Flags().StringVar(&entry.Transport, "transport", "", "mcp transport")
	command.Flags().StringVar(&entry.Command, "command", "", "mcp command")
	command.Flags().StringSliceVar(&entry.Args, "args", nil, "mcp command arguments")
	command.Flags().StringVar(&entry.URL, "url", "", "mcp url")
	command.Flags().StringSliceVar(&entry.Env, "env", nil, "mcp environment entries")
	command.Flags().StringVar(&entry.ApplyTo, "apply-to", "", "instruction apply_to glob")
	command.Flags().StringVar(&entry.Description, "description", "", "entry description")
	command.Flags().StringVar(&repository, "repository", "", "GitHub owner/repo for a remote skill or plugin")
	_ = command.MarkFlagRequired("type")

	return command
}

func newLibraryUpdateCommand(cfg commandConfig) *cobra.Command {
	var typ instill.LibraryType
	var name string
	command := &cobra.Command{
		Use:   "update",
		Short: "Refresh a remote skill or plugin default-branch commit pin",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{Stdin: cfg.stdin, Stderr: cfg.stderr})
			if err != nil {
				return err
			}
			switch typ {
			case instill.LibraryTypeSkill:
				return instill.UpdateRemoteSkill(libraryPath, name, cfg.runner)
			case instill.LibraryTypePlugin:
				return instill.UpdateRemotePlugin(libraryPath, name, cfg.runner)
			default:
				return instill.NewExitError(instill.ExitGeneral, "error: update is only supported for skills and plugins")
			}
		},
	}
	command.Flags().Var(newLibraryTypeValue(&typ), "type", "catalog type: skill or plugin")
	command.Flags().StringVar(&name, "name", "", "remote skill or plugin name")
	_ = command.MarkFlagRequired("type")
	_ = command.MarkFlagRequired("name")
	return command
}

func newLibraryShowCommand(cfg commandConfig) *cobra.Command {
	var typ instill.LibraryType
	var filter string

	command := &cobra.Command{
		Use:   "show",
		Short: "Show typed catalog entries from the configured library",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{
				Stdin:  cfg.stdin,
				Stderr: cfg.stderr,
			})
			if err != nil {
				return err
			}

			return instill.ShowCatalog(libraryPath, typ, filter, cfg.stdout)
		},
	}

	command.Flags().Var(newLibraryTypeValue(&typ), "type", "catalog type: skill, plugin, mcp, instruction, or prompt")
	command.Flags().StringVar(&filter, "filter", "", "case-insensitive name substring")
	_ = command.MarkFlagRequired("type")

	return command
}

type libraryTypeValue struct {
	target *instill.LibraryType
}

func newLibraryTypeValue(target *instill.LibraryType) *libraryTypeValue {
	return &libraryTypeValue{target: target}
}

func (value *libraryTypeValue) String() string {
	if value == nil || value.target == nil {
		return ""
	}
	return string(*value.target)
}

func (value *libraryTypeValue) Set(raw string) error {
	typ := instill.LibraryType(raw)
	switch typ {
	case instill.LibraryTypeSkill, instill.LibraryTypePlugin, instill.LibraryTypeMCP, instill.LibraryTypeInstruction, instill.LibraryTypePrompt:
		*value.target = typ
		return nil
	default:
		return instill.NewExitError(instill.ExitGeneral, "error: invalid library type: "+raw)
	}
}

func (value *libraryTypeValue) Type() string {
	return "library-type"
}
