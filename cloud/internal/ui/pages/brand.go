package pages

import (
	"strconv"

	"github.com/axadrn/moin/cloud/internal/brand"
	"github.com/axadrn/moin/cloud/internal/ui/components"
)

// brandMarkPath is the mark of assets/img/mark.svg on its 24 grid: a disc
// with the sun on the horizon cut out of it.
const brandMarkPath = "M0 12a12 12 0 1 0 24 0a12 12 0 1 0 -24 0ZM4 12A8 8 0 0 1 20 12Z"

// brandFiles are the downloads of one drawing of assets/brand, in black
// for a light ground or in white for a dark one.
func brandFiles(drawing, color string) []components.Download {
	var files []components.Download
	for _, ext := range []string{"svg", "png"} {
		name := drawing + "-" + color + "." + ext
		files = append(files, components.Download{
			Label: map[string]string{"svg": "SVG", "png": "PNG"}[ext],
			Href:  components.AssetURL("/assets/brand/" + name),
			Name:  brand.Name + "-" + name,
		})
	}
	return files
}

// brandDrawings are the drawings of the brand: the name on the page and
// the file name in assets/brand.
var brandDrawings = []struct{ Name, File string }{
	{"Mark", "mark"},
	{"Wordmark", "wordmark"},
	{"Lockup", "lockup"},
}

// brandNeutrals are the grays of site.css, light and dark.
var brandNeutrals = []components.SwatchProps{
	{Name: "Ink", Token: "primary", Light: "#0d0d0d", Dark: "#f2f2f2"},
	{Name: "Text", Token: "foreground", Light: "#000000", Dark: "#ffffff"},
	{Name: "Quiet text", Token: "muted-foreground", Light: "#636366", Dark: "#98989f"},
	{Name: "Ground", Token: "background", Light: "#f2f2f7", Dark: "#000000"},
	{Name: "Card", Token: "card", Light: "#ffffff", Dark: "#1c1c1e"},
	{Name: "Control", Token: "muted", Light: "#e5e5ea", Dark: "#2c2c2e"},
	{Name: "Terminal", Token: "terminal", Light: "#0d0d0d", Dark: "#2c2c2e"},
	{Name: "Terminal text", Token: "terminal-foreground", Light: "#dedede", Dark: "#d7d7d7"},
}

// brandStates are the only colors there are: a status.
var brandStates = []components.SwatchProps{
	{Name: "Live", Token: "live", Light: "#24ae56", Dark: "#47c86e"},
	{Name: "Running", Token: "running", Light: "#c57800", Dark: "#f7b83d"},
	{Name: "Failed", Token: "failed", Light: "#e62b34", Dark: "#fc5855"},
	{Name: "Idle", Token: "idle", Light: "#808080", Dark: "#717171"},
}

// brandCompare is the table of the home page with its first three
// columns: the featured one and two others.
func brandCompare() components.CompareProps {
	p := homeCompare()
	p.Columns = p.Columns[:min(3, len(p.Columns))]
	return p
}

// brandSpace is the steps of space in site.css, in pixels: what sits
// between things.
var brandSpace = []struct {
	Name string
	Px   int64
}{
	{"--space-8", 32},
	{"--space-6", 24},
	{"--space-5", 20},
	{"--space-4", 16},
	{"--space-3", 12},
	{"--space-2", 8},
	{"--space-1", 4},
}

// brandSpaceBars is the steps of space as Bars, each as long as its step.
func brandSpaceBars() components.BarsProps {
	var p components.BarsProps
	for _, step := range brandSpace {
		p.Rows = append(p.Rows, components.BarRow{Label: step.Name, Value: strconv.FormatInt(step.Px, 10) + " px", Share: step.Px * 100 / brandSpace[0].Px})
	}
	return p
}

// brandVoice is how moin speaks: a rule, then what it sounds like and what
// it never does.
var brandVoice = []struct {
	Rule, Do, Dont string
}{
	{"Short sentences. No commas in titles.", "Deploy anything. Lose nothing.", "Deploy anything, and never lose a thing again!"},
	{"One idea per line.", "Your server updates when you say so.", "Updates are manual, signed and safe, so nothing breaks."},
	{"Plain words.", brand.Name + " restores a backup every day.", "Leverage automated restore verification workflows."},
	{"“I” in the blog.", "I built " + brand.Name + " and named it after home.", "The team is excited to announce our journey."},
	{"“We” only for the company.", "We can't read your backups.", "We deploy your app to your server."},
}
