package pages

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/cloud/internal/service"
	"github.com/axadrn/moin/cloud/internal/ui/components"
)

// KillSwap is one bill of /kill-your-subscriptions and what replaces it.
type KillSwap struct {
	service.Price
	// ID names the bill in the page, from its tool: ghost-pro.
	ID string
	// Name is what replaces the tool, the catalog app or moin cloud, and
	// Href the app's page in the catalog.
	Name    string
	Href    string
	Command string
}

// killSeats are the team sizes to pick from, and killTeam the one picked
// when the page opens.
var killSeats = []string{"1", "5", "10", "25"}

const killTeam = 5

// killSeatOptions are the team sizes as a Choice.
func killSeatOptions() []components.Option {
	options := make([]components.Option, 0, len(killSeats))
	for _, seats := range killSeats {
		options = append(options, components.Option{Value: seats, Label: seats})
	}
	return options
}

// killCloud is moin cloud a month in cents, as the pricing on the home
// page says.
const killCloud = 700

// killCost is what a bill costs a month for the team picked first.
func killCost(s KillSwap) int64 {
	if s.PerSeat {
		return s.Cents() * killTeam
	}
	return s.Cents()
}

// killTotal is what the ticked bills cost a month together.
func killTotal(swaps []KillSwap) int64 {
	var total int64
	for _, s := range swaps {
		if s.Ticked {
			total += killCost(s)
		}
	}
	return total
}

// killCommands are the commands of the ticked bills, each once: two tools
// can have one app.
func killCommands(swaps []KillSwap) []string {
	var commands []string
	for _, s := range swaps {
		if s.Ticked && !slices.Contains(commands, s.Command) {
			commands = append(commands, s.Command)
		}
	}
	return commands
}

// killCount says how many bills are ticked and how many commands replace
// them. site.js says it again on every tick.
func killCount(swaps []KillSwap) string {
	bills := 0
	for _, s := range swaps {
		if s.Ticked {
			bills++
		}
	}
	commands := len(killCommands(swaps))
	if bills == 0 {
		return "Tick a bill to see its command."
	}
	return killPlural(bills, "bill") + ". " + killPlural(commands, "command") + "."
}

func killPlural(n int, word string) string {
	if n != 1 {
		word += "s"
	}
	return strconv.Itoa(n) + " " + word
}

// killChecked is the month the newest price was read, as November 2026.
func killChecked(swaps []KillSwap) string {
	var newest time.Time
	for _, s := range swaps {
		if day, err := s.CheckedDay(); err == nil && day.After(newest) {
			newest = day
		}
	}
	return newest.Format("January 2006")
}

// killMoney is cents as US dollars, the way site.js writes them too:
// $2,498.88.
func killMoney(cents int64) string {
	whole := strconv.FormatInt(cents/100, 10)
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	fraction := strconv.FormatInt(cents%100, 10)
	if len(fraction) == 1 {
		fraction = "0" + fraction
	}
	return "$" + whole + "." + fraction
}

// killYearBack is what a bill costs in a year, without cents when there
// are none: $435 a year back.
func killYearBack(s KillSwap) string {
	return strings.TrimSuffix(killMoney(killCost(s)*12), ".00") + " a year back."
}
