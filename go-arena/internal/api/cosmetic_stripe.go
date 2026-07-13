package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"arena-server/internal/db"

	stripe "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"
)

// CosmeticCheckoutRequest is provider-neutral input assembled from the
// authenticated account and the server-side catalog/order record. Callers,
// never browser input, own UnitAmount, Currency, and Quantity.
type CosmeticCheckoutRequest struct {
	OrderID         string
	AccountID       string
	CustomerEmail   string
	PackID          string
	PackName        string
	PackDescription string
	UnitAmount      int64
	Currency        string
	Quantity        int64
	Presentation    CosmeticCheckoutPresentation
}

// CosmeticCheckoutPresentation is provider-neutral. Stripe's current API
// serializes the embedded value as "embedded_page", but the Arena HTTP
// contract deliberately exposes the stable, provider-agnostic "embedded".
type CosmeticCheckoutPresentation string

const (
	CosmeticCheckoutPresentationEmbedded CosmeticCheckoutPresentation = "embedded"
	CosmeticCheckoutPresentationHosted   CosmeticCheckoutPresentation = "hosted"
)

func normalizeCosmeticCheckoutPresentation(value CosmeticCheckoutPresentation) (CosmeticCheckoutPresentation, bool) {
	switch CosmeticCheckoutPresentation(strings.ToLower(strings.TrimSpace(string(value)))) {
	case "", CosmeticCheckoutPresentationEmbedded:
		return CosmeticCheckoutPresentationEmbedded, true
	case CosmeticCheckoutPresentationHosted:
		return CosmeticCheckoutPresentationHosted, true
	default:
		return "", false
	}
}

// CosmeticCheckoutSession contains only the checkout fields a handler may
// safely return to the browser. URL and ClientSecret are mutually exclusive.
type CosmeticCheckoutSession struct {
	ID             string
	URL            string
	ClientSecret   string
	Presentation   CosmeticCheckoutPresentation
	ExpiresAt      time.Time
	Status         CosmeticCheckoutSessionStatus
	Mode           string
	OrderID        string
	PackID         string
	SubscriptionID string
	AccountID      string
}

type CosmeticCheckoutSessionStatus string

const (
	CosmeticCheckoutSessionStatusOpen     CosmeticCheckoutSessionStatus = "open"
	CosmeticCheckoutSessionStatusComplete CosmeticCheckoutSessionStatus = "complete"
	CosmeticCheckoutSessionStatusExpired  CosmeticCheckoutSessionStatus = "expired"
)

type CosmeticPortalSession struct {
	URL string
}

type CosmeticSubscriptionCheckoutRequest struct {
	SubscriptionID string
	AccountID      string
	CustomerEmail  string
	CustomerID     string
	Presentation   CosmeticCheckoutPresentation
}

type CosmeticSubscriptionProviderState struct {
	ID                string
	CustomerID        string
	Status            string
	CancelAtPeriodEnd bool
	CurrentPeriodEnd  *time.Time
	Terminal          bool
}

type CosmeticPaymentKind string

const CosmeticPaymentKindSubscription CosmeticPaymentKind = "subscription"

// CosmeticPaymentEventType is independent from Stripe's event names so the
// fulfillment layer does not need provider-specific imports.
type CosmeticPaymentEventType string

const (
	CosmeticPaymentEventCheckoutCompleted             CosmeticPaymentEventType = "checkout_completed"
	CosmeticPaymentEventCheckoutAsyncSucceeded        CosmeticPaymentEventType = "checkout_async_succeeded"
	CosmeticPaymentEventCheckoutAsyncFailed           CosmeticPaymentEventType = "checkout_async_failed"
	CosmeticPaymentEventCheckoutExpired               CosmeticPaymentEventType = "checkout_expired"
	CosmeticPaymentEventRefundCreated                 CosmeticPaymentEventType = "refund_created"
	CosmeticPaymentEventRefundUpdated                 CosmeticPaymentEventType = "refund_updated"
	CosmeticPaymentEventRefundFailed                  CosmeticPaymentEventType = "refund_failed"
	CosmeticPaymentEventChargeRefunded                CosmeticPaymentEventType = "charge_refunded"
	CosmeticPaymentEventDisputeCreated                CosmeticPaymentEventType = "dispute_created"
	CosmeticPaymentEventSubscriptionCheckoutCompleted CosmeticPaymentEventType = "subscription_checkout_completed"
	CosmeticPaymentEventSubscriptionCheckoutExpired   CosmeticPaymentEventType = "subscription_checkout_expired"
	CosmeticPaymentEventSubscriptionCreated           CosmeticPaymentEventType = "subscription_created"
	CosmeticPaymentEventSubscriptionUpdated           CosmeticPaymentEventType = "subscription_updated"
	CosmeticPaymentEventSubscriptionDeleted           CosmeticPaymentEventType = "subscription_deleted"
)

