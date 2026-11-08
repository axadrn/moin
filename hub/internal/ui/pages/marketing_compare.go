package pages

import "github.com/axadrn/moin/hub/internal/brand"

// compareRead is when every competitor fact below was read at its source.
const compareRead = "November 8, 2026"

// CompareRow is one question of the comparison and the answer for this
// product; each competitor answers the same questions, in this order, in
// Competitor.Cells.
type CompareRow struct {
	Label string
	Ours  string
}

// CompareCell is a competitor's answer to one row and the page that says so.
type CompareCell struct {
	Text   string
	Source string
}

// Competitor is one other tool: its page at /compare/<slug> and its line on
// the home page. "Not documented" means its own docs say nothing, which is
// weaker than "does not exist".
type Competitor struct {
	Slug string
	Name string
	// Note is the line on the home page: where the apps run.
	Note string
	// Lead says in a few sentences what differs.
	Lead string
	// Them and Us are when to pick which: Them first, because it is the
	// honest half.
	Them  []string
	Us    []string
	Cells []CompareCell
}

// CompareRows are the questions every competitor answers.
var CompareRows = []CompareRow{
	{"Where apps run", "On your servers."},
	{"How it reaches them", "The daemon on your server dials out to the hub. " + brand.Name + " holds no SSH key."},
	{"Open port it needs", "None. Visitors use 80 and 443, or a Cloudflare Tunnel."},
	{"Price", "Self-hosted is free. Hosted is $29 a month per workspace."},
	{"Source code", "AGPL-3.0, all of it."},
	{"Database backups", "SQLite and Postgres, on your schedule, in your bucket."},
	{"Back to any moment", "SQLite only, with Every change. Postgres goes back to a copy, made as often as every 15 minutes."},
	{"File backups", "No. Databases only."},
	{"Check that a copy restores", "Yes. Every day, by itself."},
	{"MCP for AI agents", "Yes, built into the hub. It can deploy from GitHub or an image."},
}

const notDocumented = "Not documented."

