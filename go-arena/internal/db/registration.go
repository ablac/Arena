package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const insertAPIKeySQL = `INSERT INTO api_keys (id, key_hash, key_prefix, created_at, is_active, ip_created)
 VALUES ($1, $2, $3, NOW(), true, $4)`

const insertBotSQL = `INSERT INTO bots (id, api_key_id, name, avatar_color, default_weapon, default_stats,
                    default_fallback, created_at, updated_at)
 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

type beginRegistrationTx func(context.Context) (pgx.Tx, error)

// CreateAPIKeyAndBot inserts a new credential and its required bot row in one
// transaction. Neither row is visible if either insert or the commit fails.
func CreateAPIKeyAndBot(ctx context.Context, id, keyHash, keyPrefix, ipCreated string, bot *Bot) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	return createAPIKeyAndBotWithBegin(ctx, id, keyHash, keyPrefix, ipCreated, bot, Pool.Begin)
}

func createAPIKeyAndBotWithBegin(
	ctx context.Context,
	id, keyHash, keyPrefix, ipCreated string,
	bot *Bot,
	begin beginRegistrationTx,
) error {
	if bot == nil {
		return errors.New("CreateAPIKeyAndBot: bot is required")
	}

	tx, err := begin(ctx)
	if err != nil {
		return fmt.Errorf("CreateAPIKeyAndBot begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, insertAPIKeySQL, id, keyHash, keyPrefix, ipCreated); err != nil {
		return fmt.Errorf("CreateAPIKeyAndBot key insert: %w", err)
	}
	if _, err := tx.Exec(ctx, insertBotSQL,
		bot.ID, bot.APIKeyID, bot.Name, bot.AvatarColor, bot.DefaultWeapon, bot.DefaultStats,
		bot.DefaultFallback, bot.CreatedAt, bot.UpdatedAt,
	); err != nil {
		return fmt.Errorf("CreateAPIKeyAndBot bot insert: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("CreateAPIKeyAndBot commit: %w", err)
	}
	return nil
}

const consumeRegistrationRateLimitSQL = `INSERT INTO rate_limits (ip_address, keys_generated, window_start)
 VALUES ($1, 1, $2)
 ON CONFLICT (ip_address) DO UPDATE SET
   keys_generated = CASE
     WHEN rate_limits.window_start < EXCLUDED.window_start - INTERVAL '1 hour' THEN 1
     ELSE rate_limits.keys_generated + 1
   END,
   window_start = CASE
     WHEN rate_limits.window_start < EXCLUDED.window_start - INTERVAL '1 hour' THEN EXCLUDED.window_start
     ELSE rate_limits.window_start
   END
 WHERE rate_limits.window_start < EXCLUDED.window_start - INTERVAL '1 hour'
    OR rate_limits.keys_generated < $3
 RETURNING keys_generated`

type registrationRateLimitQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// consumeRegistrationRateLimit performs admission and counter mutation in one
// PostgreSQL statement. ON CONFLICT serializes contenders for the same IP, and
// the guarded update returns no row once the current window is at capacity.
func consumeRegistrationRateLimit(
	ctx context.Context,
	querier registrationRateLimitQuerier,
	ip string,
	maxPerHour int,
	now time.Time,
) (bool, int, error) {
	if maxPerHour <= 0 {
		return false, 0, nil
	}

	var count int
	err := querier.QueryRow(ctx, consumeRegistrationRateLimitSQL, ip, now, maxPerHour).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("CheckRateLimit consume: %w", err)
	}

	remaining := maxPerHour - count
	if remaining < 0 {
		remaining = 0
	}
	return true, remaining, nil
}
