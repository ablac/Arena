package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"arena-server/internal/db"

	"github.com/go-chi/chi/v5"
	stripe "github.com/stripe/stripe-go/v86"
)

func invokeCosmeticOrderResume(t *testing.T, handler *CosmeticCommerceHandler, orderID string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := commerceCustomerRequest(http.MethodPost, "/api/v1/account/cosmetics/orders/"+orderID+"/checkout", "")
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("order_id", orderID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	handler.ResumeCheckout(recorder, request)
	return recorder
}

func TestCosmeticOrderResumeReturnsSameOpenEmbeddedSessionToOwner(t *testing.T) {
	order := commerceTestOrder()
	order.Status = db.CosmeticOrderStatusCheckout
	order.CheckoutSessionID = "cs_resume_embedded"
	store := &fakeCosmeticCommerceStore{getOrder: order}
	provider := &fakeCosmeticPaymentProvider{retrievedSession: &CosmeticCheckoutSession{
		ID:           order.CheckoutSessionID,
		Presentation: CosmeticCheckoutPresentationEmbedded,
		ClientSecret: order.CheckoutSessionID + "_secret_browser",
		Status:       CosmeticCheckoutSessionStatusOpen,
		Mode:         "payment",
		OrderID:      order.ID,
		AccountID:    order.AccountID,
		PackID:       order.PackID,
	}}
	handler := newCosmeticCommerceHandlerWithStore(store, provider, true)

	recorder := invokeCosmeticOrderResume(t, handler, order.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["resumed"] != true || response["resumable"] != true || response["order_id"] != order.ID ||
		response["order_status"] != db.CosmeticOrderStatusCheckout || response["checkout_status"] != string(CosmeticCheckoutSessionStatusOpen) ||
		response["presentation"] != string(CosmeticCheckoutPresentationEmbedded) || response["session_id"] != order.CheckoutSessionID ||
		response["client_secret"] != order.CheckoutSessionID+"_secret_browser" || response["checkout_url"] != nil {
		t.Fatalf("response=%#v", response)
	}
	if store.getAccount != order.AccountID || store.getID != order.ID || provider.retrievedSessionID != order.CheckoutSessionID {
		t.Fatalf("store/provider lookup account=%q order=%q session=%q", store.getAccount, store.getID, provider.retrievedSessionID)
	}
}

func TestCosmeticOrderResumeReplaysUnattachedFailureWithStoredPresentation(t *testing.T) {
	order := commerceTestOrder()
	order.Status = db.CosmeticOrderStatusPaymentFailed
	order.CheckoutPresentation = db.CosmeticCheckoutPresentationHosted
	store := &fakeCosmeticCommerceStore{getOrder: order, order: order}
	provider := &fakeCosmeticPaymentProvider{session: &CosmeticCheckoutSession{
		ID: "cs_resume_retry", Presentation: CosmeticCheckoutPresentationHosted,
		URL: "https://checkout.stripe.com/c/pay/cs_resume_retry",
	}}
	handler := newCosmeticCommerceHandlerWithStore(store, provider, true)

	recorder := invokeCosmeticOrderResume(t, handler, order.ID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if provider.request.OrderID != order.ID || provider.request.Presentation != CosmeticCheckoutPresentationHosted ||
		store.attachedID != order.ID || store.attachedCS != provider.session.ID {
		t.Fatalf("provider=%+v store=%+v", provider.request, store)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["resumed"] != true || response["presentation"] != string(CosmeticCheckoutPresentationHosted) ||
		response["checkout_url"] != provider.session.URL || response["order_id"] != order.ID {
		t.Fatalf("response=%#v", response)
	}
}

func TestStripeCosmeticOrderResumeReportsClosedEmbeddedSessionWithoutBrowserSecret(t *testing.T) {
	retriever := &recordingStripeCheckoutRetriever{session: &stripe.CheckoutSession{
		ID:     "cs_resume_complete",
		UIMode: stripe.CheckoutSessionUIModeEmbeddedPage,
		Status: stripe.CheckoutSessionStatusComplete,
		Mode:   stripe.CheckoutSessionModePayment,
		Metadata: map[string]string{
			"order_id": "order-complete", "account_id": "account-1", "pack_id": "pack-complete",
		},
	}}
	provider := newStripeCosmeticPaymentProviderWithCreator(nil, nil, "", "", false)
	provider.checkoutRetriever = retriever

	checkout, err := provider.RetrieveCheckoutSession(context.Background(), "cs_resume_complete")
	if err != nil {
		t.Fatalf("RetrieveCheckoutSession() error = %v", err)
	}
	if checkout == nil || checkout.ID != "cs_resume_complete" || checkout.Status != CosmeticCheckoutSessionStatusComplete ||
		checkout.Presentation != CosmeticCheckoutPresentationEmbedded || checkout.ClientSecret != "" || checkout.URL != "" ||
		checkout.Mode != "payment" || checkout.OrderID != "order-complete" || checkout.AccountID != "account-1" || checkout.PackID != "pack-complete" {
		t.Fatalf("checkout=%#v", checkout)
	}
}

func resumableCosmeticOrder() *db.CosmeticOrder {
	order := commerceTestOrder()
	order.Status = db.CosmeticOrderStatusCheckout
	order.CheckoutSessionID = "cs_resume_owned"
	return order
}

func matchingCosmeticOrderCheckout(order *db.CosmeticOrder) *CosmeticCheckoutSession {
	return &CosmeticCheckoutSession{
		ID: order.CheckoutSessionID, Presentation: CosmeticCheckoutPresentationEmbedded,
		ClientSecret: order.CheckoutSessionID + "_secret_browser", Status: CosmeticCheckoutSessionStatusOpen,
		Mode: "payment", OrderID: order.ID, AccountID: order.AccountID, PackID: order.PackID,
	}
}

func TestCosmeticOrderResumeFailsClosedForWrongOwnerOrProviderClaims(t *testing.T) {
	baseOrder := resumableCosmeticOrder()
	tests := []struct {
		name       string
		order      func(*db.CosmeticOrder)
		checkout   func(*CosmeticCheckoutSession)
		storeErr   error
		wantStatus int
	}{
		{name: "unknown order", storeErr: db.ErrCosmeticOrderNotFound, wantStatus: http.StatusNotFound},
		{name: "wrong owner", order: func(order *db.CosmeticOrder) { order.AccountID = "account-other" }, wantStatus: http.StatusNotFound},
		{name: "paid order", order: func(order *db.CosmeticOrder) { order.Status = db.CosmeticOrderStatusPaid }, wantStatus: http.StatusConflict},
		{name: "wrong session", checkout: func(checkout *CosmeticCheckoutSession) { checkout.ID = "cs_other" }, wantStatus: http.StatusBadGateway},
		{name: "wrong mode", checkout: func(checkout *CosmeticCheckoutSession) { checkout.Mode = "subscription" }, wantStatus: http.StatusBadGateway},
		{name: "wrong order metadata", checkout: func(checkout *CosmeticCheckoutSession) { checkout.OrderID = "order-other" }, wantStatus: http.StatusBadGateway},
		{name: "wrong account metadata", checkout: func(checkout *CosmeticCheckoutSession) { checkout.AccountID = "account-other" }, wantStatus: http.StatusBadGateway},
		{name: "wrong pack metadata", checkout: func(checkout *CosmeticCheckoutSession) { checkout.PackID = "pack-other" }, wantStatus: http.StatusBadGateway},
		{name: "unknown UI presentation", checkout: func(checkout *CosmeticCheckoutSession) { checkout.Presentation = "custom" }, wantStatus: http.StatusBadGateway},
		{name: "mixed embedded browser fields", checkout: func(checkout *CosmeticCheckoutSession) { checkout.URL = "https://checkout.stripe.com/c/pay/mixed" }, wantStatus: http.StatusBadGateway},
		{name: "unknown provider status", checkout: func(checkout *CosmeticCheckoutSession) { checkout.Status = "pending" }, wantStatus: http.StatusBadGateway},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			orderCopy := *baseOrder
			if test.order != nil {
				test.order(&orderCopy)
			}
			checkout := matchingCosmeticOrderCheckout(baseOrder)
			if test.checkout != nil {
				test.checkout(checkout)
			}
			store := &fakeCosmeticCommerceStore{getOrder: &orderCopy, getErr: test.storeErr}
			provider := &fakeCosmeticPaymentProvider{retrievedSession: checkout}
			recorder := invokeCosmeticOrderResume(t, newCosmeticCommerceHandlerWithStore(store, provider, true), baseOrder.ID)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s, want %d", recorder.Code, recorder.Body.String(), test.wantStatus)
			}
			if test.wantStatus == http.StatusNotFound && provider.retrievedSessionID != "" {
				t.Fatalf("provider was called for unowned order: %q", provider.retrievedSessionID)
			}
			if recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control=%q", recorder.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestCosmeticOrderResumeRequiresAuthenticatedCustomer(t *testing.T) {
	order := resumableCosmeticOrder()
	store := &fakeCosmeticCommerceStore{getOrder: order}
	provider := &fakeCosmeticPaymentProvider{retrievedSession: matchingCosmeticOrderCheckout(order)}
	handler := newCosmeticCommerceHandlerWithStore(store, provider, true)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/account/cosmetics/orders/"+order.ID+"/checkout", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("order_id", order.ID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))

	handler.ResumeCheckout(recorder, request)
	if recorder.Code != http.StatusUnauthorized || store.getID != "" || provider.retrievedSessionID != "" {
		t.Fatalf("status=%d store=%q provider=%q body=%s", recorder.Code, store.getID, provider.retrievedSessionID, recorder.Body.String())
	}
}

func TestCosmeticOrderResumeReturnsProviderStatusWithoutLeakingClosedSessionFields(t *testing.T) {
	for _, status := range []CosmeticCheckoutSessionStatus{CosmeticCheckoutSessionStatusComplete, CosmeticCheckoutSessionStatusExpired} {
		t.Run(string(status), func(t *testing.T) {
			order := resumableCosmeticOrder()
			checkout := matchingCosmeticOrderCheckout(order)
			checkout.Status = status
			checkout.ClientSecret = "closed_secret_must_not_leave_server"
			store := &fakeCosmeticCommerceStore{getOrder: order}
			provider := &fakeCosmeticPaymentProvider{retrievedSession: checkout}
			recorder := invokeCosmeticOrderResume(t, newCosmeticCommerceHandlerWithStore(store, provider, true), order.ID)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var response map[string]interface{}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response["resumable"] != false || response["resumed"] != false || response["checkout_status"] != string(status) ||
				response["client_secret"] != nil || response["checkout_url"] != nil || response["session_id"] != nil {
				t.Fatalf("closed response=%#v", response)
			}
		})
	}
}

func TestStripeCosmeticOrderResumePreservesEmbeddedAndLegacyHostedUIModes(t *testing.T) {
	tests := []struct {
		name         string
		stripe       *stripe.CheckoutSession
		presentation CosmeticCheckoutPresentation
	}{
		{name: "embedded", presentation: CosmeticCheckoutPresentationEmbedded, stripe: &stripe.CheckoutSession{
			ID: "cs_embedded", UIMode: stripe.CheckoutSessionUIModeEmbeddedPage,
			ClientSecret: "cs_embedded_secret_browser", Status: stripe.CheckoutSessionStatusOpen, Mode: stripe.CheckoutSessionModePayment,
		}},
		{name: "legacy hosted", presentation: CosmeticCheckoutPresentationHosted, stripe: &stripe.CheckoutSession{
			ID: "cs_hosted", URL: "https://checkout.stripe.com/c/pay/cs_hosted",
			Status: stripe.CheckoutSessionStatusOpen, Mode: stripe.CheckoutSessionModePayment,
		}},
		{name: "hosted page", presentation: CosmeticCheckoutPresentationHosted, stripe: &stripe.CheckoutSession{
			ID: "cs_hosted_page", UIMode: stripe.CheckoutSessionUIModeHostedPage, URL: "https://checkout.stripe.com/c/pay/cs_hosted_page",
			Status: stripe.CheckoutSessionStatusOpen, Mode: stripe.CheckoutSessionModePayment,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.stripe.Metadata = map[string]string{"order_id": "order-1", "account_id": "account-1", "pack_id": "pack-1"}
			provider := newStripeCosmeticPaymentProviderWithCreator(nil, nil, "", "", false)
			provider.checkoutRetriever = &recordingStripeCheckoutRetriever{session: test.stripe}
			checkout, err := provider.RetrieveCheckoutSession(context.Background(), test.stripe.ID)
			if err != nil {
				t.Fatal(err)
			}
			if checkout.Presentation != test.presentation || checkout.ID != test.stripe.ID || checkout.Mode != "payment" ||
				checkout.OrderID != "order-1" || checkout.AccountID != "account-1" || checkout.PackID != "pack-1" {
				t.Fatalf("checkout=%#v", checkout)
			}
		})
	}
}

func TestStripeCosmeticOrderResumeRejectsUnsupportedOrMixedOpenUIModes(t *testing.T) {
	for _, session := range []*stripe.CheckoutSession{
		{ID: "cs_custom", UIMode: "custom", URL: "https://checkout.stripe.com/c/pay/custom", Status: stripe.CheckoutSessionStatusOpen},
		{ID: "cs_mixed", UIMode: stripe.CheckoutSessionUIModeEmbeddedPage, ClientSecret: "cs_mixed_secret", URL: "https://checkout.stripe.com/c/pay/mixed", Status: stripe.CheckoutSessionStatusOpen},
	} {
		provider := newStripeCosmeticPaymentProviderWithCreator(nil, nil, "", "", false)
		provider.checkoutRetriever = &recordingStripeCheckoutRetriever{session: session}
		if checkout, err := provider.RetrieveCheckoutSession(context.Background(), session.ID); err == nil || checkout != nil {
			t.Fatalf("session %s result=(%#v, %v), want rejection", session.ID, checkout, err)
		}
	}
}
