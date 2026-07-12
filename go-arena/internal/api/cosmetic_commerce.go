package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
)

const (
	cosmeticWebhookMaxBytes = 1 << 20
	cosmeticCheckoutTimeout = 15 * time.Second
)

var cosmeticSubscriptionObservationNanos atomic.Int64

func nextCosmeticSubscriptionObservationTime() time.Time {
	// PostgreSQL timestamptz persists microseconds. Generate monotonic values at
	// that same resolution so distinct local observations cannot collapse to an
	// equal stored timestamp and be misclassified after a concurrent commit.
	now := time.Now().UTC().Truncate(time.Microsecond).UnixNano()
	for {
		previous := cosmeticSubscriptionObservationNanos.Load()
		if now <= previous {
			now = previous + int64(time.Microsecond)
		}
		if cosmeticSubscriptionObservationNanos.CompareAndSwap(previous, now) {
			return time.Unix(0, now).UTC()
		}
	}
}

var cosmeticCheckoutPackIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)

type cosmeticCommerceStore interface {
	CreateOrder(context.Context, string, string, int) (*db.CosmeticOrder, error)
	AttachCheckout(context.Context, string, string, string) (*db.CosmeticOrder, error)
	MarkCheckoutFailed(context.Context, string, string, string) (*db.CosmeticOrder, error)
	ProcessEvent(context.Context, db.CosmeticPaymentEventInput) (*db.CosmeticPaymentEventResult, error)
	ListCustomerOrders(context.Context, string, int) ([]db.CosmeticOrder, error)
	ListAdminOrders(context.Context, string, string, int) ([]db.CosmeticOrder, error)
	EquippedForBots(context.Context, []string) (map[string]map[string]string, error)
}

type databaseCosmeticCommerceStore struct{}

func (databaseCosmeticCommerceStore) CreateOrder(ctx context.Context, accountID, packID string, quantity int) (*db.CosmeticOrder, error) {
	return db.CreateCosmeticOrder(ctx, accountID, packID, quantity)
}

func (databaseCosmeticCommerceStore) AttachCheckout(ctx context.Context, accountID, orderID, checkoutSessionID string) (*db.CosmeticOrder, error) {
	return db.AttachCosmeticOrderCheckout(ctx, accountID, orderID, checkoutSessionID)
}

func (databaseCosmeticCommerceStore) MarkCheckoutFailed(ctx context.Context, accountID, orderID, message string) (*db.CosmeticOrder, error) {
	return db.MarkCosmeticOrderCheckoutFailed(ctx, accountID, orderID, message)
}

func (databaseCosmeticCommerceStore) ProcessEvent(ctx context.Context, event db.CosmeticPaymentEventInput) (*db.CosmeticPaymentEventResult, error) {
	return db.ProcessCosmeticPaymentEvent(ctx, event)
}

func (databaseCosmeticCommerceStore) ListCustomerOrders(ctx context.Context, accountID string, limit int) ([]db.CosmeticOrder, error) {
	return db.ListCustomerCosmeticOrders(ctx, accountID, limit)
}

func (databaseCosmeticCommerceStore) ListAdminOrders(ctx context.Context, query, status string, limit int) ([]db.CosmeticOrder, error) {
	return db.ListAdminCosmeticOrders(ctx, query, status, limit)
}

func (databaseCosmeticCommerceStore) EquippedForBots(ctx context.Context, botIDs []string) (map[string]map[string]string, error) {
	return db.GetEquippedCosmeticsForBots(ctx, botIDs)
}

// CosmeticCommerceHandler keeps authenticated order creation, provider IO, and
// signature-verified fulfillment separate from the catalog/equip handler.
// Webhook reconciliation and the billing portal remain available when new
// sales are paused, provided the operator retains the webhook secret, Stripe
// API key, and portal return URL required by config validation.
type CosmeticCommerceHandler struct {
	store             cosmeticCommerceStore
	subscriptionStore cosmeticSubscriptionStore
	provider          CosmeticPaymentProvider
	checkoutEnabled   bool
	engine            *game.GameEngine
}

func NewCosmeticCommerceHandler(engine *game.GameEngine) *CosmeticCommerceHandler {
	secrets := config.ParseStripeWebhookSecrets(config.C.StripeWebhookSecrets)
	var provider CosmeticPaymentProvider
	if len(secrets) > 0 {
		provider = NewStripeCosmeticPaymentProvider(
			config.C.StripeSecretKey,
			secrets,
			config.C.StripeSuccessURL,
			config.C.StripeCancelURL,
			config.C.StripePortalReturnURL,
			config.C.StripeAutomaticTax,
		)
	}
	return &CosmeticCommerceHandler{
		store:             databaseCosmeticCommerceStore{},
		subscriptionStore: databaseCosmeticSubscriptionStore{},
		provider:          provider,
		checkoutEnabled:   config.C.CosmeticsCheckoutEnabled && provider != nil,
		engine:            engine,
	}
}

