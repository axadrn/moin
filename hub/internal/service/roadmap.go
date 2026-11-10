package service

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/axadrn/moin/hub/internal/markdown"
)

// roadmapEntry is a list item of the roadmap: "**Name**: the rest".
var roadmapEntry = regexp.MustCompile(`^- (?:\*\*(.+?)\*\*[\s:,]*)?(.*)$`)

// Roadmap is docs/roadmap.md, read for its page. Intro and Text are HTML.
type Roadmap struct {
	Title       string
	Description string
	Intro       string
	Sections    []RoadmapSection
}

// RoadmapSection is one "## Heading": the paragraphs under it and its list.
type RoadmapSection struct {
	Title   string
	Intro   string
	Entries []RoadmapEntry
}

// RoadmapEntry is one list item: its bold name and the rest as HTML.
type RoadmapEntry struct {
	Name string
	Text string
}

// ReadRoadmap reads docs/roadmap.md, the only place the roadmap is written.
// The file keeps each paragraph and each list item on one line.
func ReadRoadmap(files fs.FS) (Roadmap, error) {
	source, err := fs.ReadFile(files, "docs/roadmap.md")
	if err != nil {
		return Roadmap{}, fmt.Errorf("read roadmap: %w", err)
	}
	parser := markdown.NewParser()
	meta := parser.ExtractFrontmatter(source)
	title, _ := meta["title"].(string)
	description, _ := meta["description"].(string)
	roadmap := Roadmap{Title: title, Description: description}

	// Paragraphs are gathered as markdown and rendered at the end.
	intro := &roadmap.Intro
	_, body, _ := strings.Cut(strings.TrimPrefix(string(source), "---\n"), "\n---\n")
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "## "):
			roadmap.Sections = append(roadmap.Sections, RoadmapSection{Title: strings.TrimPrefix(line, "## ")})
			intro = &roadmap.Sections[len(roadmap.Sections)-1].Intro
		case strings.HasPrefix(line, "- ") && len(roadmap.Sections) > 0:
			parts := roadmapEntry.FindStringSubmatch(line)
			section := &roadmap.Sections[len(roadmap.Sections)-1]
			section.Entries = append(section.Entries, RoadmapEntry{Name: parts[1], Text: markdown.Inline(parts[2])})
		default:
			*intro += line + "\n\n"
		}
	}

	render := func(text *string) error {
		html, err := parser.Parse([]byte(*text))
		if err != nil {
			return fmt.Errorf("render roadmap: %w", err)
		}
		*text = string(html)
		return nil
	}
	if err := render(&roadmap.Intro); err != nil {
		return Roadmap{}, err
	}
	for i := range roadmap.Sections {
		if err := render(&roadmap.Sections[i].Intro); err != nil {
			return Roadmap{}, err
		}
	}
	return roadmap, nil
}
