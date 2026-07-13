package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
)

func TestStripeCosmeticPaymentProviderBuildsEmbeddedCheckout(t *testing.T) {
	creator := &recordingStripeCheckoutCreator{session: &stripe.CheckoutSession{
		ID:           "cs_embedded_arena",
		ClientSecret: "cs_embedded_arena_secret_browser",
		ExpiresAt:    1_900_000_000,
	}}
	provider := newStripeCosmeticPaymentProviderWithCreator(
		creator,
		[]string{"whsec_current"},
		"https://arena.example/dashboard/?tab=cosmetics&checkout=success&session_id={CHECKOUT_SESSION_ID}",
		"https://arena.example/dashboard/?tab=cosmetics&checkout=cancel",
		true,
	)
	provider.returnURL = "https://arena.example/dashboard/?tab=cosmetics&checkout=return&session_id={CHECKOUT_SESSION_ID}"

	got, err := provider.CreateCheckoutSession(context.Background(), CosmeticCheckoutRequest{
		OrderID: "order-embedded", AccountID: "account-embedded", CustomerEmail: "owner@example.com",
		PackID: "pack-embedded", PackName: "Embedded Pack", PackDescription: "Cosmetic items.",
		UnitAmount: 199, Currency: "USD", Quantity: 1, Presentation: CosmeticCheckoutPresentationEmbedded,
	})
	if err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}
	if got.ID != "cs_embedded_arena" || got.Presentation != CosmeticCheckoutPresentationEmbedded ||
		got.ClientSecret != "cs_embedded_arena_secret_browser" || got.URL != "" ||
		!got.ExpiresAt.Equal(time.Unix(1_900_000_000, 0).UTC()) {
		t.Fatalf("embedded checkout result = %#v", got)
	}

	params := creator.params
	if params == nil {
		t.Fatal("Stripe checkout creator was not called")
	}
	if stripe.StringValue(params.UIMode) != string(stripe.CheckoutSessionUIModeEmbeddedPage) {
		t.Fatalf("UIMode = %q, want %q", stripe.StringValue(params.UIMode), stripe.CheckoutSessionUIModeEmbeddedPage)
	}
	if stripe.StringValue(params.ReturnURL) != "https://arena.example/dashboard/?tab=cosmetics&checkout=return&session_id={CHECKOUT_SESSION_ID}" {
		t.Fatalf("ReturnURL = %q", stripe.StringValue(params.ReturnURL))
	}
	if stripe.StringValue(params.RedirectOnCompletion) != string(stripe.CheckoutSessionRedirectOnCompletionIfRequired) {
		t.Fatalf("RedirectOnCompletion = %q, want if_required", stripe.StringValue(params.RedirectOnCompletion))
	}
	if params.SuccessURL != nil || params.CancelURL != nil {
		t.Fatalf("embedded Checkout must omit hosted URLs, got success=%q cancel=%q", stripe.StringValue(params.SuccessURL), stripe.StringValue(params.CancelURL))
	}
}

