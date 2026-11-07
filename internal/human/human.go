// Package human writes times, sizes and durations the way people read
// them, the same on the web, in the CLI, the TUI and MCP.
package human

import (
	"fmt"
	"time"
)

// Ago is how long ago t was, short: "just now", "4m ago", "3h ago",
// "12d ago", and the date after 60 days, "Jan 2, 2006".
func Ago(t, now time.Time) string {
	age := now.Sub(t)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	case age < 60*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(age.Hours()/24))
	}
	return t.Format("Jan 2, 2006")
}

// Size is a number of bytes: "512 B", "1.5 KB", "38 GB". One decimal
// below 10, whole numbers above, 1024 bytes to a KB.
func Size(bytes float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	unit := 0
	for bytes >= 1024 && unit < len(units)-1 {
		bytes /= 1024
		unit++
	}
	if unit == 0 || bytes >= 10 {
		return fmt.Sprintf("%.0f %s", bytes, units[unit])
	}
	return fmt.Sprintf("%.1f %s", bytes, units[unit])
}

// Duration is a length of time in its two largest units: "850ms", "16s",
// "2m 5s", "3h 10m", "3d 4h". A zero second unit is left out: "2m", "3h".
func Duration(d time.Duration) string {
	d = max(0, d)
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return pair(int(d.Minutes()), "m", int(d.Seconds())%60, "s")
	case d < 48*time.Hour:
		return pair(int(d.Hours()), "h", int(d.Minutes())%60, "m")
	}
	return pair(int(d.Hours())/24, "d", int(d.Hours())%24, "h")
}

// pair is "3h 10m", or "3h" when the small part is zero.
func pair(big int, bigUnit string, small int, smallUnit string) string {
	if small == 0 {
		return fmt.Sprintf("%d%s", big, bigUnit)
	}
	return fmt.Sprintf("%d%s %d%s", big, bigUnit, small, smallUnit)
}
