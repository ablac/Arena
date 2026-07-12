package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"
)

type recordingStripeCheckoutCreator struct {
	params  *stripe.CheckoutSessionCreateParams
	session *stripe.CheckoutSession
	err     error
}

func (c *recordingStripeCheckoutCreator) Create(_ context.Context, params *stripe.CheckoutSessionCreateParams) (*stripe.CheckoutSession, error) {
	c.params = params
	return c.session, c.err
}

func TestStripeCosmeticPaymentProviderBuildsServerControlledCheckout(t *testing.T) {
	creator := &recordingStripeCheckoutCreator{session: &stripe.CheckoutSession{
		ID:        "cs_test_arena",
		URL:       "https://checkout.stripe.test/c/pay/cs_test_arena",
		ExpiresAt: 1_900_000_000,
	}}
	provider := newStripeCosmeticPaymentProviderWithCreator(
		creator,
		[]string{"whsec_current"},
		"https://arena.example/dashboard?checkout=success",
		"https://arena.example/shop?checkout=cancelled",
		true,
	)

	got, err := provider.CreateCheckoutSession(context.Background(), CosmeticCheckoutRequest{
		OrderID:         "order-123",
		AccountID:       "account-456",
		CustomerEmail:   "owner@example.com",
		PackID:          "pack-neon-founders",
		PackName:        "Neon Founders Set",
		PackDescription: "A launch set of presentation-only cosmetics.",
		UnitAmount:      1299,
		Currency:        "USD",
		Quantity:        2,
	})
	if err != nil {
		t.Fatalf("CreateCheckoutSession() error = %v", err)
	}
	if want := (&CosmeticCheckoutSession{
		ID:        "cs_test_arena",
		URL:       "https://checkout.stripe.test/c/pay/cs_test_arena",
		ExpiresAt: time.Unix(1_900_000_000, 0).UTC(),
	}); !reflect.DeepEqual(got, want) {
		t.Fatalf("CreateCheckoutSession() = %#v, want %#v", got, want)
	}

	params := creator.params
	if params == nil {
		t.Fatal("Stripe checkout creator was not called")
	}
	if stripe.StringValue(params.Mode) != string(stripe.CheckoutSessionModePayment) {
		t.Fatalf("Mode = %q, want payment", stripe.StringValue(params.Mode))
	}
	if stripe.StringValue(params.CustomerEmail) != "owner@example.com" || stripe.StringValue(params.ClientReferenceID) != "order-123" {
		t.Fatalf("checkout identity fields = email %q reference %q", stripe.StringValue(params.CustomerEmail), stripe.StringValue(params.ClientReferenceID))
	}
	if stripe.StringValue(params.SuccessURL) != "https://arena.example/dashboard?checkout=success" ||
		stripe.StringValue(params.CancelURL) != "https://arena.example/shop?checkout=cancelled" {
		t.Fatalf("checkout return URLs = %q / %q", stripe.StringValue(params.SuccessURL), stripe.StringValue(params.CancelURL))
	}
	if params.AutomaticTax == nil || !stripe.BoolValue(params.AutomaticTax.Enabled) {
		t.Fatal("automatic tax flag was not forwarded")
	}
	if params.IdempotencyKey == nil || *params.IdempotencyKey != "cosmetics_checkout_order-123" {
		t.Fatalf("IdempotencyKey = %v, want order-derived key", params.IdempotencyKey)
	}

	wantMetadata := map[string]string{
		"order_id":   "order-123",
		"account_id": "account-456",
		"pack_id":    "pack-neon-founders",
		"pack_name":  "Neon Founders Set",
	}
	if !reflect.DeepEqual(params.Metadata, wantMetadata) {
		t.Fatalf("session metadata = %#v, want %#v", params.Metadata, wantMetadata)
	}
	if params.PaymentIntentData == nil || !reflect.DeepEqual(params.PaymentIntentData.Metadata, wantMetadata) {
		t.Fatalf("PaymentIntent metadata = %#v, want %#v", params.PaymentIntentData, wantMetadata)
	}
	if stripe.StringValue(params.PaymentIntentData.ReceiptEmail) != "owner@example.com" {
		t.Fatalf("PaymentIntent receipt email = %q", stripe.StringValue(params.PaymentIntentData.ReceiptEmail))
	}
	if len(params.LineItems) != 1 || params.LineItems[0].PriceData == nil || params.LineItems[0].PriceData.ProductData == nil {
		t.Fatalf("checkout line items = %#v, want one inline server-controlled price", params.LineItems)
	}
	line := params.LineItems[0]
	if stripe.Int64Value(line.Quantity) != 2 || stripe.Int64Value(line.PriceData.UnitAmount) != 1299 || stripe.StringValue(line.PriceData.Currency) != "usd" {
		t.Fatalf("checkout amount = quantity %d amount %d currency %q", stripe.Int64Value(line.Quantity), stripe.Int64Value(line.PriceData.UnitAmount), stripe.StringValue(line.PriceData.Currency))
	}
	if stripe.StringValue(line.PriceData.ProductData.Name) != "Neon Founders Set" ||
		stripe.StringValue(line.PriceData.ProductData.Description) != "A launch set of presentation-only cosmetics." {
		t.Fatalf("checkout product = %#v", line.PriceData.ProductData)
	}
}

