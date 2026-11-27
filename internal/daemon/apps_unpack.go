package daemon

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/axadrn/moin/protocol"
)

// download fetches the GitHub tarball of a commit and unpacks it into dir.
// A source from anywhere else is refused.
func download(ctx context.Context, start protocol.DeployStart, dir string) error {
	if _, err := sourceName(start.SourceURL); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, start.SourceURL, nil)
	if err != nil {
		return err
	}
	if start.SourceAuthorization != "" {
		request.Header.Set("Authorization", start.SourceAuthorization)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSourceSize+1))
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if len(body) > maxSourceSize {
		return errors.New("download: the source is larger than 200 MB")
	}
	return unpack(bytes.NewReader(body), dir, start.SourceStripTop)
}

// unpackFile extracts the gzipped tar at path into dir.
func unpackFile(path, dir string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return unpack(file, dir, false)
}

// unpack extracts a gzipped tar of regular files and directories into dir.
// Links and paths that leave dir are refused. stripTop drops the first
// folder of every path, which GitHub wraps a repo in.
func unpack(source io.Reader, dir string, stripTop bool) error {
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
		name := header.Name
		if stripTop {
			_, name, _ = strings.Cut(name, "/")
			if name == "" || header.Typeflag == tar.TypeXGlobalHeader {
				continue
			}
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
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
