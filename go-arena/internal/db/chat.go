package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"arena-server/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// EnsureChatSchema creates the developer lobby chat table and adds the
// chat-scoped ban column to customer_accounts. It runs after
// EnsureCosmeticsSchema (which creates customer_accounts) and takes its own
// advisory lock because multiple arena-server replicas can race startup DDL.
//
// It always runs, regardless of ARENA_CHAT_ENABLED: that env var only seeds
// the initial value of the chat_runtime_settings row the first time this
// schema is created (ON CONFLICT DO NOTHING leaves an admin's later choice
// alone across restarts). The schema existing unconditionally is what lets
// an operator turn chat on entirely from the admin panel, with no env var
// edit or restart required.
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
		// Blocked-keyword list: admin-managed, checked against every outgoing
		// message body before it is persisted or broadcast.
		`CREATE TABLE IF NOT EXISTS chat_blocked_keywords (
			id BIGSERIAL PRIMARY KEY,
			keyword TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		// Audit trail of every ban/unban action, independent of the current
		// chat_banned_until value (which only holds the latest state).
		`CREATE TABLE IF NOT EXISTS chat_ban_log (
			id BIGSERIAL PRIMARY KEY,
			account_id TEXT NOT NULL,
			minutes INT NOT NULL,
			until TIMESTAMPTZ,
			reason TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_ban_log_account ON chat_ban_log (account_id, created_at DESC)`,
		// Singleton row holding the live admin on/off switch for chat. This is
		// the single source of truth ChatHandler and the rest of the chat
		// package check; see the seed INSERT below for how it starts out.
		`CREATE TABLE IF NOT EXISTS chat_runtime_settings (
			id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("EnsureChatSchema exec: %w", err)
		}
	}
	// Separate from the statements loop because it takes a parameter: seeds
	// the row from ARENA_CHAT_ENABLED only the first time this schema is
	// created (ON CONFLICT DO NOTHING), so an admin's later toggle in the
	// panel is never silently reverted by a restart.
	if _, err := tx.Exec(ctx,
		`INSERT INTO chat_runtime_settings (id, enabled) VALUES (TRUE, $1) ON CONFLICT (id) DO NOTHING`,
		config.C.ChatEnabled,
	); err != nil {
		return fmt.Errorf("EnsureChatSchema seed runtime settings: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("EnsureChatSchema commit: %w", err)
	}
	return nil
}

// InsertChatMessage persists a lobby chat message, fills in the assigned id
// and created_at on the passed struct, and prunes the table back down to
// config.C.ChatHistorySize rows so it never grows unbounded: the lobby is a
// live, ephemeral surface, not a permanent record.
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
	if err := pruneChatMessages(ctx, config.C.ChatHistorySize); err != nil {
		// The message itself is already durably saved; a failed prune just
		// means the table is a little larger than intended until the next
		// successful insert retries it.
		slog.Warn("chat message prune failed", "error", err)
	}
	return nil
}

