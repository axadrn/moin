package daemon

import (
	"os"
	"strings"
	"testing"
)

// The CLI sets a schedule for one app or for the server. The comments
// in the config stay, and what is too short is refused.
func TestScheduleHere(t *testing.T) {
	file := useConfig(t, `backups:
  # The shop changes all day.
  apps:
    shop:
      every: 15m # often
`)
	if got, err := ScheduleHere("blog", Schedule{}); err != nil || got != (BackupSchedule{App: "blog", Every: "1h", Keep: "7d"}) {
		t.Errorf("an app without its own: %+v %v", got, err)
	}
	if got, err := ScheduleHere("shop", Schedule{Keep: "30d"}); err != nil || got != (BackupSchedule{App: "shop", Every: "15m", Keep: "30d", Own: true}) {
		t.Errorf("keep for the shop: %+v %v", got, err)
	}
	if got, err := ScheduleHere("", Schedule{Every: "6h"}); err != nil || got != (BackupSchedule{Every: "6h", Keep: "7d", Own: true}) {
		t.Errorf("the server's: %+v %v", got, err)
	}
	if got, err := ScheduleHere("blog", Schedule{}); err != nil || got.Every != "6h" || got.Own {
		t.Errorf("an app takes the server's: %+v %v", got, err)
	}
	if _, err := ScheduleHere("blog", Schedule{Every: "1m"}); err == nil || !strings.Contains(err.Error(), "the least is 5m") {
		t.Errorf("every minute: %v", err)
	}
	if _, err := ScheduleHere("blog", Schedule{Keep: "a week"}); err == nil {
		t.Error("a word that is no span passed")
	}
	if _, err := ScheduleHere("../x", Schedule{Every: "1h"}); err == nil {
		t.Error("a name that is no app passed")
	}
	body, _ := os.ReadFile(file)
	for _, kept := range []string{"# The shop changes all day.", "every: 15m # often", "keep: 30d", "every: 6h"} {
		if !strings.Contains(string(body), kept) {
			t.Errorf("%q is not in the config:\n%s", kept, body)
		}
	}
	if strings.Contains(string(body), "blog") {
		t.Errorf("a refused schedule was written:\n%s", body)
	}
}