// CosmeticPaymentEvent is the signed, normalized payment fact consumed by a
// separate idempotent order/fulfillment handler. Parsing performs no DB writes.
type CosmeticPaymentEvent struct {
	ID                      string
	Type                    CosmeticPaymentEventType
	Kind                    CosmeticPaymentKind
	PayloadSHA256           string
	OrderID                 string
	AccountID               string
	CheckoutSessionID       string
	PaymentIntentID         string
	AmountTotal             int64
	AmountRefunded          int64
	Currency                string
	PaymentStatus           string
	RefundID                string
	RefundStatus            string
	DisputeID               string
	DisputeStatus           string
	SubscriptionID          string
	CustomerID              string
	ProviderSubscriptionID  string
	SubscriptionStatus      string
	CancelAtPeriodEnd       bool
	CurrentPeriodEnd        *time.Time
	ProviderCreatedAt       time.Time
	ProviderStateObservedAt time.Time
	Terminal                bool
}

// CosmeticPaymentProvider is the provider-neutral seam used by checkout and
// webhook HTTP handlers.
type CosmeticPaymentProvider interface {
	CreateCheckoutSession(context.Context, CosmeticCheckoutRequest) (*CosmeticCheckoutSession, error)
	RetrieveCheckoutSession(context.Context, string) (*CosmeticCheckoutSession, error)
	ParseWebhook(payload []byte, signatureHeader string) (*CosmeticPaymentEvent, error)
}

var ErrUnsupportedCosmeticPaymentEvent = errors.New("unsupported cosmetic payment event")

type stripeCheckoutSessionCreator interface {
	Create(context.Context, *stripe.CheckoutSessionCreateParams) (*stripe.CheckoutSession, error)
}

type stripeBillingPortalSessionCreator interface {
	Create(context.Context, *stripe.BillingPortalSessionCreateParams) (*stripe.BillingPortalSession, error)
}

type stripeCheckoutSessionRetriever interface {
	Retrieve(context.Context, string, *stripe.CheckoutSessionRetrieveParams) (*stripe.CheckoutSession, error)
}

type stripeSubscriptionRetriever interface {
	Retrieve(context.Context, string, *stripe.SubscriptionRetrieveParams) (*stripe.Subscription, error)
}

// StripeCosmeticPaymentProvider adapts Stripe Checkout and signed Stripe
// webhooks to the provider-neutral commerce contract.
type StripeCosmeticPaymentProvider struct {
	checkoutCreator       stripeCheckoutSessionCreator
	checkoutRetriever     stripeCheckoutSessionRetriever
	portalCreator         stripeBillingPortalSessionCreator
	subscriptionRetriever stripeSubscriptionRetriever
	webhookSecrets        []string
	successURL            string
	cancelURL             string
	returnURL             string
	portalReturnURL       string
	automaticTax          bool
}

var _ CosmeticPaymentProvider = (*StripeCosmeticPaymentProvider)(nil)

func NewStripeCosmeticPaymentProvider(secretKey string, webhookSecrets []string, successURL, cancelURL, returnURL, portalReturnURL string, automaticTax bool) *StripeCosmeticPaymentProvider {
	client := stripe.NewClient(strings.TrimSpace(secretKey))
	provider := newStripeCosmeticPaymentProviderWithCreator(client.V1CheckoutSessions, webhookSecrets, successURL, cancelURL, automaticTax)
	provider.checkoutRetriever = client.V1CheckoutSessions
	provider.portalCreator = client.V1BillingPortalSessions
	provider.subscriptionRetriever = client.V1Subscriptions
	provider.returnURL = strings.TrimSpace(returnURL)
	provider.portalReturnURL = strings.TrimSpace(portalReturnURL)
	return provider
}

