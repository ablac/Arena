package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	stripe "github.com/stripe/stripe-go/v86"
)

type fakeCosmeticCommerceStore struct {
	order              *db.CosmeticOrder
	orders             []db.CosmeticOrder
	result             *db.CosmeticPaymentEventResult
	createErr          error
	attachErr          error
	processErr         error
	listErr            error
	createdAccount     string
	createdPack        string
	createdQty         int
	attachedID         string
	attachedCS         string
	failedID           string
	failedMessage      string
	processed          db.CosmeticPaymentEventInput
	listAccount        string
	adminQuery         string
	adminStatus        string
	listLimit          int
	equipped           map[string]map[string]string
	equippedErr        error
	equippedBots       []string
	equippedBatchCalls int
}

func (s *fakeCosmeticCommerceStore) CreateOrder(_ context.Context, accountID, packID string, quantity int) (*db.CosmeticOrder, error) {
	s.createdAccount, s.createdPack, s.createdQty = accountID, packID, quantity
	return s.order, s.createErr
}

func (s *fakeCosmeticCommerceStore) AttachCheckout(_ context.Context, accountID, orderID, checkoutSessionID string) (*db.CosmeticOrder, error) {
	s.createdAccount, s.attachedID, s.attachedCS = accountID, orderID, checkoutSessionID
	return s.order, s.attachErr
}

func (s *fakeCosmeticCommerceStore) MarkCheckoutFailed(_ context.Context, accountID, orderID, message string) (*db.CosmeticOrder, error) {
	s.createdAccount, s.failedID, s.failedMessage = accountID, orderID, message
	return s.order, nil
}

func (s *fakeCosmeticCommerceStore) ProcessEvent(_ context.Context, event db.CosmeticPaymentEventInput) (*db.CosmeticPaymentEventResult, error) {
	s.processed = event
	return s.result, s.processErr
}

func (s *fakeCosmeticCommerceStore) ListCustomerOrders(_ context.Context, accountID string, limit int) ([]db.CosmeticOrder, error) {
	s.listAccount, s.listLimit = accountID, limit
	return s.orders, s.listErr
}

func (s *fakeCosmeticCommerceStore) ListAdminOrders(_ context.Context, query, status string, limit int) ([]db.CosmeticOrder, error) {
	s.adminQuery, s.adminStatus, s.listLimit = query, status, limit
	return s.orders, s.listErr
}

func (s *fakeCosmeticCommerceStore) EquippedForBots(_ context.Context, botIDs []string) (map[string]map[string]string, error) {
	s.equippedBatchCalls++
	s.equippedBots = append([]string(nil), botIDs...)
	if s.equippedErr != nil {
		return nil, s.equippedErr
	}
	result := make(map[string]map[string]string, len(botIDs))
	for _, botID := range botIDs {
		result[botID] = make(map[string]string)
		for slot, assetKey := range s.equipped[botID] {
			result[botID][slot] = assetKey
		}
	}
	return result, nil
}

type fakeCosmeticPaymentProvider struct {
	request   CosmeticCheckoutRequest
	session   *CosmeticCheckoutSession
	createErr error
	deadline  time.Time
	event     *CosmeticPaymentEvent
	parseErr  error
	payload   []byte
	signature string
}

func (p *fakeCosmeticPaymentProvider) CreateCheckoutSession(ctx context.Context, request CosmeticCheckoutRequest) (*CosmeticCheckoutSession, error) {
	p.request = request
	p.deadline, _ = ctx.Deadline()
	return p.session, p.createErr
}

func (p *fakeCosmeticPaymentProvider) ParseWebhook(payload []byte, signature string) (*CosmeticPaymentEvent, error) {
	p.payload = append([]byte(nil), payload...)
	p.signature = signature
	return p.event, p.parseErr
}

