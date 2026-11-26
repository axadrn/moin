package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/service"
)

// HooksHandler serves deploy hooks: secret URLs that CI calls after it
// pushed an image. The token in the path is the only credential.
type HooksHandler struct {
	apps *service.AppService
}

func NewHooksHandler(apps *service.AppService) *HooksHandler {
	return &HooksHandler{apps: apps}
}

// Deploy deploys the image from ?image= or a JSON body {"image": "..."},
// with the compose file of ?commit= or "commit", the commit CI built the
// image from, and answers 202 with the deploy.
func (h *HooksHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	image, commit := r.URL.Query().Get("image"), r.URL.Query().Get("commit")
	if image == "" {
		var request struct {
			Image  string `json:"image"`
			Commit string `json:"commit"`
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		if len(body) > 0 && json.Unmarshal(body, &request) != nil {
			api.Error(w, http.StatusBadRequest, "invalid_body", `Send {"image": "registry/name:tag", "commit": "<sha>"} or ?image=&commit=.`)
			return
		}
		image, commit = request.Image, request.Commit
	}
	if image == "" {
		api.ValidationError(w, map[string]string{"image": "Name the image to deploy, for example ghcr.io/me/app:tag."})
		return
	}
	_, deploy, err := h.apps.DeployHook(r.Context(), r.PathValue("token"), image, commit)
	switch {
	case errors.Is(err, service.ErrRefName):
		api.ValidationError(w, map[string]string{"commit": "This is not a commit. Send the SHA CI built the image from."})
	case errors.Is(err, service.ErrRef):
		api.ValidationError(w, map[string]string{"commit": "GitHub knows no commit by this name. Push it first."})
	case err != nil:
		api.ServiceError(w, err)
	default:
		api.JSON(w, http.StatusAccepted, api.NewDeploy(*deploy))
	}
}
