package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	MaxActiveAccountAPIKeys = 5
	MaxAccountAPIKeyHistory = 100
	AccountAPIKeyListLimit  = 100
)

type AccountAPIKeyQuotaAction string

const (
	AccountAPIKeyQuotaCreate AccountAPIKeyQuotaAction = "create"
	AccountAPIKeyQuotaRevoke AccountAPIKeyQuotaAction = "revoke"
	AccountAPIKeyQuotaLink   AccountAPIKeyQuotaAction = "link"
)

var (
	ErrCustomerAPIKeyLimit        = errors.New("account already has the maximum number of active API keys")
	ErrCustomerAPIKeyHistoryLimit = errors.New("account API key history limit reached; contact Arena support to review archived credentials")
	ErrCustomerAPIKeyAlreadyOwned = errors.New("API key is already owned by another account")
	ErrCustomerAPIKeyNotOwned     = errors.New("API key is not owned by this account")
)

// AccountAPIKey is the account-safe view of one credential. Plaintext and all
// stored verification material are intentionally absent.
type AccountAPIKey struct {
	ID         string     `json:"id"`
	KeyPrefix  string     `json:"key_prefix"`
	BotID      string     `json:"bot_id"`
	BotName    string     `json:"bot_name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	IsActive   bool       `json:"is_active"`
}

const accountAPIKeyColumns = `
	SELECT k.id, k.key_prefix, b.id, b.name, k.created_at, k.last_seen, k.is_active
	FROM account_api_keys owned
	JOIN api_keys k ON k.id = owned.api_key_id
	JOIN bots b ON b.api_key_id = k.id`

func scanAccountAPIKey(row pgx.Row) (*AccountAPIKey, error) {
	key := &AccountAPIKey{}
	if err := row.Scan(&key.ID, &key.KeyPrefix, &key.BotID, &key.BotName,
		&key.CreatedAt, &key.LastUsedAt, &key.IsActive); err != nil {
		return nil, err
	}
	return key, nil
}

func countActiveAccountAPIKeys(ctx context.Context, tx pgx.Tx, accountID string) (int, error) {
	activeCount, _, err := accountAPIKeyCapacity(ctx, tx, accountID)
	return activeCount, err
}

type accountAPIKeyCapacityQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func accountAPIKeyCapacity(ctx context.Context, querier accountAPIKeyCapacityQuerier, accountID string) (int, int, error) {
	var activeCount, totalCount int
	if err := querier.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE k.is_active = true), COUNT(*)
		FROM account_api_keys owned
		JOIN api_keys k ON k.id = owned.api_key_id
		WHERE owned.account_id = $1`, accountID).Scan(&activeCount, &totalCount); err != nil {
		return 0, 0, fmt.Errorf("load account API key capacity: %w", err)
	}
	return activeCount, totalCount, nil
}

func GetAccountAPIKeyCapacity(ctx context.Context, accountID string) (int, int, error) {
	if Pool == nil {
		return 0, 0, ErrNoDatabase
	}
	return accountAPIKeyCapacity(ctx, Pool, accountID)
}

func accountAPIKeyQuotaBucket(accountID string, action AccountAPIKeyQuotaAction) string {
	return "account-api-key:" + string(action) + ":" + accountID
}

// ConsumeAccountAPIKeyQuota atomically consumes a durable, per-account hourly
// mutation slot. The rate_limits key is namespaced by action and never includes
// an email address, so rotating source IPs cannot bypass the quota.
func ConsumeAccountAPIKeyQuota(ctx context.Context, accountID string, action AccountAPIKeyQuotaAction, maxPerHour int) (bool, int, error) {
	if Pool == nil {
		return false, 0, ErrNoDatabase
	}
	if strings.TrimSpace(accountID) == "" {
		return false, 0, errors.New("account API key quota requires an account")
	}
	switch action {
	case AccountAPIKeyQuotaCreate, AccountAPIKeyQuotaRevoke, AccountAPIKeyQuotaLink:
	default:
		return false, 0, fmt.Errorf("unsupported account API key quota action %q", action)
	}
	return CheckRateLimit(ctx, accountAPIKeyQuotaBucket(accountID, action), maxPerHour)
}

