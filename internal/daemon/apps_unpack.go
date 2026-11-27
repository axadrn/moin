package daemon

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// unpackFile extracts the gzipped tar at path into dir.
func unpackFile(path, dir string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return unpack(file, dir)
}

// unpack extracts a gzipped tar of regular files and directories into dir.
// Links and paths that leave dir are refused.
func unpack(source io.Reader, dir string) error {
	archive, err := gzip.NewReader(source)
	if err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	reader := tar.NewReader(archive)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("unpack: %w", err)
		}
		target := filepath.Join(dir, filepath.FromSlash(header.Name))
		if !within(dir, target) {
			return fmt.Errorf("unpack: %s leaves the source directory", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			err = writeFile(target, reader, header.FileInfo().Mode())
		default:
			continue
		}
		if err != nil {
			return fmt.Errorf("unpack: %w", err)
		}
	}
}

func writeFile(path string, content io.Reader, mode fs.FileMode) error {
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, content)
	closeErr := file.Close()
	return errors.Join(err, closeErr)
}

// within reports whether path is dir or inside it.
func within(dir, path string) bool {
	relative, err := filepath.Rel(dir, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
