package payment

import (
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/stripe/stripe-go/v85"
)

func TestCheckoutSetsTheCouponOrAllowsCodes(t *testing.T) {
	coupon := "coupon_test"
	with := &stripe.CheckoutSessionParams{}
	setCheckoutDiscount(with, &model.Subscription{CouponID: &coupon})
	if len(with.Discounts) != 1 || *with.Discounts[0].Coupon != coupon || with.AllowPromotionCodes != nil {
		t.Fatalf("with coupon: %+v", with)
	}
	without := &stripe.CheckoutSessionParams{}
	setCheckoutDiscount(without, &model.Subscription{})
	if without.Discounts != nil || without.AllowPromotionCodes == nil || !*without.AllowPromotionCodes {
		t.Fatalf("without coupon: %+v", without)
	}
}
