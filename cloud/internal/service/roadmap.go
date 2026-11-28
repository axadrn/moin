package service

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/axadrn/moin/cloud/internal/markdown"
)

// roadmapEntry is a list item of the roadmap: "**Name**: the rest".
var roadmapEntry = regexp.MustCompile(`^- (?:\*\*(.+?)\*\*[\s:,]*)?(.*)$`)

// roadmapLink is the link a list item ends with: where it is documented.
var roadmapLink = regexp.MustCompile(`\s*\[[^\]]+\]\(([^)\s]+)\)$`)

// Roadmap is docs/roadmap.md, read for its page. Lead is the text over
// the first heading.
type Roadmap struct {
	Title       string
	Description string
	Lead        string
	Sections    []RoadmapSection
}

// RoadmapSection is one "## Heading" and its list.
type RoadmapSection struct {
	Title   string
	Entries []RoadmapEntry
}

// RoadmapEntry is one list item: its bold name and the rest, as HTML, and
// Href, the link the item ends with, which makes the whole row a link.
type RoadmapEntry struct {
	Name string
	Text string
	Href string
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
	var lead []string
	_, body, _ := strings.Cut(strings.TrimPrefix(string(source), "---\n"), "\n---\n")
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "## "):
			roadmap.Sections = append(roadmap.Sections, RoadmapSection{Title: strings.TrimPrefix(line, "## ")})
		case strings.HasPrefix(line, "- ") && len(roadmap.Sections) > 0:
			parts := roadmapEntry.FindStringSubmatch(line)
			entry := RoadmapEntry{Name: markdown.Inline(parts[1])}
			text := parts[2]
			if link := roadmapLink.FindStringSubmatch(text); link != nil && !strings.Contains(text[:len(text)-len(link[0])], "](") {
				entry.Href = link[1]
				text = text[:len(text)-len(link[0])]
			}
			if entry.Name != "" {
				text = capitalize(text)
			}
			entry.Text = markdown.Inline(text)
			section := &roadmap.Sections[len(roadmap.Sections)-1]
			section.Entries = append(section.Entries, entry)
		case len(roadmap.Sections) == 0:
			lead = append(lead, line)
		default:
			return Roadmap{}, fmt.Errorf("roadmap: under %q write a list item, not a paragraph: %q", roadmap.Sections[len(roadmap.Sections)-1].Title, line)
		}
	}
	roadmap.Lead = strings.Join(lead, " ")
	return roadmap, nil
}

// capitalize starts the text after a name with a capital letter: it stands
// on its own line under the name.
func capitalize(text string) string {
	first, size := utf8.DecodeRuneInString(text)
	return string(unicode.ToUpper(first)) + text[size:]
}
