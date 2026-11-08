package daemon

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// download fetches the source tarball, checks its SHA-256 and unpacks it into dir.
func download(ctx context.Context, start protocol.DeployStart, dir string) error {
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
	got := sha256.Sum256(body)
	if start.SHA256 != "" && hex.EncodeToString(got[:]) != start.SHA256 {
		return errors.New("download: the source does not match its SHA-256")
	}
	err = unpack(bytes.NewReader(body), dir, start.SourceStripTop)
	if err != nil {
		return err
	}
	if start.SourceDeleteURL != "" {
		// The server keeps the source now; the bucket does not need it.
		deletion, err := http.NewRequestWithContext(ctx, http.MethodDelete, start.SourceDeleteURL, nil)
		if err == nil {
			response, err := http.DefaultClient.Do(deletion)
			if err == nil {
				response.Body.Close()
			}
		}
	}
	return nil
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
