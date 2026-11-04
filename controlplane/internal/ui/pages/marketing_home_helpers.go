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

// compareTools are the columns of the comparison, moin first.
var compareTools = []string{"moin", "Coolify Cloud", "Dokploy Cloud", "Kamal", "Heroku", "Render", "Railway", "Fly.io", "Uncloud"}

// compareColumn is one column of a tab: a tool, named for that tab, and
// what it costs there when that differs from its row, such as a hosted
// tool that is free to run yourself.
type compareColumn struct {
	Name  string
	Tool  int
	Price compareCell
}

// compareTab is one view of the comparison: moin next to the tools people
// weigh it against.
type compareTab struct {
	Value   string
	Title   string
	Columns []compareColumn
}

var (
	free     = plain("Free", "you run it")
	moinFree = plain("Free", "every feature")
	selfHost = []compareColumn{{"moin", 0, moinFree}, {"Coolify", 1, free}, {"Dokploy", 2, free}, {"Kamal", 3, compareCell{}}, {"Uncloud", 8, compareCell{}}}
)

var compareTabs = []compareTab{
	{"hosted", "Hosted", []compareColumn{{"moin Hosted", 0, compareCell{}}, {"Coolify Cloud", 1, compareCell{}}, {"Dokploy Cloud", 2, compareCell{}}}},
	{"self-hosted", "Self-hosted", selfHost},
	{"platforms", "Platforms", []compareColumn{{"moin Hosted", 0, compareCell{}}, {"Heroku", 4, compareCell{}}, {"Render", 5, compareCell{}}, {"Railway", 6, compareCell{}}, {"Fly.io", 7, compareCell{}}}},
}

// cellFor is what a column says in a row: its own price on the price row
// when it has one, else the tool's cell.
func cellFor(row compareRow, column compareColumn) compareCell {
	if row.Price && column.Price.Short != "" {
		return column.Price
	}
	return row.Cells[column.Tool]
}

// compareMark is how a cell reads at a glance: good, partly, no, not said,
// or nothing to judge (such as a price).
type compareMark string

const (
	markYes     compareMark = "yes"
	markPart    compareMark = "part"
	markNo      compareMark = "no"
	markUnknown compareMark = "unknown"
	markNone    compareMark = ""
)

// compareCell is one tool's answer to one fact: its mark, a word or two,
// and an optional detail below.
type compareCell struct {
	Mark   compareMark
	Short  string
	Detail string
}

func yes(short string, detail ...string) compareCell  { return cell(markYes, short, detail) }
func part(short string, detail ...string) compareCell { return cell(markPart, short, detail) }
func no(short string, detail ...string) compareCell   { return cell(markNo, short, detail) }
func plain(short string, detail ...string) compareCell {
	return cell(markNone, short, detail)
}
func unsaid() compareCell { return compareCell{Mark: markUnknown, Short: "Not said"} }

func cell(mark compareMark, short string, detail []string) compareCell {
	c := compareCell{Mark: mark, Short: short}
	if len(detail) > 0 {
		c.Detail = detail[0]
	}
	return c
}

// compareRow is one fact, one cell per tool in compareTools order. Every
// cell of another tool comes from its own public docs or pricing page, as
// of November 2026. Uncloud: github.com/psviderski/uncloud and uncloud.run,
// what ships today, not the announced Hub.
type compareRow struct {
	Label string
	Cells []compareCell
	Price bool
}

// compareGroup is a few facts under one heading.
type compareGroup struct {
	Title string
	Rows  []compareRow
}

// compareFacts counts the facts a tool can win: every row where its cell
// has a mark, so prices, and machines a platform runs itself, stay out.
func compareFacts(tool int) int {
	facts := 0
	for _, group := range compareGroups {
		for _, row := range group.Rows {
			if row.Cells[tool].Mark != markNone {
				facts++
			}
		}
	}
	return facts
}

// compareWins counts the facts a tool gets a full yes on.
func compareWins(tool int) int {
	wins := 0
	for _, group := range compareGroups {
		for _, row := range group.Rows {
			if row.Cells[tool].Mark == markYes {
				wins++
			}
		}
	}
	return wins
}

