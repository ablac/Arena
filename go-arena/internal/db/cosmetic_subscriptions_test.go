package db

import "testing"

func TestCosmeticSubscriptionOfferIsServerOwned(t *testing.T) {
	offer := DefaultCosmeticSubscriptionOffer(false)
	if offer.Enabled || offer.PriceCents != 1999 || offer.Currency != "USD" || offer.Interval != "month" ||
		!offer.IncludesFutureSets || offer.MaxAPIKeys != 5 {
		t.Fatalf("disabled subscription offer = %+v", offer)
	}

	offer = DefaultCosmeticSubscriptionOffer(true)
	if !offer.Enabled || offer.PriceCents != 1999 || offer.Currency != "USD" || offer.Interval != "month" ||
		!offer.IncludesFutureSets || offer.MaxAPIKeys != 5 {
		t.Fatalf("enabled subscription offer = %+v", offer)
	}
}

func TestCosmeticSubscriptionLifecycleRules(t *testing.T) {
	for _, status := range []string{CosmeticSubscriptionStatusActive, CosmeticSubscriptionStatusTrialing} {
		if !cosmeticSubscriptionGrantsAccess(status) {
			t.Errorf("status %q should grant subscription access", status)
		}
	}
	for _, status := range []string{CosmeticSubscriptionStatusCreated, CosmeticSubscriptionStatusCheckoutPending,
		CosmeticSubscriptionStatusPastDue, CosmeticSubscriptionStatusBillingMismatch,
		CosmeticSubscriptionStatusCanceled, CosmeticSubscriptionStatusExpired} {
		if cosmeticSubscriptionGrantsAccess(status) {
			t.Errorf("status %q should not mint subscription licenses", status)
		}
	}
	if cosmeticSubscriptionIsTerminal(CosmeticSubscriptionStatusActive) {
		t.Fatal("active subscription marked terminal")
	}
	if cosmeticSubscriptionIsTerminal(CosmeticSubscriptionStatusBillingMismatch) ||
		!validCosmeticSubscriptionStatus(CosmeticSubscriptionStatusBillingMismatch) {
		t.Fatal("billing mismatch must be recoverable, valid, and nonterminal")
	}
	if !cosmeticSubscriptionIsTerminal(CosmeticSubscriptionStatusCanceled) ||
		!cosmeticSubscriptionIsTerminal(CosmeticSubscriptionStatusExpired) {
		t.Fatal("terminal subscription status was not recognized")
	}
}
