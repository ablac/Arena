package db

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func TestPostgresCustomerEmailVerificationIsSingleUseAndClaimsPendingOwner(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	pending, err := GetOrCreateCustomerAccountByEmail(ctx, "pilot@example.com")
	if err != nil {
		t.Fatalf("pending account: %v", err)
	}
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte("raw-token-never-stored"))
	if err := CreateCustomerEmailVerification(ctx, " Pilot@Example.COM ", "Pilot One", "/dashboard/?tab=cosmetics", digest[:], now, 15*time.Minute, time.Minute); err != nil {
		t.Fatalf("create verification: %v", err)
	}
	account, returnTo, err := ConsumeCustomerEmailVerification(ctx, digest[:], now.Add(time.Second))
	if err != nil {
		t.Fatalf("consume verification: %v", err)
	}
	if account.ID != pending.ID || account.Email != "pilot@example.com" || account.DisplayName != "Pilot One" || account.EmailVerifiedAt == nil {
		t.Fatalf("verified account = %+v, pending=%+v", account, pending)
	}
	if returnTo != "/dashboard/?tab=cosmetics" {
		t.Fatalf("return_to = %q", returnTo)
	}
	if _, _, err := ConsumeCustomerEmailVerification(ctx, digest[:], now.Add(2*time.Second)); !errors.Is(err, ErrCustomerEmailVerificationInvalid) {
		t.Fatalf("second consume error = %v", err)
	}
}

func TestPostgresCustomerEmailVerificationEnforcesCooldownExpiryAndReplacement(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	now := time.Now().UTC()
	first := sha256.Sum256([]byte("first-token"))
	second := sha256.Sum256([]byte("second-token"))
	if err := CreateCustomerEmailVerification(ctx, "pilot@example.com", "", "/dashboard/", first[:], now, time.Minute, time.Minute); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := CreateCustomerEmailVerification(ctx, "pilot@example.com", "", "/dashboard/", second[:], now.Add(30*time.Second), time.Minute, time.Minute); !errors.Is(err, ErrCustomerEmailVerificationRateLimited) {
		t.Fatalf("cooldown error = %v", err)
	}
	if err := CreateCustomerEmailVerification(ctx, "pilot@example.com", "", "/dashboard/", second[:], now.Add(61*time.Second), time.Minute, time.Minute); err != nil {
		t.Fatalf("replace after cooldown: %v", err)
	}
	if _, _, err := ConsumeCustomerEmailVerification(ctx, first[:], now.Add(62*time.Second)); !errors.Is(err, ErrCustomerEmailVerificationInvalid) {
		t.Fatalf("superseded token error = %v", err)
	}
	if _, _, err := ConsumeCustomerEmailVerification(ctx, second[:], now.Add(3*time.Minute)); !errors.Is(err, ErrCustomerEmailVerificationInvalid) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestPostgresCustomerEmailVerificationCanSignIntoOIDCBoundEmail(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	existing, err := UpsertVerifiedCustomerAccount(ctx, "pilot@example.com", "https://id.example", "pilot-subject", "OIDC Name")
	if err != nil {
		t.Fatalf("create OIDC account: %v", err)
	}
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte("email-token"))
	if err := CreateCustomerEmailVerification(ctx, "pilot@example.com", "Magic Link Name", "/dashboard/", digest[:], now, 15*time.Minute, time.Minute); err != nil {
		t.Fatalf("create verification: %v", err)
	}
	verified, _, err := ConsumeCustomerEmailVerification(ctx, digest[:], now.Add(time.Second))
	if err != nil {
		t.Fatalf("consume verification: %v", err)
	}
	if verified.ID != existing.ID || verified.DisplayName != "Magic Link Name" {
		t.Fatalf("email sign-in changed durable owner: existing=%+v verified=%+v", existing, verified)
	}
	var issuer, subject *string
	if err := Pool.QueryRow(context.Background(), `SELECT oidc_issuer, oidc_subject FROM customer_accounts WHERE id = $1`, existing.ID).Scan(&issuer, &subject); err != nil {
		t.Fatal(err)
	}
	if issuer == nil || subject == nil || *issuer != "https://id.example" || *subject != "pilot-subject" {
		t.Fatalf("email sign-in changed OIDC binding: issuer=%v subject=%v", issuer, subject)
	}
}

func TestPostgresCustomerEmailVerificationConcurrentConsumeAdmitsExactlyOne(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	now := time.Now().UTC()
	digest := sha256.Sum256([]byte("one-time-concurrent-token"))
	if err := CreateCustomerEmailVerification(ctx, "race@example.com", "Race", "/dashboard/", digest[:], now, 15*time.Minute, time.Minute); err != nil {
		t.Fatalf("create verification: %v", err)
	}
	type result struct {
		account *CustomerAccount
		err     error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() {
			account, _, err := ConsumeCustomerEmailVerification(context.Background(), digest[:], now.Add(time.Second))
			results <- result{account: account, err: err}
		}()
	}
	successes := 0
	invalid := 0
	for range 8 {
		outcome := <-results
		switch {
		case outcome.err == nil && outcome.account != nil:
			successes++
		case errors.Is(outcome.err, ErrCustomerEmailVerificationInvalid):
			invalid++
		default:
			t.Fatalf("unexpected consume outcome: account=%+v err=%v", outcome.account, outcome.err)
		}
	}
	if successes != 1 || invalid != 7 {
		t.Fatalf("concurrent consumes: success=%d invalid=%d", successes, invalid)
	}
}