func newStripeCosmeticPaymentProviderWithCreator(creator stripeCheckoutSessionCreator, webhookSecrets []string, successURL, cancelURL string, automaticTax bool) *StripeCosmeticPaymentProvider {
	secrets := make([]string, 0, len(webhookSecrets))
	for _, value := range webhookSecrets {
		if secret := strings.TrimSpace(value); secret != "" {
			secrets = append(secrets, secret)
		}
	}
	provider := &StripeCosmeticPaymentProvider{
		checkoutCreator: creator,
		webhookSecrets:  secrets,
		successURL:      strings.TrimSpace(successURL),
		cancelURL:       strings.TrimSpace(cancelURL),
		returnURL:       strings.TrimSpace(successURL),
		portalReturnURL: strings.TrimSpace(successURL),
		automaticTax:    automaticTax,
	}
	if retriever, ok := creator.(stripeCheckoutSessionRetriever); ok {
		provider.checkoutRetriever = retriever
	}
	return provider
}

func (p *StripeCosmeticPaymentProvider) CreateCheckoutSession(ctx context.Context, request CosmeticCheckoutRequest) (*CosmeticCheckoutSession, error) {
	request.OrderID = strings.TrimSpace(request.OrderID)
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.CustomerEmail = strings.TrimSpace(strings.ToLower(request.CustomerEmail))
	request.PackID = strings.TrimSpace(request.PackID)
	request.PackName = strings.TrimSpace(request.PackName)
	request.Currency = strings.ToLower(strings.TrimSpace(request.Currency))
	presentation, validPresentation := normalizeCosmeticCheckoutPresentation(request.Presentation)
	if p == nil || p.checkoutCreator == nil {
		return nil, errors.New("cosmetic checkout provider is not configured")
	}
	if !validPresentation || request.OrderID == "" || request.AccountID == "" || request.CustomerEmail == "" ||
		request.PackID == "" || request.PackName == "" || request.UnitAmount <= 0 ||
		len(request.Currency) != 3 || request.Quantity <= 0 {
		return nil, errors.New("invalid cosmetic checkout request")
	}

	metadata := map[string]string{
		"order_id":   request.OrderID,
		"account_id": request.AccountID,
		"pack_id":    request.PackID,
		"pack_name":  request.PackName,
	}
	paymentMetadata := make(map[string]string, len(metadata))
	for key, value := range metadata {
		paymentMetadata[key] = value
	}
	params := &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModePayment)),
		CustomerEmail:     stripe.String(request.CustomerEmail),
		ClientReferenceID: stripe.String(request.OrderID),
		AutomaticTax: &stripe.CheckoutSessionCreateAutomaticTaxParams{
			Enabled: stripe.Bool(p.automaticTax),
		},
		Metadata: metadata,
		PaymentIntentData: &stripe.CheckoutSessionCreatePaymentIntentDataParams{
			Description:  stripe.String(request.PackDescription),
			Metadata:     paymentMetadata,
			ReceiptEmail: stripe.String(request.CustomerEmail),
		},
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(request.Quantity),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String(request.Currency),
				UnitAmount: stripe.Int64(request.UnitAmount),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name:        stripe.String(request.PackName),
					Description: stripe.String(request.PackDescription),
				},
			},
		}},
	}
	applyStripeCheckoutPresentation(params, presentation, p.successURL, p.cancelURL, p.returnURL)
	params.SetIdempotencyKey("cosmetics_checkout_" + request.OrderID)

	session, err := p.checkoutCreator.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe checkout session: %w", err)
	}
	if session == nil {
		return nil, errors.New("Stripe checkout returned no session")
	}
	result := cosmeticCheckoutSessionFromStripe(session, presentation)
	return result, nil
}

