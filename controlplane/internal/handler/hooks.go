package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/axadrn/moin/controlplane/internal/api"
	"github.com/axadrn/moin/controlplane/internal/service"
)

// HooksHandler serves deploy hooks: secret URLs that CI calls after it
// pushed an image. The token in the path is the only credential.
type HooksHandler struct {
	apps *service.AppService
}

func NewHooksHandler(apps *service.AppService) *HooksHandler {
	return &HooksHandler{apps: apps}
}

// Deploy deploys the image from ?image= or a JSON body {"image": "..."}
// and answers 202 with the deploy.
func (h *HooksHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	image := r.URL.Query().Get("image")
	if image == "" {
		var request struct {
			Image string `json:"image"`
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		if len(body) > 0 && json.Unmarshal(body, &request) != nil {
			api.Error(w, http.StatusBadRequest, "invalid_body", `Send {"image": "registry/name:tag"} or ?image=.`)
			return
		}
		image = request.Image
	}
	if image == "" {
		api.ValidationError(w, map[string]string{"image": "Name the image to deploy, for example ghcr.io/me/app:tag."})
		return
	}
	_, deploy, err := h.apps.DeployHook(r.PathValue("token"), image)
	if err != nil {
		api.ServiceError(w, err)
		return
	}
	api.JSON(w, http.StatusAccepted, api.NewDeploy(*deploy))
}