func signedStripePayload(payload []byte, secret string, timestamp time.Time) string {
	return webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: timestamp,
	}).Header
}

func stripeEventPayload(t *testing.T, id string, eventType stripe.EventType, object map[string]interface{}) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{
		"id":          id,
		"object":      "event",
		"api_version": stripe.APIVersion,
		"created":     time.Now().Unix(),
		"type":        eventType,
		"data":        map[string]interface{}{"object": object},
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestStripeCosmeticPaymentProviderRejectsInvalidAndStaleSignatures(t *testing.T) {
	provider := newStripeCosmeticPaymentProviderWithCreator(nil, []string{"whsec_current"}, "", "", false)
	payload := stripeEventPayload(t, "evt_signature", stripe.EventTypeCheckoutSessionCompleted, map[string]interface{}{
		"id": "cs_signature",
	})

	wrongHeader := signedStripePayload(payload, "whsec_wrong", time.Now())
	if _, err := provider.ParseWebhook(payload, wrongHeader); err == nil {
		t.Fatal("ParseWebhook() accepted a signature from an unconfigured secret")
	}

	staleHeader := signedStripePayload(payload, "whsec_current", time.Now().Add(-webhook.DefaultTolerance-time.Second))
	if _, err := provider.ParseWebhook(payload, staleHeader); err == nil {
		t.Fatal("ParseWebhook() accepted a signature outside Stripe's default tolerance")
	}
}

func TestStripeCosmeticPaymentProviderRejectsSignedMismatchedAPIVersion(t *testing.T) {
	const secret = "whsec_version"
	provider := newStripeCosmeticPaymentProviderWithCreator(nil, []string{secret}, "", "", false)
	payload := stripeEventPayload(t, "evt_old_version", stripe.EventTypeCheckoutSessionCompleted, map[string]interface{}{
		"id": "cs_old_version",
	})
	var event map[string]interface{}
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	event["api_version"] = "2025-12-15.clover"
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	header := signedStripePayload(payload, secret, time.Now())

	if _, err := provider.ParseWebhook(payload, header); err == nil {
		t.Fatalf("ParseWebhook() accepted signed API version %v, want %s only", event["api_version"], stripe.APIVersion)
	}
}

func TestStripeCosmeticPaymentProviderAcceptsRotatedWebhookSecret(t *testing.T) {
	provider := newStripeCosmeticPaymentProviderWithCreator(nil, []string{"whsec_current", "whsec_previous"}, "", "", false)
	payload := stripeEventPayload(t, "evt_rotation", stripe.EventTypeCheckoutSessionCompleted, map[string]interface{}{
		"id":             "cs_rotation",
		"metadata":       map[string]string{"order_id": "order-rotation", "account_id": "account-rotation"},
		"payment_intent": "pi_rotation",
	})
	header := signedStripePayload(payload, "whsec_previous", time.Now())

	event, err := provider.ParseWebhook(payload, header)
	if err != nil {
		t.Fatalf("ParseWebhook() rejected the previous rotation secret: %v", err)
	}
	if event.ID != "evt_rotation" || event.OrderID != "order-rotation" || event.PaymentIntentID != "pi_rotation" {
		t.Fatalf("rotated webhook normalized as %#v", event)
	}
}

func TestStripeCosmeticPaymentProviderNormalizesSupportedEvents(t *testing.T) {
	checkoutObject := map[string]interface{}{
		"id":             "cs_checkout",
		"metadata":       map[string]string{"order_id": "order-checkout", "account_id": "account-checkout"},
		"payment_intent": map[string]string{"id": "pi_checkout"},
		"amount_total":   int64(1299),
		"currency":       "usd",
		"payment_status": "paid",
	}
	refundObject := map[string]interface{}{
		"id":             "re_refund",
		"metadata":       map[string]string{"order_id": "order-refund", "account_id": "account-refund"},
		"payment_intent": "pi_refund",
		"amount":         int64(700),
		"currency":       "usd",
		"status":         "succeeded",
	}
	chargeObject := map[string]interface{}{
		"id":              "ch_refunded",
		"metadata":        map[string]string{"order_id": "order-charge", "account_id": "account-charge"},
		"payment_intent":  map[string]string{"id": "pi_charge"},
		"amount":          int64(1299),
		"amount_refunded": int64(1299),
		"currency":        "usd",
		"status":          "succeeded",
	}
	disputeObject := map[string]interface{}{
		"id":             "dp_dispute",
		"metadata":       map[string]string{"order_id": "order-dispute", "account_id": "account-dispute"},
		"payment_intent": "pi_dispute",
		"amount":         int64(1299),
		"currency":       "usd",
		"status":         "needs_response",
	}

	tests := []struct {
		name       string
		stripeType stripe.EventType
		object     map[string]interface{}
		want       CosmeticPaymentEvent
	}{
		{name: "checkout completed", stripeType: stripe.EventTypeCheckoutSessionCompleted, object: checkoutObject, want: CosmeticPaymentEvent{Type: CosmeticPaymentEventCheckoutCompleted, OrderID: "order-checkout", AccountID: "account-checkout", CheckoutSessionID: "cs_checkout", PaymentIntentID: "pi_checkout", AmountTotal: 1299, Currency: "usd", PaymentStatus: "paid"}},
		{name: "checkout async success", stripeType: stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded, object: checkoutObject, want: CosmeticPaymentEvent{Type: CosmeticPaymentEventCheckoutAsyncSucceeded, OrderID: "order-checkout", AccountID: "account-checkout", CheckoutSessionID: "cs_checkout", PaymentIntentID: "pi_checkout", AmountTotal: 1299, Currency: "usd", PaymentStatus: "paid"}},
		{name: "checkout async failure", stripeType: stripe.EventTypeCheckoutSessionAsyncPaymentFailed, object: checkoutObject, want: CosmeticPaymentEvent{Type: CosmeticPaymentEventCheckoutAsyncFailed, OrderID: "order-checkout", AccountID: "account-checkout", CheckoutSessionID: "cs_checkout", PaymentIntentID: "pi_checkout", AmountTotal: 1299, Currency: "usd", PaymentStatus: "paid"}},
		{name: "checkout expired", stripeType: stripe.EventTypeCheckoutSessionExpired, object: checkoutObject, want: CosmeticPaymentEvent{Type: CosmeticPaymentEventCheckoutExpired, OrderID: "order-checkout", AccountID: "account-checkout", CheckoutSessionID: "cs_checkout", PaymentIntentID: "pi_checkout", AmountTotal: 1299, Currency: "usd", PaymentStatus: "paid"}},
		{name: "refund created", stripeType: stripe.EventTypeRefundCreated, object: refundObject, want: CosmeticPaymentEvent{Type: CosmeticPaymentEventRefundCreated, OrderID: "order-refund", AccountID: "account-refund", PaymentIntentID: "pi_refund", AmountRefunded: 700, Currency: "usd", RefundID: "re_refund", RefundStatus: "succeeded"}},
		{name: "refund updated", stripeType: stripe.EventTypeRefundUpdated, object: refundObject, want: CosmeticPaymentEvent{Type: CosmeticPaymentEventRefundUpdated, OrderID: "order-refund", AccountID: "account-refund", PaymentIntentID: "pi_refund", AmountRefunded: 700, Currency: "usd", RefundID: "re_refund", RefundStatus: "succeeded"}},
		{name: "refund failed", stripeType: stripe.EventTypeRefundFailed, object: refundObject, want: CosmeticPaymentEvent{Type: CosmeticPaymentEventRefundFailed, OrderID: "order-refund", AccountID: "account-refund", PaymentIntentID: "pi_refund", AmountRefunded: 700, Currency: "usd", RefundID: "re_refund", RefundStatus: "succeeded"}},
		{name: "charge refunded", stripeType: stripe.EventTypeChargeRefunded, object: chargeObject, want: CosmeticPaymentEvent{Type: CosmeticPaymentEventChargeRefunded, OrderID: "order-charge", AccountID: "account-charge", PaymentIntentID: "pi_charge", AmountTotal: 1299, AmountRefunded: 1299, Currency: "usd", PaymentStatus: "succeeded"}},
		{name: "dispute created", stripeType: stripe.EventTypeChargeDisputeCreated, object: disputeObject, want: CosmeticPaymentEvent{Type: CosmeticPaymentEventDisputeCreated, OrderID: "order-dispute", AccountID: "account-dispute", PaymentIntentID: "pi_dispute", AmountTotal: 1299, Currency: "usd", DisputeID: "dp_dispute", DisputeStatus: "needs_response"}},
	}

	provider := newStripeCosmeticPaymentProviderWithCreator(nil, []string{"whsec_current"}, "", "", false)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := stripeEventPayload(t, "evt_"+tt.name, tt.stripeType, tt.object)
			header := signedStripePayload(payload, "whsec_current", time.Now())
			got, err := provider.ParseWebhook(payload, header)
			if err != nil {
				t.Fatalf("ParseWebhook() error = %v", err)
			}
			sum := sha256.Sum256(payload)
			tt.want.ID = "evt_" + tt.name
			tt.want.PayloadSHA256 = hex.EncodeToString(sum[:])
			if !reflect.DeepEqual(*got, tt.want) {
				t.Fatalf("ParseWebhook() = %#v, want %#v", *got, tt.want)
			}
		})
	}
}
