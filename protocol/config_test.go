package protocol

import (
	"encoding/json"
	"testing"
)

// TestConfigDomainForms: x-moin domains as a list is the main service's; as
// a map, domains by service.
func TestConfigDomainForms(t *testing.T) {
	var list, byService Config
	if err := json.Unmarshal([]byte(`{"domains":["shop.example.net"],"port":3000}`), &list); err != nil || len(list.Domains) != 1 || list.Port != 3000 {
		t.Errorf("list: %+v %v", list, err)
	}
	if err := json.Unmarshal([]byte(`{"domains":{"web":["shop.example.net"],"api":["api.shop.example.net"]},"ports":{"api":9000},"instant":"nip.io"}`), &byService); err != nil || len(byService.Domains) != 0 || byService.ServiceDomains["api"][0] != "api.shop.example.net" || byService.Ports["api"] != 9000 || byService.Instant != "nip.io" {
		t.Errorf("map: %+v %v", byService, err)
	}
	var bad Config
	if err := json.Unmarshal([]byte(`{"domains":7}`), &bad); err == nil {
		t.Error("a number passed as domains")
	}
}

// TestInstantHost: <app>.<ip with dashes>.<provider> for a public IPv4 and
// a known provider, nothing else.
func TestInstantHost(t *testing.T) {
	for _, test := range []struct{ app, ip, provider, want string }{
		{"shop", "203.0.113.7", "sslip.io", "shop.203-0-113-7.sslip.io"},
		{"shop", "203.0.113.7", "nip.io", "shop.203-0-113-7.nip.io"},
		{"shop", "203.0.113.7", "off", ""},
		{"shop", "203.0.113.7", "", ""},
		{"shop", "2a01:4f8::1", "sslip.io", ""},
		{"shop", "", "sslip.io", ""},
		{"", "203.0.113.7", "sslip.io", ""},
	} {
		if got := InstantHost(test.app, test.ip, test.provider); got != test.want {
			t.Errorf("InstantHost(%q, %q, %q) = %q, want %q", test.app, test.ip, test.provider, got, test.want)
		}
	}
}

// The backups block of x-moin: what it leaves out is the default, and a
// word moin does not know is an error.
func TestConfigBackupsSchedule(t *testing.T) {
	got, err := ConfigBackups{Every: "15m", Keep: "30d"}.Schedule()
	if err != nil || got != (Schedule{Every: 15, Keep: 30}) {
		t.Errorf("all set: %+v, %v", got, err)
	}
	got, err = ConfigBackups{}.Schedule()
	if err != nil || got != DefaultSchedule {
		t.Errorf("empty: %+v, %v", got, err)
	}
	if _, err := (ConfigBackups{Every: "60m"}).Schedule(); err == nil {
		t.Error("an unknown every passed")
	}
	if _, err := (ConfigBackups{Keep: "2d"}).Schedule(); err == nil {
		t.Error("an unknown keep passed")
	}
}