func newCosmeticCommerceHandlerWithStore(store cosmeticCommerceStore, provider CosmeticPaymentProvider, enabled bool) *CosmeticCommerceHandler {
	return &CosmeticCommerceHandler{store: store, provider: provider, checkoutEnabled: enabled && provider != nil}
}

func (h *CosmeticCommerceHandler) Enabled() bool {
	return h != nil && h.checkoutEnabled && h.provider != nil
}

type cosmeticCheckoutBody struct {
	PackID   string `json:"pack_id"`
	Quantity int    `json:"quantity"`
}

func (h *CosmeticCommerceHandler) Checkout(w http.ResponseWriter, r *http.Request) {
	setCustomerNoStore(w)
	if h == nil || !h.Enabled() || h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "cosmetic checkout is not available")
		return
	}
	session, ok := customerSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "customer authentication required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var request cosmeticCheckoutBody
	if err := decodeStrictCosmeticAdminJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid cosmetic checkout request")
		return
	}
	request.PackID = strings.ToLower(strings.TrimSpace(request.PackID))
	if !cosmeticCheckoutPackIDPattern.MatchString(request.PackID) || request.Quantity < 1 || request.Quantity > 10 {
		writeError(w, http.StatusBadRequest, "pack_id and quantity between 1 and 10 are required")
		return
	}

	order, err := h.store.CreateOrder(r.Context(), session.AccountID, request.PackID, request.Quantity)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCosmeticOrderQuantity):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, db.ErrCosmeticOrderPackUnavailable):
			writeError(w, http.StatusConflict, "cosmetic pack is not available for purchase")
		case errors.Is(err, db.ErrCustomerAccountNotFound):
			writeError(w, http.StatusUnauthorized, "customer account is unavailable")
		case errors.Is(err, db.ErrCustomerAccountUnverified):
			writeError(w, http.StatusForbidden, "a verified customer email is required")
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "cosmetic orders are unavailable")
		default:
			slog.Error("failed to create cosmetic order", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create cosmetic order")
		}
		return
	}
	if order == nil || order.ID == "" || order.AccountID != session.AccountID {
		writeError(w, http.StatusInternalServerError, "failed to create cosmetic order")
		return
	}

	providerCtx, cancel := context.WithTimeout(r.Context(), cosmeticCheckoutTimeout)
	checkout, err := h.provider.CreateCheckoutSession(providerCtx, CosmeticCheckoutRequest{
		OrderID:         order.ID,
		AccountID:       order.AccountID,
		CustomerEmail:   order.AccountEmail,
		PackID:          order.PackID,
		PackName:        order.PackName,
		PackDescription: order.PackDescription,
		UnitAmount:      order.UnitPriceCents,
		Currency:        order.Currency,
		Quantity:        int64(order.Quantity),
	})
	cancel()
	if err != nil {
		slog.Error("failed to create hosted cosmetic checkout", "order_id", order.ID, "error", err)
		h.markCheckoutFailed(r.Context(), order, "checkout provider unavailable")
		writeError(w, http.StatusBadGateway, "failed to open cosmetic checkout")
		return
	}
	if !validHostedCheckoutSession(checkout) {
		slog.Error("cosmetic checkout provider returned an invalid hosted session", "order_id", order.ID)
		h.markCheckoutFailed(r.Context(), order, "checkout provider returned an invalid session")
		writeError(w, http.StatusBadGateway, "failed to open cosmetic checkout")
		return
	}
	if _, err := h.store.AttachCheckout(r.Context(), order.AccountID, order.ID, checkout.ID); err != nil {
		slog.Error("failed to attach cosmetic checkout session", "order_id", order.ID, "error", err)
		h.markCheckoutFailed(r.Context(), order, "checkout session could not be attached")
		writeError(w, http.StatusInternalServerError, "failed to save cosmetic checkout")
		return
	}

	response := map[string]interface{}{
		"order_id": order.ID, "checkout_url": checkout.URL,
	}
	if !checkout.ExpiresAt.IsZero() {
		response["expires_at"] = checkout.ExpiresAt
	}
	writeJSON(w, http.StatusCreated, response)
}

