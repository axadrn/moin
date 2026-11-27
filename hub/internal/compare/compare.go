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
var Groups = []string{"Your server", "Your data", "Every day"}

// Rows are the facts every competitor answers. A row whose answers carry no
// mark is a fact to know, not a point: it counts for nobody.
var Rows = []Row{
	{"Your server", "Who can command your server", yes("Who has SSH", "Your key, over SSH. The watcher and "+brand.Name+" cloud cannot.", "")},
	{"Your server", "Open to the internet", yes("80 and 443", "Public SSH closes after the first deploy. With a Cloudflare Tunnel, no port at all.", "")},
	{"Your data", "Database backups", yes("SQLite and Postgres", "Found by themselves. On a schedule, into your bucket.", "")},
	{"Your data", "Restored and checked every day", yes("Yes", "The newest copy is restored into a throwaway database and read.", "")},
	{"Your data", "A copy a hacked server can't delete", plain("Coming, with "+brand.Name+" cloud", "The server can add copies there, never delete them. The server's key to your own bucket can delete, like everyone's.", "")},
	{"Every day", "Notices when a server is gone", yes("The watcher", "On another server, or "+brand.Name+" cloud. It alerts when a server goes silent.", "")},
	{"Every day", "What you write", yes("Nothing, or a compose file", "No Dockerfile needed. "+brand.Name+" builds without one.", "")},
	{"Every day", "Where it builds", yes("On your server", "The source goes over SSH. No Docker on your laptop, no registry.", "")},
	{"Every day", "Several servers, no downtime", plain("One server per app", brand.Name+" is built for one server. For several, look at uncloud or Kamal.", "")},
	{"Every day", "Open source", yes("Yes", "All of it.", "")},
}

