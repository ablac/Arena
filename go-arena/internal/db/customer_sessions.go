package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnsureCustomerSessionsSchema creates the durable customer session table.
// It runs unconditionally (unlike chat) because "stay signed in" applies to
// every customer-auth surface (dashboard, chat, cosmetics), not just chat,
// and depends on customer_accounts existing (EnsureCosmeticsSchema).
//
// Only a SHA-256 digest of the session token is stored, never the bearer
// token itself, mirroring how API keys are stored: a leaked database row
// cannot be replayed as a cookie.
func EnsureCustomerSessionsSchema(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS customer_sessions (
			token_hash BYTEA PRIMARY KEY,
			account_id TEXT NOT NULL REFERENCES customer_accounts(id) ON DELETE CASCADE,
			csrf_token TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			last_seen_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_sessions_account ON customer_sessions (account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_customer_sessions_expires ON customer_sessions (expires_at)`,
	}
	for _, stmt := range statements {
		if _, err := Pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("EnsureCustomerSessionsSchema exec: %w", err)
		}
	}
	return nil
}

// CustomerSessionRow is one durable session, joined with the identity fields
// a freshly-restored in-memory session needs. Reloading email/display_name
// from customer_accounts on every cache-miss (rather than duplicating them
// into this table) means a display-name change is picked up immediately
// rather than only at next login.
type CustomerSessionRow struct {
	AccountID       string
	Email           string
	DisplayName     string
	EmailVerifiedAt *time.Time
	CSRFToken       string
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

// InsertCustomerSession persists a newly established session. Best-effort
// from the caller's perspective (dev mode without a database keeps working
// with the in-memory-only session).
func InsertCustomerSession(ctx context.Context, tokenHash []byte, accountID, csrfToken string, createdAt, expiresAt time.Time) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO customer_sessions (token_hash, account_id, csrf_token, created_at, last_seen_at, expires_at)
		 VALUES ($1, $2, $3, $4, $4, $5)
		 ON CONFLICT (token_hash) DO UPDATE SET
			account_id = EXCLUDED.account_id, csrf_token = EXCLUDED.csrf_token,
			last_seen_at = EXCLUDED.last_seen_at, expires_at = EXCLUDED.expires_at`,
		tokenHash, accountID, csrfToken, createdAt, expiresAt)
	if err != nil {
		return fmt.Errorf("InsertCustomerSession: %w", err)
	}
	return nil
}

// GetCustomerSessionByTokenHash loads a session by its hashed token. Used
// only on an in-memory cache miss (process restart or a different replica),
// so the extra JOIN cost is paid rarely, not on every request.
func GetCustomerSessionByTokenHash(ctx context.Context, tokenHash []byte) (*CustomerSessionRow, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	var row CustomerSessionRow
	err := Pool.QueryRow(ctx,
		`SELECT s.account_id, a.email, a.display_name, a.email_verified_at,
		        s.csrf_token, s.created_at, s.expires_at
		 FROM customer_sessions s
		 JOIN customer_accounts a ON a.id = s.account_id
		 WHERE s.token_hash = $1`, tokenHash).
		Scan(&row.AccountID, &row.Email, &row.DisplayName, &row.EmailVerifiedAt,
			&row.CSRFToken, &row.CreatedAt, &row.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetCustomerSessionByTokenHash: %w", err)
	}
	return &row, nil
}

// TouchCustomerSession extends a session's expiry (sliding renewal) and
// updates last_seen_at. Best effort: a failure just means the DB copy falls
// behind the in-memory one until the next successful touch.
func TouchCustomerSession(ctx context.Context, tokenHash []byte, lastSeenAt, expiresAt time.Time) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`UPDATE customer_sessions SET last_seen_at = $2, expires_at = $3 WHERE token_hash = $1`,
		tokenHash, lastSeenAt, expiresAt)
	if err != nil {
		return fmt.Errorf("TouchCustomerSession: %w", err)
	}
	return nil
}

// DeleteCustomerSession removes one session row (logout).
func DeleteCustomerSession(ctx context.Context, tokenHash []byte) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx, `DELETE FROM customer_sessions WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("DeleteCustomerSession: %w", err)
	}
	return nil
}

// DeleteExpiredCustomerSessions purges rows past their expiry so the table
// stays bounded. Called from the handler's periodic cleanup loop.
func DeleteExpiredCustomerSessions(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx, `DELETE FROM customer_sessions WHERE expires_at < NOW()`)
	if err != nil {
		return fmt.Errorf("DeleteExpiredCustomerSessions: %w", err)
	}
	return nil
}
