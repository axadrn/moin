package compare

import (
	"fmt"

	"github.com/axadrn/moin/hub/internal/brand"
)

// Mark is how a cell reads at a glance. The answer is judged by
// what is better for you, so "No" to holding an SSH key carries a yes.
type Mark string

const (
	Yes     Mark = "yes"
	Part    Mark = "part"
	No      Mark = "no"
	Unknown Mark = "unknown"
	None    Mark = ""
)

// counted is whether a mark takes part in the score: "not documented" and
// the rows with nothing to judge, such as a price, do not count either way.
func (m Mark) counted() bool {
	return m == Yes || m == Part || m == No
}

// Cell is one answer to one row: its mark, a word or two, a line
// under them, and the page that says so.
type Cell struct {
	Mark   Mark
	Short  string
	Detail string
	Source string
}

func yes(short, detail, source string) Cell {
	return Cell{Yes, short, detail, source}
}

func part(short, detail, source string) Cell {
	return Cell{Part, short, detail, source}
}

func no(short, detail, source string) Cell {
	return Cell{No, short, detail, source}
}

func plain(short, detail, source string) Cell {
	return Cell{None, short, detail, source}
}

// notDocumented is a tool whose docs say nothing, which is weaker than "it
// does not exist": the cell counts neither for nor against it.
func notDocumented(source string) Cell {
	return Cell{Unknown, "Not documented", "", source}
}

// Row is one fact and the answer for this product; each competitor
// answers the same facts, in this order, in Competitor.Cells.
type Row struct {
	Group string
	Label string
	Ours  Cell
}

// Competitor is one other tool: its line on the home page and its page at
// /compare/<slug>.
type Competitor struct {
	Slug string
	Name string
	// Read is when its facts were read at their sources.
	Read string
	// Lead says calmly what differs.
	Lead  string
	Cells []Cell
}

// Groups are the headings of the table, in order.
var Groups = []string{"Security", "Your data", "Every day", "Price"}

// Rows are the facts every competitor answers.
var Rows = []Row{
	{"Security", "Holds an SSH key to your servers", yes("No", "The daemon on your server dials out.", "")},
	{"Security", "Port your server must open", yes("None", "None for "+brand.Name+". Visitors use 80 and 443, or a Cloudflare Tunnel.", "")},
	{"Your data", "Keeps your code and data", yes("No", "Your server and your bucket.", "")},
	{"Your data", "Database backups", yes("SQLite and Postgres", "On your schedule, in your own bucket: every 15 minutes, hourly or daily.", "")},
	{"Your data", "Restore tested every day", yes("Yes", "A real restore into a throwaway database, every day.", "")},
	{"Your data", "Browse and edit live data", yes("Yes", "SQLite and Postgres. Read, edit, run SQL.", "")},
	{"Every day", "Uptime", yes("Every minute", "Built in, with alerts.", "")},
	{"Every day", "Deploy from", yes("GitHub, image, catalog", "A GitHub push, a CI image or an app from the catalog.", "")},
	{"Every day", "Ways in", yes("All five", "Web, CLI, terminal UI, API and MCP.", "")},
	{"Every day", "Activity", yes("Yes", "Who did what, when, in every workspace.", "")},
	{"Price", "Host it yourself, free", yes("Yes", "Every feature, no license key. Open source.", "https://github.com/axadrn/moin/blob/main/LICENSE")},
	{"Price", "You pay", plain("$7 a month per workspace", "Or $49 a year. Unlimited servers, apps and team. The tenth server costs the same as the first. Every feature is in.", "")},
}

// hasNoServer is the answer on a row about your server for a tool that runs
// the apps on its own.
func hasNoServer(name, source string) Cell {
	return plain("You have no server", name+" runs them.", source)
}

