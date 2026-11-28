package daemon

import (
	"cmp"
	"fmt"
	"time"

	"github.com/axadrn/moin/protocol"
)

// BackupSchedule is how often the databases of an app are copied and how
// long each copy is kept, as the server runs it. App is "" for the
// server's own schedule, which every app without one takes. Own says it
// is written in the config, not taken from the server or the default.
type BackupSchedule struct {
	App   string `json:"app,omitempty"`
	Every string `json:"every"`
	Keep  string `json:"keep"`
	Own   bool   `json:"own"`
}

// ScheduleHere is the backup schedule of app, or of the server for "".
// What set names changes first, in the config; the comments in it stay
// (SaveConfig). The daemon reads it again within a minute.
func ScheduleHere(app string, set Schedule) (BackupSchedule, error) {
	if app != "" && !protocol.ValidAppName(app) {
		return BackupSchedule{}, fmt.Errorf("%q is not an app name", app)
	}
	config, err := readConfig()
	if err != nil {
		return BackupSchedule{}, err
	}
	if set != (Schedule{}) {
		schedule := config.Backups.Schedule
		if app != "" {
			schedule = config.Backups.Apps[app]
		}
		schedule.Every = cmp.Or(set.Every, schedule.Every)
		schedule.Keep = cmp.Or(set.Keep, schedule.Keep)
		if app == "" {
			config.Backups.Schedule = schedule
		} else {
			if config.Backups.Apps == nil {
				config.Backups.Apps = map[string]Schedule{}
			}
			config.Backups.Apps[app] = schedule
		}
		if err := SaveConfig(config); err != nil {
			return BackupSchedule{}, err
		}
	}
	return scheduleOf(config, app), nil
}

// scheduleOf is the schedule of app in config, "" for the server's.
func scheduleOf(config Config, app string) BackupSchedule {
	own := config.Backups.Schedule
	if app != "" {
		own = config.Backups.Apps[app]
	}
	every, keep := config.schedule(app)
	return BackupSchedule{App: app, Every: spanText(every), Keep: spanText(keep), Own: own != (Schedule{})}
}

// spanText writes a span the way the config does: 15m, 1h or 7d.
func spanText(d time.Duration) string {
	switch {
	case d%day == 0:
		return fmt.Sprintf("%dd", d/day)
	case d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dm", d/time.Minute)
}