func commerceTestOrder() *db.CosmeticOrder {
	return &db.CosmeticOrder{
		ID: "order-1", AccountID: "account-1", AccountEmail: "owner@example.com",
		PackID: "arena-set-003-ember-vanguard-pack", PackName: "Ember Vanguard Set",
		UnitPriceCents: 199, Quantity: 2, ExpectedSubtotalCents: 398, Currency: "USD",
		Items: []db.CosmeticOrderItem{{ID: "skin-1"}, {ID: "weapon-1"}, {ID: "attachment-1"}},
	}
}

func commerceCustomerRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	return req.WithContext(withCustomerSession(req.Context(), &CustomerSession{AccountID: "account-1", Email: "owner@example.com"}))
}

func TestCosmeticCommerceCheckoutUsesOnlyServerOrderSnapshot(t *testing.T) {
	order := commerceTestOrder()
	store := &fakeCosmeticCommerceStore{order: order}
	provider := &fakeCosmeticPaymentProvider{session: &CosmeticCheckoutSession{
		ID: "cs_test_1", URL: "https://checkout.stripe.com/c/pay/cs_test_1", ExpiresAt: time.Unix(1_900_000_000, 0).UTC(),
	}}
	handler := newCosmeticCommerceHandlerWithStore(store, provider, true)
	recorder := httptest.NewRecorder()
	handler.Checkout(recorder, commerceCustomerRequest(http.MethodPost, "/api/v1/account/cosmetics/checkout", `{
		"pack_id":"arena-set-003-ember-vanguard-pack", "quantity":2
	}`))

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.createdAccount != "account-1" || store.createdPack != order.PackID || store.createdQty != 2 ||
		store.attachedID != order.ID || store.attachedCS != provider.session.ID {
		t.Fatalf("store calls create=%q/%q/%d attach=%q/%q", store.createdAccount, store.createdPack, store.createdQty, store.attachedID, store.attachedCS)
	}
	request := provider.request
	if request.OrderID != order.ID || request.AccountID != order.AccountID || request.CustomerEmail != order.AccountEmail ||
		request.PackID != order.PackID || request.PackName != order.PackName || request.UnitAmount != order.UnitPriceCents ||
		request.Currency != order.Currency || request.Quantity != int64(order.Quantity) {
		t.Fatalf("provider received non-snapshot checkout data: %+v", request)
	}
	if !strings.Contains(recorder.Body.String(), `"order_id":"order-1"`) ||
		!strings.Contains(recorder.Body.String(), `"checkout_url":"https://checkout.stripe.com/c/pay/cs_test_1"`) {
		t.Fatalf("checkout response=%s", recorder.Body.String())
	}
	remaining := time.Until(provider.deadline)
	if provider.deadline.IsZero() || remaining < 14*time.Second || remaining > cosmeticCheckoutTimeout {
		t.Fatalf("provider deadline remaining=%v, want a bounded %v timeout", remaining, cosmeticCheckoutTimeout)
	}
}

