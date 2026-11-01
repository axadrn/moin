package api

import (
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/brand"
	"github.com/axadrn/ops/internal/build"
)

// MetaHandler answers public facts about the project for a frontend.
type MetaHandler struct {
	supportEmail string
}

func NewMetaHandler(supportEmail string) *MetaHandler {
	return &MetaHandler{supportEmail: supportEmail}
}

type metaResponse struct {
	Name         string `json:"name"`
	SupportEmail string `json:"support_email"`
	// Version is the control plane's version, the one agents update to.
	Version string `json:"version"`
}

// Meta returns the project name, the support address and the version.
func (h *MetaHandler) Meta(w http.ResponseWriter, _ *http.Request) {
	JSON(w, http.StatusOK, metaResponse{Name: brand.Name, SupportEmail: h.supportEmail, Version: build.Version})
}