func TestStripeCosmeticPaymentProviderBuildsAndRetrievesEmbeddedSubscriptionCheckout(t *testing.T) {
	creator := &recordingStripeCheckoutCreator{session: &stripe.CheckoutSession{
		ID: "cs_embedded_subscription", ClientSecret: "cs_embedded_subscription_secret_browser", ExpiresAt: 1_900_000_000,
	}}
	provider := newStripeCosmeticPaymentProviderWithCreator(
		creator, []string{"whsec_current"},
		"https://arena.example/dashboard/?checkout=success",
		"https://arena.example/dashboard/?checkout=cancel", false,
	)
	provider.returnURL = "https://arena.example/dashboard/?checkout=return&session_id={CHECKOUT_SESSION_ID}"

	created, err := provider.CreateSubscriptionCheckoutSession(context.Background(), CosmeticSubscriptionCheckoutRequest{
		SubscriptionID: "subscription-embedded", AccountID: "account-embedded", CustomerEmail: "owner@example.com",
		Presentation: CosmeticCheckoutPresentationEmbedded,
	})
	if err != nil {
		t.Fatalf("CreateSubscriptionCheckoutSession() error = %v", err)
	}
	if created.Presentation != CosmeticCheckoutPresentationEmbedded || created.ClientSecret == "" || created.URL != "" {
		t.Fatalf("embedded subscription result = %#v", created)
	}
	params := creator.params
	if params == nil || stripe.StringValue(params.UIMode) != string(stripe.CheckoutSessionUIModeEmbeddedPage) ||
		stripe.StringValue(params.RedirectOnCompletion) != string(stripe.CheckoutSessionRedirectOnCompletionIfRequired) ||
		stripe.StringValue(params.ReturnURL) != provider.returnURL || params.SuccessURL != nil || params.CancelURL != nil {
		t.Fatalf("embedded subscription params = %#v", params)
	}

	provider.checkoutRetriever = &recordingStripeCheckoutRetriever{session: &stripe.CheckoutSession{
		ID: "cs_embedded_subscription", ClientSecret: "cs_embedded_subscription_secret_browser",
		UIMode: stripe.CheckoutSessionUIModeEmbeddedPage, Status: stripe.CheckoutSessionStatusOpen,
		Mode: stripe.CheckoutSessionModeSubscription, ExpiresAt: 1_900_000_000,
		Metadata: map[string]string{"subscription_id": "subscription-embedded", "account_id": "account-embedded"},
	}}
	retrieved, err := provider.RetrieveSubscriptionCheckoutSession(context.Background(), "cs_embedded_subscription")
	if err != nil {
		t.Fatalf("RetrieveSubscriptionCheckoutSession() error = %v", err)
	}
	if retrieved.Presentation != CosmeticCheckoutPresentationEmbedded || retrieved.ClientSecret == "" || retrieved.URL != "" ||
		retrieved.SubscriptionID != "subscription-embedded" || retrieved.AccountID != "account-embedded" {
		t.Fatalf("retrieved embedded subscription = %#v", retrieved)
	}
}

func TestCosmeticCommerceCheckoutReturnsOnlyEmbeddedBrowserSecret(t *testing.T) {
	order := commerceTestOrder()
	order.Quantity = 1
	order.ExpectedSubtotalCents = order.UnitPriceCents
	store := &fakeCosmeticCommerceStore{order: order}
	provider := &fakeCosmeticPaymentProvider{session: &CosmeticCheckoutSession{
		ID: "cs_embedded_handler", Presentation: CosmeticCheckoutPresentationEmbedded,
		ClientSecret: "cs_embedded_handler_secret_browser", ExpiresAt: time.Unix(1_900_000_000, 0).UTC(),
	}}
	handler := newCosmeticCommerceHandlerWithStore(store, provider, true)
	recorder := httptest.NewRecorder()
	handler.Checkout(recorder, commerceCustomerRequest(http.MethodPost, "/api/v1/account/cosmetics/checkout", `{
		"pack_id":"arena-set-003-ember-vanguard-pack", "quantity":1, "presentation":"embedded"
	}`))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if provider.request.Presentation != CosmeticCheckoutPresentationEmbedded {
		t.Fatalf("provider presentation = %q", provider.request.Presentation)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`"order_id":"order-1"`, `"presentation":"embedded"`,
		`"session_id":"cs_embedded_handler"`, `"client_secret":"cs_embedded_handler_secret_browser"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("checkout response missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "checkout_url") {
		t.Fatalf("embedded response leaked a hosted redirect field: %s", body)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestCosmeticCheckoutConfigPublishesOnlyBrowserSafeConfiguration(t *testing.T) {
	handler := &CosmeticCommerceHandler{checkoutEnabled: true, provider: &fakeCosmeticPaymentProvider{}, publishableKey: "pk_live_browser_safe"}
	recorder := httptest.NewRecorder()
	handler.CheckoutConfig(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/cosmetics/checkout/config", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"enabled":true`) || !strings.Contains(body, `"publishable_key":"pk_live_browser_safe"`) ||
		!strings.Contains(body, `"default_presentation":"embedded"`) || !strings.Contains(body, `"hosted_fallback_enabled":true`) {
		t.Fatalf("checkout config response=%s", body)
	}
	if strings.Contains(body, "secret_key") || strings.Contains(body, "webhook") || strings.Contains(body, "sk_live") || strings.Contains(body, "rk_live") {
		t.Fatalf("checkout config exposed server-only Stripe material: %s", body)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("Cache-Control = %q, want public, max-age=300", got)
	}
}
