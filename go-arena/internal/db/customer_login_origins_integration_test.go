package db

import (
	"crypto/sha256"
	"testing"
	"time"
)

func TestPostgresCustomerLoginBoundCallback(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCustomerLoginTransactionsSchema(ctx); err != nil {
		t.Fatal(err)
	}
	// Mimic the old binary's existing table before the additive migration.
	if _, err := Pool.Exec(ctx, `ALTER TABLE customer_login_transactions DROP COLUMN redirect_uri`); err != nil {
		t.Fatal(err)
	}
	oldState := sha256.Sum256([]byte("old-state"))
	binding := sha256.Sum256([]byte("binding"))
	if _, err := Pool.Exec(ctx, `INSERT INTO customer_login_transactions VALUES($1,$2,'nonce','verifier','/dashboard/',false,NOW()+INTERVAL '10 minutes')`, oldState[:], binding[:]); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCustomerLoginTransactionsSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCustomerLoginTransactionsSchema(ctx); err != nil {
		t.Fatal(err)
	}
	legacy, found, err := ConsumeCustomerLoginTransaction(ctx, oldState[:])
	if err != nil || !found || legacy.RedirectURI != "" {
		t.Fatalf("legacy: %v %v %+v", found, err, legacy)
	}
	for _, uri := range []string{"https://arena.angel-serv.com/account/callback", "https://arena.angel-gaming.com/account/callback"} {
		state := sha256.Sum256([]byte(uri))
		if err := InsertCustomerLoginTransaction(ctx, state[:], binding[:], "nonce", "verifier", "/dashboard/", false, time.Now().Add(time.Minute), uri); err != nil {
			t.Fatal(err)
		}
		// Consume independently from persisted state, with no in-memory handler.
		got, found, err := ConsumeCustomerLoginTransaction(ctx, state[:])
		if err != nil || !found || got.RedirectURI != uri {
			t.Fatalf("bound callback: %v %v %+v", found, err, got)
		}
		if _, found, err := ConsumeCustomerLoginTransaction(ctx, state[:]); err != nil || found {
			t.Fatal("replay accepted", err)
		}
	}
}