func TestCosmeticCommerceCheckoutFailsClosed(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		newCosmeticCommerceHandlerWithStore(&fakeCosmeticCommerceStore{}, nil, false).Checkout(
			recorder, commerceCustomerRequest(http.MethodPost, "/", `{"pack_id":"pack","quantity":1}`),
		)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	})

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"pack_id":"pack","quantity":1,"price_cents":1}`},
		{name: "quantity zero", body: `{"pack_id":"pack","quantity":0}`},
		{name: "quantity high", body: `{"pack_id":"pack","quantity":11}`},
		{name: "invalid pack", body: `{"pack_id":"../pack","quantity":1}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeCosmeticCommerceStore{order: commerceTestOrder()}
			recorder := httptest.NewRecorder()
			newCosmeticCommerceHandlerWithStore(store, &fakeCosmeticPaymentProvider{}, true).Checkout(
				recorder, commerceCustomerRequest(http.MethodPost, "/", test.body),
			)
			if recorder.Code != http.StatusBadRequest || store.createdAccount != "" {
				t.Fatalf("status=%d createAccount=%q body=%s", recorder.Code, store.createdAccount, recorder.Body.String())
			}
		})
	}

	t.Run("provider error records order failure", func(t *testing.T) {
		store := &fakeCosmeticCommerceStore{order: commerceTestOrder()}
		provider := &fakeCosmeticPaymentProvider{createErr: errors.New("provider down")}
		recorder := httptest.NewRecorder()
		newCosmeticCommerceHandlerWithStore(store, provider, true).Checkout(
			recorder, commerceCustomerRequest(http.MethodPost, "/", `{"pack_id":"arena-set-003-ember-vanguard-pack","quantity":2}`),
		)
		if recorder.Code != http.StatusBadGateway || store.failedID != "order-1" || store.failedMessage == "" {
			t.Fatalf("status=%d failed=%q/%q body=%s", recorder.Code, store.failedID, store.failedMessage, recorder.Body.String())
		}
	})

	for name, redirectURL := range map[string]string{
		"script scheme": "javascript:alert(1)",
		"plain HTTP":    "http://checkout.stripe.example/session",
		"URL userinfo":  "https://user:password@checkout.stripe.example/session",
	} {
		t.Run("unsafe provider redirect "+name, func(t *testing.T) {
			store := &fakeCosmeticCommerceStore{order: commerceTestOrder()}
			provider := &fakeCosmeticPaymentProvider{session: &CosmeticCheckoutSession{ID: "cs_bad", URL: redirectURL}}
			recorder := httptest.NewRecorder()
			newCosmeticCommerceHandlerWithStore(store, provider, true).Checkout(
				recorder, commerceCustomerRequest(http.MethodPost, "/", `{"pack_id":"arena-set-003-ember-vanguard-pack","quantity":2}`),
			)
			if recorder.Code != http.StatusBadGateway || store.attachedID != "" || store.failedID != "order-1" {
				t.Fatalf("status=%d attached=%q failed=%q body=%s", recorder.Code, store.attachedID, store.failedID, recorder.Body.String())
			}
		})
	}
}

