package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnsureChatSchema creates the developer lobby chat table and adds the
// chat-scoped ban column to customer_accounts. It runs after
// EnsureCosmeticsSchema (which creates customer_accounts) and takes its own
// advisory lock because multiple arena-server replicas can race startup DDL.
func EnsureChatSchema(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("EnsureChatSchema begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(2026071102::BIGINT)`); err != nil {
		return fmt.Errorf("EnsureChatSchema advisory lock: %w", err)
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id BIGSERIAL PRIMARY KEY,
			account_id TEXT REFERENCES customer_accounts(id) ON DELETE SET NULL,
			handle TEXT NOT NULL,
			body TEXT NOT NULL,
			ip TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			hidden BOOLEAN NOT NULL DEFAULT false
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_recent
			ON chat_messages (id DESC) WHERE hidden = false`,
		// Chat-scoped mute: lets an admin silence an account in chat without
		// banning it from the game (the IP ban is the only other lever and it
		// blocks everything).
		`ALTER TABLE customer_accounts ADD COLUMN IF NOT EXISTS chat_banned_until TIMESTAMPTZ`,
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("EnsureChatSchema exec: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("EnsureChatSchema commit: %w", err)
	}
	return nil
}

// InsertChatMessage persists a lobby chat message and fills in the assigned
// id and created_at on the passed struct.
func InsertChatMessage(ctx context.Context, m *ChatMessage) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	err := Pool.QueryRow(ctx,
		`INSERT INTO chat_messages (account_id, handle, body, ip)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		m.AccountID, m.Handle, m.Body, m.IP,
	).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return fmt.Errorf("InsertChatMessage: %w", err)
	}
	return nil
}

// ListRecentChatMessages returns the most recent non-hidden chat messages in
// chronological (oldest first) order, ready for history backfill.
func ListRecentChatMessages(ctx context.Context, limit int) ([]ChatMessage, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if limit <= 0 {
		return nil, nil
	}
	rows, err := Pool.Query(ctx,
		`SELECT id, account_id, handle, body, created_at
		 FROM chat_messages
		 WHERE hidden = false
		 ORDER BY id DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("ListRecentChatMessages: %w", err)
	}
	defer rows.Close()

	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.AccountID, &m.Handle, &m.Body, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("ListRecentChatMessages scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListRecentChatMessages rows: %w", err)
	}
	// Query is newest-first for the LIMIT; reverse to oldest-first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// HideChatMessage soft-deletes one chat message. The bool reports whether
// the id existed, so a bad id is not a silent success.
func HideChatMessage(ctx context.Context, id int64) (bool, error) {
	if Pool == nil {
		return false, ErrNoDatabase
	}
	tag, err := Pool.Exec(ctx,
		`UPDATE chat_messages SET hidden = true WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("HideChatMessage: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// SetCustomerChatBan sets (or clears, with nil) the chat-scoped ban on a
// customer account. The bool reports whether the account existed.
func SetCustomerChatBan(ctx context.Context, accountID string, until *time.Time) (bool, error) {
	if Pool == nil {
		return false, ErrNoDatabase
	}
	tag, err := Pool.Exec(ctx,
		`UPDATE customer_accounts SET chat_banned_until = $2, updated_at = NOW() WHERE id = $1`,
		accountID, until)
	if err != nil {
		return false, fmt.Errorf("SetCustomerChatBan: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// GetCustomerChatBan returns the chat ban expiry for an account, nil when
// the account has no active ban row value. A missing account returns nil
// (no ban) rather than an error, matching the lookup conventions here.
func GetCustomerChatBan(ctx context.Context, accountID string) (*time.Time, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	var until *time.Time
	err := Pool.QueryRow(ctx,
		`SELECT chat_banned_until FROM customer_accounts WHERE id = $1`,
		accountID).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetCustomerChatBan: %w", err)
	}
	return until, nil
}

// ListLinkedBotIDs returns the ids of all bots linked to a customer account.
// Used by the chat alive-lock to answer "does this poster have a bot in the
// current round".
func ListLinkedBotIDs(ctx context.Context, accountID string) ([]string, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx,
		`SELECT bot_id FROM account_bot_links WHERE account_id = $1`, accountID)
	if err != nil {
		return nil, fmt.Errorf("ListLinkedBotIDs: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("ListLinkedBotIDs scan: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListLinkedBotIDs rows: %w", err)
	}
	return out, nil
}
