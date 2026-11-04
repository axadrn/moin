package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/service"
)

// AlertsHandler serves the alert destinations of a workspace.
type AlertsHandler struct {
	alerts *service.AlertService
}

func NewAlertsHandler(alerts *service.AlertService) *AlertsHandler {
	return &AlertsHandler{alerts: alerts}
}

// AlertDestination is a destination without its URL, token or signing
// secret: hint shows its host and last characters.
type AlertDestination struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	Hint   string   `json:"hint"`
	ChatID string   `json:"chat_id"`
	Events []string `json:"events"`
	// Failing says sends to it fail since failing_since, with last_error.
	Failing      bool       `json:"failing"`
	FailingSince *time.Time `json:"failing_since"`
	LastError    string     `json:"last_error"`
	LastSentAt   *time.Time `json:"last_sent_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// AlertDestinationList is the answer of GET /api/v1/alert-destinations.
type AlertDestinationList struct {
	Items []AlertDestination `json:"items"`
}

// CreatedAlertDestination is the answer of POST /api/v1/alert-destinations:
// a webhook comes with its signing secret, this once.
type CreatedAlertDestination struct {
	AlertDestination
	SigningSecret string `json:"signing_secret,omitempty"`
}

// AlertDestinationRequest is the body of POST /api/v1/alert-destinations.
type AlertDestinationRequest struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	BotToken string   `json:"bot_token"`
	ChatID   string   `json:"chat_id"`
	Events   []string `json:"events"`
}

// NewAlertDestination is the JSON shape of a destination.
func NewAlertDestination(destination model.AlertDestination) AlertDestination {
	answer := AlertDestination{
		ID: destination.ID, Name: destination.Name, Kind: destination.Kind, Hint: destination.Hint, ChatID: destination.ChatID,
		Events: destination.EventList(), Failing: destination.FailingSince != nil, LastError: destination.LastError,
		CreatedAt: destination.CreatedAt.UTC(),
	}
	if destination.FailingSince != nil {
		at := destination.FailingSince.UTC()
		answer.FailingSince = &at
	}
	if destination.LastSentAt != nil {
		at := destination.LastSentAt.UTC()
		answer.LastSentAt = &at
	}
	return answer
}

// NewAlertDestinationList is the JSON shape of the destinations.
func NewAlertDestinationList(destinations []model.AlertDestination) AlertDestinationList {
	list := AlertDestinationList{Items: make([]AlertDestination, 0, len(destinations))}
	for _, destination := range destinations {
		list.Items = append(list.Items, NewAlertDestination(destination))
	}
	return list
}

// List returns the alert destinations of the workspace.
func (h *AlertsHandler) List(w http.ResponseWriter, r *http.Request) {
	destinations, err := h.alerts.Destinations(ctxkeys.Workspace(r.Context()).ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewAlertDestinationList(destinations))
}

// Create adds an alert destination.
func (h *AlertsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request AlertDestinationRequest
	if !Decode(w, r, &request) {
		return
	}
	destination, signingSecret, err := h.alerts.AddDestination(ctxkeys.WorkspaceMember(r.Context()), source(r), service.AlertDestinationInput{
		Kind: request.Kind, Name: request.Name, URL: request.URL, BotToken: request.BotToken, ChatID: request.ChatID, Events: request.Events,
	})
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusCreated, CreatedAlertDestination{AlertDestination: NewAlertDestination(destination), SigningSecret: signingSecret})
}

// Delete removes an alert destination.
func (h *AlertsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.alerts.RemoveDestination(ctxkeys.WorkspaceMember(r.Context()), source(r), r.PathValue("destination"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Test sends a test alert to a destination.
func (h *AlertsHandler) Test(w http.ResponseWriter, r *http.Request) {
	destination, err := h.alerts.TestDestination(r.Context(), ctxkeys.WorkspaceMember(r.Context()), source(r), r.PathValue("destination"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewAlertDestination(destination))
}
