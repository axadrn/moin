package model

type DocPage struct {
	Title string
	Slug  string
	Path  string
	Order int
	// Group is the topic the page sits under on the docs home.
	Group       string
	Description string
	Content     string
	HTMLContent string
	Headings    []DocHeading
	Children    []*DocPage
	Parent      *DocPage
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
