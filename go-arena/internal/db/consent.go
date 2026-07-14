package db

import (
	"context"
	"fmt"
)

// EnsureConsentSchema creates the audit table backing the TOS/Privacy
// acceptance beacon. The actual enforcement (blocking sign-in or key
// generation until accepted) lives client-side; this table exists purely so
// there is a durable record of who agreed to which version and when.
func EnsureConsentSchema(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS consent_acceptances (
			id BIGSERIAL PRIMARY KEY,
			account_id TEXT,
			version TEXT NOT NULL,
			ip TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		return fmt.Errorf("EnsureConsentSchema: %w", err)
	}
	return nil
}

// InsertConsentAcceptance records one acceptance event. accountID is empty
// for a visitor who accepted before signing in (e.g. right before generating
// their first API key).
func InsertConsentAcceptance(ctx context.Context, accountID, version, ip string) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	var acct interface{}
	if accountID != "" {
		acct = accountID
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO consent_acceptances (account_id, version, ip) VALUES ($1, $2, $3)`,
		acct, version, ip)
	if err != nil {
		return fmt.Errorf("InsertConsentAcceptance: %w", err)
	}
	return nil
}
