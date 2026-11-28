package watchers

import (
	"context"
	"log/slog"
	"time"

	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/internal/build"
)

// mailTrials sends the trial mails that are due: five days before the
// end, and at the end. Each goes out once.
func (ws *Watchers) mailTrials(ctx context.Context) {
	due, err := ws.store.TrialMailsDue(time.Now())
	if err != nil {
		slog.Error("trial mails", "error", err)
		return
	}
	for _, m := range due {
		message := trialMail(m.Day, m.Workspace.TrialEnds, ws.appURL)
		message.To = m.Workspace.Mail
		if err := ws.mailer.Send(ctx, message); err != nil {
			slog.Error("trial mail not sent, trying again soon", "workspace", m.Workspace.ID, "error", err)
			continue
		}
		if err := ws.store.TrialMailSent(m.Workspace.ID, m.Day); err != nil {
			slog.Error("trial mail sent, but not noted", "workspace", m.Workspace.ID, "error", err)
		}
	}
}

// trialMail is the mail of day 25 or day 30 of the trial.
func trialMail(day int, ends time.Time, appURL string) mailer.Message {
	name := build.Name + " cloud"
	if day == 25 {
		return mailer.Message{
			Subject: "Your " + name + " trial ends in 5 days",
			Text: "Your trial of " + name + " ends on " + ends.Format("January 2, 2006") + ". After that your servers still report, but no alert goes out.\n\n" +
				"Keep the alerts on: " + appURL + "/cloud",
		}
	}
	return mailer.Message{
		Subject: "Your " + name + " trial is over. Alerts are off",
		Text: "Your servers still report to " + name + ", but no alert goes out when one goes silent.\n\n" +
			"Turn the alerts back on: " + appURL + "/cloud",
	}
}