// pruneChatMessages deletes every chat_messages row older than the most
// recent keep rows (by id, which is monotonic with insertion order). A
// non-positive keep is treated as "no limit" (nothing pruned) rather than
// wiping the table.
func pruneChatMessages(ctx context.Context, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := Pool.Exec(ctx,
		`DELETE FROM chat_messages
		 WHERE id < (
			SELECT COALESCE(MIN(id), 0) FROM (
				SELECT id FROM chat_messages ORDER BY id DESC LIMIT $1
			) recent
		 )`, keep)
	if err != nil {
		return fmt.Errorf("pruneChatMessages: %w", err)
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

// InsertChatBanLogEntry appends one audit row for a ban/unban action. Best
// effort from the caller's perspective: a failure here should not block the
// underlying ban from applying.
func InsertChatBanLogEntry(ctx context.Context, accountID string, minutes int, until *time.Time, reason string) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO chat_ban_log (account_id, minutes, until, reason) VALUES ($1, $2, $3, $4)`,
		accountID, minutes, until, reason)
	if err != nil {
		return fmt.Errorf("InsertChatBanLogEntry: %w", err)
	}
	return nil
}

// ChatBanLogEntry is one row of ban/unban history.
type ChatBanLogEntry struct {
	ID        int64      `json:"id"`
	AccountID string     `json:"account_id"`
	Minutes   int        `json:"minutes"`
	Until     *time.Time `json:"until,omitempty"`
	Reason    string     `json:"reason"`
	CreatedAt time.Time  `json:"created_at"`
}

// ListChatBanLog returns the most recent ban/unban actions, newest first.
func ListChatBanLog(ctx context.Context, limit int) ([]ChatBanLogEntry, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := Pool.Query(ctx,
		`SELECT id, account_id, minutes, until, reason, created_at
		 FROM chat_ban_log ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("ListChatBanLog: %w", err)
	}
	defer rows.Close()

	var out []ChatBanLogEntry
	for rows.Next() {
		var e ChatBanLogEntry
		if err := rows.Scan(&e.ID, &e.AccountID, &e.Minutes, &e.Until, &e.Reason, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("ListChatBanLog scan: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ActiveChatBan is one currently-banned account, joined with its account
// identity for display in the admin panel.
type ActiveChatBan struct {
	AccountID   string    `json:"account_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	BannedUntil time.Time `json:"banned_until"`
}

// ListActiveChatBans returns every customer_account with a chat ban that has
// not yet expired.
func ListActiveChatBans(ctx context.Context) ([]ActiveChatBan, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx,
		`SELECT id, email, display_name, chat_banned_until
		 FROM customer_accounts
		 WHERE chat_banned_until IS NOT NULL AND chat_banned_until > NOW()
		 ORDER BY chat_banned_until DESC`)
	if err != nil {
		return nil, fmt.Errorf("ListActiveChatBans: %w", err)
	}
	defer rows.Close()

	var out []ActiveChatBan
	for rows.Next() {
		var b ActiveChatBan
		if err := rows.Scan(&b.AccountID, &b.Email, &b.DisplayName, &b.BannedUntil); err != nil {
			return nil, fmt.Errorf("ListActiveChatBans scan: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListChatMessagesForAdmin returns the most recent chat messages including
// hidden ones and the poster IP, for the admin moderation view. Unlike
// ListRecentChatMessages this is not filtered to non-hidden rows.
func ListChatMessagesForAdmin(ctx context.Context, limit int) ([]ChatMessage, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := Pool.Query(ctx,
		`SELECT id, account_id, handle, body, ip, created_at, hidden
		 FROM chat_messages ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("ListChatMessagesForAdmin: %w", err)
	}
	defer rows.Close()

	var out []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.AccountID, &m.Handle, &m.Body, &m.IP, &m.CreatedAt, &m.Hidden); err != nil {
			return nil, fmt.Errorf("ListChatMessagesForAdmin scan: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UnhideChatMessage clears the soft-delete flag on a chat message. The bool
// reports whether the id existed.
func UnhideChatMessage(ctx context.Context, id int64) (bool, error) {
	if Pool == nil {
		return false, ErrNoDatabase
	}
	tag, err := Pool.Exec(ctx, `UPDATE chat_messages SET hidden = false WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("UnhideChatMessage: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ChatBlockedKeyword is one row of the admin-managed blocklist.
type ChatBlockedKeyword struct {
	ID        int64     `json:"id"`
	Keyword   string    `json:"keyword"`
	CreatedAt time.Time `json:"created_at"`
}

// ListChatBlockedKeywords returns every blocked keyword, oldest first.
func ListChatBlockedKeywords(ctx context.Context) ([]ChatBlockedKeyword, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx,
		`SELECT id, keyword, created_at FROM chat_blocked_keywords ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("ListChatBlockedKeywords: %w", err)
	}
	defer rows.Close()

	var out []ChatBlockedKeyword
	for rows.Next() {
		var k ChatBlockedKeyword
		if err := rows.Scan(&k.ID, &k.Keyword, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("ListChatBlockedKeywords scan: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ErrChatKeywordExists is returned when a keyword is already blocked.
var ErrChatKeywordExists = errors.New("keyword is already blocked")

// InsertChatBlockedKeyword adds a keyword to the blocklist (case-insensitive
// by convention; callers normalize to lowercase before calling).
func InsertChatBlockedKeyword(ctx context.Context, keyword string) (*ChatBlockedKeyword, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	var k ChatBlockedKeyword
	err := Pool.QueryRow(ctx,
		`INSERT INTO chat_blocked_keywords (keyword) VALUES ($1) RETURNING id, keyword, created_at`,
		keyword).Scan(&k.ID, &k.Keyword, &k.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrChatKeywordExists
		}
		return nil, fmt.Errorf("InsertChatBlockedKeyword: %w", err)
	}
	return &k, nil
}

// DeleteChatBlockedKeyword removes a keyword from the blocklist. The bool
// reports whether the id existed.
func DeleteChatBlockedKeyword(ctx context.Context, id int64) (bool, error) {
	if Pool == nil {
		return false, ErrNoDatabase
	}
	tag, err := Pool.Exec(ctx, `DELETE FROM chat_blocked_keywords WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("DeleteChatBlockedKeyword: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// GetChatRuntimeEnabled reads the admin-toggled live kill switch. A missing
// row (should not normally happen once EnsureChatSchema has run) is treated
// as enabled, matching the table's DEFAULT TRUE.
func GetChatRuntimeEnabled(ctx context.Context) (bool, error) {
	if Pool == nil {
		return false, ErrNoDatabase
	}
	var enabled bool
	err := Pool.QueryRow(ctx, `SELECT enabled FROM chat_runtime_settings WHERE id = TRUE`).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("GetChatRuntimeEnabled: %w", err)
	}
	return enabled, nil
}

// SetChatRuntimeEnabled persists the admin-toggled live kill switch.
func SetChatRuntimeEnabled(ctx context.Context, enabled bool) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO chat_runtime_settings (id, enabled, updated_at) VALUES (TRUE, $1, NOW())
		 ON CONFLICT (id) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`,
		enabled)
	if err != nil {
		return fmt.Errorf("SetChatRuntimeEnabled: %w", err)
	}
	return nil
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
