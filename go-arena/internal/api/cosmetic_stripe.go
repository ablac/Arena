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
}

// CosmeticCheckoutSession contains only the hosted-checkout fields a handler
// may safely return to the browser.
type CosmeticCheckoutSession struct {
	ID        string
	URL       string
	ExpiresAt time.Time
}

// CosmeticPaymentEventType is independent from Stripe's event names so the
// fulfillment layer does not need provider-specific imports.
type CosmeticPaymentEventType string

const (
	CosmeticPaymentEventCheckoutCompleted      CosmeticPaymentEventType = "checkout_completed"
	CosmeticPaymentEventCheckoutAsyncSucceeded CosmeticPaymentEventType = "checkout_async_succeeded"
	CosmeticPaymentEventCheckoutAsyncFailed    CosmeticPaymentEventType = "checkout_async_failed"
	CosmeticPaymentEventCheckoutExpired        CosmeticPaymentEventType = "checkout_expired"
	CosmeticPaymentEventRefundCreated          CosmeticPaymentEventType = "refund_created"
	CosmeticPaymentEventRefundUpdated          CosmeticPaymentEventType = "refund_updated"
	CosmeticPaymentEventRefundFailed           CosmeticPaymentEventType = "refund_failed"
	CosmeticPaymentEventChargeRefunded         CosmeticPaymentEventType = "charge_refunded"
	CosmeticPaymentEventDisputeCreated         CosmeticPaymentEventType = "dispute_created"
)

// CosmeticPaymentEvent is the signed, normalized payment fact consumed by a
// separate idempotent order/fulfillment handler. Parsing performs no DB writes.
type CosmeticPaymentEvent struct {
	ID                string
	Type              CosmeticPaymentEventType
	PayloadSHA256     string
	OrderID           string
	AccountID         string
	CheckoutSessionID string
	PaymentIntentID   string
	AmountTotal       int64
	AmountRefunded    int64
	Currency          string
	PaymentStatus     string
	RefundID          string
	RefundStatus      string
	DisputeID         string
	DisputeStatus     string
}

// CosmeticPaymentProvider is the provider-neutral seam used by checkout and
// webhook HTTP handlers.
type CosmeticPaymentProvider interface {
	CreateCheckoutSession(context.Context, CosmeticCheckoutRequest) (*CosmeticCheckoutSession, error)
	ParseWebhook(payload []byte, signatureHeader string) (*CosmeticPaymentEvent, error)
}

var ErrUnsupportedCosmeticPaymentEvent = errors.New("unsupported cosmetic payment event")

type stripeCheckoutSessionCreator interface {
	Create(context.Context, *stripe.CheckoutSessionCreateParams) (*stripe.CheckoutSession, error)
}

// StripeCosmeticPaymentProvider adapts Stripe Checkout and signed Stripe
// webhooks to the provider-neutral commerce contract.
type StripeCosmeticPaymentProvider struct {
	checkoutCreator stripeCheckoutSessionCreator
	webhookSecrets  []string
	successURL      string
	cancelURL       string
	automaticTax    bool
}

var _ CosmeticPaymentProvider = (*StripeCosmeticPaymentProvider)(nil)

func NewStripeCosmeticPaymentProvider(secretKey string, webhookSecrets []string, successURL, cancelURL string, automaticTax bool) *StripeCosmeticPaymentProvider {
	client := stripe.NewClient(strings.TrimSpace(secretKey))
	return newStripeCosmeticPaymentProviderWithCreator(client.V1CheckoutSessions, webhookSecrets, successURL, cancelURL, automaticTax)
}

func newStripeCosmeticPaymentProviderWithCreator(creator stripeCheckoutSessionCreator, webhookSecrets []string, successURL, cancelURL string, automaticTax bool) *StripeCosmeticPaymentProvider {
	secrets := make([]string, 0, len(webhookSecrets))
	for _, value := range webhookSecrets {
		if secret := strings.TrimSpace(value); secret != "" {
			secrets = append(secrets, secret)
		}
	}
	return &StripeCosmeticPaymentProvider{
		checkoutCreator: creator,
		webhookSecrets:  secrets,
		successURL:      strings.TrimSpace(successURL),
		cancelURL:       strings.TrimSpace(cancelURL),
		automaticTax:    automaticTax,
	}
}

func (p *StripeCosmeticPaymentProvider) CreateCheckoutSession(ctx context.Context, request CosmeticCheckoutRequest) (*CosmeticCheckoutSession, error) {
	request.OrderID = strings.TrimSpace(request.OrderID)
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.CustomerEmail = strings.TrimSpace(strings.ToLower(request.CustomerEmail))
	request.PackID = strings.TrimSpace(request.PackID)
	request.PackName = strings.TrimSpace(request.PackName)
	request.Currency = strings.ToLower(strings.TrimSpace(request.Currency))
	if p == nil || p.checkoutCreator == nil {
		return nil, errors.New("cosmetic checkout provider is not configured")
	}
	if request.OrderID == "" || request.AccountID == "" || request.CustomerEmail == "" ||
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
		SuccessURL:        stripe.String(p.successURL),
		CancelURL:         stripe.String(p.cancelURL),
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
	params.SetIdempotencyKey("cosmetics_checkout_" + request.OrderID)

	session, err := p.checkoutCreator.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create Stripe checkout session: %w", err)
	}
	if session == nil {
		return nil, errors.New("Stripe checkout returned no session")
	}
	result := &CosmeticCheckoutSession{ID: session.ID, URL: session.URL}
	if session.ExpiresAt > 0 {
		result.ExpiresAt = time.Unix(session.ExpiresAt, 0).UTC()
	}
	return result, nil
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
