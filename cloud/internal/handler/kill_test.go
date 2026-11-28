package handler

import (
	"io"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	catalogfiles "github.com/axadrn/moin/catalog"
	"github.com/axadrn/moin/cloud/content"
	"github.com/axadrn/moin/cloud/internal/catalog"
	"github.com/axadrn/moin/cloud/internal/service"
)

// TestPricesHaveSources: every price of /kill-your-subscriptions says where
// it is from and when someone read it there, and its app is in the catalog.
func TestPricesHaveSources(t *testing.T) {
	prices, err := service.Prices(content.FS(false))
	if err != nil || len(prices) == 0 {
		t.Fatal("no prices", err)
	}
	entries, err := catalog.Load(catalogfiles.Files)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range prices {
		if p.Tool == "" || p.Plan == "" || p.What == "" || p.USD <= 0 {
			t.Errorf("%q: a price needs a tool, a plan, what replaces it and dollars", p.Tool)
		}
		if !strings.HasPrefix(p.Source, "https://") {
			t.Errorf("%s: the source %q is no https:// link", p.Tool, p.Source)
		}
		day, err := p.CheckedDay()
		if err != nil || day.After(time.Now()) {
			t.Errorf("%s: checked %q is no day in the past, like 2026-11-28", p.Tool, p.Checked)
		}
		if p.App != "" && !slices.ContainsFunc(entries, func(e catalog.Entry) bool { return e.Slug == p.App }) {
			t.Errorf("%s: the catalog has no app %q", p.Tool, p.App)
		}
	}
	if swaps := KillSwaps(prices, entries); len(swaps) != len(prices) {
		t.Errorf("the page shows %d of %d prices", len(swaps), len(prices))
	}
}

// TestKillCommandsWork: every command on the page deploys an app the
// catalog has, and the page links to the whole catalog with its count.
func TestKillCommandsWork(t *testing.T) {
	entries, err := catalog.Load(catalogfiles.Files)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	Kill(content.FS(false), entries)(w, httptest.NewRequest("GET", "/kill-your-subscriptions", nil))
	data, _ := io.ReadAll(w.Result().Body)
	body := string(data)
	if w.Code != 200 {
		t.Fatal("status", w.Code)
	}
	commands := regexp.MustCompile(`deploy my-server --catalog ([a-z0-9-]+)`).FindAllStringSubmatch(body, -1)
	if len(commands) == 0 {
		t.Fatal("no commands on the page")
	}
	for _, command := range commands {
		if !slices.ContainsFunc(entries, func(e catalog.Entry) bool { return e.Slug == command[1] }) {
			t.Errorf("%s: no such app in the catalog", command[0])
		}
	}
	if !strings.Contains(body, `href="/catalog"`) || !strings.Contains(body, "All "+strconv.Itoa(len(entries))+" apps in the catalog") {
		t.Error("no link to the whole catalog with its count")
	}
}
