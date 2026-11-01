package api

import (
	"net/http"
	"testing"

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