// RetrieveCheckoutSession resolves the existing one-time Checkout Session.
// The UI mode is read from Stripe rather than accepted from the browser, so a
// resumed Checkout cannot be silently replaced with a second session.
func (p *StripeCosmeticPaymentProvider) RetrieveCheckoutSession(ctx context.Context, checkoutSessionID string) (*CosmeticCheckoutSession, error) {
	checkoutSessionID = strings.TrimSpace(checkoutSessionID)
	if p == nil || p.checkoutRetriever == nil || checkoutSessionID == "" {
		return nil, errors.New("cosmetic Checkout retrieval is not configured")
	}
	session, err := p.checkoutRetriever.Retrieve(ctx, checkoutSessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("retrieve Stripe cosmetic checkout session: %w", err)
	}
	if session == nil || strings.TrimSpace(session.ID) == "" {
		return nil, errors.New("Stripe cosmetic checkout retrieval returned no session")
	}
	status := CosmeticCheckoutSessionStatus(strings.ToLower(strings.TrimSpace(string(session.Status))))

	var presentation CosmeticCheckoutPresentation
	switch session.UIMode {
	case stripe.CheckoutSessionUIModeEmbeddedPage:
		presentation = CosmeticCheckoutPresentationEmbedded
		if status == CosmeticCheckoutSessionStatusOpen &&
			(strings.TrimSpace(session.ClientSecret) == "" || strings.TrimSpace(session.URL) != "") {
			return nil, errors.New("Stripe embedded cosmetic checkout session has invalid browser fields")
		}
	case "", stripe.CheckoutSessionUIModeHostedPage:
		presentation = CosmeticCheckoutPresentationHosted
		if status == CosmeticCheckoutSessionStatusOpen &&
			(strings.TrimSpace(session.URL) == "" || strings.TrimSpace(session.ClientSecret) != "") {
			return nil, errors.New("Stripe hosted cosmetic checkout session has invalid browser fields")
		}
	default:
		return nil, fmt.Errorf("unsupported Stripe cosmetic checkout UI mode %q", session.UIMode)
	}

	result := cosmeticCheckoutSessionFromStripe(session, presentation)
	result.Status = status
	result.Mode = strings.ToLower(strings.TrimSpace(string(session.Mode)))
	result.OrderID, result.AccountID = cosmeticPaymentMetadata(session.Metadata)
	result.PackID = strings.TrimSpace(session.Metadata["pack_id"])
	return result, nil
}

func (p *StripeCosmeticPaymentProvider) CreateSubscriptionCheckoutSession(ctx context.Context, request CosmeticSubscriptionCheckoutRequest) (*CosmeticCheckoutSession, error) {
	request.SubscriptionID = strings.TrimSpace(request.SubscriptionID)
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.CustomerEmail = strings.TrimSpace(strings.ToLower(request.CustomerEmail))
	request.CustomerID = strings.TrimSpace(request.CustomerID)
	presentation, validPresentation := normalizeCosmeticCheckoutPresentation(request.Presentation)
	if p == nil || p.checkoutCreator == nil {
		return nil, errors.New("cosmetic subscription checkout provider is not configured")
	}
	if !validPresentation || request.SubscriptionID == "" || request.AccountID == "" || request.CustomerEmail == "" {
		return nil, errors.New("invalid cosmetic subscription checkout request")
	}
	metadata := map[string]string{
		"commerce_kind":   "cosmetic_subscription",
		"subscription_id": request.SubscriptionID,
		"account_id":      request.AccountID,
	}
	params := &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		ClientReferenceID: stripe.String(request.SubscriptionID),
		AutomaticTax: &stripe.CheckoutSessionCreateAutomaticTaxParams{
			Enabled: stripe.Bool(p.automaticTax),
		},
		Metadata: metadata,
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Description: stripe.String("Arena Cosmetics Pass: every current and future cosmetic set, full-body skin, and trail."),
			Metadata:    metadata,
		},
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String(strings.ToLower(db.CosmeticSubscriptionCurrency)),
				UnitAmount: stripe.Int64(db.CosmeticSubscriptionPriceCents),
				Recurring: &stripe.CheckoutSessionCreateLineItemPriceDataRecurringParams{
					Interval:      stripe.String(db.CosmeticSubscriptionInterval),
					IntervalCount: stripe.Int64(1),
				},
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name:        stripe.String("Arena Cosmetics Pass"),
					Description: stripe.String("Every current and future Arena cosmetic set, full-body skin, and trail, for up to five API keys."),
				},
			},
		}},
	}
	if request.CustomerID != "" {
		params.Customer = stripe.String(request.CustomerID)
	} else {
		params.CustomerEmail = stripe.String(request.CustomerEmail)
	}
	applyStripeCheckoutPresentation(params, presentation, p.successURL, p.cancelURL, p.returnURL)
	params.SetIdempotencyKey("cosmetics_subscription_" + request.SubscriptionID)
	session, err := p.checkoutCreator.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe subscription checkout session: %w", err)
	}
	if session == nil {
		return nil, errors.New("Stripe subscription checkout returned no session")
	}
	result := cosmeticCheckoutSessionFromStripe(session, presentation)
	return result, nil
}

