package pages

import (
	"fmt"

	"github.com/axadrn/moin/hub/internal/brand"
)

// compareRead is when every competitor fact below was read at its source.
const compareRead = "November 8, 2026"

// compareMark is how a cell reads at a glance. The answer is judged by
// what is better for you, so "No" to holding an SSH key carries a yes.
type compareMark string

const (
	markYes     compareMark = "yes"
	markPart    compareMark = "part"
	markNo      compareMark = "no"
	markUnknown compareMark = "unknown"
	markNone    compareMark = ""
)

// counted is whether a mark takes part in the score: "not documented" and
// the rows with nothing to judge, such as a price, do not count either way.
func (m compareMark) counted() bool {
	return m == markYes || m == markPart || m == markNo
}

// CompareCell is one answer to one row: its mark, a word or two, a line
// under them, and the page that says so.
type CompareCell struct {
	Mark   compareMark
	Short  string
	Detail string
	Source string
}

func yes(short, detail, source string) CompareCell {
	return CompareCell{markYes, short, detail, source}
}

func part(short, detail, source string) CompareCell {
	return CompareCell{markPart, short, detail, source}
}

func no(short, detail, source string) CompareCell {
	return CompareCell{markNo, short, detail, source}
}

func plain(short, detail, source string) CompareCell {
	return CompareCell{markNone, short, detail, source}
}

// notDocumented is a tool whose docs say nothing, which is weaker than "it
// does not exist": the cell counts neither for nor against it.
func notDocumented(source string) CompareCell {
	return CompareCell{markUnknown, "Not documented", "", source}
}

// CompareRow is one fact and the answer for this product; each competitor
// answers the same facts, in this order, in Competitor.Cells.
type CompareRow struct {
	Group string
	Label string
	Ours  CompareCell
}

// Competitor is one other tool: its line on the home page and its page at
// /compare/<slug>.
type Competitor struct {
	Slug string
	Name string
	// Lead says calmly what differs.
	Lead  string
	Cells []CompareCell
}

// compareGroups are the headings of the table, in order.
var compareGroups = []string{"Security", "Your data", "Every day", "Price"}

// CompareRows are the facts every competitor answers.
var CompareRows = []CompareRow{
	{"Security", "Holds an SSH key to your servers", yes("No", "The daemon on your server dials out.", "")},
	{"Security", "Port your server must open", yes("None", "None for "+brand.Name+". Visitors use 80 and 443, or a Cloudflare Tunnel.", "")},
	{"Your data", "Keeps your code and data", yes("No", "Your server and your bucket.", "")},
	{"Your data", "Database backups", yes("SQLite and Postgres", "On your schedule, in your own bucket: every 15 minutes, hourly or daily.", "")},
	{"Your data", "Restore tested every day", yes("Yes", "A real restore into a throwaway database, every day.", "")},
	{"Your data", "Browse and edit live data", yes("Yes", "SQLite and Postgres. Read, edit, run SQL.", "")},
	{"Every day", "Uptime checks", yes("Every minute", "Built in, with alerts.", "")},
	{"Every day", "Deploy from", yes("Folder, GitHub, image", "Your folder, a GitHub push or a CI image.", "")},
	{"Every day", "Ways in", yes("All five", "Web, CLI, terminal UI, API and MCP.", "")},
	{"Every day", "Activity log", yes("Yes", "Who did what, when, in every workspace.", "")},
	{"Price", "Host it yourself, free", yes("Yes", "AGPL-3.0, all of it.", "")},
	{"Price", "You pay", plain("$29 per workspace", "Unlimited servers, apps and team. $249 a year. The tenth server costs the same as the first, the whole team is included and every feature is in.", "")},
}

// hasNoServer is the answer on a row about your server for a tool that runs
// the apps on its own.
func hasNoServer(name, source string) CompareCell {
	return plain("You have no server", name+" runs them.", source)
}

