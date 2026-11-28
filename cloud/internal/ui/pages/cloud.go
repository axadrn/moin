package pages

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/cloud/internal/brand"
	"github.com/axadrn/moin/cloud/internal/ui/components"
	"github.com/axadrn/moin/cloud/internal/workspace"
	"github.com/axadrn/moin/internal/watcher"
)

// Cloud is what every page of a signed-in workspace shows in its bar:
// whose it is and its plan.
type Cloud struct {
	Mail string
	Plan Plan
}

// Plan is the plan of a workspace in words: its name, a few words after
// it, its state, a sentence, and what it offers: to subscribe, or Billing
// at Stripe.
type Plan struct {
	Name, Note, Text string
	State            components.State
	// AlertsOn says whether alerts and copies are on.
	AlertsOn bool
	// Subscribe offers the prices. Billing links Stripe's portal.
	Subscribe, Billing bool
}

// PlanOf is the plan of space at now. sells says whether this site can
// sell: it has Stripe.
func PlanOf(space workspace.Workspace, now time.Time, sells bool) Plan {
	plan := Plan{
		AlertsOn:  space.AlertsOn(now),
		Subscribe: sells && space.Status != "paid",
		Billing:   sells && space.StripeCustomer != "",
	}
	switch {
	case space.Status == "paid":
		plan.Name, plan.State = "Paid", components.StateLive
		plan.Text = "Alerts and copies are on. Invoices, the card and cancel are in Billing."
		if !space.PaidUntil.IsZero() {
			plan.Note = "until " + space.PaidUntil.Format("Jan 2, 2006")
			plan.Text = "Paid until " + longDate(space.PaidUntil) + ". " + plan.Text
		}
	case !plan.AlertsOn:
		plan.Name, plan.State = "Alerts off", components.StateFailed
		plan.Note = "subscription ended"
		if space.Status == "trial" {
			plan.Note = "trial ended " + space.TrialEnds.Format("Jan 2")
		}
		plan.Text = "Your servers still report, and this page still shows them. Nobody gets a mail when one goes silent, and no new copy is taken."
	case space.TrialDays > 30:
		plan.Name, plan.State = "Free year", components.StateLive
		plan.Note = "starts with the first report"
		plan.Text = "A year with alerts and copies, from the first report. Then $49 a year. The launch price stays yours."
		if !space.TrialEnds.IsZero() {
			plan.Note = "until " + space.TrialEnds.Format("Jan 2, 2006")
			plan.Text = "Free until " + longDate(space.TrialEnds) + ". Then $49 a year. The launch price stays yours."
		}
	case space.TrialEnds.IsZero():
		plan.Name, plan.State = "Trial", components.StateIdle
		plan.Note = "starts with the first report"
		plan.Text = strconv.Itoa(space.TrialDays) + " days with alerts and copies, from the first report. 5 GB of copies during the trial. No card needed."
	default:
		plan.Name, plan.State = "Trial", components.StateIdle
		plan.Note = daysLeft(space.TrialEnds.Sub(now))
		plan.Text = "Alerts and copies are on until " + longDate(space.TrialEnds) + ". 5 GB of copies during the trial. No card needed."
	}
	return plan
}

// longDate is a date as the pages write it: November 28, 2026.
func longDate(t time.Time) string {
	return t.Format("January 2, 2006")
}

// daysLeft is how many days are left, rounded up.
func daysLeft(d time.Duration) string {
	days := int((d + 24*time.Hour - 1) / (24 * time.Hour))
	if days == 1 {
		return "1 day left"
	}
	return strconv.Itoa(days) + " days left"
}

// cloudFreeYear is where a bug or an idea wins a free year of moin cloud:
// the same place the landing page points to.
const cloudFreeYear = homeFreeYear

// connectCommand is the command that connects a server to moin cloud and
// signs the laptop in.
func connectCommand(server string) string {
	return brand.Name + " watcher connect " + server
}

// summary counts what a workspace's servers report, for the overview.
type summary struct {
	Servers, Silent       int
	Apps, Stopped         int
	Databases, Fine       int
	Domains, Up           int
	KnocksToday, KnocksYr int64
	// FirstRed is the first database with a problem, FirstDown the
	// first server or app that is not fine.
	FirstRed string
}

