package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

type commandConfig struct {
	stdin         *os.File
	stdout        io.Writer
	stderr        io.Writer
	args          []string
	cwd           string
	isTTY         func(*os.File) bool
	pickSkillsTUI func(instill.PickSkillsTUIOptions) error
}

func Execute() int {
	return execute(commandConfig{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
	})
}

func execute(cfg commandConfig) int {
	root := newRootCommand(cfg)
	if err := root.Execute(); err != nil {
		code := instill.ExitCode(err)
		_, _ = fmt.Fprintln(cfg.stderr, instill.ErrorMessage(err))
		return code
	}

	return 0
}

func newRootCommand(cfg commandConfig) *cobra.Command {
	root := &cobra.Command{
		Use:           "instill",
		Short:         "Curate and sync a project-specific skill library",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(cfg.stdout)
	root.SetErr(cfg.stderr)
	if cfg.args != nil {
		root.SetArgs(cfg.args)
	}

	root.AddCommand(newCheckSkillsCommand(cfg))
	root.AddCommand(newAddHooksCommand(cfg))
	root.AddCommand(newInitProjectCommand(cfg))
	root.AddCommand(newPickSkillsCommand(cfg))
	root.AddCommand(newShowLibraryCommand(cfg))
	return root
}