// Competitors are the tools, in the order the home page lists them: the
// ones on your own servers first, then the hosted ones.
var Competitors = []Competitor{
	{
		Slug: "coolify",
		Name: "Coolify",
		Read: "November 8, 2026",
		Lead: "Both run your apps on your own servers. Coolify connects to them over SSH. The " + brand.Name + " daemon dials out instead, and " + brand.Name + " holds no key.",
		Cells: []Cell{
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
		Read: "November 8, 2026",
		Lead: "Both run your apps on your own servers. Dokploy connects to them over SSH with a key. The " + brand.Name + " daemon dials out instead, and " + brand.Name + " holds no key.",
		Cells: []Cell{
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
			plain("From $15 + $4.50 per extra server", "Dokploy Cloud Startup, 3 servers included.", "https://dokploy.com/pricing"),
		},
	},
	{
		Slug: "kamal",
		Name: "Kamal",
		Read: "November 8, 2026",
		Lead: "Kamal is a command-line tool that runs Docker over SSH, with no hub to run. " + brand.Name + " has a hub, with a web UI, an API and MCP.",
		Cells: []Cell{
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
		Slug: "dokku",
		Name: "Dokku",
		Read: "November 25, 2026",
		Lead: "Dokku runs on your server, driven over SSH with git push. A web UI and an API need paid Dokku Pro. " + brand.Name + " has both and MCP, and its daemon dials out.",
		Cells: []Cell{
			part("Your SSH key", "You add your public key to the server. Pushes log in with your key.", "https://dokku.com/docs/deployment/user-management/"),
			no("SSH", "Remote commands and git remotes use SSH. The official client uses port 22 by default.", "https://dokku.com/docs/deployment/remote-commands/"),
			yes("No", "It runs on hardware you pick.", "https://dokku.com/"),
			yes("Postgres, on a schedule", "With the official Postgres plugin. To S3 or an S3-compatible bucket.", "https://github.com/dokku/dokku-postgres"),
			no("By hand", "Its docs say to test your backups regularly yourself.", "https://dokku.com/docs/advanced-usage/backup-recovery/"),
			part("In the terminal", "postgres:connect opens the Postgres connection tool.", "https://github.com/dokku/dokku-postgres"),
			no("Deploy checks", "Checks run at deploy. For later ones the docs point to cron, checks:run and external software.", "https://dokku.com/docs/deployment/zero-downtime-deploys/"),
			part("Git, an image or an archive", "git push, a remote repository, a Docker image or an archive file.", "https://dokku.com/docs/deployment/methods/git/"),
			part("CLI, more with Pro", "Dokku is driven from the command line. A web UI and a REST API come with paid Dokku Pro.", "https://pro.dokku.com/docs/getting-started/"),
			part("With paid Pro", "Dokku Pro records every command it runs, who triggered it and the outcome.", "https://pro.dokku.com/docs/features/activity/"),
			yes("Yes", "MIT.", "https://github.com/dokku/dokku"),
			plain("Free", "The Dokku Pro add-on costs $849 once. One production server and two pre-production servers.", "https://pro.dokku.com/"),
		},
	},
	{
		Slug: "caprover",
		Name: "CapRover",
		Read: "November 25, 2026",
		Lead: "CapRover puts its dashboard on your server and leaves database backups and restore tests to you. " + brand.Name + " has them built in, and its daemon dials out.",
		Cells: []Cell{
			part("To add a server", "You give CapRover a private SSH key to add a node. Its docs do not say if it keeps the key.", "https://caprover.com/docs/scaling/multi-node"),
			part("3000 at setup", "The dashboard starts on port 3000. Its docs say to restrict it once HTTPS works.", "https://caprover.com/docs/domains/firewall"),
			yes("No", "It runs on your own server.", "https://caprover.com/"),
			no("Not built in", "Its own backup leaves out database contents. You export and schedule each database yourself.", "https://caprover.com/docs/server/backup"),
			no("By hand", "Its docs say to restore to a test server yourself and check the apps.", "https://caprover.com/docs/server/backup"),
			part("With an add-on", "Adminer, pgAdmin or phpMyAdmin, as One-Click Apps you add.", "https://github.com/caprover/one-click-apps/tree/master/public/v4/apps"),
			part("With an add-on", "Uptime Kuma, as a One-Click App you add.", "https://github.com/caprover/one-click-apps/tree/master/public/v4/apps"),
			yes("Git, image, catalog", "Git, a prebuilt image, a tar archive, your CI/CD pipeline or a One-Click App.", "https://caprover.com/"),
			part("Three ways", "Web, CLI and API.", "https://caprover.com/docs/deployments/ci-cd"),
			notDocumented("https://caprover.com/docs/server"),
			yes("Yes", "Open source. Apache-2.0.", "https://github.com/caprover/caprover/blob/master/LICENSE"),
			plain("Pro is a subscription", "Two-factor login and alerts need CapRover Pro. Its docs name no price.", "https://caprover.com/docs/server"),
		},
	},
	{
		Slug: "easypanel",
		Name: "Easypanel",
		Read: "November 25, 2026",
		Lead: "Easypanel puts its panel on your server and sells a license per server. The " + brand.Name + " daemon dials out to a hub, and one price covers every server.",
		Cells: []Cell{
			yes("No", "Easypanel is installed on the server itself.", "https://easypanel.io/docs"),
			part("80 and 443", "The install says both must be available. The panel runs on that same server.", "https://easypanel.io/docs"),
			yes("No", "Its terms say it does not host your apps or their data.", "https://easypanel.io/terms"),
			part("Four engines, paid plans", "MySQL, MariaDB, PostgreSQL and MongoDB, on a cron schedule. Schedules need a paid license.", "https://easypanel.io/docs/backups/database"),
			no("By hand", "Its docs say to practice a restore yourself on non-production data.", "https://easypanel.io/docs/backups/database"),
			part("Built-in tools", "PgWeb or DbGate, administration tools you switch on per database.", "https://easypanel.io/docs/services/postgres"),
			notDocumented("https://easypanel.io/docs/guides/notifications"),
			part("Git, an image or an upload", "A GitHub push, any Git repository, an existing image or an uploaded archive.", "https://easypanel.io/docs/services/app"),
			part("Four ways, three paid", "Web, CLI, API and MCP. CLI, API and MCP need a paid license.", "https://easypanel.io/pricing"),
			part("Recent actions", "Its API lists recent builds, deploys, backups and settings changes.", "https://easypanel.io/docs/api/actions/listActions"),
			part("Up to 3 projects", "Free forever with 3 projects. Backups, notifications, CLI, API and MCP need a paid license.", "https://easypanel.io/pricing"),
			plain("From $14.90 per server", "Hobby, a month, or $10.90 a month billed yearly. Growth $23.90 adds more users, Business $37.90.", "https://easypanel.io/pricing"),
		},
	},
	{
		Slug: "cloudron",
		Name: "Cloudron",
		Read: "November 25, 2026",
		Lead: "Cloudron installs on your server and offers ready apps from its App Store. " + brand.Name + " deploys your own code, from a hub your servers dial out to.",
		Cells: []Cell{
			yes("No", "Its docs say there is no remote access unless you grant it, and nothing reaches in from cloudron.io.", "https://docs.cloudron.io/security"),
			part("80 and 443", "The dashboard runs on the server itself, at my.example.com. The firewall must not block 80 and 443.", "https://docs.cloudron.io/installation/"),
			yes("No", "Self-hosted. Cloudron says it has no access to your server.", "https://www.cloudron.io/pricing.html"),
			yes("Per app, on a schedule", "The database and data of each app. To S3, a disk or many other stores.", "https://docs.cloudron.io/backups"),
			no("By hand", "Check integrity and the restore dry run are actions you start yourself.", "https://docs.cloudron.io/backups"),
			part("In a terminal", "The web terminal opens a database shell. Or a desktop client over an SSH tunnel.", "https://docs.cloudron.io/guides/connect-postgresql"),
			part("App down alerts", "A notice and a mail when an app is down. For the server itself, its docs say to use an external service.", "https://docs.cloudron.io/notifications"),
			part("A folder or an image", "The CLI uploads a folder and the server builds it. Or an image from a registry.", "https://docs.cloudron.io/packaging/tutorial"),
			part("Three ways", "Web, CLI and API.", "https://docs.cloudron.io/api/cloudron-api"),
			yes("Yes", "The event log records app usage and configuration changes.", "https://docs.cloudron.io/security"),
			part("Two apps", "The Free plan has two apps. Pro and Max have unlimited apps.", "https://www.cloudron.io/pricing.html"),
			plain("€15 a month, paid yearly", "Pro, unlimited apps. €30 paid monthly. Max is €25 or €50. Free for two apps. Prices without VAT.", "https://www.cloudron.io/pricing.html"),
		},
	},
	{
		Slug: "vercel",
		Name: "Vercel",
		Read: "November 25, 2026",
		Lead: "Vercel builds and runs your apps on its own platform, with databases from Marketplace providers. " + brand.Name + " runs apps and databases on servers you bring.",
		Cells: []Cell{
			hasNoServer("Vercel", "https://vercel.com/docs/functions"),
			hasNoServer("Vercel", "https://vercel.com/docs/functions"),
			no("Yes", "Vercel runs your code on its own platform. On Enterprise, functions can run in your own AWS account.", "https://vercel.com/docs/networking/bring-your-own-cloud"),
			part("Up to the provider", "Databases come from Marketplace providers like Neon or Supabase. Point-in-time recovery depends on the provider.", "https://vercel.com/docs/marketplace-storage"),
			notDocumented("https://vercel.com/docs/marketplace-storage"),
			yes("Yes", "Postgres and Redis from supported providers. Run SQL, edit rows. Owners only.", "https://vercel.com/docs/marketplace-storage"),
			no("Error and usage alerts", "Alerts on unusual errors, usage or your own metric rules. On Pro with Observability Plus, and Enterprise.", "https://vercel.com/docs/alerts"),
			part("Git, a folder or a Dockerfile", "GitHub, GitLab, Bitbucket or Azure DevOps. Or a folder from the CLI, the browser or the API. A Dockerfile in beta.", "https://vercel.com/docs/deployments"),
			part("Four ways", "Web, CLI, REST API and MCP.", "https://vercel.com/docs/agent-resources/vercel-mcp"),
			yes("Yes", "On all plans. Audit logs with export on Enterprise.", "https://vercel.com/docs/activity-log"),
			no("No", "Hosted plans only. Enterprise can run functions in its own AWS account.", "https://vercel.com/pricing"),
			plain("$20 a seat, plus use", "Pro is $20 a month with $20 of use included. Developer seats are $20 each, viewers are free. More use costs more. Hobby is free.", "https://vercel.com/pricing"),
		},
	},
	{
		Slug: "railway",
		Name: "Railway",
		Read: "November 8, 2026",
		Lead: "Railway runs your apps on its own servers. " + brand.Name + " runs them on servers you bring.",
		Cells: []Cell{
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
		Read: "November 8, 2026",
		Lead: "Render runs your apps on its own servers. " + brand.Name + " runs them on servers you bring.",
		Cells: []Cell{
			hasNoServer("Render", "https://render.com/pricing"),
			hasNoServer("Render", "https://render.com/pricing"),
			no("Yes", "Everything runs on Render's servers.", "https://render.com/pricing"),
			part("Continuous, on paid Postgres", "None on the free plan.", "https://render.com/docs/postgresql-backups"),
			notDocumented("https://render.com/docs/postgresql-backups"),
			notDocumented("https://render.com/docs/postgresql-backups"),
			no("No", "Render recommends a third-party monitor.", "https://render.com/docs/uptime-best-practices"),
			part("Git or an image", "A linked Git branch or a prebuilt image.", "https://render.com/docs/deploying-an-image"),
			yes("All five", "Web, CLI, terminal menus, API and MCP.", "https://render.com/docs/cli"),
			part("Pro and up", "Audit logs from the Pro workspace plan.", "https://render.com/docs/audit-logs"),
			no("No", "Hosted only.", "https://render.com/pricing"),
			plain("From $7 a web service", "Postgres from $6. Free plans with limits.", "https://render.com/pricing"),
		},
	},
	{
		Slug: "fly",
		Name: "Fly.io",
		Read: "November 8, 2026",
		Lead: "Fly.io runs your apps as Machines in its own regions. " + brand.Name + " runs them on servers you bring.",
		Cells: []Cell{
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
		Read: "November 8, 2026",
		Lead: "Heroku runs your apps on its own dynos, and since February 2026 it gets no new features. " + brand.Name + " runs your apps on servers you bring.",
		Cells: []Cell{
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

// Score is one tool next to this one, over the facts both answered
// with a mark. Ours and Theirs are the full yeses of each side; Wins are
// the facts this product has a full yes on and the tool has not.
type Score struct {
	Facts, Wins, Ours, Theirs int
}

// Score counts the facts. A tie is no win, and a cell without a mark takes
// no part.
func (c Competitor) Score() Score {
	var score Score
	for i, cell := range c.Cells {
		if !cell.Mark.counted() {
			continue
		}
		ours := Rows[i].Ours.Mark
		if !ours.counted() {
			continue
		}
		score.Facts++
		if ours == Yes {
			score.Ours++
		}
		if cell.Mark == Yes {
			score.Theirs++
		}
		if ours == Yes && cell.Mark != Yes {
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

// Find is the competitor with this slug.
func Find(slug string) (Competitor, bool) {
	for _, competitor := range Competitors {
		if competitor.Slug == slug {
			return competitor, true
		}
	}
	return Competitor{}, false
}

// Slugs are the slugs of every competitor, for the sitemap.
func Slugs() []string {
	slugs := make([]string, 0, len(Competitors))
	for _, competitor := range Competitors {
		slugs = append(slugs, competitor.Slug)
	}
	return slugs
}