// summarize counts the servers, apps, databases and domains of v.
func summarize(servers []watcher.ServerView) summary {
	var s summary
	for _, server := range servers {
		c := serverCount(server)
		s.Servers++
		if server.Problem != "" {
			s.Silent++
		}
		s.Apps += c.Apps
		s.Stopped += c.Stopped
		s.Databases += c.Databases
		s.Fine += c.Fine
		s.Domains += c.Domains
		s.Up += c.Up
		s.KnocksToday += c.KnocksToday
		s.KnocksYr += c.KnocksYr
		if s.FirstRed == "" {
			s.FirstRed = c.FirstRed
		}
	}
	return s
}

// serverCount counts one server.
func serverCount(server watcher.ServerView) summary {
	s := summary{Servers: 1}
	if server.Knocks != nil {
		s.KnocksToday, s.KnocksYr = server.Knocks.Today, server.Knocks.Year
	}
	for _, app := range server.Apps {
		s.Apps++
		if app.Problem != "" {
			s.Stopped++
		}
		for _, d := range app.Domains {
			s.Domains++
			if d.Problem == "" {
				s.Up++
			}
		}
		for _, db := range app.Databases {
			s.Databases++
			if db.Problem == "" {
				s.Fine++
			} else if s.FirstRed == "" {
				s.FirstRed = db.Name
			}
		}
	}
	return s
}

// serverState is the dot of a server: red when it is silent, open before
// its first report, green while it reports.
func serverState(server watcher.ServerView) components.State {
	switch {
	case server.Problem != "":
		return components.StateFailed
	case server.Version == "":
		return components.StateIdle
	}
	return components.StateLive
}

// serverWord is the word next to a server's dot.
func serverWord(server watcher.ServerView) string {
	switch {
	case server.Problem != "":
		return "silent"
	case server.Version == "":
		return "no report yet"
	}
	return "online"
}

// problemsOf is what is red on one server, one sentence each.
func problemsOf(server watcher.ServerView) []string {
	var out []string
	if server.Problem != "" {
		out = append(out, server.Problem)
	}
	for _, app := range server.Apps {
		if app.Problem != "" {
			out = append(out, app.Problem)
		}
		for _, line := range slices.Concat(app.Domains, app.Databases) {
			if line.Problem != "" {
				out = append(out, line.Problem)
			}
		}
	}
	return out
}

// firstRed is the first server that is not fine, the one the overview's
// notice opens.
func firstRed(servers []watcher.ServerView) string {
	for _, server := range servers {
		if !server.OK {
			return server.Name
		}
	}
	return ""
}

// serverHref is the page of a server.
func serverHref(name string) string {
	return "/cloud/servers/" + name
}

// okState is the dot of something fine or red.
func okState(ok bool) components.State {
	if ok {
		return components.StateLive
	}
	return components.StateFailed
}

// lineState is the dot of a domain or a database: red with a problem,
// open while a domain was not visited yet, green otherwise.
func lineState(line watcher.LineView) components.State {
	switch {
	case line.Problem != "":
		return components.StateFailed
	case strings.HasPrefix(line.State, "not visited"):
		return components.StateIdle
	}
	return components.StateLive
}

// outageState is the dot of an outage: red while it lasts, quiet after.
func outageState(open bool) components.State {
	if open {
		return components.StateFailed
	}
	return components.StateIdle
}

// checkState is the dot of a restore check: a quiet check when it
// restored, red when not.
func checkState(ok bool) components.State {
	if ok {
		return components.StateDone
	}
	return components.StateFailed
}

// outageNote says how long an outage lasted, and why.
func outageNote(o watcher.OutageView) string {
	note := o.Lasted
	if o.Reason != "" {
		note += ": " + o.Reason
	}
	return note
}

// newestChecks is the newest check of each database, newest first.
func newestChecks(checks []watcher.CheckView) []watcher.CheckView {
	seen := map[string]bool{}
	var out []watcher.CheckView
	for _, c := range checks {
		key := c.Server + "\x00" + c.What
		if !seen[key] {
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}

// first is at most n of list.
func first[T any](list []T, n int) []T {
	return list[:min(len(list), n)]
}

// reportHere says how many servers report here.
func reportHere(n int) string {
	if n == 1 {
		return "1 server reports here."
	}
	return strconv.Itoa(n) + " servers report here."
}

// plural is n and its word, with an s when it is not one.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// database is one database of a server, with the app it belongs to.
type database struct {
	watcher.LineView
	App string
}

// databasesOf are the databases of every app of a server.
func databasesOf(server watcher.ServerView) []database {
	var out []database
	for _, app := range server.Apps {
		for _, db := range app.Databases {
			out = append(out, database{LineView: db, App: app.Name})
		}
	}
	return out
}
