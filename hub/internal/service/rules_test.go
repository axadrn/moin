package service

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

func TestServerRulesKeepWhatTheDaemonReports(t *testing.T) {
	if ServerRules(model.Server{Rules: rulesJSON(nil)}) != nil {
		t.Fatal("a server without rules has rules")
	}
	many := make([]string, 80)
	for i := range many {
		many[i] = "github.com/acme/" + strings.Repeat("x", 200)
	}
	rules := ServerRules(model.Server{Rules: rulesJSON(&protocol.Rules{DataOff: true, DeployFrom: many, BackupsTo: "s3.example.com/acme"})})
	if rules == nil || !rules.DataOff || rules.BackupsTo != "s3.example.com/acme" || len(rules.DeployFrom) != 50 || len(rules.DeployFrom[0]) != 128 {
		t.Fatalf("rules = %+v", rules)
	}
}
