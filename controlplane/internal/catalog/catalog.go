// Package catalog is the list of open source apps ops installs in one
// click. Each entry is a folder in content/catalog: app.md with what the
// app is, compose.yml with how it runs, and an optional logo.svg. They ship
// with ops, so a self-hosted control plane has the same catalog. Versions
// come from the image's registry, asked by every control plane itself.
package catalog

import (
	"errors"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/markdown"
)

// Entry is one app of the catalog.
type Entry struct {
	Slug     string
	Name     string
	Pitch    string
	Category string
	Website  string
	// Source is the app's own repository, such as github.com/umami-software/umami.
	Source  string
	License string
	// Image is the image the version picks the tag of, without a tag.
	Image string
	// Database is postgres, sqlite or empty: what ops backs up.
	Database string
	// Port is what the app listens on.
	Port int
	// Filter picks the tags that are releases; empty takes x.y.z, with or
	// without a v.
	Filter *regexp.Regexp
	// Login says how to sign in the first time, if the app has a default.
	Login string
	// Body is the description as HTML.
	Body    template.HTML
	Compose []byte
	Logo    []byte
	// Secrets are the ${NAME}s of compose.yml ops generates on install.
	Secrets []string
}

// Categories in the order the catalog shows them.
var Categories = []string{"Analytics", "Automation", "Monitoring", "Security", "Developer tools", "Marketing", "Productivity"}

var (
	slugPattern     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,40}[a-z0-9])?$`)
	variablePattern = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)(:?[-?][^}]*)?\}`)
	imagePattern    = regexp.MustCompile(`^[a-z0-9.-]+(:[0-9]+)?(/[a-z0-9._-]+)+$|^[a-z0-9._-]+(/[a-z0-9._-]+)*$`)
	// defaultFilter takes 1.2.3 and v1.2.3: releases, not latest, nightly,
	// rc or a commit.
	defaultFilter = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)
)

// VersionVar is the env var that holds the version: compose.yml uses it as
// the image tag.
const VersionVar = "VERSION"

// Load reads every entry under dir, sorted by name.
func Load(dir string) ([]Entry, error) {
	folders, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	parser := markdown.NewParser()
	var entries []Entry
	for _, folder := range folders {
		if !folder.IsDir() {
			continue
		}
		entry, err := loadEntry(parser, filepath.Join(dir, folder.Name()), folder.Name())
		if err != nil {
			return nil, fmt.Errorf("catalog %s: %w", folder.Name(), err)
		}
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return entries, nil
}

func loadEntry(parser *markdown.Parser, dir, slug string) (Entry, error) {
	if !slugPattern.MatchString(slug) {
		return Entry{}, errors.New("the folder name is not a slug: lowercase letters, digits and dashes")
	}
	source, err := os.ReadFile(filepath.Join(dir, "app.md"))
	if err != nil {
		return Entry{}, err
	}
	body, meta, err := parser.ParseWithFrontmatter(source)
	if err != nil {
		return Entry{}, err
	}
	text := func(key string) string {
		value, _ := meta[key].(string)
		return strings.TrimSpace(value)
	}
	entry := Entry{
		Slug: slug, Name: text("name"), Pitch: text("pitch"), Category: text("category"),
		Website: text("website"), Source: text("source"), License: text("license"),
		Image: text("image"), Database: text("database"), Login: text("login"),
		Body: template.HTML(body),
	}
	switch port := meta["port"].(type) {
	case int:
		entry.Port = port
	case uint64:
		entry.Port = int(port)
	case float64:
		entry.Port = int(port)
	}
	entry.Filter = defaultFilter
	if filter := text("versions"); filter != "" {
		entry.Filter, err = regexp.Compile(filter)
		if err != nil {
			return Entry{}, fmt.Errorf("versions: %w", err)
		}
	}
	entry.Compose, err = os.ReadFile(filepath.Join(dir, "compose.yml"))
	if err != nil {
		return Entry{}, err
	}
	logo, err := os.ReadFile(filepath.Join(dir, "logo.svg"))
	if err == nil {
		entry.Logo = logo
	} else if !errors.Is(err, os.ErrNotExist) {
		return Entry{}, err
	}
	entry.Secrets = Secrets(entry.Compose)
	return entry, entry.validate()
}

func (e Entry) validate() error {
	switch {
	case e.Name == "" || e.Pitch == "":
		return errors.New("name and pitch are required")
	case !slices.Contains(Categories, e.Category):
		return fmt.Errorf("category %q is none of %s", e.Category, strings.Join(Categories, ", "))
	case !imagePattern.MatchString(e.Image):
		return fmt.Errorf("image %q is not an image without a tag", e.Image)
	case !strings.Contains(string(e.Compose), e.Image+":${"+VersionVar+"}"):
		return fmt.Errorf("compose.yml must run %s:${%s}", e.Image, VersionVar)
	case e.Database != "" && e.Database != "postgres" && e.Database != "sqlite":
		return errors.New("database is postgres, sqlite or empty")
	case e.Port <= 0 || e.Port > 65535:
		return errors.New("port is required")
	case !strings.HasPrefix(e.Website, "https://"):
		return errors.New("website must be an https:// link")
	}
	return nil
}

// Secrets lists the ${NAME}s compose uses, except VERSION and those with a
// default, sorted: ops generates a value for each on install.
func Secrets(compose []byte) []string {
	var names []string
	for _, match := range variablePattern.FindAllStringSubmatch(string(compose), -1) {
		name, rest := match[1], match[2]
		if name == VersionVar || strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, ":-") {
			continue
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}
