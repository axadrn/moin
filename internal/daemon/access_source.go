package daemon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// checkSourceFiles refuses a compose file that would read files of the
// server into the app.
//
//   - include, extends with a file and label_file read other files, which
//     could be anywhere on the server, so refuseIncludes refuses them before
//     compose reads the file. Put the services in one compose file.
//   - env_file is read when the containers start, so its path must be in
//     the source. compose itself says where it points, in config, the
//     compose file as moin keeps it (readComposeConfig): after ~ and the
//     project folder, which a second guess here would get wrong. A path
//     with ${VAR} in it would point elsewhere once the env changes, so it
//     is refused.
func checkSourceFiles(src string, config map[string]any) error {
	services, _ := config["services"].(map[string]any)
	for _, name := range sortedKeys(services) {
		service, _ := services[name].(map[string]any)
		files, ok := envFilePaths(service)
		if !ok {
			return fmt.Errorf("service %s: env_file is not a list of files, so the server refuses it", name)
		}
		for _, file := range files {
			if strings.Contains(file, "$") {
				return fmt.Errorf("service %s: env_file %s takes a variable, so the server refuses it: name the file", name, file)
			}
			if !within(resolved(src), resolved(file)) {
				return fmt.Errorf("service %s: env_file %s is not in the app's source, so the server refuses it", name, file)
			}
		}
	}
	return nil
}

// envFilePaths are the env files a service reads. compose writes each as
// {path, required}; moin adds the app's env file as a path. It reports
// false for any other shape, which the checks refuse.
func envFilePaths(service map[string]any) ([]string, bool) {
	value, found := service["env_file"]
	items, isList := value.([]any)
	if found && !isList {
		return nil, false
	}
	paths := []string{}
	for _, item := range items {
		switch file := item.(type) {
		case string:
			paths = append(paths, file)
		case map[string]any:
			path, ok := file["path"].(string)
			if !ok {
				return nil, false
			}
			paths = append(paths, path)
		default:
			return nil, false
		}
	}
	return paths, true
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