// Competitors are the seven tools, in the order the home page lists them.
var Competitors = []Competitor{
	{
		Slug: "coolify",
		Name: "Coolify",
		Note: "On your servers, over SSH.",
		Lead: "Both run your apps on your own servers. Coolify connects to them over SSH. The " + brand.Name + " daemon calls out instead, and " + brand.Name + " holds no key. Coolify can also use a Cloudflare Tunnel, so no open port is not unique to " + brand.Name + ".",
		Them: []string{
			"You want files backed up too, not only databases.",
			"You run MySQL, MariaDB, MongoDB or ClickHouse and want them backed up.",
		},
		Us: []string{
			"You want a daily check that a copy restores.",
			"You want no tool to hold a key to your server.",
			"You want a rules file on the server that limits what the hub may do.",
		},
		Cells: []CompareCell{
			{"On your servers. Coolify Cloud uses your servers too.", "https://coolify.io/pricing"},
			{"Over SSH from Coolify. It can also go through a Cloudflare Tunnel.", "https://coolify.io/docs/knowledge-base/server/introduction"},
			{"Port 22 open to Coolify, or a Cloudflare Tunnel instead.", "https://coolify.io/docs/knowledge-base/server/firewall"},
			{"Free to host yourself. Coolify Cloud from $5 a month with 2 servers.", "https://coolify.io/pricing"},
			{"Apache-2.0.", "https://github.com/coollabsio/coolify"},
			{"PostgreSQL, MySQL, MariaDB, MongoDB, ClickHouse and SQLite, on a schedule, on the server and in S3 if you add it.", "https://coolify.io/docs/databases/backups"},
			{notDocumented, "https://coolify.io/docs/databases/backups"},
			{"Yes. Named volumes and folders, on a schedule. No restore in the dashboard.", "https://coolify.io/docs/core/persistent-storage/storage-mounts/backups"},
			{"Not documented. Checking a copy is up to you.", "https://coolify.io/docs/databases/backups"},
			{"Yes, built in. Can deploy with a token that allows it.", "https://coolify.io/docs/mcp/setup"},
		},
	},
	{
		Slug: "dokploy",
		Name: "Dokploy",
		Note: "On your servers, over SSH.",
		Lead: "Both run your apps on your own servers. Dokploy connects to them over SSH with a key. The " + brand.Name + " daemon calls out instead, and " + brand.Name + " holds no key.",
		Them: []string{
			"You want Docker volumes backed up too, not only databases.",
			"You run MySQL, MariaDB or MongoDB and want them backed up.",
		},
		Us: []string{
			"You want a daily check that a copy restores.",
			"You want no tool to hold a key to your server.",
			"You want all of the code open source. Some Dokploy parts are under a source-available license.",
		},
		Cells: []CompareCell{
			{"On your servers, also with Dokploy Cloud.", "https://docs.dokploy.com/docs/core/cloud"},
			{"Over SSH, with a key pair from Dokploy.", "https://docs.dokploy.com/docs/core/remote-servers/instructions"},
			{"SSH has to be reachable for Dokploy.", "https://docs.dokploy.com/docs/core/cloud"},
			{"Free to host yourself. Dokploy Cloud from $4.50 a month per server.", "https://dokploy.com/pricing"},
			{"Apache-2.0, except a proprietary part under a source-available license.", "https://github.com/Dokploy/dokploy/blob/canary/LICENSE.MD"},
			{"PostgreSQL, MySQL, MariaDB and MongoDB, on a schedule, to S3.", "https://docs.dokploy.com/docs/core/databases/backups"},
			{notDocumented, "https://docs.dokploy.com/docs/core/databases/backups"},
			{"Yes. Docker named volumes, to S3.", "https://docs.dokploy.com/docs/core/volume-backups"},
			{"Not documented. Its test button runs a backup, not a restore.", "https://docs.dokploy.com/docs/core/databases/backups"},
			{"Yes, the @dokploy/mcp package. It can deploy.", "https://github.com/Dokploy/mcp"},
		},
	},
	{
		Slug: "kamal",
		Name: "Kamal",
		Note: "On your servers, over SSH from your laptop or CI.",
		Lead: "Kamal is a command-line tool that runs Docker over SSH. There is nothing to sign in to and no hub to run. " + brand.Name + " has a hub, with a web UI, an API and MCP.",
		Them: []string{
			"You want a free command-line tool and nothing else.",
			"You want no hub at all, only SSH and your own config.",
		},
		Us: []string{
			"You want backups in the tool. Kamal documents none.",
			"You want a web UI, a terminal UI, an API and MCP.",
			"You want no SSH key on every laptop and CI that deploys.",
		},
		Cells: []CompareCell{
			{"On your servers.", "https://kamal-deploy.org/"},
			{"Over SSH from where you run it, as root by default, with your SSH key.", "https://kamal-deploy.org/docs/installation/"},
			{"SSH, port 22 by default, and 80 and 443 for its proxy. A jump host also works.", "https://kamal-deploy.org/docs/configuration/ssh/"},
			{"Free. No paid tier found.", "https://github.com/basecamp/kamal"},
			{"MIT.", "https://github.com/basecamp/kamal"},
			{notDocumented, "https://github.com/basecamp/kamal-site"},
			{notDocumented, "https://github.com/basecamp/kamal-site"},
			{notDocumented, "https://github.com/basecamp/kamal-site"},
			{notDocumented, "https://github.com/basecamp/kamal-site"},
			{notDocumented, "https://github.com/basecamp/kamal-site"},
		},
	},
	{
		Slug: "railway",
		Name: "Railway",
		Note: "On Railway's servers.",
		Lead: "Railway runs your apps on its own servers, so you have no server to look after. " + brand.Name + " runs them on servers you bring.",
		Them: []string{
			"You do not want to look after a server.",
			"You want point-in-time recovery for Postgres or MySQL. " + brand.Name + " has that only for SQLite.",
		},
		Us: []string{
			"You want your apps on your own servers.",
			"You want backups in your own bucket.",
			"You want a daily check that a copy restores.",
		},
		Cells: []CompareCell{
			{"On Railway's servers.", "https://docs.railway.com/platform/compare-to-northflank"},
			{"Railway runs them. You have no server to reach.", "https://docs.railway.com/platform/compare-to-northflank"},
			{"None: you have no server.", "https://docs.railway.com/platform/compare-to-northflank"},
			{"Hobby is $5 a month with $5 of use included. More use costs more.", "https://railway.com/pricing"},
			{"No public source found for the platform. CLI: MIT.", "https://github.com/railwayapp/cli"},
			{"Daily, weekly and monthly, on the Pro plan.", "https://docs.railway.com/reference/backups"},
			{"Point-in-time recovery for Postgres and MySQL, on the Pro plan.", "https://docs.railway.com/volumes/point-in-time-recovery"},
			{"Yes. Volume backups, on the Pro plan.", "https://docs.railway.com/reference/backups"},
			{"Not documented. Railway describes doing it by hand.", "https://railway.com/guides/postgres-backups-restores"},
			{"Yes, hosted by Railway. It can deploy.", "https://docs.railway.com/ai/mcp-server"},
		},
	},
	{
		Slug: "render",
		Name: "Render",
		Note: "On Render's servers.",
		Lead: "Render runs your apps on its own servers, so you have no server to look after. " + brand.Name + " runs them on servers you bring.",
		Them: []string{
			"You do not want to look after a server.",
			"You want point-in-time recovery for Postgres. " + brand.Name + " has that only for SQLite.",
		},
		Us: []string{
			"You want your apps on your own servers.",
			"You want backups in your own bucket.",
			"You want a daily check that a copy restores.",
		},
		Cells: []CompareCell{
			{"On Render's servers.", "https://render.com/pricing"},
			{"Render runs them. You have no server to reach.", "https://render.com/pricing"},
			{"None: you have no server.", "https://render.com/pricing"},
			{"A web service from $7 a month, Postgres from $6. Free plans with limits.", "https://render.com/pricing"},
			{"No public source found for the platform. CLI and MCP server: Apache-2.0.", "https://github.com/render-oss/cli"},
			{"Continuous on paid Postgres. None on the free plan.", "https://render.com/docs/postgresql-backups"},
			{"Point-in-time recovery for Postgres: 3 days on Hobby, 7 on Pro and up.", "https://render.com/docs/postgresql-backups"},
			{"Yes. Disks are saved every 24 hours and kept 7 days.", "https://render.com/docs/disks"},
			{"Not documented. You check the restored database yourself.", "https://render.com/docs/postgresql-backups"},
			{"Yes, hosted by Render. It can start deploys.", "https://render.com/docs/mcp-server"},
		},
	},
	{
		Slug: "fly",
		Name: "Fly.io",
		Note: "On Fly's servers.",
		Lead: "Fly.io runs your apps as Machines in its own regions, so you have no server to look after. " + brand.Name + " runs them on servers you bring.",
		Them: []string{
			"You want your apps in Fly's regions and no server to look after.",
			"You want Managed Postgres with point-in-time recovery. " + brand.Name + " has that only for SQLite.",
		},
		Us: []string{
			"You want your apps on your own servers.",
			"You want backups in your own bucket.",
			"You want a daily check that a copy restores.",
		},
		Cells: []CompareCell{
			{"On Fly's servers.", "https://docs.fly.io/about/pricing"},
			{"Fly runs them. You have no server to reach.", "https://docs.fly.io/about/pricing"},
			{"None: you have no server.", "https://docs.fly.io/about/pricing"},
			{"A Machine from $2.19 a month, Managed Postgres from $38. No free tier for new accounts.", "https://docs.fly.io/about/pricing"},
			{"No public source found for the platform. flyctl: Apache-2.0.", "https://github.com/superfly/flyctl"},
			{"Managed Postgres, by itself, kept ten days.", "https://fly.io/mpg/"},
			{"Point-in-time recovery for Managed Postgres, into a new cluster.", "https://docs.fly.io/flyctl/mpg-restore"},
			{"Yes. Volumes are saved daily and kept 5 days unless you change it.", "https://docs.fly.io/volumes/snapshots"},
			{notDocumented, "https://docs.fly.io/mpg"},
			{"Experimental, as fly mcp server.", "https://docs.fly.io/flyctl/mcp-server/"},
		},
	},
	{
		Slug: "heroku",
		Name: "Heroku",
		Note: "On Heroku's servers.",
		Lead: "Heroku runs your apps on its own dynos, so you have no server to look after. In February 2026 it said it will focus on stability and support rather than new features. It named no end date. " + brand.Name + " runs your apps on servers you bring.",
		Them: []string{
			"You like git push heroku main and want no server to look after.",
			"You want point-in-time recovery for Postgres, on a Standard plan or above. " + brand.Name + " has that only for SQLite.",
		},
		Us: []string{
			"You want your apps on your own servers.",
			"You want backups in your own bucket, wherever you like. Heroku stores its Postgres backups in the U.S.",
			"You want a daily check that a copy restores.",
		},
		Cells: []CompareCell{
			{"On Heroku's dynos.", "https://www.heroku.com/pricing"},
			{"Heroku runs them. You have no server to reach.", "https://www.heroku.com/pricing"},
			{"None: you have no server.", "https://www.heroku.com/pricing"},
			{"Eco is $5 a month, Postgres from $5. No free plan.", "https://www.heroku.com/pricing"},
			{"No public source found for the platform. CLI: ISC. MCP server: Apache-2.0.", "https://github.com/heroku/cli"},
			{"Postgres, daily at a time you pick. Stored in the U.S.", "https://devcenter.heroku.com/articles/heroku-postgres-backups"},
			{"Rollback, Heroku's point-in-time recovery for Postgres. Not on Essential plans.", "https://devcenter.heroku.com/articles/heroku-postgres-rollback"},
			{"No: files on a dyno are gone when it restarts.", "https://devcenter.heroku.com/articles/dynos"},
			{"Not documented. Heroku says to verify scheduled backups by hand.", "https://devcenter.heroku.com/articles/heroku-postgres-backups"},
			{"Yes, in early development.", "https://devcenter.heroku.com/articles/heroku-mcp-server"},
		},
	},
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
