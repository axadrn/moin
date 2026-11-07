package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/service/payment"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

type BillingHandler struct {
	paymentService   payment.Provider
	workspaceService *service.WorkspaceService
}

func NewBillingHandler(paymentService payment.Provider, workspaceService *service.WorkspaceService) *BillingHandler {
	return &BillingHandler{
		paymentService:   paymentService,
		workspaceService: workspaceService,
	}
}

func (h *BillingHandler) BillingPage(w http.ResponseWriter, r *http.Request) {
	if service.ManageBilling(ctxkeys.WorkspaceMember(r.Context())) != nil {
		w.WriteHeader(http.StatusForbidden)
		render(w, r, pages.ErrorPage(http.StatusForbidden, "Access denied", "Only workspace owners can manage billing."))
		return
	}
	render(w, r, pages.Billing())
}

func (h *BillingHandler) CreateCheckout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	checkoutURL, err := h.workspaceService.Checkout(ctxkeys.WorkspaceMember(ctx), ctxkeys.Workspace(ctx), ctxkeys.User(ctx), r.FormValue("plan_id"), r.FormValue("interval"), service.RequestSource(ctx), h.paymentService.CreateCheckoutURL)
	switch {
	case err == nil:
		redirect(w, r, checkoutURL, http.StatusSeeOther)
	case errors.Is(err, service.ErrBillingPermission):
		checkoutFailed(w, r, http.StatusForbidden, "Action not allowed", "Action not allowed", "Only workspace owners can start checkout.")
	case errors.Is(err, service.ErrPlan):
		checkoutFailed(w, r, http.StatusBadRequest, "Invalid plan", "Invalid plan", "Choose an available paid plan.")
	case errors.Is(err, service.ErrInterval):
		checkoutFailed(w, r, http.StatusBadRequest, "Invalid billing interval", "Invalid billing interval", "Choose monthly or yearly billing.")
	case errors.Is(err, repository.ErrCheckoutPending), errors.Is(err, repository.ErrCheckoutSelection), errors.Is(err, repository.ErrSubscriptionAlreadyPaid):
		checkoutFailed(w, r, http.StatusConflict, "Checkout already active", "Checkout already active", "Finish the existing checkout or manage the current subscription first.")
	default:
		checkoutFailed(w, r, http.StatusInternalServerError, "Checkout not started", "Checkout unavailable", "Checkout could not be started. Please try again.")
	}
}

// checkoutFailed answers a failed checkout: a toast for an action, else
// an error page with the status.
func checkoutFailed(w http.ResponseWriter, r *http.Request, status int, actionTitle, pageTitle, message string) {
	if isAction(r) {
		answerFailed(w, r, actionTitle, message)
		return
	}
	w.WriteHeader(status)
	render(w, r, pages.ErrorPage(status, pageTitle, message))
}

func (h *BillingHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read webhook payload", "error", err)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "Payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Failed to read payload", http.StatusBadRequest)
		return
	}
	defer func() {
		closeErr := r.Body.Close()
		if closeErr != nil {
			slog.Error("failed to close request body", "error", closeErr)
		}
	}()

	err = h.paymentService.HandleWebhook(payload, r.Header)
	if err != nil {
		slog.Error("failed to handle webhook", "error", err, "provider", h.paymentService.Name())
		http.Error(w, "Failed to process webhook", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"received": true}`))
}

func (h *BillingHandler) BillingManagement(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	managementURL, err := h.workspaceService.BillingManagement(ctxkeys.WorkspaceMember(ctx), ctxkeys.Workspace(ctx), ctxkeys.Subscription(ctx), service.RequestSource(ctx), h.paymentService.BillingManagementURL)
	switch {
	case err == nil:
		redirect(w, r, managementURL, http.StatusSeeOther)
	case errors.Is(err, service.ErrBillingPermission):
		w.WriteHeader(http.StatusForbidden)
		render(w, r, pages.ErrorPage(http.StatusForbidden, "Action not allowed", "Only workspace owners can manage billing."))
	default:
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Billing unavailable", "Billing management could not be opened. Please try again."))
	}
}