var compareGroups = []compareGroup{
	{"Security", []compareRow{
		{"Holds an SSH key to your servers", []compareCell{yes("No", "the agent dials out"), no("Yes", "root over SSH"), no("Yes", "over SSH"), part("Your laptop", "deploys over SSH"), plain("Their machines"), plain("Their machines"), plain("Their machines"), plain("Their machines"), part("Your laptop", "over SSH, root or sudo")}, false},
		{"Port your server must open", []compareCell{yes("None", "outbound HTTPS only"), no("SSH"), no("SSH"), no("SSH"), plain("Their machines"), plain("Their machines"), plain("Their machines"), plain("Their machines"), no("SSH + WireGuard", "plus UDP 51820")}, false},
	}},
	{"Your data", []compareRow{
		{"Keeps your code and data", []compareCell{yes("Never", "your bucket, your server"), yes("No"), yes("No"), yes("No"), no("Yes", "on their disks"), no("Yes", "on their disks"), no("Yes", "on their disks"), no("Yes", "on their disks"), yes("No")}, false},
		{"SQLite backups", []compareCell{yes("Every change", "in your bucket within a second, restore to any second"), part("Scheduled"), part("Scheduled", "volume backups"), unsaid(), no("Not possible", "the disk resets daily"), part("Daily", "disk snapshot"), part("Daily", "volume backup"), part("Daily", "volume snapshot"), unsaid()}, false},
		// Postgres sources: coolify.io/docs/databases/backups (pg_dump on a cron),
		// docs.dokploy.com/docs/core/databases/backups (cron),
		// devcenter.heroku.com/articles/heroku-postgres-plans (rollback: Essential no,
		// Standard 4 days, Premium and Advanced 1 week), render.com/docs/postgresql-backups
		// (paid databases, 3 days on Hobby, 7 on Pro), docs.railway.com/volumes/point-in-time-recovery
		// (turned on per database, about 4 weeks), fly.io/mpg (Managed Postgres,
		// point-in-time restore, backups kept ten days). Kamal and Uncloud say nothing.
		{"Postgres backups", []compareCell{yes("Every change", "in your bucket within a minute, restore to any second"), part("Scheduled", "pg_dump on a cron"), part("Scheduled", "on a cron"), unsaid(), yes("Point in time", "Standard and up, 4 to 7 days"), yes("Point in time", "paid plans, 3 or 7 days"), yes("Point in time", "turn it on, about 4 weeks"), yes("Point in time", "Managed Postgres, 10 days"), unsaid()}, false},
		{"Restore tested every day", []compareCell{yes("Yes", "a real restore, daily"), no("No"), no("No"), unsaid(), no("No"), no("No"), no("No"), no("No"), unsaid()}, false},
		{"Browse and edit live data", []compareCell{yes("Yes", "SQLite and Postgres, read and write"), part("Add-on", "pgAdmin or CloudBeaver"), unsaid(), no("No"), part("Postgres only"), no("No"), part("Tables", "of its databases"), unsaid(), unsaid()}, false},
	}},
	{"Every day", []compareRow{
		// Uptime sources: coolify.io/docs/knowledge-base/monitoring,
		// docs.dokploy.com/docs/core/monitoring, kamal-deploy.org/docs/configuration/proxy,
		// devcenter.heroku.com/articles/metrics, render.com/docs/uptime-best-practices,
		// docs.railway.com/reference/healthchecks, docs.fly.io/reference/health-checks.
		{"Uptime checks", []compareCell{yes("Every minute", "built in, with alerts"), no("No", "container health only"), no("No", "server metrics only"), no("No", "deploy check only"), no("No", "metric alerts only"), no("No", "health checks only"), no("No", "deploy check only"), no("No", "health checks only"), no("No", "alerts only announced")}, false},
		{"Deploy from", []compareCell{yes("Anywhere", "laptop, GitHub, CI image"), part("Two ways", "GitHub, image"), part("Three ways", "GitHub, image, zip"), part("Laptop"), yes("Anywhere", "laptop, GitHub, CI image"), part("Two ways", "GitHub, image"), part("Laptop", "others not said"), part("Two ways", "laptop, image"), part("Laptop, CI", "no GitHub push")}, false},
		{"Ways in", []compareCell{yes("All four", "web, CLI, terminal UI, MCP"), part("Three", "no terminal UI"), part("Three", "no terminal UI"), part("CLI only"), part("CLI, MCP", "API not said"), part("Three", "no terminal UI"), part("CLI, MCP", "API not said"), part("CLI, MCP", "API not said"), part("CLI only", "web UI announced")}, false},
		{"Activity log", []compareCell{yes("Every plan"), yes("Yes", "90 days"), part("Enterprise"), no("No"), part("Enterprise"), part("Pro and up"), yes("Every plan"), unsaid(), unsaid()}, false},
	}},
	{"Price", []compareRow{
		{"Host it yourself, free", []compareCell{yes("Yes", "AGPL"), yes("Yes", "Apache 2.0"), yes("Yes", "Apache 2.0"), yes("Yes", "MIT"), no("No"), no("No"), no("No"), no("No"), yes("Yes", "Apache 2.0")}, false},
		{"You pay", []compareCell{plain("$29/mo", "per workspace, unlimited everything"), plain("$5/mo", "2 servers, $3 per extra"), plain("$4.50/server", "teams from $15/mo"), plain("Free"), plain("From $7/mo", "per dyno"), plain("$25/mo", "plus each instance"), plain("$20/mo", "plus usage"), plain("From $2/mo", "per machine"), plain("Free")}, true},
	}},
}
