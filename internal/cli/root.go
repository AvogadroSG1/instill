// Package cli wires the instill Cobra command tree. Each sub-command receives
// a commandConfig that injects stdin, stdout, stderr, cwd, and optional TUI
// function so commands remain testable without a real terminal.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/AvogadroSG1/instill/internal/instill"
	"github.com/spf13/cobra"
)

type commandConfig struct {
	stdin        *os.File
	stdout       io.Writer
	stderr       io.Writer
	args         []string
	cwd          string
	runner       instill.CommandRunner
	isTTY        func(*os.File) bool
	pickTUI      func(instill.PickTUIOptions) error
	initPicker   func(instill.PickSkillsTUIOptions) (instill.InitialSkillSelectionPlan, bool, error)
	targetPicker func(instill.TargetPickerOptions) ([]string, bool, error)
}

// Execute is the entry point for the instill CLI. It runs the root Cobra
// command wired with os.Stdin/Stdout/Stderr and returns the process exit code.
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

	root.AddCommand(newAddHooksCommand(cfg))
	root.AddCommand(newBootstrapCommand(cfg))
	categorize := newCategorizeCommand(cfg)
	categorize.Hidden = true
	root.AddCommand(categorize)
	root.AddCommand(newImportCommand(cfg))
	root.AddCommand(newInitProjectCommand(cfg))
	root.AddCommand(newLibraryCommand(cfg))
	root.AddCommand(newPickSkillsCommand(cfg))
	root.AddCommand(newSyncCommand(cfg))
	root.AddCommand(newStatusCommand(cfg))
	root.AddCommand(newTargetsCommand(cfg))
	checkSkills := newCheckSkillsCommand(cfg)
	checkSkills.Hidden = true
	root.AddCommand(checkSkills)
	showLibrary := newShowLibraryCommand(cfg)
	showLibrary.Hidden = true
	root.AddCommand(showLibrary)
	return root
}