func TestCosmeticCommerceWebhookVerifiesRawBodyAndMapsEvents(t *testing.T) {
	store := &fakeCosmeticCommerceStore{result: &db.CosmeticPaymentEventResult{Order: commerceTestOrder(), Applied: true, LicensesCreated: 6}}
	provider := &fakeCosmeticPaymentProvider{event: &CosmeticPaymentEvent{
		ID: "evt_paid", Type: CosmeticPaymentEventCheckoutCompleted, PayloadSHA256: strings.Repeat("a", 64),
		OrderID: "order-1", AccountID: "account-1", CheckoutSessionID: "cs_test_1", PaymentIntentID: "pi_test_1",
		AmountTotal: 418, Currency: "usd", PaymentStatus: "paid",
	}}
	handler := newCosmeticCommerceHandlerWithStore(store, provider, true)
	body := []byte(`{"id":"evt_paid","object":"event"}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cosmetics/webhooks/stripe", bytes.NewReader(body))
	request.Header.Set("Stripe-Signature", "t=1,v1=signed")
	handler.StripeWebhook(recorder, request)

	if recorder.Code != http.StatusOK || !bytes.Equal(provider.payload, body) || provider.signature != "t=1,v1=signed" {
		t.Fatalf("status=%d parsed=%q signature=%q body=%s", recorder.Code, provider.payload, provider.signature, recorder.Body.String())
	}
	input := store.processed
	if input.Provider != "stripe" || input.EventID != "evt_paid" || input.EventType != db.CosmeticStripeCheckoutCompleted ||
		input.PayloadHash != strings.Repeat("a", 64) || input.OrderID != "order-1" || input.AccountID != "account-1" ||
		input.CheckoutSessionID != "cs_test_1" || input.PaymentIntentID != "pi_test_1" || input.Currency != "USD" ||
		!input.Paid || input.AmountReceivedCents != 418 {
		t.Fatalf("processed input=%+v", input)
	}
}

func TestCosmeticCommerceTerminalReversalRefreshesConnectedAndWaitingBots(t *testing.T) {
	order := commerceTestOrder()
	order.Status = db.CosmeticOrderStatusDisputed
	store := &fakeCosmeticCommerceStore{
		result: &db.CosmeticPaymentEventResult{Order: order, Applied: true},
		equipped: map[string]map[string]string{
			"bot-active":  {},
			"bot-waiting": {"bot_skin": "starter_blue"},
		},
	}
	provider := &fakeCosmeticPaymentProvider{event: &CosmeticPaymentEvent{
		ID: "evt_dispute_refresh", Type: CosmeticPaymentEventDisputeCreated, PayloadSHA256: strings.Repeat("d", 64),
		OrderID: order.ID, AccountID: order.AccountID, PaymentIntentID: "pi_test_1", Currency: "usd",
	}}
	engine := game.NewGameEngine()
	engine.Bots["bot-active"] = &game.BotState{BotID: "bot-active", Cosmetics: map[string]string{"bot_skin": "refunded_skin"}}
	engine.WaitingBots["bot-waiting"] = &game.BotState{BotID: "bot-waiting", Cosmetics: map[string]string{"bot_skin": "refunded_skin"}}
	handler := newCosmeticCommerceHandlerWithStore(store, provider, true)
	handler.engine = engine

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cosmetics/webhooks/stripe", strings.NewReader(`{"object":"event"}`))
	request.Header.Set("Stripe-Signature", "signed")
	handler.StripeWebhook(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"live_refreshed":2`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(engine.Bots["bot-active"].Cosmetics) != 0 {
		t.Fatalf("active bot retained reversed cosmetics: %+v", engine.Bots["bot-active"].Cosmetics)
	}
	if got := engine.WaitingBots["bot-waiting"].Cosmetics["bot_skin"]; got != "starter_blue" {
		t.Fatalf("waiting bot cosmetics = %q, want starter_blue", got)
	}
	if strings.Join(store.equippedBots, ",") != "bot-active,bot-waiting" {
		t.Fatalf("refreshed bot IDs = %v", store.equippedBots)
	}
	if store.equippedBatchCalls != 1 {
		t.Fatalf("loadout batch queries = %d, want 1", store.equippedBatchCalls)
	}
}

func TestCosmeticCommerceTerminalRefreshFailureReturnsRetryableFiveHundred(t *testing.T) {
	order := commerceTestOrder()
	order.Status = db.CosmeticOrderStatusRefunded
	store := &fakeCosmeticCommerceStore{
		result:      &db.CosmeticPaymentEventResult{Order: order, Duplicate: true},
		equippedErr: errors.New("temporary loadout read failure"),
		equipped: map[string]map[string]string{
			"bot-active": {},
		},
	}
	provider := &fakeCosmeticPaymentProvider{event: &CosmeticPaymentEvent{
		ID: "evt_refund_refresh_retry", Type: CosmeticPaymentEventRefundUpdated, PayloadSHA256: strings.Repeat("e", 64),
		OrderID: order.ID, AccountID: order.AccountID, PaymentIntentID: "pi_test_1", Currency: "usd",
		RefundID: "re_retry", RefundStatus: "succeeded", AmountRefunded: order.AmountReceivedCents,
	}}
	engine := game.NewGameEngine()
	engine.Bots["bot-active"] = &game.BotState{BotID: "bot-active", Cosmetics: map[string]string{"bot_skin": "refunded_skin"}}
	handler := newCosmeticCommerceHandlerWithStore(store, provider, true)
	handler.engine = engine

	deliver := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/cosmetics/webhooks/stripe", strings.NewReader(`{"object":"event"}`))
		request.Header.Set("Stripe-Signature", "signed")
		handler.StripeWebhook(recorder, request)
		return recorder
	}

	failed := deliver()
	if failed.Code != http.StatusInternalServerError || strings.Contains(failed.Body.String(), `"received":true`) {
		t.Fatalf("failed refresh status=%d body=%s", failed.Code, failed.Body.String())
	}
	if got := engine.Bots["bot-active"].Cosmetics["bot_skin"]; got != "refunded_skin" {
		t.Fatalf("failed refresh changed stale cache to %q", got)
	}

	store.equippedErr = nil
	retried := deliver()
	if retried.Code != http.StatusOK || !strings.Contains(retried.Body.String(), `"duplicate":true`) ||
		!strings.Contains(retried.Body.String(), `"live_refreshed":1`) {
		t.Fatalf("retry status=%d body=%s", retried.Code, retried.Body.String())
	}
	if len(engine.Bots["bot-active"].Cosmetics) != 0 {
		t.Fatalf("duplicate retry did not clear stale cache: %+v", engine.Bots["bot-active"].Cosmetics)
	}
	if store.equippedBatchCalls != 2 {
		t.Fatalf("loadout batch calls = %d, want one per delivery", store.equippedBatchCalls)
	}
}