func (h *CosmeticCommerceHandler) markCheckoutFailed(ctx context.Context, order *db.CosmeticOrder, message string) {
	if h == nil || h.store == nil || order == nil {
		return
	}
	if _, err := h.store.MarkCheckoutFailed(ctx, order.AccountID, order.ID, message); err != nil {
		slog.Error("failed to persist cosmetic checkout failure", "order_id", order.ID, "error", err)
	}
}

func validHostedCheckoutSession(session *CosmeticCheckoutSession) bool {
	if session == nil || strings.TrimSpace(session.ID) == "" || len(session.ID) > 255 ||
		strings.TrimSpace(session.URL) == "" || len(session.URL) > 2048 {
		return false
	}
	return validHostedHTTPSURL(session.URL)
}

func validHostedHTTPSURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && strings.EqualFold(parsed.Scheme, "https") && parsed.Hostname() != "" && parsed.User == nil
}

func (h *CosmeticCommerceHandler) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h == nil || h.provider == nil || h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "cosmetic payment webhooks are not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, cosmeticWebhookMaxBytes)
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "payment webhook is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "failed to read payment webhook")
		return
	}
	event, err := h.provider.ParseWebhook(payload, r.Header.Get("Stripe-Signature"))
	if err != nil {
		if errors.Is(err, ErrUnsupportedCosmeticPaymentEvent) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"received": true, "ignored": true})
			return
		}
		writeError(w, http.StatusBadRequest, "invalid payment webhook")
		return
	}
	if event == nil {
		writeError(w, http.StatusBadRequest, "invalid payment webhook")
		return
	}
	if event.Kind == CosmeticPaymentKindSubscription {
		h.subscriptionWebhook(w, r, *event)
		return
	}
	input, err := databaseCosmeticPaymentEvent(*event)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unsupported payment webhook")
		return
	}
	result, err := h.store.ProcessEvent(r.Context(), input)
	if err != nil {
		if errors.Is(err, db.ErrCosmeticOrderTerminal) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"received": true, "ignored": true, "terminal": true})
			return
		}
		switch {
		case errors.Is(err, db.ErrCosmeticOrderMismatch),
			errors.Is(err, db.ErrCosmeticPaymentEventInvalid),
			errors.Is(err, db.ErrCosmeticPaymentEventConflict),
			errors.Is(err, db.ErrCosmeticPaymentEventRejected):
			slog.Warn("rejected cosmetic payment event", "event_id", event.ID, "event_type", event.Type, "error", err)
			writeError(w, http.StatusUnprocessableEntity, "payment event did not match an order")
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "cosmetic order ledger is unavailable")
		default:
			slog.Error("failed to process cosmetic payment event", "event_id", event.ID, "event_type", event.Type, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to process payment event")
		}
		return
	}
	if result == nil {
		writeError(w, http.StatusInternalServerError, "failed to process payment event")
		return
	}
	liveRefreshed, err := h.refreshTerminalBotVisuals(r.Context(), result.Order)
	if err != nil {
		// The payment mutation is already committed and idempotent. A non-2xx
		// response makes Stripe retry the same event; duplicate processing then
		// re-attempts this authoritative cache repair without reapplying money.
		slog.Error("failed to refresh connected bot cosmetics after payment reversal", "event_id", event.ID, "event_type", event.Type)
		writeError(w, http.StatusInternalServerError, "payment event requires retry")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"received": true, "applied": result.Applied, "duplicate": result.Duplicate,
		"licenses_created": result.LicensesCreated, "live_refreshed": liveRefreshed, "order": result.Order,
	})
}

