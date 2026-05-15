package instill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// InitProjectOptions configures project initialization.
type InitProjectOptions struct {
	Root         string
	LibraryPath  string
	Skills       []string
	Force        bool
	Stdout       io.Writer
	SelectSkills func(Project) error
}

// InitProject initializes an instill manifest, skill symlink directory, and gitignore entry.
func InitProject(opts InitProjectOptions) error {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return NewExitError(ExitGeneral, "error: cannot resolve project path: "+err.Error())
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	project := Project{
		Root:         root,
		ManifestPath: filepath.Join(root, claudeDirName, manifestFileName),
		SymlinkDir:   filepath.Join(root, claudeDirName, skillsDirName),
	}

	if HasManifest(root) && !opts.Force {
		return NewExitError(ExitGeneral, "error: manifest already exists; use --force to reinitialize")
	}

	manifest := Manifest{Skills: []string{}}
	if len(opts.Skills) > 0 {
		normalized := normalizeSkills(opts.Skills)
		if err := ValidateSkillNames(normalized); err != nil {
			return err
		}
		for _, skill := range normalized {
			exists, err := SkillExists(opts.LibraryPath, skill)
			if err != nil {
				return err
			}
			if !exists {
				return NewExitError(
					ExitGeneral,
					"error: unknown skill: "+skill+" - run 'instill show-library' to see available skills",
				)
			}
		}
		manifest = Manifest{Skills: normalized}
	}

	warning := ""
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		if !os.IsNotExist(err) {
			return NewExitError(ExitFilesystem, "error: cannot inspect git repository: "+err.Error())
		}
		warning = "warning:     not a git repository — manifest will not be committed"
	}

	if err := os.MkdirAll(filepath.Dir(project.ManifestPath), 0o755); err != nil { //nolint:gosec // Project metadata directory must be user-accessible in the repository.
		return NewExitError(ExitFilesystem, "error: cannot create .claude directory: "+err.Error())
	}
	if err := WriteManifestAtomic(project.ManifestPath, manifest); err != nil {
		return err
	}
	if err := writeLine(stdout, "initialized: .claude/skill-manifest.json"); err != nil {
		return err
	}

	if err := os.MkdirAll(project.SymlinkDir, 0o755); err != nil { //nolint:gosec // Project symlink directory must be user-accessible in the repository.
		return NewExitError(ExitFilesystem, "error: cannot create .claude/skills: "+err.Error())
	}
	if err := writeLine(stdout, "created:     .claude/skills/"); err != nil {
		return err
	}

	changed, err := ensureGitignoreEntry(root, ".claude/skills/")
	if err != nil {
		return err
	}
	if changed {
		if err := writeLine(stdout, "updated:     .gitignore (+.claude/skills/)"); err != nil {
			return err
		}
	}
	changed, err = ensureGitignoreEntry(root, ".claude/settings.local.json")
	if err != nil {
		return err
	}
	if changed {
		if err := writeLine(stdout, "updated:     .gitignore (+.claude/settings.local.json)"); err != nil {
			return err
		}
	}

	if warning != "" {
		if err := writeLine(stdout, warning); err != nil {
			return err
		}
	}

	if len(opts.Skills) == 0 {
		if opts.SelectSkills != nil {
			return opts.SelectSkills(project)
		}
		if opts.Force {
			return ReconcileManifest(project, manifest, opts.LibraryPath, stdout)
		}
		return writeLine(stdout, "use pick-skills to add skills")
	}

	return ReconcileManifest(project, manifest, opts.LibraryPath, stdout)
}

func ensureGitignoreEntry(root, entry string) (bool, error) {
	path := filepath.Join(root, ".gitignore")
	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return false, NewExitError(ExitFilesystem, "error: refusing to replace symlinked .gitignore")
	}
	if err != nil && !os.IsNotExist(err) {
		return false, NewExitError(ExitFilesystem, "error: cannot inspect .gitignore: "+err.Error())
	}

	data, err := os.ReadFile(path) //nolint:gosec // Path is constrained to the selected project root.
	if err != nil && !os.IsNotExist(err) {
		return false, NewExitError(ExitFilesystem, "error: cannot read .gitignore: "+err.Error())
	}
	if containsLine(string(data), entry) {
		return false, nil
	}

	output := append([]byte{}, data...)
	if len(output) > 0 && output[len(output)-1] != '\n' {
		output = append(output, '\n')
	}
	if len(output) == 0 || !strings.Contains(string(output), "# instill — managed symlinks, do not commit\n") {
		output = append(output, []byte("# instill — managed symlinks, do not commit\n")...)
	}
	output = append(output, []byte(entry+"\n")...)
	if err := writeFileAtomic(path, output, 0o644); err != nil {
		return false, NewExitError(ExitFilesystem, "error: cannot write .gitignore: "+err.Error())
	}
	return true, nil
}

func containsLine(data string, line string) bool {
	for existing := range strings.Lines(data) {
		normalized := strings.TrimSuffix(strings.TrimSuffix(existing, "\n"), "\r")
		if normalized == line || normalized == "/"+line {
			return true
		}
	}
	return false
}

func writeLine(writer io.Writer, line string) error {
	if _, err := fmt.Fprintln(writer, line); err != nil {
		return NewExitError(ExitFilesystem, "error: cannot write output: "+err.Error())
	}
	return nil
}
