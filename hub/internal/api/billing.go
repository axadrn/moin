package api

import (
	"net/http"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/service/payment"
)

// BillingHandler serves the subscription of a workspace, checkout, and the
// provider billing management. Only members who may manage billing reach
// them, like the web billing page.
type BillingHandler struct {
	paymentService   payment.Provider
	workspaceService *service.WorkspaceService
}

func NewBillingHandler(paymentService payment.Provider, workspaceService *service.WorkspaceService) *BillingHandler {
	return &BillingHandler{paymentService: paymentService, workspaceService: workspaceService}
}

// Billing returns the workspace subscription, its access, and the
// purchasable plans.
func (h *BillingHandler) Billing(w http.ResponseWriter, r *http.Request) {
	billing, err := NewWorkspaceBilling(ctxkeys.WorkspaceMember(r.Context()), ctxkeys.Subscription(r.Context()))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, billing)
}

// NewWorkspaceBilling returns the billing summary of a workspace for a member
// who may manage billing, and service.ErrBillingPermission for everyone else.
func NewWorkspaceBilling(member *model.WorkspaceMember, subscription *model.Subscription) (Billing, error) {
	if err := service.ManageBilling(member); err != nil {
		return Billing{}, err
	}
	return NewBilling(subscription), nil
}

// CreateCheckout starts a provider checkout for the workspace and returns
// its URL.
func (h *BillingHandler) CreateCheckout(w http.ResponseWriter, r *http.Request) {
	var request checkoutRequest
	if !Decode(w, r, &request) {
		return
	}
	ctx := r.Context()
	checkoutURL, err := h.workspaceService.Checkout(ctxkeys.WorkspaceMember(ctx), ctxkeys.Workspace(ctx), ctxkeys.User(ctx), request.PlanID, request.Interval, source(r), h.paymentService.CreateCheckoutURL)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, URLResponse{URL: checkoutURL})
}

// BillingManagement returns the URL of the provider billing management for
// the workspace. A workspace without a provider customer gets 409
// no_billing_account.
func (h *BillingHandler) BillingManagement(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	managementURL, err := h.workspaceService.BillingManagement(ctxkeys.WorkspaceMember(ctx), ctxkeys.Workspace(ctx), ctxkeys.Subscription(ctx), source(r), h.paymentService.BillingManagementURL)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, URLResponse{URL: managementURL})
}