// Competitors are the seven tools, in the order the home page lists them.
var Competitors = []Competitor{
	{
		Slug: "coolify",
		Name: "Coolify",
		Lead: "Both run your apps on your own servers. Coolify connects to them over SSH. The " + brand.Name + " daemon dials out instead, and " + brand.Name + " holds no key.",
		Cells: []CompareCell{
			no("Yes", "Coolify stores the private key and connects over SSH.", "https://coolify.io/docs/core/security/credentials/ssh-keys"),
			part("SSH, or a Cloudflare Tunnel", "Port 22 open to Coolify, unless a tunnel carries it.", "https://coolify.io/docs/knowledge-base/server/firewall"),
			yes("No", "Your servers, also with Coolify Cloud.", "https://coolify.io/pricing"),
			yes("Six engines, on a schedule", "PostgreSQL, MySQL, MariaDB, MongoDB, ClickHouse and SQLite. On the server, and in S3 if you add it.", "https://coolify.io/docs/databases/backups"),
			no("By hand", "Its docs say to restore a copy yourself and check it.", "https://coolify.io/docs/databases/backups"),
			part("With an add-on", "CloudBeaver, as a service you add.", "https://coolify.io/docs/services/cloudbeaver"),
			no("No", "Its docs say endpoint checks need an external system.", "https://coolify.io/docs/knowledge-base/monitoring"),
			part("Git or an image", "A Git repository, a Dockerfile or an existing image.", "https://coolify.io/docs/applications/"),
			part("Four ways", "Web, CLI, API and MCP.", "https://coolify.io/docs/api-reference/authorization"),
			yes("Yes", "Kept 90 days, for admins and owners.", "https://coolify.io/docs/core/team/audit-log"),
			yes("Yes", "Free forever. Apache-2.0.", "https://coolify.io/pricing"),
			plain("$5 + $3 per extra server", "Coolify Cloud, 2 servers included. $29 at 10 servers, $59 at 20.", "https://coolify.io/pricing"),
		},
	},
	{
		Slug: "dokploy",
		Name: "Dokploy",
		Lead: "Both run your apps on your own servers. Dokploy connects to them over SSH with a key. The " + brand.Name + " daemon dials out instead, and " + brand.Name + " holds no key.",
		Cells: []CompareCell{
			no("Yes", "SSH with a key pair from Dokploy.", "https://docs.dokploy.com/docs/core/remote-servers/instructions"),
			no("SSH", "It has to reach the server over SSH.", "https://docs.dokploy.com/docs/core/cloud"),
			yes("No", "On your servers, also with Dokploy Cloud.", "https://docs.dokploy.com/docs/core/cloud"),
			yes("Four engines, on a schedule", "PostgreSQL, MySQL, MariaDB and MongoDB, to S3.", "https://docs.dokploy.com/docs/core/databases/backups"),
			notDocumented("https://docs.dokploy.com/docs/core/databases/backups"),
			notDocumented("https://docs.dokploy.com/docs/core/differences"),
			notDocumented("https://docs.dokploy.com/docs/core/monitoring"),
			part("Git, an image or a zip", "GitHub, GitLab, Bitbucket, Gitea, Git, Docker, or a zip you upload.", "https://docs.dokploy.com/docs/core/providers"),
			part("Four ways", "Web, CLI, API and MCP.", "https://docs.dokploy.com/docs/api"),
			part("Enterprise plans", "Audit logs on the Enterprise and Agency plans.", "https://docs.dokploy.com/docs/core/differences"),
			yes("Yes", "Free to run. Apache-2.0, except a source-available part.", "https://github.com/Dokploy/dokploy/blob/canary/LICENSE.MD"),
			plain("From $15 + $4.50 per extra server", "Dokploy Cloud Startup, 3 servers included. About $29 at 6 servers.", "https://dokploy.com/pricing"),
		},
	},
	{
		Slug: "kamal",
		Name: "Kamal",
		Lead: "Kamal is a command-line tool that runs Docker over SSH, with no hub to run. " + brand.Name + " has a hub, with a web UI, an API and MCP.",
		Cells: []CompareCell{
			part("Your SSH key", "On each laptop and CI that deploys.", "https://kamal-deploy.org/docs/installation/"),
			no("SSH", "Port 22 by default.", "https://kamal-deploy.org/docs/configuration/ssh/"),
			yes("No", "Apps and data stay on your servers.", "https://kamal-deploy.org/"),
			notDocumented("https://github.com/basecamp/kamal-site"),
			notDocumented("https://github.com/basecamp/kamal-site"),
			notDocumented("https://github.com/basecamp/kamal-site"),
			no("Deploy check only", "The proxy stops checking once the app is up.", "https://kamal-deploy.org/docs/configuration/proxy/"),
			part("Your laptop or CI", "kamal deploy builds your checkout and pushes it.", "https://kamal-deploy.org/docs/commands/deploy/"),
			part("CLI only", "It is a command-line tool.", "https://kamal-deploy.org/docs/installation/"),
			part("On each server", "kamal audit shows the latest commands.", "https://kamal-deploy.org/docs/commands/audit/"),
			yes("Yes", "MIT.", "https://github.com/basecamp/kamal"),
			plain("Free", "No paid tier found.", "https://github.com/basecamp/kamal"),
		},
	},
	{
		Slug: "railway",
		Name: "Railway",
		Lead: "Railway runs your apps on its own servers. " + brand.Name + " runs them on servers you bring.",
		Cells: []CompareCell{
			hasNoServer("Railway", "https://docs.railway.com/platform/compare-to-northflank"),
			hasNoServer("Railway", "https://docs.railway.com/platform/compare-to-northflank"),
			no("Yes", "Everything runs on Railway's servers.", "https://docs.railway.com/platform/compare-to-northflank"),
			part("Daily, weekly, monthly", "Volume backups, on the Pro plan.", "https://docs.railway.com/reference/backups"),
			no("By hand", "Railway describes a manual restore drill.", "https://railway.com/guides/postgres-backups-restores"),
			yes("Yes", "View and edit rows of its one-click databases.", "https://docs.railway.com/databases/database-view"),
			no("Deploy check only", "It does not watch the endpoint once a deploy is live.", "https://docs.railway.com/deployments/healthchecks"),
			yes("Folder, GitHub, image", "railway up, a GitHub push or a ready image.", "https://docs.railway.com/quick-start"),
			part("Four ways", "Web, CLI, API and MCP.", "https://docs.railway.com/integrations/api"),
			yes("Yes", "48 hours on Hobby, 30 days on Pro, 18 months on Enterprise.", "https://docs.railway.com/enterprise/audit-logs"),
			no("No", "Hosted only.", "https://railway.com/pricing"),
			plain("$5 or $20, plus use", "Hobby includes $5 of use, Pro $20. More use costs more.", "https://railway.com/pricing"),
		},
	},
	{
		Slug: "render",
		Name: "Render",
		Lead: "Render runs your apps on its own servers. " + brand.Name + " runs them on servers you bring.",
		Cells: []CompareCell{
			hasNoServer("Render", "https://render.com/pricing"),
			hasNoServer("Render", "https://render.com/pricing"),
			no("Yes", "Everything runs on Render's servers.", "https://render.com/pricing"),
			part("Continuous, on paid Postgres", "None on the free plan.", "https://render.com/docs/postgresql-backups"),
			notDocumented("https://render.com/docs/postgresql-backups"),
			notDocumented("https://render.com/docs/postgresql-backups"),
			no("No", "Render recommends a third-party monitor.", "https://render.com/docs/uptime-best-practices"),
			part("Git or an image", "A linked Git branch or a prebuilt image.", "https://render.com/docs/deploying-an-image"),
			yes("All five", "Web, CLI with terminal menus, API and MCP.", "https://render.com/docs/cli"),
			part("Pro and up", "Audit logs from the Pro workspace plan.", "https://render.com/docs/audit-logs"),
			no("No", "Hosted only.", "https://render.com/pricing"),
			plain("From $7 a web service", "Postgres from $6. Free plans with limits.", "https://render.com/pricing"),
		},
	},
	{
		Slug: "fly",
		Name: "Fly.io",
		Lead: "Fly.io runs your apps as Machines in its own regions. " + brand.Name + " runs them on servers you bring.",
		Cells: []CompareCell{
			hasNoServer("Fly.io", "https://docs.fly.io/about/pricing"),
			hasNoServer("Fly.io", "https://docs.fly.io/about/pricing"),
			no("Yes", "Everything runs on Fly's servers.", "https://docs.fly.io/about/pricing"),
			part("Managed Postgres", "By itself, kept ten days.", "https://fly.io/mpg/"),
			notDocumented("https://docs.fly.io/mpg/"),
			notDocumented("https://docs.fly.io/mpg/"),
			notDocumented("https://docs.fly.io/reference/health-checks/"),
			part("Folder or image", "fly deploy takes source or an image. GitHub through Actions.", "https://docs.fly.io/flyctl/deploy/"),
			part("Four ways", "Web, flyctl, Machines API and an experimental MCP.", "https://docs.fly.io/machines/api/"),
			no("No", "Its security questionnaire says there is no customer-facing audit log.", "https://fly.io/legal/security-questionnaire/"),
			no("No", "Hosted only.", "https://docs.fly.io/about/pricing"),
			plain("From $2.19 a Machine", "Managed Postgres from $38. No free tier for new accounts.", "https://docs.fly.io/about/pricing"),
		},
	},
	{
		Slug: "heroku",
		Name: "Heroku",
		Lead: "Heroku runs your apps on its own dynos. In February 2026 it moved to sustaining engineering, with no new features and no end date named. " + brand.Name + " runs your apps on servers you bring.",
		Cells: []CompareCell{
			hasNoServer("Heroku", "https://www.heroku.com/pricing"),
			hasNoServer("Heroku", "https://www.heroku.com/pricing"),
			no("Yes", "Everything runs on Heroku's dynos.", "https://www.heroku.com/pricing"),
			part("Postgres, daily", "Stored in the U.S.", "https://devcenter.heroku.com/articles/heroku-postgres-backups"),
			no("By hand", "Heroku says to verify scheduled backups yourself.", "https://devcenter.heroku.com/articles/heroku-postgres-backups"),
			part("Queries, with Dataclips", "Saved queries on Heroku Postgres.", "https://devcenter.heroku.com/articles/dataclips"),
			no("No", "Uptime monitoring is a third-party add-on.", "https://devcenter.heroku.com/articles/monitoring-apps"),
			yes("Laptop, GitHub, image", "git push, GitHub deploys or its registry.", "https://devcenter.heroku.com/articles/git"),
			part("Four ways", "Web, CLI, Platform API and MCP in early development.", "https://devcenter.heroku.com/articles/platform-api-reference"),
			part("Per app", "Releases say who deployed or changed config.", "https://devcenter.heroku.com/articles/releases"),
			no("No", "Hosted only.", "https://www.heroku.com/pricing"),
			plain("Eco $5, Postgres from $5", "No free plan.", "https://www.heroku.com/pricing"),
		},
	},
}