func (p *StripeCosmeticPaymentProvider) RetrieveSubscriptionCheckoutSession(ctx context.Context, checkoutSessionID string) (*CosmeticCheckoutSession, error) {
	checkoutSessionID = strings.TrimSpace(checkoutSessionID)
	if p == nil || p.checkoutRetriever == nil || checkoutSessionID == "" {
		return nil, errors.New("cosmetic subscription Checkout retrieval is not configured")
	}
	session, err := p.checkoutRetriever.Retrieve(ctx, checkoutSessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("retrieve Stripe subscription checkout session: %w", err)
	}
	if session == nil || strings.TrimSpace(session.ID) == "" {
		return nil, errors.New("Stripe subscription checkout retrieval returned no session")
	}
	presentation := CosmeticCheckoutPresentationHosted
	if string(session.UIMode) == string(stripe.CheckoutSessionUIModeEmbeddedPage) || strings.TrimSpace(session.ClientSecret) != "" {
		presentation = CosmeticCheckoutPresentationEmbedded
	}
	result := cosmeticCheckoutSessionFromStripe(session, presentation)
	result.Status = CosmeticCheckoutSessionStatus(strings.ToLower(strings.TrimSpace(string(session.Status))))
	result.Mode = strings.ToLower(strings.TrimSpace(string(session.Mode)))
	result.SubscriptionID, result.AccountID = cosmeticSubscriptionMetadata(session.Metadata)
	return result, nil
}

func applyStripeCheckoutPresentation(params *stripe.CheckoutSessionCreateParams, presentation CosmeticCheckoutPresentation, successURL, cancelURL, returnURL string) {
	if params == nil {
		return
	}
	if presentation == CosmeticCheckoutPresentationHosted {
		params.SuccessURL = stripe.String(successURL)
		params.CancelURL = stripe.String(cancelURL)
		return
	}
	params.UIMode = stripe.String(string(stripe.CheckoutSessionUIModeEmbeddedPage))
	params.ReturnURL = stripe.String(returnURL)
	params.RedirectOnCompletion = stripe.String(string(stripe.CheckoutSessionRedirectOnCompletionIfRequired))
}

func cosmeticCheckoutSessionFromStripe(session *stripe.CheckoutSession, presentation CosmeticCheckoutPresentation) *CosmeticCheckoutSession {
	if session == nil {
		return nil
	}
	result := &CosmeticCheckoutSession{
		ID: strings.TrimSpace(session.ID), Presentation: presentation,
	}
	if presentation == CosmeticCheckoutPresentationEmbedded {
		result.ClientSecret = strings.TrimSpace(session.ClientSecret)
	} else {
		result.URL = strings.TrimSpace(session.URL)
	}
	if session.ExpiresAt > 0 {
		result.ExpiresAt = time.Unix(session.ExpiresAt, 0).UTC()
	}
	return result
}

func normalizeStripeSubscriptionStatus(status string) (string, bool) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case string(stripe.SubscriptionStatusIncompleteExpired):
		return db.CosmeticSubscriptionStatusExpired, true
	case string(stripe.SubscriptionStatusCanceled):
		return db.CosmeticSubscriptionStatusCanceled, true
	case db.CosmeticSubscriptionStatusExpired:
		return db.CosmeticSubscriptionStatusExpired, true
	default:
		return status, false
	}
}

