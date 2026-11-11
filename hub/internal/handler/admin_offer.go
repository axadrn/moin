package handler

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// Offers is the page of offers: add one, see who got which.
func (h *AdminHandler) Offers(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	offers, err := h.admin.Offers(ctxkeys.User(r.Context()), ctxkeys.Session(r.Context()), r.URL.Query().Get("q"), page)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	render(w, r, pages.AdminOffers(offers))
}

// CreateOffer adds an offer and shows it in the list.
func (h *AdminHandler) CreateOffer(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.FormValue("trial_days"))
	input := service.OfferInput{
		Email:     r.FormValue("email"),
		TrialDays: days,
		CouponID:  r.FormValue("coupon_id"),
		Reason:    r.FormValue("reason"),
	}
	if err := h.admin.CreateOffer(ctxkeys.User(r.Context()), ctxkeys.Session(r.Context()), input); err != nil {
		h.handleError(w, r, err)
		return
	}
	redirect(w, r, "/admin/offers?q="+url.QueryEscape(r.FormValue("email")), http.StatusSeeOther)
}

// DeleteOffer removes an offer nobody claimed yet.
func (h *AdminHandler) DeleteOffer(w http.ResponseWriter, r *http.Request) {
	if err := h.admin.DeleteOffer(ctxkeys.User(r.Context()), ctxkeys.Session(r.Context()), r.FormValue("email")); err != nil {
		h.handleError(w, r, err)
		return
	}
	redirect(w, r, "/admin/offers", http.StatusSeeOther)
}

// SaveUserNote keeps what admins wrote about a user.
func (h *AdminHandler) SaveUserNote(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")
	if err := h.admin.SetUserNote(ctxkeys.User(r.Context()), ctxkeys.Session(r.Context()), userID, r.FormValue("note")); err != nil {
		h.handleError(w, r, err)
		return
	}
	if !isAction(r) {
		redirect(w, r, "/admin/users/"+url.PathEscape(userID), http.StatusSeeOther)
		return
	}
	user, err := h.admin.User(ctxkeys.User(r.Context()), ctxkeys.Session(r.Context()), userID)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	answerDone(w, r, "Note saved", "")
	render(w, r, pages.AdminUserNote(user))
}