// CreateAccountAPIKeyAndBot atomically creates a credential, its bot, durable
// account ownership, and the initial bot link. Locking the customer account
// serializes concurrent issuance so the five-active-key cap cannot be raced.
func CreateAccountAPIKeyAndBot(
	ctx context.Context,
	accountID, id, keyHash, keyPrefix, ipCreated string,
	bot *Bot,
) (*AccountAPIKey, int, error) {
	if Pool == nil {
		return nil, 0, ErrNoDatabase
	}
	if bot == nil || bot.APIKeyID != id {
		return nil, 0, errors.New("CreateAccountAPIKeyAndBot: bot must reference the new API key")
	}

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("CreateAccountAPIKeyAndBot begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := lockCustomerAccount(ctx, tx, accountID, true); err != nil {
		return nil, 0, err
	}
	activeCount, totalCount, err := accountAPIKeyCapacity(ctx, tx, accountID)
	if err != nil {
		return nil, 0, err
	}
	if activeCount >= MaxActiveAccountAPIKeys {
		return nil, activeCount, ErrCustomerAPIKeyLimit
	}
	if totalCount >= MaxAccountAPIKeyHistory {
		return nil, activeCount, ErrCustomerAPIKeyHistoryLimit
	}
	if err := enforcePlatformAgentCapacityTx(ctx, tx, accountID); err != nil {
		return nil, activeCount, err
	}

	if _, err := tx.Exec(ctx, insertAPIKeySQL, id, keyHash, keyPrefix, ipCreated); err != nil {
		return nil, 0, fmt.Errorf("CreateAccountAPIKeyAndBot key insert: %w", err)
	}
	if _, err := tx.Exec(ctx, insertBotSQL,
		bot.ID, bot.APIKeyID, bot.Name, bot.AvatarColor, bot.DefaultWeapon, bot.DefaultStats,
		bot.DefaultFallback, bot.CreatedAt, bot.UpdatedAt,
	); err != nil {
		return nil, 0, fmt.Errorf("CreateAccountAPIKeyAndBot bot insert: %w", err)
	}
	if err := enrollArenaAgentTx(ctx, tx, bot, "arena"); err != nil {
		return nil, 0, fmt.Errorf("CreateAccountAPIKeyAndBot enrollment: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO account_api_keys (account_id, api_key_id, linked_at)
		VALUES ($1, $2, NOW())`, accountID, id); err != nil {
		return nil, 0, fmt.Errorf("CreateAccountAPIKeyAndBot ownership insert: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO account_bot_links (account_id, bot_id, linked_at)
		VALUES ($1, $2, NOW())`, accountID, bot.ID); err != nil {
		return nil, 0, fmt.Errorf("CreateAccountAPIKeyAndBot bot link insert: %w", err)
	}
	if err := appendPlatformAgentLinkEventTx(ctx, tx, accountID, bot.ID, "linked", "arena_account_registration", time.Now()); err != nil {
		return nil, 0, fmt.Errorf("CreateAccountAPIKeyAndBot platform link: %w", err)
	}

	key, err := scanAccountAPIKey(tx.QueryRow(ctx,
		accountAPIKeyColumns+` WHERE owned.account_id = $1 AND owned.api_key_id = $2`, accountID, id))
	if err != nil {
		return nil, 0, fmt.Errorf("CreateAccountAPIKeyAndBot load: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("CreateAccountAPIKeyAndBot commit: %w", err)
	}
	return key, activeCount + 1, nil
}

func ListAccountAPIKeys(ctx context.Context, accountID string) ([]AccountAPIKey, int, error) {
	if Pool == nil {
		return nil, 0, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx,
		accountAPIKeyColumns+` WHERE owned.account_id = $1 ORDER BY k.created_at DESC, k.id DESC LIMIT $2`,
		accountID, AccountAPIKeyListLimit)
	if err != nil {
		return nil, 0, fmt.Errorf("ListAccountAPIKeys: %w", err)
	}
	defer rows.Close()

	keys := make([]AccountAPIKey, 0)
	for rows.Next() {
		key, err := scanAccountAPIKey(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("ListAccountAPIKeys scan: %w", err)
		}
		keys = append(keys, *key)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("ListAccountAPIKeys rows: %w", err)
	}
	activeCount, _, err := GetAccountAPIKeyCapacity(ctx, accountID)
	if err != nil {
		return nil, 0, err
	}
	return keys, activeCount, nil
}

func DeactivateAccountAPIKey(ctx context.Context, accountID, keyID string) (*AccountAPIKey, int, error) {
	if Pool == nil {
		return nil, 0, ErrNoDatabase
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("DeactivateAccountAPIKey begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := lockCustomerAccount(ctx, tx, accountID, true); err != nil {
		return nil, 0, err
	}
	var owned bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM account_api_keys WHERE account_id = $1 AND api_key_id = $2
		)`, accountID, keyID).Scan(&owned); err != nil {
		return nil, 0, fmt.Errorf("DeactivateAccountAPIKey ownership: %w", err)
	}
	if !owned {
		return nil, 0, ErrCustomerAPIKeyNotOwned
	}
	if _, err := tx.Exec(ctx, `UPDATE api_keys SET is_active = false WHERE id = $1`, keyID); err != nil {
		return nil, 0, fmt.Errorf("DeactivateAccountAPIKey update: %w", err)
	}
	key, err := scanAccountAPIKey(tx.QueryRow(ctx,
		accountAPIKeyColumns+` WHERE owned.account_id = $1 AND owned.api_key_id = $2`, accountID, keyID))
	if err != nil {
		return nil, 0, fmt.Errorf("DeactivateAccountAPIKey load: %w", err)
	}
	activeCount, err := countActiveAccountAPIKeys(ctx, tx, accountID)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("DeactivateAccountAPIKey commit: %w", err)
	}
	return key, activeCount, nil
}