func (h *CosmeticCommerceHandler) subscriptionWebhook(w http.ResponseWriter, r *http.Request, event CosmeticPaymentEvent) {
	if h.subscriptionStore == nil {
		writeError(w, http.StatusServiceUnavailable, "cosmetic subscription ledger is unavailable")
		return
	}
	if cosmeticSubscriptionEventNeedsProviderReconciliation(event) {
		retriever, ok := h.provider.(cosmeticSubscriptionStateRetriever)
		if !ok || strings.TrimSpace(event.ProviderSubscriptionID) == "" {
			writeError(w, http.StatusServiceUnavailable, "subscription provider reconciliation is unavailable")
			return
		}
		// Capture before Retrieve so concurrent requests have a stable local
		// observation order even if the earlier request returns more slowly.
		observedAt := nextCosmeticSubscriptionObservationTime()
		providerCtx, cancel := context.WithTimeout(r.Context(), cosmeticCheckoutTimeout)
		state, retrieveErr := retriever.RetrieveCosmeticSubscription(providerCtx, event.ProviderSubscriptionID)
		cancel()
		if retrieveErr != nil || state == nil || state.ID != event.ProviderSubscriptionID || state.CustomerID == "" {
			slog.Error("failed to reconcile cosmetic subscription from provider", "event_id", event.ID,
				"provider_subscription_id", event.ProviderSubscriptionID, "error", retrieveErr)
			writeError(w, http.StatusServiceUnavailable, "subscription provider reconciliation failed")
			return
		}
		event.CustomerID = state.CustomerID
		event.SubscriptionStatus = state.Status
		event.CancelAtPeriodEnd = state.CancelAtPeriodEnd
		event.CurrentPeriodEnd = state.CurrentPeriodEnd
		event.Terminal = state.Terminal
		event.ProviderStateObservedAt = observedAt
	}
	input, err := databaseCosmeticSubscriptionEvent(event)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unsupported subscription webhook")
		return
	}
	result, err := h.subscriptionStore.Process(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrCosmeticSubscriptionMismatch),
			errors.Is(err, db.ErrCosmeticSubscriptionEventInvalid),
			errors.Is(err, db.ErrCosmeticSubscriptionEventConflict),
			errors.Is(err, db.ErrCosmeticSubscriptionNotFound):
			writeError(w, http.StatusUnprocessableEntity, "payment event did not match a cosmetic subscription")
		case errors.Is(err, db.ErrNoDatabase):
			writeError(w, http.StatusServiceUnavailable, "cosmetic subscription ledger is unavailable")
		default:
			slog.Error("failed to process cosmetic subscription event", "event_id", event.ID, "event_type", event.Type, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to process subscription event")
		}
		return
	}
	if result == nil || result.Subscription == nil {
		writeError(w, http.StatusInternalServerError, "failed to process subscription event")
		return
	}
	liveRefreshed := 0
	if !result.Subscription.HasAccess {
		liveRefreshed, err = h.refreshConnectedBotVisuals(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "subscription event requires retry")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"received": true, "applied": result.Applied, "duplicate": result.Duplicate,
		"licenses_created": result.LicensesCreated, "licenses_revoked": result.LicensesRevoked,
		"live_refreshed": liveRefreshed, "subscription": result.Subscription,
	})
}

func cosmeticSubscriptionEventNeedsProviderReconciliation(event CosmeticPaymentEvent) bool {
	if event.Type != CosmeticPaymentEventSubscriptionCreated && event.Type != CosmeticPaymentEventSubscriptionUpdated {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(event.SubscriptionStatus))
	return !event.Terminal && status != db.CosmeticSubscriptionStatusCanceled &&
		status != db.CosmeticSubscriptionStatusExpired && status != "incomplete_expired"
}

// refreshTerminalBotVisuals repairs the engine's presentation-only cache after
// the committed order transaction deletes refunded/chargeback loadouts. The
// connected set is bounded and includes waiting bots; refreshing every member
// also makes an idempotent webhook retry able to repair a prior transient read
// failure without retaining deleted assignment metadata.
func (h *CosmeticCommerceHandler) refreshTerminalBotVisuals(ctx context.Context, order *db.CosmeticOrder) (int, error) {
	if h == nil || h.engine == nil || h.store == nil || order == nil ||
		(order.Status != db.CosmeticOrderStatusRefunded && order.Status != db.CosmeticOrderStatusDisputed) {
		return 0, nil
	}
	return h.refreshConnectedBotVisuals(ctx)
}

func (h *CosmeticCommerceHandler) refreshConnectedBotVisuals(ctx context.Context) (int, error) {
	if h == nil || h.engine == nil || h.store == nil {
		return 0, nil
	}
	botIDs := h.engine.ConnectedBotIDs()
	if len(botIDs) == 0 {
		return 0, nil
	}
	equippedByBot, err := h.store.EquippedForBots(ctx, botIDs)
	if err != nil {
		return 0, err
	}
	refreshed := 0
	for _, botID := range botIDs {
		if h.engine.UpdateBotCosmetics(botID, equippedByBot[botID]) {
			refreshed++
		}
	}
	return refreshed, nil
}