// CompareScore is one tool next to this one, over the facts both answered
// with a mark. Ours and Theirs are the full yeses of each side; Wins are
// the facts this product has a full yes on and the tool has not.
type CompareScore struct {
	Facts, Wins, Ours, Theirs int
}

// Score counts the facts. A tie is no win, and a cell without a mark takes
// no part.
func (c Competitor) Score() CompareScore {
	var score CompareScore
	for i, cell := range c.Cells {
		if !cell.Mark.counted() {
			continue
		}
		ours := CompareRows[i].Ours.Mark
		if !ours.counted() {
			continue
		}
		score.Facts++
		if ours == markYes {
			score.Ours++
		}
		if cell.Mark == markYes {
			score.Theirs++
		}
		if ours == markYes && cell.Mark != markYes {
			score.Wins++
		}
	}
	return score
}

// ScoreLine is the score in words, on the home page and over the table.
func (c Competitor) ScoreLine() string {
	score := c.Score()
	return fmt.Sprintf("%s wins %d of %d", brand.Name, score.Wins, score.Facts)
}

// FindCompetitor is the competitor with this slug.
func FindCompetitor(slug string) (Competitor, bool) {
	for _, competitor := range Competitors {
		if competitor.Slug == slug {
			return competitor, true
		}
	}
	return Competitor{}, false
}

// CompareSlugs are the slugs of every competitor, for the sitemap.
func CompareSlugs() []string {
	slugs := make([]string, 0, len(Competitors))
	for _, competitor := range Competitors {
		slugs = append(slugs, competitor.Slug)
	}
	return slugs
}