func (p *StripeCosmeticPaymentProvider) RetrieveCosmeticSubscription(ctx context.Context, providerSubscriptionID string) (*CosmeticSubscriptionProviderState, error) {
	providerSubscriptionID = strings.TrimSpace(providerSubscriptionID)
	if p == nil || p.subscriptionRetriever == nil || providerSubscriptionID == "" {
		return nil, errors.New("cosmetic subscription retrieval is not configured")
	}
	subscription, err := p.subscriptionRetriever.Retrieve(ctx, providerSubscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("retrieve Stripe cosmetic subscription: %w", err)
	}
	if subscription == nil || strings.TrimSpace(subscription.ID) == "" {
		return nil, errors.New("Stripe cosmetic subscription retrieval returned no subscription")
	}
	state := &CosmeticSubscriptionProviderState{
		ID: strings.TrimSpace(subscription.ID), CancelAtPeriodEnd: subscription.CancelAtPeriodEnd,
	}
	if subscription.Customer != nil {
		state.CustomerID = strings.TrimSpace(subscription.Customer.ID)
	}
	state.Status, state.Terminal = normalizeStripeSubscriptionStatus(string(subscription.Status))

	activeItems := make([]*stripe.SubscriptionItem, 0, 1)
	if subscription.Items != nil {
		for _, item := range subscription.Items.Data {
			if item == nil || item.Deleted {
				continue
			}
			activeItems = append(activeItems, item)
			if item.CurrentPeriodEnd > 0 && (state.CurrentPeriodEnd == nil || item.CurrentPeriodEnd > state.CurrentPeriodEnd.Unix()) {
				periodEnd := time.Unix(item.CurrentPeriodEnd, 0).UTC()
				state.CurrentPeriodEnd = &periodEnd
			}
		}
	}
	if subscription.CancelAt > 0 {
		state.CancelAtPeriodEnd = true
		cancelAt := time.Unix(subscription.CancelAt, 0).UTC()
		state.CurrentPeriodEnd = &cancelAt
	}
	if !state.Terminal && !validStripeCosmeticSubscriptionBilling(activeItems) {
		state.Status = db.CosmeticSubscriptionStatusBillingMismatch
	}
	return state, nil
}

func validStripeCosmeticSubscriptionBilling(items []*stripe.SubscriptionItem) bool {
	if len(items) != 1 || items[0].Quantity != 1 || items[0].Price == nil || items[0].Price.Recurring == nil {
		return false
	}
	price := items[0].Price
	return price.UnitAmount == db.CosmeticSubscriptionPriceCents &&
		strings.EqualFold(string(price.Currency), db.CosmeticSubscriptionCurrency) &&
		string(price.Recurring.Interval) == db.CosmeticSubscriptionInterval && price.Recurring.IntervalCount == 1
}

func (p *StripeCosmeticPaymentProvider) CreateBillingPortalSession(ctx context.Context, customerID string) (*CosmeticPortalSession, error) {
	customerID = strings.TrimSpace(customerID)
	if p == nil || p.portalCreator == nil || customerID == "" || p.portalReturnURL == "" {
		return nil, errors.New("cosmetic subscription billing portal is not configured")
	}
	session, err := p.portalCreator.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(p.portalReturnURL),
	})
	if err != nil {
		return nil, fmt.Errorf("create Stripe billing portal session: %w", err)
	}
	if session == nil || strings.TrimSpace(session.URL) == "" {
		return nil, errors.New("Stripe billing portal returned no session")
	}
	return &CosmeticPortalSession{URL: session.URL}, nil
}

func (p *StripeCosmeticPaymentProvider) ParseWebhook(payload []byte, signatureHeader string) (*CosmeticPaymentEvent, error) {
	if p == nil || len(p.webhookSecrets) == 0 {
		return nil, errors.New("Stripe webhook secrets are not configured")
	}
	var (
		event     stripe.Event
		verified  bool
		verifyErr error
	)
	for _, secret := range p.webhookSecrets {
		event, verifyErr = webhook.ConstructEvent(payload, signatureHeader, secret)
		if verifyErr == nil {
			verified = true
			break
		}
	}
	if !verified {
		return nil, fmt.Errorf("verify Stripe webhook signature: %w", verifyErr)
	}

	normalized, err := normalizeStripeCosmeticPaymentEvent(event)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	normalized.PayloadSHA256 = hex.EncodeToString(sum[:])
	return normalized, nil
}

type stripeExpandableID string

func (id *stripeExpandableID) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) || len(data) == 0 {
		*id = ""
		return nil
	}
	var direct string
	if err := json.Unmarshal(data, &direct); err == nil {
		*id = stripeExpandableID(direct)
		return nil
	}
	var expanded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &expanded); err != nil {
		return err
	}
	*id = stripeExpandableID(expanded.ID)
	return nil
}

type stripeCheckoutWebhookObject struct {
	ID            string             `json:"id"`
	Metadata      map[string]string  `json:"metadata"`
	PaymentIntent stripeExpandableID `json:"payment_intent"`
	AmountTotal   int64              `json:"amount_total"`
	Currency      string             `json:"currency"`
	PaymentStatus string             `json:"payment_status"`
	Mode          string             `json:"mode"`
	Customer      stripeExpandableID `json:"customer"`
	Subscription  stripeExpandableID `json:"subscription"`
}

