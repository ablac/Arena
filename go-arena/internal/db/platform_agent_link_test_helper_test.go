package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LinkBotToCustomerAccount gives legacy integration fixtures direct
// access to the private link-state core. Production callers cannot bypass
// control-proof verification through this test-only helper.
func LinkBotToCustomerAccount(ctx context.Context, accountID, botID string) (*AccountBot, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("LinkBotToCustomerAccount begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := lockCustomerAccount(ctx, tx, accountID, true); err != nil {
		return nil, err
	}
	var bot AccountBot
	var apiKeyID string
	if err := tx.QueryRow(ctx, `
		SELECT b.id, b.name, b.avatar_color, b.default_weapon, b.api_key_id,
		       k.key_prefix, k.is_active, NOW()
		FROM bots b JOIN api_keys k ON k.id = b.api_key_id
		WHERE b.id = $1
		FOR UPDATE OF b, k`, botID).Scan(
		&bot.BotID, &bot.Name, &bot.AvatarColor, &bot.DefaultWeapon, &apiKeyID,
		&bot.KeyPrefix, &bot.KeyIsActive, &bot.LinkedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCosmeticBotNotFound
		}
		return nil, fmt.Errorf("LinkBotToCustomerAccount bot: %w", err)
	}
	if !bot.KeyIsActive {
		return nil, ErrCustomerBotKeyInactive
	}
	linked, _, err := linkBotToCustomerAccountTx(ctx, tx, accountID, bot, apiKeyID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("LinkBotToCustomerAccount commit: %w", err)
	}
	return linked, nil
}