func TestCosmeticCommerceWebhookAllowsMetadataFreeReversalByPaymentIntent(t *testing.T) {
	store := &fakeCosmeticCommerceStore{result: &db.CosmeticPaymentEventResult{Order: commerceTestOrder(), Applied: true}}
	provider := &fakeCosmeticPaymentProvider{event: &CosmeticPaymentEvent{
		ID: "evt_refund", Type: CosmeticPaymentEventRefundUpdated, PayloadSHA256: strings.Repeat("b", 64),
		PaymentIntentID: "pi_test_1", Currency: "usd", RefundID: "re_test_1", RefundStatus: "succeeded", AmountRefunded: 199,
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"object":"event"}`))
	request.Header.Set("Stripe-Signature", "signed")
	newCosmeticCommerceHandlerWithStore(store, provider, true).StripeWebhook(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.processed.OrderID != "" || store.processed.AccountID != "" || store.processed.PaymentIntentID != "pi_test_1" ||
		store.processed.EventType != db.CosmeticStripeRefundUpdated || store.processed.RefundAmountCents != 199 {
		t.Fatalf("metadata-free refund mapping=%+v", store.processed)
	}
}

func TestCosmeticCommerceWebhookKeepsRetryableOrderingFailuresOnFiveHundredPath(t *testing.T) {
	store := &fakeCosmeticCommerceStore{processErr: db.ErrCosmeticPaymentEventRetryable}
	provider := &fakeCosmeticPaymentProvider{event: &CosmeticPaymentEvent{
		ID: "evt_retryable", Type: CosmeticPaymentEventRefundUpdated, PayloadSHA256: strings.Repeat("c", 64),
		PaymentIntentID: "pi_test_1", Currency: "usd", RefundID: "re_test_1", RefundStatus: "succeeded", AmountRefunded: 199,
	}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cosmetics/webhooks/stripe", strings.NewReader(`{"object":"event"}`))
	request.Header.Set("Stripe-Signature", "signed")
	newCosmeticCommerceHandlerWithStore(store, provider, true).StripeWebhook(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s, want retryable 5xx", recorder.Code, recorder.Body.String())
	}
}

func TestCosmeticCommerceWebhookAcknowledgesSignedUnsupportedEventsWithoutDatabaseWork(t *testing.T) {
	const secret = "whsec_unsupported"
	store := &fakeCosmeticCommerceStore{}
	provider := newStripeCosmeticPaymentProviderWithCreator(nil, []string{secret}, "", "", false)
	payload := stripeEventPayload(t, "evt_unsupported", stripe.EventType("customer.created"), map[string]interface{}{"id": "cus_test"})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cosmetics/webhooks/stripe", bytes.NewReader(payload))
	request.Header.Set("Stripe-Signature", signedStripePayload(payload, secret, time.Now()))
	newCosmeticCommerceHandlerWithStore(store, provider, true).StripeWebhook(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"ignored":true`) {
		t.Fatalf("status=%d body=%s, want signed unsupported event acknowledged", recorder.Code, recorder.Body.String())
	}
	if store.processed.EventID != "" {
		t.Fatalf("unsupported event reached database store: %+v", store.processed)
	}
}