type stripeRefundWebhookObject struct {
	ID            string             `json:"id"`
	Metadata      map[string]string  `json:"metadata"`
	PaymentIntent stripeExpandableID `json:"payment_intent"`
	Amount        int64              `json:"amount"`
	Currency      string             `json:"currency"`
	Status        string             `json:"status"`
}

type stripeChargeWebhookObject struct {
	Metadata       map[string]string  `json:"metadata"`
	PaymentIntent  stripeExpandableID `json:"payment_intent"`
	Amount         int64              `json:"amount"`
	AmountRefunded int64              `json:"amount_refunded"`
	Currency       string             `json:"currency"`
	Status         string             `json:"status"`
}

type stripeDisputeWebhookObject struct {
	ID            string             `json:"id"`
	Metadata      map[string]string  `json:"metadata"`
	PaymentIntent stripeExpandableID `json:"payment_intent"`
	Amount        int64              `json:"amount"`
	Currency      string             `json:"currency"`
	Status        string             `json:"status"`
}

type stripeSubscriptionWebhookObject struct {
	ID                string             `json:"id"`
	Customer          stripeExpandableID `json:"customer"`
	Status            string             `json:"status"`
	CancelAt          int64              `json:"cancel_at"`
	CancelAtPeriodEnd bool               `json:"cancel_at_period_end"`
	CurrentPeriodEnd  int64              `json:"current_period_end"`
	Metadata          map[string]string  `json:"metadata"`
}

