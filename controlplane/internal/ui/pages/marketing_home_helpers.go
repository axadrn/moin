package pages

import "fmt"

// liveCount writes a count with thousands separators: 12,480.
func liveCount(count int) string {
	text := fmt.Sprint(count)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

// compareTools are the columns of the comparison, ops first.
var compareTools = []string{"ops", "Coolify Cloud", "Dokploy Cloud", "Kamal", "Heroku", "Render", "Railway", "Fly.io"}

// compareRow is one fact, one cell per tool in compareTools order. Every
// cell of another tool comes from its own public docs or pricing page, as
// of November 2026; "?" where they do not say.
type compareRow struct {
	Label string
	Cells []string
}

var compareRows = []compareRow{
	{"Holds an SSH key to your servers", []string{"No, the agent dials out", "Yes", "Yes", "On your laptop", "Their machines", "Their machines", "Their machines", "Their machines"}},
	{"Port your server must open for it", []string{"None", "SSH", "SSH", "SSH", "Their machines", "Their machines", "Their machines", "Their machines"}},
	{"Stores your code and data", []string{"No", "No", "No", "No", "Yes", "Yes", "Yes", "Yes"}},
	{"SQLite backups", []string{"Every second, restore to any second", "Scheduled", "Scheduled volume backups", "?", "Not possible, the disk resets daily", "Daily disk snapshot", "Daily volume backup", "Daily volume snapshot"}},
	{"Restore checked every day", []string{"Yes", "No", "No", "?", "No", "No", "No", "No"}},
	{"Data browser and SQL", []string{"Yes, for SQLite", "Through pgAdmin or CloudBeaver", "?", "No", "Postgres only", "No", "Tables of its databases", "?"}},
	{"Deploy from laptop, GitHub, CI image", []string{"All three", "GitHub, image", "GitHub, image, zip upload", "Laptop", "All three", "GitHub, image", "Laptop, others ?", "Laptop, image, GitHub ?"}},
	{"CLI, terminal UI, API, MCP", []string{"All four", "CLI, API, MCP", "CLI, API, MCP", "CLI", "CLI, MCP, API ?", "CLI, API, MCP", "CLI, MCP, API ?", "CLI, MCP, API ?"}},
	{"Activity log", []string{"Every plan", "Yes, 90 days", "Enterprise", "No", "Enterprise", "Pro and up", "Every plan", "?"}},
	{"Free to host yourself", []string{"Yes, AGPL", "Yes, Apache 2.0", "Yes, Apache 2.0", "Yes, MIT", "No", "No", "No", "No"}},
	{"How you pay", []string{"$29 a month per workspace, any number of servers", "$5 a month for 2 servers, $3 per extra", "$4.50 per server, teams from $15 a month", "Free", "Per dyno, from $7 a month", "$25 a month plus each instance", "$20 a month plus usage", "Per machine, from about $2 a month"}},
}
