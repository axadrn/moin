package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/service"
)

func TestErrorForReadOnlyQuery(t *testing.T) {
	status, detail, known := ErrorFor(service.ErrReadOnly)
	if !known || status != http.StatusConflict || detail.Code != "read_only_query" {
		t.Fatalf("ErrorFor(ErrReadOnly) = %d, %+v, %v", status, detail, known)
	}
	if detail.Message != "This query changes data. Run it with execute_sql (MCP), --write (CLI) or Allow changes (web)." {
		t.Fatalf("message = %q", detail.Message)
	}
}

// Zone Settings Edit only turns on Always Use HTTPS, so a token without it
// is fine and the message must not ask for it.
func TestErrorForCloudflareTokenNeedsTunnelAndDNSOnly(t *testing.T) {
	_, detail, _ := ErrorFor(service.ErrCloudflareToken)
	message := detail.Fields["cloudflare_token"]
	if !strings.Contains(message, "Tunnel Edit and Zone DNS Edit.") || !strings.Contains(message, "Zone Settings Edit is optional") {
		t.Fatalf("message = %q", message)
	}
	if strings.Contains(service.ErrCloudflareToken.Error(), "Settings") {
		t.Fatalf("error = %q", service.ErrCloudflareToken)
	}
}

func TestNewDeploysSayWhichReleasesAreOnTheServer(t *testing.T) {
	deploys := []model.Deploy{{Number: 7, Status: model.DeploySucceeded}}
	for number := 6; number >= 1; number-- {
		deploys = append(deploys, model.Deploy{Number: number, Status: model.DeploySucceeded, SourceKey: "server"})
	}
	converted := NewDeploys(deploys, 7)
	for _, deploy := range converted {
		if deploy.OnServer == nil || *deploy.OnServer != (deploy.Number > 1) {
			t.Errorf("v%d on_server = %v", deploy.Number, deploy.OnServer)
		}
	}
}