func TestCosmeticCommerceWebhookRejectsInvalidOrOversizedPayload(t *testing.T) {
	t.Run("invalid signature", func(t *testing.T) {
		store := &fakeCosmeticCommerceStore{}
		provider := &fakeCosmeticPaymentProvider{parseErr: errors.New("bad signature")}
		recorder := httptest.NewRecorder()
		newCosmeticCommerceHandlerWithStore(store, provider, true).StripeWebhook(
			recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)),
		)
		if recorder.Code != http.StatusBadRequest || store.processed.EventID != "" {
			t.Fatalf("status=%d processed=%+v", recorder.Code, store.processed)
		}
	})

	t.Run("oversized", func(t *testing.T) {
		store := &fakeCosmeticCommerceStore{}
		provider := &fakeCosmeticPaymentProvider{}
		recorder := httptest.NewRecorder()
		newCosmeticCommerceHandlerWithStore(store, provider, true).StripeWebhook(
			recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", cosmeticWebhookMaxBytes+1))),
		)
		if recorder.Code != http.StatusRequestEntityTooLarge || provider.payload != nil {
			t.Fatalf("status=%d parsedBytes=%d", recorder.Code, len(provider.payload))
		}
	})
}

func TestCosmeticCommerceOrderHistoryIsScopedAndBounded(t *testing.T) {
	store := &fakeCosmeticCommerceStore{orders: []db.CosmeticOrder{*commerceTestOrder()}}
	handler := newCosmeticCommerceHandlerWithStore(store, &fakeCosmeticPaymentProvider{}, true)

	customer := httptest.NewRecorder()
	handler.CustomerOrders(customer, commerceCustomerRequest(http.MethodGet, "/api/v1/account/cosmetics/orders?limit=500", ""))
	if customer.Code != http.StatusOK || store.listAccount != "account-1" || store.listLimit != 100 {
		t.Fatalf("customer status=%d account=%q limit=%d body=%s", customer.Code, store.listAccount, store.listLimit, customer.Body.String())
	}

	admin := httptest.NewRecorder()
	handler.AdminOrders(admin, httptest.NewRequest(http.MethodGet, "/api/v1/admin/cosmetics/orders?query=owner%40example.com&status=paid&limit=999", nil))
	if admin.Code != http.StatusOK || store.adminQuery != "owner@example.com" || store.adminStatus != "paid" || store.listLimit != 100 {
		t.Fatalf("admin status=%d query=%q filter=%q limit=%d body=%s", admin.Code, store.adminQuery, store.adminStatus, store.listLimit, admin.Body.String())
	}
}

func TestCosmeticCommerceRoutesExistAtRootAndArenaPrefixes(t *testing.T) {
	previous := config.C
	t.Cleanup(func() { config.C = previous })
	config.C.OIDCEnabled = false
	config.C.CustomerOIDCEnabled = false
	config.C.AdminLocalhostBypass = false
	config.C.AdminToken = ""
	config.C.CosmeticsCheckoutEnabled = false
	config.C.StripeSecretKey = ""
	config.C.StripeWebhookSecrets = ""
	router := NewRouter(game.NewGameEngine())

	for _, prefix := range []string{"/api/v1", "/arena/api/v1"} {
		tests := []struct {
			method string
			path   string
			want   int
		}{
			{method: http.MethodPost, path: "/cosmetics/webhooks/stripe", want: http.StatusServiceUnavailable},
			{method: http.MethodGet, path: "/account/cosmetics/orders", want: http.StatusServiceUnavailable},
			{method: http.MethodPost, path: "/account/cosmetics/checkout", want: http.StatusServiceUnavailable},
			{method: http.MethodGet, path: "/admin/cosmetics/orders", want: http.StatusUnauthorized},
		}
		for _, test := range tests {
			target := prefix + test.path
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, target, nil)
			request.RemoteAddr = "198.51.100.10:4444"
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Errorf("%s %s status=%d body=%s, want %d", test.method, target, recorder.Code, recorder.Body.String(), test.want)
			}
		}
	}
}

