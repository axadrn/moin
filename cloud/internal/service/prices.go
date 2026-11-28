package service

import (
	"encoding/json"
	"io/fs"
	"math"
	"time"
)

// Price is one bill of /kill-your-saas-subscriptions, from prices.json, the one
// place the prices live: a tool's plan, in US dollars a month, from the
// tool's own pricing page, and the open source app that replaces it.
type Price struct {
	Tool string `json:"tool"`
	Plan string `json:"plan"`
	// USD is the price a month, billed monthly; per person when PerSeat.
	USD     float64 `json:"usd"`
	PerSeat bool    `json:"perSeat"`
	// Source is the tool's pricing page, Checked the day someone read it
	// there, as 2026-11-28.
	Source  string `json:"source"`
	Checked string `json:"checked"`
	// App is the catalog app that replaces the tool; empty for moin cloud.
	App string `json:"app"`
	// What says in a sentence what the app does instead.
	What string `json:"what"`
	// Ticked bills are ticked when the page opens.
	Ticked bool `json:"ticked"`
}

// Cents is the price a month for one person, or the whole plan, in cents.
func (p Price) Cents() int64 { return int64(math.Round(p.USD * 100)) }

// CheckedDay is the day the price was read on its source.
func (p Price) CheckedDay() (time.Time, error) { return time.Parse(time.DateOnly, p.Checked) }

// Prices reads prices.json, in the order the page shows them.
func Prices(files fs.FS) ([]Price, error) {
	data, err := fs.ReadFile(files, "prices.json")
	if err != nil {
		return nil, err
	}
	var prices []Price
	return prices, json.Unmarshal(data, &prices)
}
