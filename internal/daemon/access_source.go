package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// checkSourceFiles refuses a compose file that names a file outside the
// app's source, before docker compose reads it. include, extends and
// env_file pull another file into the compose file, so /root/.aws/credentials
// would reach the app before the server could check what runs. A path with
// $ is refused too: the app's env, which the hub sets, would choose it.
func checkSourceFiles(src string) error {
	return checkFilesOf(resolved(src), composePath(src), 0)
}

func checkFilesOf(src, file string, depth int) error {
	if depth > 8 {
		return errors.New("compose files include each other too deep")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("compose.yml: %w", err)
	}
	var config struct {
		Include  []any `yaml:"include"`
		Services map[string]struct {
			Extends any `yaml:"extends"`
			EnvFile any `yaml:"env_file"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(body, &config); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(file), err)
	}
	dir := filepath.Dir(file)
	var included, named []string
	for _, item := range config.Include {
		if entry, ok := item.(map[string]any); ok {
			included = append(included, paths(entry["path"])...)
			named = append(named, paths(entry["env_file"])...)
			named = append(named, paths(entry["project_directory"])...)
			continue
		}
		included = append(included, paths(item)...)
	}
	for _, service := range config.Services {
		if extends, ok := service.Extends.(map[string]any); ok {
			included = append(included, paths(extends["file"])...)
		}
		named = append(named, paths(service.EnvFile)...)
	}
	for _, path := range append(named, included...) {
		if strings.Contains(path, "$") {
			return fmt.Errorf("%s: the path %s has a variable, so the server refuses it", filepath.Base(file), path)
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if !within(src, resolved(path)) && resolved(path) != src {
			return fmt.Errorf("%s: %s is not in the app's source, so the server refuses it", filepath.Base(file), path)
		}
	}
	for _, path := range included {
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if err := checkFilesOf(src, path, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// paths are the file paths of a compose setting: one path, a list of them,
// or a list of {path: ...}.
func paths(value any) []string {
	switch value := value.(type) {
	case string:
		return []string{value}
	case []any:
		var list []string
		for _, item := range value {
			if entry, ok := item.(map[string]any); ok {
				list = append(list, paths(entry["path"])...)
				continue
			}
			list = append(list, paths(item)...)
		}
		return list
	}
	return nil
}
