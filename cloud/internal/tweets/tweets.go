// Package tweets reads the posts from X in content/tweets.json. Real
// people say the problem: servers getting hacked, backups that did not
// come back. The landing page shows their words as they were posted.
package tweets

import (
	"cmp"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"slices"
	"strings"
	"time"
)

// Tweet is one post, as X gave it. Text is raw: HTML entities like &gt;
// and t.co links. Every At counts characters (runes), not bytes.
type Tweet struct {
	ID       string    `json:"id"`
	URL      string    `json:"url"`
	Name     string    `json:"name"`
	Handle   string    `json:"handle"`
	Avatar   string    `json:"avatar"`
	Verified bool      `json:"verified"`
	Date     time.Time `json:"date"`
	Likes    int       `json:"likes"`
	Replies  int       `json:"replies"`
	Text     string    `json:"text"`
	Range    [2]int    `json:"range"`
	Links    []Link    `json:"links"`
	Mentions []Mention `json:"mentions"`
	Media    []Media   `json:"media"`
}

// Link is a t.co link in the text: X shows Shown and goes to Href.
type Link struct {
	URL   string `json:"url"`
	Href  string `json:"href"`
	Shown string `json:"shown"`
	At    [2]int `json:"at"`
}

// Mention is an @handle in the text.
type Mention struct {
	Handle string `json:"handle"`
	At     [2]int `json:"at"`
}

// Media is the t.co link of a photo. The site shows no photos, so the
// link goes.
type Media struct {
	URL string `json:"url"`
	At  [2]int `json:"at"`
}

// Part is a piece of the text: words, or a link when Href is set.
type Part struct {
	Text string
	Href string
}

// Load reads tweets.json from files.
func Load(files fs.FS) ([]Tweet, error) {
	data, err := fs.ReadFile(files, "tweets.json")
	if err != nil {
		return nil, err
	}
	var all []Tweet
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("tweets.json: %w", err)
	}
	return all, nil
}

// Parts is the text as X shows it: up to the end of Range, links with
// their short text, mentions linked to their profile, photo links gone.
func (t Tweet) Parts() []Part {
	text := []rune(t.Text)
	end := min(t.Range[1], len(text))

	// A mark is a link, a mention or a photo link, at its place in the
	// text. A photo link has no part: it is dropped.
	type mark struct {
		at   [2]int
		part Part
	}
	var marks []mark
	for _, link := range t.Links {
		marks = append(marks, mark{link.At, Part{Text: link.Shown, Href: link.Href}})
	}
	for _, mention := range t.Mentions {
		marks = append(marks, mark{mention.At, Part{Href: "https://x.com/" + mention.Handle}})
	}
	for _, media := range t.Media {
		marks = append(marks, mark{media.At, Part{}})
	}
	slices.SortFunc(marks, func(a, b mark) int { return cmp.Compare(a.at[0], b.at[0]) })

	var parts []Part
	from := t.Range[0]
	for _, m := range marks {
		if m.at[0] < from || m.at[0] >= end {
			continue
		}
		parts = addWords(parts, text[from:m.at[0]])
		if m.part.Href != "" {
			if m.part.Text == "" {
				// A mention shows as it was typed: "@digitalocean".
				m.part.Text = string(text[m.at[0]:min(m.at[1], end)])
			}
			parts = append(parts, m.part)
		}
		from = m.at[1]
	}
	if from < end {
		parts = addWords(parts, text[from:end])
	}

	// A dropped photo link leaves a space or a line break at the end.
	if last := len(parts) - 1; last >= 0 && parts[last].Href == "" {
		parts[last].Text = strings.TrimRight(parts[last].Text, " \n")
		if parts[last].Text == "" {
			parts = parts[:last]
		}
	}
	return parts
}

// addWords adds raw text as words, with &gt; and the like turned back
// into > and the like.
func addWords(parts []Part, raw []rune) []Part {
	if len(raw) == 0 {
		return parts
	}
	return append(parts, Part{Text: html.UnescapeString(string(raw))})
}

// Day is the date of the post as X writes it: Oct 20, 2025.
func (t Tweet) Day() string {
	return t.Date.Format("Jan 2, 2006")
}

// Count is a number as X writes it: 112, 3.7K, 10K, 1.2M.
func Count(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 10_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "K"
	case n < 1_000_000:
		return fmt.Sprint(n/1000) + "K"
	default:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1_000_000), ".0") + "M"
	}
}