func normalizeStripeCosmeticPaymentEvent(event stripe.Event) (*CosmeticPaymentEvent, error) {
	if event.Data == nil || len(event.Data.Raw) == 0 {
		return nil, errors.New("Stripe webhook is missing event data")
	}
	result := &CosmeticPaymentEvent{ID: event.ID}
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted,
		stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded,
		stripe.EventTypeCheckoutSessionAsyncPaymentFailed,
		stripe.EventTypeCheckoutSessionExpired:
		var object stripeCheckoutWebhookObject
		if err := json.Unmarshal(event.Data.Raw, &object); err != nil {
			return nil, fmt.Errorf("decode Stripe checkout event: %w", err)
		}
		if object.Mode == string(stripe.CheckoutSessionModeSubscription) ||
			strings.EqualFold(strings.TrimSpace(object.Metadata["commerce_kind"]), "cosmetic_subscription") {
			if event.Type != stripe.EventTypeCheckoutSessionCompleted && event.Type != stripe.EventTypeCheckoutSessionExpired {
				return nil, fmt.Errorf("%w: %s", ErrUnsupportedCosmeticPaymentEvent, event.Type)
			}
			result.Kind = CosmeticPaymentKindSubscription
			result.Type = CosmeticPaymentEventSubscriptionCheckoutCompleted
			if event.Type == stripe.EventTypeCheckoutSessionExpired {
				result.Type = CosmeticPaymentEventSubscriptionCheckoutExpired
				result.Terminal = true
			}
			result.SubscriptionID, result.AccountID = cosmeticSubscriptionMetadata(object.Metadata)
			result.CheckoutSessionID = object.ID
			result.CustomerID = string(object.Customer)
			result.ProviderSubscriptionID = string(object.Subscription)
			result.ProviderCreatedAt = time.Unix(event.Created, 0).UTC()
			return result, nil
		}
		result.Type = normalizedCheckoutEventType(event.Type)
		result.OrderID, result.AccountID = cosmeticPaymentMetadata(object.Metadata)
		result.CheckoutSessionID = object.ID
		result.PaymentIntentID = string(object.PaymentIntent)
		result.AmountTotal = object.AmountTotal
		result.Currency = object.Currency
		result.PaymentStatus = object.PaymentStatus

	case stripe.EventTypeRefundCreated, stripe.EventTypeRefundUpdated, stripe.EventTypeRefundFailed:
		var object stripeRefundWebhookObject
		if err := json.Unmarshal(event.Data.Raw, &object); err != nil {
			return nil, fmt.Errorf("decode Stripe refund event: %w", err)
		}
		result.Type = normalizedRefundEventType(event.Type)
		result.OrderID, result.AccountID = cosmeticPaymentMetadata(object.Metadata)
		result.PaymentIntentID = string(object.PaymentIntent)
		result.AmountRefunded = object.Amount
		result.Currency = object.Currency
		result.RefundID = object.ID
		result.RefundStatus = object.Status

	case stripe.EventTypeChargeRefunded:
		var object stripeChargeWebhookObject
		if err := json.Unmarshal(event.Data.Raw, &object); err != nil {
			return nil, fmt.Errorf("decode Stripe charge event: %w", err)
		}
		result.Type = CosmeticPaymentEventChargeRefunded
		result.OrderID, result.AccountID = cosmeticPaymentMetadata(object.Metadata)
		result.PaymentIntentID = string(object.PaymentIntent)
		result.AmountTotal = object.Amount
		result.AmountRefunded = object.AmountRefunded
		result.Currency = object.Currency
		result.PaymentStatus = object.Status

	case stripe.EventTypeChargeDisputeCreated:
		var object stripeDisputeWebhookObject
		if err := json.Unmarshal(event.Data.Raw, &object); err != nil {
			return nil, fmt.Errorf("decode Stripe dispute event: %w", err)
		}
		result.Type = CosmeticPaymentEventDisputeCreated
		result.OrderID, result.AccountID = cosmeticPaymentMetadata(object.Metadata)
		result.PaymentIntentID = string(object.PaymentIntent)
		result.AmountTotal = object.Amount
		result.Currency = object.Currency
		result.DisputeID = object.ID
		result.DisputeStatus = object.Status

	case stripe.EventTypeCustomerSubscriptionCreated,
		stripe.EventTypeCustomerSubscriptionUpdated,
		stripe.EventTypeCustomerSubscriptionDeleted:
		var object stripeSubscriptionWebhookObject
		if err := json.Unmarshal(event.Data.Raw, &object); err != nil {
			return nil, fmt.Errorf("decode Stripe subscription event: %w", err)
		}
		result.Kind = CosmeticPaymentKindSubscription
		switch event.Type {
		case stripe.EventTypeCustomerSubscriptionCreated:
			result.Type = CosmeticPaymentEventSubscriptionCreated
		case stripe.EventTypeCustomerSubscriptionUpdated:
			result.Type = CosmeticPaymentEventSubscriptionUpdated
		default:
			result.Type = CosmeticPaymentEventSubscriptionDeleted
			result.Terminal = true
		}
		result.SubscriptionID, result.AccountID = cosmeticSubscriptionMetadata(object.Metadata)
		result.ProviderSubscriptionID = object.ID
		result.CustomerID = string(object.Customer)
		status, terminal := normalizeStripeSubscriptionStatus(object.Status)
		result.SubscriptionStatus = status
		result.Terminal = result.Terminal || terminal
		result.CancelAtPeriodEnd = object.CancelAtPeriodEnd
		if object.CurrentPeriodEnd > 0 {
			periodEnd := time.Unix(object.CurrentPeriodEnd, 0).UTC()
			result.CurrentPeriodEnd = &periodEnd
		}
		if object.CancelAt > 0 {
			result.CancelAtPeriodEnd = true
			cancelAt := time.Unix(object.CancelAt, 0).UTC()
			result.CurrentPeriodEnd = &cancelAt
		}
		result.ProviderCreatedAt = time.Unix(event.Created, 0).UTC()

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedCosmeticPaymentEvent, event.Type)
	}
	return result, nil
}

func normalizedCheckoutEventType(eventType stripe.EventType) CosmeticPaymentEventType {
	switch eventType {
	case stripe.EventTypeCheckoutSessionCompleted:
		return CosmeticPaymentEventCheckoutCompleted
	case stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded:
		return CosmeticPaymentEventCheckoutAsyncSucceeded
	case stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		return CosmeticPaymentEventCheckoutAsyncFailed
	default:
		return CosmeticPaymentEventCheckoutExpired
	}
}

func normalizedRefundEventType(eventType stripe.EventType) CosmeticPaymentEventType {
	switch eventType {
	case stripe.EventTypeRefundCreated:
		return CosmeticPaymentEventRefundCreated
	case stripe.EventTypeRefundUpdated:
		return CosmeticPaymentEventRefundUpdated
	default:
		return CosmeticPaymentEventRefundFailed
	}
}

func cosmeticPaymentMetadata(metadata map[string]string) (string, string) {
	return strings.TrimSpace(metadata["order_id"]), strings.TrimSpace(metadata["account_id"])
}

func cosmeticSubscriptionMetadata(metadata map[string]string) (string, string) {
	return strings.TrimSpace(metadata["subscription_id"]), strings.TrimSpace(metadata["account_id"])
}
