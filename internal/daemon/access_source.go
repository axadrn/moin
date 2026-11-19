package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

// checkSourceFiles refuses a compose file that would read files of the
// server into the app before the server sees what runs.
//
//   - include, extends with a file and label_file read other files, which
//     could be anywhere on the server, so they are refused. Put the services
//     in one compose file.
//   - env_file is read into the compose file, so its path must be in the
//     source. compose itself says where it points: after ~, ${VAR} and the
//     project folder, which a second guess here would get wrong.
//
// envFiles are the --env-file arguments the deploy reads the file with.
func (a *apps) checkSourceFiles(ctx context.Context, app, src string, envFiles []string) error {
	if err := refuseIncludes(composePath(src)); err != nil {
		return err
	}
	args := append([]string{"compose", "-p", project(app), "--project-directory", src, "-f", composePath(src)}, envFiles...)
	raw, err := a.output(ctx, "docker", append(args, "config", "--format", "json", "--no-env-resolution")...)
	if err != nil {
		return fmt.Errorf("compose.yml: %w", err)
	}
	var config struct {
		Services map[string]struct {
			EnvFile []struct {
				Path string `json:"path"`
			} `json:"env_file"`
		} `json:"services"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return fmt.Errorf("compose config: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(config.Services)) {
		for _, file := range config.Services[name].EnvFile {
			if !within(resolved(src), resolved(file.Path)) {
				return fmt.Errorf("service %s: env_file %s is not in the app's source, so the server refuses it", name, file.Path)
			}
		}
	}
	return nil
}

// refuseIncludes refuses include, extends with a file and label_file, in any
// document of the compose file.
func refuseIncludes(file string) error {
	reader, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("compose.yml: %w", err)
	}
	defer reader.Close()
	decoder := yaml.NewDecoder(reader)
	for {
		var document struct {
			Include  any `yaml:"include"`
			Services map[string]struct {
				Extends   any `yaml:"extends"`
				LabelFile any `yaml:"label_file"`
			} `yaml:"services"`
		}
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("compose.yml: %w", err)
		}
		if document.Include != nil {
			return errors.New("compose.yml: include is not allowed, so the server refuses it: put the services in one compose file")
		}
		for name, service := range document.Services {
			if extends, ok := service.Extends.(map[string]any); ok && extends["file"] != nil {
				return fmt.Errorf("compose.yml: service %s extends another file, so the server refuses it: extend a service of the same file", name)
			}
			if service.LabelFile != nil {
				return fmt.Errorf("compose.yml: service %s has a label_file, so the server refuses it: write the labels in the compose file", name)
			}
		}
	}
}
