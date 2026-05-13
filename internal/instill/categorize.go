package instill

import (
	"io"
	"strings"
)

type CategorizeOptions struct {
	LibraryPath string
	Stdout      io.Writer
}

func CategorizeLibrary(opts CategorizeOptions) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	skills, err := ListLibrarySkills(opts.LibraryPath)
	if err != nil {
		return err
	}
	categories := map[string][]string{}
	if CategoryRegistryExists(opts.LibraryPath) {
		categories, err = LoadCategoriesStrict(opts.LibraryPath)
		if err != nil {
			return NewExitError(ExitGeneral, "error: cannot load category registry: "+err.Error())
		}
	}
	for _, skill := range skills {
		if CategoryForSkill(categories, skill) != "" {
			continue
		}
		category := autoCategoryForSkill(skill)
		if category == "" {
			if err := writeLine(stdout, "uncategorized: "+skill); err != nil {
				return err
			}
			continue
		}
		categories[category] = append(categories[category], skill)
	}

	if err := WriteCategoriesAtomic(opts.LibraryPath, categories); err != nil {
		return err
	}
	return nil
}

func autoCategoryForSkill(skill string) string {
	switch {
	case strings.HasPrefix(skill, "golang-"):
		return "golang"
	case strings.HasPrefix(skill, "azure-"):
		return "cloud/azure"
	case strings.HasPrefix(skill, "dd-"):
		return "datadog"
	case strings.HasPrefix(skill, "k8s-"), skill == "docker":
		return "cloud"
	default:
		return ""
	}
}