func TestCosmeticCheckoutRouteFailsClosedAtBothPrefixesWithoutRedis(t *testing.T) {
	previousConfig := config.C
	previousRedis := security.RedisClient
	t.Cleanup(func() {
		config.C = previousConfig
		security.RedisClient = previousRedis
	})
	config.C.CosmeticsCheckoutRPM = 3
	security.RedisClient = nil

	store := &fakeCosmeticCommerceStore{order: commerceTestOrder()}
	provider := &fakeCosmeticPaymentProvider{}
	handler := newCosmeticCommerceHandlerWithStore(store, provider, true)
	router := chi.NewRouter()
	for _, prefix := range []string{"/api/v1/account", "/arena/api/v1/account"} {
		router.Route(prefix, func(account chi.Router) {
			registerCustomerCosmeticCommerceRoutes(account, handler)
		})
	}

	for _, target := range []string{
		"/api/v1/account/cosmetics/checkout",
		"/arena/api/v1/account/cosmetics/checkout",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(`{"pack_id":"arena-set-003-ember-vanguard-pack","quantity":1}`))
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"RATE_LIMIT_UNAVAILABLE"`) {
			t.Fatalf("%s status=%d body=%s", target, recorder.Code, recorder.Body.String())
		}
	}
	if store.createdAccount != "" || provider.request.OrderID != "" {
		t.Fatalf("checkout side effects ran without limiter: store=%q provider=%+v", store.createdAccount, provider.request)
	}
}

func TestCosmeticCatalogAdvertisesCheckoutOnlyWithUsableAuthAndRedis(t *testing.T) {
	previousConfig := config.C
	previousRedis := security.RedisClient
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() {
		config.C = previousConfig
		security.RedisClient = previousRedis
		_ = redisClient.Close()
	})

	config.C.CosmeticsCheckoutEnabled = true
	config.C.CosmeticsCheckoutRPM = 10
	config.C.StripeSecretKey = "sk_test_catalog_readiness"
	config.C.StripeWebhookSecrets = "whsec_catalog_readiness"
	config.C.StripeSuccessURL = "https://arena.example/dashboard/?checkout=success"
	config.C.StripeCancelURL = "https://arena.example/dashboard/?checkout=cancel"
	config.C.CustomerOIDCEnabled = false
	config.C.CustomerEmailAuthEnabled = true
	config.C.CustomerEmailSignInURL = "https://arena.example/dashboard/"
	config.C.CustomerEmailTokenTTLMinutes = 15
	config.C.CustomerEmailSendCooldownSeconds = 60
	config.C.CustomerEmailSendRPM = 5
	config.C.SMTPHost = "100.71.171.28"
	config.C.SMTPPort = 465
	config.C.SMTPTLSMode = "implicit"
	config.C.SMTPTLSServerName = "mail.angel-serv.com"
	config.C.SMTPUsername = "noreply@angel-serv.com"
	config.C.SMTPPassword = "test-app-password"
	config.C.SMTPFrom = "Arena <noreply@angel-serv.com>"
	security.RedisClient = redisClient

	checkoutEnabled := func() bool {
		router := NewRouter(game.NewGameEngine())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/cosmetics/catalog", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("catalog status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var payload struct {
			CheckoutEnabled bool `json:"checkout_enabled"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.CheckoutEnabled
	}

	if !checkoutEnabled() {
		t.Fatal("complete native email auth with Redis did not advertise checkout")
	}

	config.C.CustomerEmailAuthEnabled = false
	config.C.CustomerOIDCEnabled = true
	config.C.CustomerOIDCIssuer = ""
	config.C.CustomerOIDCClientID = ""
	config.C.CustomerOIDCClientSecret = ""
	config.C.CustomerOIDCRedirectURI = ""
	if checkoutEnabled() {
		t.Fatal("checkout was advertised after customer auth failed to initialise")
	}

	config.C.CustomerOIDCEnabled = false
	config.C.CustomerEmailAuthEnabled = true
	security.RedisClient = nil
	if checkoutEnabled() {
		t.Fatal("checkout was advertised without the required Redis quota")
	}
}
