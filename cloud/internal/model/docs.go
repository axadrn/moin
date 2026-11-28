// Package model holds the docs pages as the site reads them.
package model

// DocPage is one file under content/docs.
type DocPage struct {
	Title string
	// Slug is the path under docs/ without .md, like getting-started.
	Slug string
	// Group is the topic the page sits under on the docs home, Order its
	// place there.
	Group       string
	Order       int
	Description string
	// Content is the Markdown, HTMLContent the page.
	Content     string
	HTMLContent string
	Headings    []DocHeading
}

type DocHeading struct {
	ID    string
	Text  string
	Level int
}

// DocGroup is a topic of the docs home: its title over the list of its
// pages.
type DocGroup struct {
	Title string
	Pages []*DocPage
}
