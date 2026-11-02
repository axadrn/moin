package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Pack writes dir as a gzipped tar. In a git repo it takes the files git
// knows plus untracked ones that are not ignored, so uncommitted changes
// deploy too. Elsewhere it takes everything but .git.
func Pack(dir string, out io.Writer) (files int, err error) {
	// A link to the project packs the project: the walk below does not
	// enter a linked directory.
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return 0, err
	}
	paths, err := sourceFiles(dir)
	if err != nil {
		return 0, err
	}
	compressed := gzip.NewWriter(out)
	archive := tar.NewWriter(compressed)
	for _, path := range paths {
		info, err := os.Lstat(filepath.Join(dir, path))
		if errors.Is(err, fs.ErrNotExist) {
			// Deleted but not yet committed.
			continue
		}
		if err != nil {
			return files, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		header := &tar.Header{Name: filepath.ToSlash(path), Mode: int64(info.Mode().Perm()), Size: info.Size(), ModTime: info.ModTime(), Typeflag: tar.TypeReg}
		err = archive.WriteHeader(header)
		if err != nil {
			return files, err
		}
		file, err := os.Open(filepath.Join(dir, path))
		if err != nil {
			return files, err
		}
		_, err = io.Copy(archive, file)
		file.Close()
		if err != nil {
			return files, err
		}
		files++
	}
	return files, errors.Join(archive.Close(), compressed.Close())
}

func sourceFiles(dir string) ([]string, error) {
	var stdout bytes.Buffer
	command := exec.Command("git", "-C", dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	command.Stdout = &stdout
	if command.Run() == nil {
		paths := []string{}
		for _, path := range strings.Split(stdout.String(), "\x00") {
			if path != "" {
				paths = append(paths, path)
			}
		}
		return paths, nil
	}
	paths := []string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			relative, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			paths = append(paths, relative)
		}
		return nil
	})
	return paths, err
}

// LocalConfig is what `ops deploy` reads from the repo before it uploads:
// the app and server names of the x-ops block. The agent reads the whole
// block on the server.
type LocalConfig struct {
	App    string
	Server string
}

var (
	xOpsBlock = regexp.MustCompile(`(?m)^x-ops:[ \t]*\n((?:[ \t]+.*\n?|[ \t]*\n)*)`)
	xOpsField = regexp.MustCompile(`(?m)^[ \t]+(app|server):[ \t]*["']?([A-Za-z0-9._-]+)["']?[ \t]*(?:#.*)?$`)
)

// ReadLocalConfig looks for x-ops.app and x-ops.server in the compose file of dir.
func ReadLocalConfig(dir string) LocalConfig {
	config := LocalConfig{}
	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		block := xOpsBlock.FindSubmatch(body)
		if block == nil {
			return config
		}
		for _, field := range xOpsField.FindAllSubmatch(block[1], -1) {
			switch string(field[1]) {
			case "app":
				config.App = string(field[2])
			case "server":
				config.Server = string(field[2])
			}
		}
		return config
	}
	return config
}

var unsafeName = regexp.MustCompile(`[^a-z0-9-]+`)

// AppName picks the app: the flag, x-ops.app in compose.yml, or the name of
// the directory.
func AppName(flagValue, dir string) string {
	name, _ := PickApp(flagValue, dir)
	return name
}

// PickApp is AppName that also says where the name came from: "--app",
// "x-ops.app in compose.yml" or "the directory name".
func PickApp(flagValue, dir string) (name, from string) {
	if flagValue != "" {
		return flagValue, "--app"
	}
	if config := ReadLocalConfig(dir); config.App != "" {
		return config.App, "x-ops.app in compose.yml"
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", ""
	}
	return strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(filepath.Base(absolute)), "-"), "-"), "the directory name"
}
