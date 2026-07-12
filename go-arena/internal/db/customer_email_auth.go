package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrCustomerEmailVerificationInvalid     = errors.New("invalid or expired customer email verification")
	ErrCustomerEmailVerificationRateLimited = errors.New("customer email verification requested too recently")
)

func validCustomerEmailReturnTo(value string) bool {
	if value == "" || len(value) > 2048 || strings.Contains(value, "\\") || strings.HasPrefix(value, "//") {
		return false
	}
	path := value
	if index := strings.IndexAny(path, "?#"); index >= 0 {
		path = path[:index]
	}
	return path == "/dashboard" || strings.HasPrefix(path, "/dashboard/") ||
		path == "/arena/dashboard" || strings.HasPrefix(path, "/arena/dashboard/")
}

func normalizeCustomerDisplayName(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if len(value) > 200 || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00") {
		return "", ErrCustomerEmailVerificationInvalid
	}
	return value, nil
}

// CreateCustomerEmailVerification replaces an older one-time claim only after
// the per-email cooldown. The raw bearer token never crosses this boundary.
func CreateCustomerEmailVerification(
	ctx context.Context,
	rawEmail, rawDisplayName, returnTo string,
	tokenHash []byte,
	createdAt time.Time,
	ttl, cooldown time.Duration,
) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	email, err := NormalizeCustomerEmail(rawEmail)
	if err != nil {
		return err
	}
	displayName, err := normalizeCustomerDisplayName(rawDisplayName)
	if err != nil || !validCustomerEmailReturnTo(returnTo) || len(tokenHash) != 32 ||
		ttl < time.Minute || ttl > time.Hour || cooldown < time.Second || cooldown > time.Hour {
		return ErrCustomerEmailVerificationInvalid
	}
	createdAt = createdAt.UTC()

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("CreateCustomerEmailVerification begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM customer_email_verifications WHERE expires_at <= $1`, createdAt); err != nil {
		return fmt.Errorf("CreateCustomerEmailVerification cleanup: %w", err)
	}
	var storedEmail string
	err = tx.QueryRow(ctx, `
		INSERT INTO customer_email_verifications
			(email, display_name, return_to, token_hash, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (email) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			return_to = EXCLUDED.return_to,
			token_hash = EXCLUDED.token_hash,
			created_at = EXCLUDED.created_at,
			expires_at = EXCLUDED.expires_at
		WHERE customer_email_verifications.created_at <= $7
		RETURNING email`,
		email, displayName, returnTo, append([]byte(nil), tokenHash...), createdAt, createdAt.Add(ttl), createdAt.Add(-cooldown),
	).Scan(&storedEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCustomerEmailVerificationRateLimited
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrCustomerEmailVerificationInvalid
		}
		return fmt.Errorf("CreateCustomerEmailVerification store: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("CreateCustomerEmailVerification commit: %w", err)
	}
	return nil
}

// ConsumeCustomerEmailVerification atomically deletes the single-use claim and
// marks the email's durable ownership account verified. An existing OIDC
// binding is intentionally preserved so native email auth can coexist safely.
func ConsumeCustomerEmailVerification(ctx context.Context, tokenHash []byte, now time.Time) (*CustomerAccount, string, error) {
	if Pool == nil {
		return nil, "", ErrNoDatabase
	}
	if len(tokenHash) != 32 {
		return nil, "", ErrCustomerEmailVerificationInvalid
	}
	now = now.UTC()
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("ConsumeCustomerEmailVerification begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var email, displayName, returnTo string
	err = tx.QueryRow(ctx, `
		SELECT email, display_name, return_to
		FROM customer_email_verifications
		WHERE token_hash = $1 AND expires_at > $2
		FOR UPDATE`, append([]byte(nil), tokenHash...), now,
	).Scan(&email, &displayName, &returnTo)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrCustomerEmailVerificationInvalid
	}
	if err != nil {
		return nil, "", fmt.Errorf("ConsumeCustomerEmailVerification claim: %w", err)
	}

	accountID := uuid.NewString()
	account, err := scanCustomerAccount(tx.QueryRow(ctx, `
		INSERT INTO customer_accounts
			(id, email, display_name, email_verified_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4, $4)
		ON CONFLICT (email) DO UPDATE SET
			display_name = CASE
				WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name
				ELSE customer_accounts.display_name
			END,
			email_verified_at = $4,
			updated_at = $4
		RETURNING id, email, display_name, email_verified_at, created_at, updated_at`,
		accountID, email, displayName, now,
	))
	if err != nil {
		return nil, "", fmt.Errorf("ConsumeCustomerEmailVerification verify account: %w", err)
	}
	result, err := tx.Exec(ctx, `DELETE FROM customer_email_verifications WHERE email = $1 AND token_hash = $2`, email, append([]byte(nil), tokenHash...))
	if err != nil {
		return nil, "", fmt.Errorf("ConsumeCustomerEmailVerification delete: %w", err)
	}
	if result.RowsAffected() != 1 {
		return nil, "", ErrCustomerEmailVerificationInvalid
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", fmt.Errorf("ConsumeCustomerEmailVerification commit: %w", err)
	}
	return account, returnTo, nil
}

func DeleteCustomerEmailVerification(ctx context.Context, tokenHash []byte) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	if len(tokenHash) != 32 {
		return ErrCustomerEmailVerificationInvalid
	}
	if _, err := Pool.Exec(ctx, `DELETE FROM customer_email_verifications WHERE token_hash = $1`, append([]byte(nil), tokenHash...)); err != nil {
		return fmt.Errorf("DeleteCustomerEmailVerification: %w", err)
	}
	return nil
}