func databaseCosmeticPaymentEvent(event CosmeticPaymentEvent) (db.CosmeticPaymentEventInput, error) {
	eventType, err := databaseCosmeticPaymentEventType(event.Type)
	if err != nil {
		return db.CosmeticPaymentEventInput{}, err
	}
	input := db.CosmeticPaymentEventInput{
		Provider: "stripe", EventID: strings.TrimSpace(event.ID), EventType: eventType,
		PayloadHash: strings.ToLower(strings.TrimSpace(event.PayloadSHA256)),
		OrderID:     strings.TrimSpace(event.OrderID), AccountID: strings.TrimSpace(event.AccountID),
		CheckoutSessionID: strings.TrimSpace(event.CheckoutSessionID), PaymentIntentID: strings.TrimSpace(event.PaymentIntentID),
		Currency:            strings.ToUpper(strings.TrimSpace(event.Currency)),
		Paid:                strings.EqualFold(strings.TrimSpace(event.PaymentStatus), "paid"),
		AmountReceivedCents: event.AmountTotal,
		RefundID:            strings.TrimSpace(event.RefundID), RefundStatus: strings.ToLower(strings.TrimSpace(event.RefundStatus)),
	}
	switch event.Type {
	case CosmeticPaymentEventRefundCreated, CosmeticPaymentEventRefundUpdated, CosmeticPaymentEventRefundFailed:
		input.RefundAmountCents = event.AmountRefunded
	case CosmeticPaymentEventChargeRefunded:
		input.CumulativeRefundedCents = event.AmountRefunded
	case CosmeticPaymentEventCheckoutAsyncFailed:
		input.FailureMessage = "asynchronous checkout payment failed"
	case CosmeticPaymentEventCheckoutExpired:
		input.FailureMessage = "checkout session expired"
	}
	return input, nil
}

func databaseCosmeticPaymentEventType(eventType CosmeticPaymentEventType) (string, error) {
	switch eventType {
	case CosmeticPaymentEventCheckoutCompleted:
		return db.CosmeticStripeCheckoutCompleted, nil
	case CosmeticPaymentEventCheckoutAsyncSucceeded:
		return db.CosmeticStripeCheckoutAsyncPaymentSucceeded, nil
	case CosmeticPaymentEventCheckoutAsyncFailed:
		return db.CosmeticStripeCheckoutAsyncPaymentFailed, nil
	case CosmeticPaymentEventCheckoutExpired:
		return db.CosmeticStripeCheckoutExpired, nil
	case CosmeticPaymentEventRefundCreated:
		return db.CosmeticStripeRefundCreated, nil
	case CosmeticPaymentEventRefundUpdated:
		return db.CosmeticStripeRefundUpdated, nil
	case CosmeticPaymentEventRefundFailed:
		return db.CosmeticStripeRefundFailed, nil
	case CosmeticPaymentEventChargeRefunded:
		return db.CosmeticStripeChargeRefunded, nil
	case CosmeticPaymentEventDisputeCreated:
		return db.CosmeticStripeDisputeCreated, nil
	default:
		return "", ErrUnsupportedCosmeticPaymentEvent
	}
}

func (h *CosmeticCommerceHandler) CustomerOrders(w http.ResponseWriter, r *http.Request) {
	setCustomerNoStore(w)
	session, ok := customerSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "customer authentication required")
		return
	}
	limit, ok := cosmeticOrderLimit(w, r)
	if !ok {
		return
	}
	orders, err := h.store.ListCustomerOrders(r.Context(), session.AccountID, limit)
	if err != nil {
		writeCosmeticOrdersListError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"orders": orders, "count": len(orders)})
}

func (h *CosmeticCommerceHandler) AdminOrders(w http.ResponseWriter, r *http.Request) {
	setAdminCatalogNoStore(w)
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	status := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("status")))
	if len(query) > 120 || !validCosmeticOrderStatusFilter(status) {
		writeError(w, http.StatusBadRequest, "invalid cosmetic order filter")
		return
	}
	limit, ok := cosmeticOrderLimit(w, r)
	if !ok {
		return
	}
	orders, err := h.store.ListAdminOrders(r.Context(), query, status, limit)
	if err != nil {
		writeCosmeticOrdersListError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"orders": orders, "count": len(orders)})
}

func cosmeticOrderLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return 0, false
		}
		limit = parsed
	}
	if limit > 100 {
		limit = 100
	}
	return limit, true
}

func validCosmeticOrderStatusFilter(status string) bool {
	switch status {
	case "", db.CosmeticOrderStatusCreated, db.CosmeticOrderStatusCheckout, db.CosmeticOrderStatusProcessing,
		db.CosmeticOrderStatusPaid, db.CosmeticOrderStatusPaymentFailed, db.CosmeticOrderStatusExpired,
		db.CosmeticOrderStatusRefundReview, db.CosmeticOrderStatusRefunded, db.CosmeticOrderStatusDisputed:
		return true
	default:
		return false
	}
}

func writeCosmeticOrdersListError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrNoDatabase) {
		writeError(w, http.StatusServiceUnavailable, "cosmetic order ledger is unavailable")
		return
	}
	slog.Error("failed to list cosmetic orders", "error", err)
	writeError(w, http.StatusInternalServerError, "failed to load cosmetic orders")
}
