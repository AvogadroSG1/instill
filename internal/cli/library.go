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
	var defaultEnabled bool

	command := &cobra.Command{
		Use:   "add",
		Short: "Add a catalog entry to the configured library",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("default-enabled") {
				if entry.Type != instill.LibraryTypeMCP {
					return instill.NewExitError(instill.ExitGeneral, "error: --default-enabled is only supported for mcp")
				}
				entry.DefaultEnabled = &defaultEnabled
			}
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
				if entry.Type != instill.LibraryTypeSkill {
					return instill.NewExitError(instill.ExitGeneral, "error: --repository is only supported for skills; add plugins with 'instill library add plugin-marketplace <owner/repo>'")
				}
				if entry.Name != "" {
					return instill.NewExitError(instill.ExitGeneral, "error: --name is not used with --repository")
				}
				return instill.AddRemoteSkill(cmd.Context(), libraryPath, repository, cfg.runner)
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
	command.Flags().StringSliceVar(&entry.Headers, "headers", nil, "mcp HTTP header entries")
	command.Flags().BoolVar(&defaultEnabled, "default-enabled", false, "initial MCP enabled state; existing server choices are preserved")
	command.Flags().StringVar(&entry.ApplyTo, "apply-to", "", "instruction apply_to glob")
	command.Flags().StringVar(&entry.Description, "description", "", "entry description")
	command.Flags().StringVar(&repository, "repository", "", "GitHub owner/repo for a remote skill")
	_ = command.MarkFlagRequired("type")
	command.AddCommand(newLibraryAddPluginMarketplaceCommand(cfg))

	return command
}

func newLibraryUpdateCommand(cfg commandConfig) *cobra.Command {
	var typ instill.LibraryType
	var name string
	command := &cobra.Command{
		Use:   "update",
		Short: "Refresh a remote skill default-branch commit pin",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{Stdin: cfg.stdin, Stderr: cfg.stderr})
			if err != nil {
				return err
			}
			switch typ {
			case instill.LibraryTypeSkill:
				return instill.UpdateRemoteSkill(cmd.Context(), libraryPath, name, cfg.runner)
			default:
				return instill.NewExitError(instill.ExitGeneral, "error: update --type is only supported for skills; refresh plugin marketplaces with 'instill library update plugin-marketplace <name>'")
			}
		},
	}
	command.Flags().Var(newLibraryTypeValue(&typ), "type", "catalog type: skill")
	command.Flags().StringVar(&name, "name", "", "remote skill name")
	_ = command.MarkFlagRequired("type")
	_ = command.MarkFlagRequired("name")
	command.AddCommand(newLibraryUpdatePluginMarketplaceCommand(cfg))
	return command
}

func newLibraryAddPluginMarketplaceCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "plugin-marketplace <owner/repo>",
		Short: "Download a GitHub plugin marketplace into the library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{Stdin: cfg.stdin, Stderr: cfg.stderr})
			if err != nil {
				return err
			}
			return instill.AddPluginMarketplace(cmd.Context(), libraryPath, args[0], cfg.runner, cfg.stdout)
		},
	}
}

func newLibraryUpdatePluginMarketplaceCommand(cfg commandConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "plugin-marketplace <name>",
		Short: "Download the latest commit of a library plugin marketplace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			libraryPath, err := instill.ResolveLibraryPath(instill.ConfigResolverOptions{Stdin: cfg.stdin, Stderr: cfg.stderr})
			if err != nil {
				return err
			}
			return instill.UpdatePluginMarketplace(cmd.Context(), libraryPath, args[0], cfg.runner, cfg.stdout)
		},
	}
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