// Competitors are the tools, in the order the home page lists them.
var Competitors = []Competitor{
	{
		Slug: "coolify",
		Name: "Coolify",
		Read: "November 27, 2026",
		Lead: "Coolify is a dashboard that logs in to your servers over SSH with a key it keeps. " + brand.Name + " has no dashboard: only who has SSH can command your server.",
		Cells: []Cell{
			no("Whoever runs Coolify", "It logs in over SSH with your private key, as root or a sudo user. Coolify Cloud gets that key from you.", "https://coolify.io/docs/start-with-cloud"),
			part("SSH, or a Cloudflare Tunnel", "Port 22 open to Coolify, unless a tunnel carries it.", "https://coolify.io/docs/knowledge-base/server/firewall"),
			yes("Six engines, on a schedule", "PostgreSQL, MySQL, MariaDB, MongoDB, ClickHouse and SQLite. On the server, and in S3 if you add it.", "https://coolify.io/docs/databases/backups"),
			no("By hand", "Its docs: a successful backup only proves that Coolify created a file. Restore one yourself to check it.", "https://coolify.io/docs/databases/backups"),
			plain("Its key can delete", "Coolify removes old copies from S3 by your retention, and deletes a backup from every S3 storage.", "https://coolify.io/docs/databases/backups"),
			yes("Yes", "It alerts when it cannot reach a server after two checks.", "https://coolify.io/docs/core/notifications/events"),
			part("Settings in its dashboard", "A Git repository, a Dockerfile or an image, set up in the dashboard.", "https://coolify.io/docs/applications/"),
			yes("On your server", "By default on the server it deploys to. A build server is optional.", "https://coolify.io/docs/core/infrastructure/servers/build-servers"),
			plain("Several servers. Rolling updates, not for compose", "Its docs say rolling updates do not guarantee zero downtime.", "https://coolify.io/docs/applications/deployments/rolling-updates"),
			yes("Yes", "Apache-2.0.", "https://github.com/coollabsio/coolify"),
		},
	},
	{
		Slug: "dokploy",
		Name: "Dokploy",
		Read: "November 27, 2026",
		Lead: "Dokploy is a dashboard that connects to your servers over SSH with a key. " + brand.Name + " has no dashboard: only who has SSH can command your server.",
		Cells: []Cell{
			no("Whoever runs Dokploy", "It connects over SSH with a key, often as root. Dokploy Cloud does it from its own control plane.", "https://docs.dokploy.com/docs/core/cloud"),
			no("SSH", "It has to reach the server over SSH.", "https://docs.dokploy.com/docs/core/cloud"),
			yes("Four engines, on a schedule", "PostgreSQL, MySQL, MariaDB and MongoDB, to S3.", "https://docs.dokploy.com/docs/core/databases/backups"),
			no("By hand", "Its docs say to run a backup and check the bucket for the result.", "https://docs.dokploy.com/docs/core/databases/backups"),
			plain("Its key can delete", "Keep the latest N backups removes older ones from the bucket.", "https://docs.dokploy.com/docs/core/databases/backups"),
			part("Deploys, backups and load", "Its alerts list builds, deploys, backups, cleanup, restarts and CPU or memory thresholds.", "https://docs.dokploy.com/docs/core/overview"),
			part("Settings in its dashboard", "GitHub, GitLab, Bitbucket, Gitea, Git, Docker, or a zip you upload.", "https://docs.dokploy.com/docs/core/providers"),
			notDocumented("https://docs.dokploy.com/docs/core/remote-servers/instructions"),
			plain("Several servers", "Remote servers over SSH.", "https://docs.dokploy.com/docs/core/remote-servers/instructions"),
			yes("Yes", "Apache-2.0, except a source-available part.", "https://github.com/Dokploy/dokploy/blob/canary/LICENSE.MD"),
		},
	},
	{
		Slug: "kamal",
		Name: "Kamal",
		Read: "November 27, 2026",
		Lead: "Both run over SSH with your key. Kamal builds on your laptop and deploys to many servers. " + brand.Name + " builds on the server and checks your backups every day.",
		Cells: []Cell{
			yes("Who has SSH", "Servers need nothing but your SSH key.", "https://kamal-deploy.org/"),
			no("SSH", "Port 22 by default.", "https://kamal-deploy.org/docs/configuration/ssh/"),
			no("Not built in", "Its docs on accessories, where a database runs, do not mention backups.", "https://kamal-deploy.org/docs/configuration/accessories/"),
			no("No backups", "Nothing to restore.", "https://kamal-deploy.org/docs/configuration/accessories/"),
			plain("No backups", "Nothing to keep.", "https://kamal-deploy.org/docs/configuration/accessories/"),
			notDocumented("https://kamal-deploy.org/docs/configuration/proxy/"),
			no("deploy.yml and a Dockerfile", "kamal init makes config/deploy.yml. The Dockerfile sits at the top of your project.", "https://kamal-deploy.org/docs/installation/"),
			part("Your laptop, then a registry", "With localhost as the registry server, Kamal runs a local registry.", "https://kamal-deploy.org/docs/configuration/docker-registry/"),
			plain("Yes", "A list of servers, zero-downtime deploys.", "https://kamal-deploy.org/"),
			yes("Yes", "MIT.", "https://github.com/basecamp/kamal"),
		},
	},
	{
		Slug: "uncloud",
		Name: "uncloud",
		Read: "November 27, 2026",
		Lead: "Both run over SSH, with no control plane. uncloud runs compose across machines without downtime. " + brand.Name + " is one server, and checks backups every day.",
		Cells: []Cell{
			yes("Who has SSH", "uc is a CLI that connects to your machines over SSH. No control plane.", "https://uncloud.run/"),
			no("SSH", "uc connects over SSH.", "https://uncloud.run/"),
			no("Not built in", "Its docs list no backups.", "https://uncloud.run/docs/"),
			no("No backups", "Nothing to restore.", "https://uncloud.run/docs/"),
			plain("No backups", "Nothing to keep.", "https://uncloud.run/docs/"),
			no("Coming", "Alerts come with Uncloud Hub, marked coming soon.", "https://uncloud.run/"),
			yes("A compose file", "Standard Compose, no new spec.", "https://uncloud.run/"),
			part("Your laptop", "Built with your local Docker, then pushed straight to the machines. No registry.", "https://uncloud.run/docs/guides/deployments/deploy-app"),
			plain("Yes", "A cluster of machines. Rolling deploys start the new container first, so there is no downtime.", "https://uncloud.run/docs/guides/deployments/rolling-deployments"),
			yes("Yes", "Apache-2.0.", "https://github.com/psviderski/uncloud"),
		},
	},
	{
		Slug: "dokku",
		Name: "Dokku",
		Read: "November 27, 2026",
		Lead: "Both run on your server, driven over SSH with your key. Dokku leaves the restore test to you. " + brand.Name + " restores and checks a copy every day.",
		Cells: []Cell{
			yes("Who has SSH", "You add your public key to the server. Commands and pushes log in with it.", "https://dokku.com/docs/deployment/user-management/"),
			no("SSH", "Remote commands and git remotes use SSH. The official client uses port 22 by default.", "https://dokku.com/docs/deployment/remote-commands/"),
			yes("Postgres, on a schedule", "With the official Postgres plugin. To S3 or an S3-compatible bucket.", "https://github.com/dokku/dokku-postgres"),
			no("By hand", "Its docs say to test your backups regularly yourself.", "https://dokku.com/docs/advanced-usage/backup-recovery/"),
			notDocumented("https://github.com/dokku/dokku-postgres"),
			notDocumented("https://dokku.com/docs/deployment/zero-downtime-deploys/"),
			yes("Nothing, or a Dockerfile", "git push with buildpacks, a Dockerfile or an image.", "https://dokku.com/docs/deployment/methods/git/"),
			yes("On your server", "git push sends the source. The server builds it.", "https://dokku.com/docs/deployment/methods/git/"),
			plain("One server, more with k3s", "Several servers with its k3s scheduler.", "https://dokku.com/docs/deployment/schedulers/k3s/"),
			yes("Yes", "MIT.", "https://github.com/dokku/dokku"),
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
