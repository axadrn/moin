package config

import (
	"slices"
	"testing"
)

func TestMissing(t *testing.T) {
	for _, key := range Needed {
		t.Setenv(key, "set")
	}
	if missing := Missing(); len(missing) != 0 {
		t.Errorf("Missing() = %v, want none", missing)
	}
	t.Setenv("STRIPE_PRODUCT_ID_PRO", " ")
	if missing := Missing(); !slices.Equal(missing, []string{"STRIPE_PRODUCT_ID_PRO"}) {
		t.Errorf("Missing() = %v, want STRIPE_PRODUCT_ID_PRO", missing)
	}
}
