package db

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func setupVerifiedAccountAPIKeyTest(t *testing.T, email, subject string) (context.Context, *CustomerAccount) {
	t.Helper()
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	if err := EnsureCosmeticsSchema(ctx); err != nil {
		t.Fatalf("EnsureCosmeticsSchema: %v", err)
	}
	account, err := UpsertVerifiedCustomerAccount(ctx, email, "https://accounts.example.test", subject, "Key Owner")
	if err != nil {
		t.Fatalf("UpsertVerifiedCustomerAccount: %v", err)
	}
	return ctx, account
}

func accountAPIKeyTestBot(keyID, botID string) *Bot {
	now := time.Now()
	return &Bot{
		ID:              botID,
		APIKeyID:        keyID,
		Name:            "Owned Bot",
		AvatarColor:     "#123456",
		DefaultWeapon:   "sword",
		DefaultStats:    JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		DefaultFallback: "aggressive",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func createOwnedAccountAPIKey(ctx context.Context, accountID string, n int) (*AccountAPIKey, int, error) {
	keyID := fmt.Sprintf("owned-key-%d-%s", n, uuid.NewString())
	botID := fmt.Sprintf("owned-bot-%d-%s", n, uuid.NewString())
	return CreateAccountAPIKeyAndBot(
		ctx,
		accountID,
		keyID,
		"bcrypt-hash-"+keyID,
		fmt.Sprintf("arena_%06d", n),
		"127.0.0.1",
		accountAPIKeyTestBot(keyID, botID),
	)
}

func seedAccountAPIKeyHistory(t *testing.T, ctx context.Context, accountID string, count int, isActive func(int) bool) {
	t.Helper()
	tx, err := Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin history seed: %v", err)
	}
	defer tx.Rollback(ctx)
	base := time.Now().UTC().Add(-time.Duration(count) * time.Minute)
	for index := 0; index < count; index++ {
		keyID := fmt.Sprintf("history-key-%03d", index)
		botID := fmt.Sprintf("history-bot-%03d", index)
		createdAt := base.Add(time.Duration(index) * time.Minute)
		if _, err := tx.Exec(ctx, `
			INSERT INTO api_keys (id, key_hash, key_prefix, created_at, is_active, ip_created)
			VALUES ($1, $2, $3, $4, $5, '127.0.0.1')`,
			keyID, "audit-hash-"+keyID, fmt.Sprintf("arena_h%04d", index), createdAt, isActive(index)); err != nil {
			t.Fatalf("insert history key %d: %v", index, err)
		}
		bot := accountAPIKeyTestBot(keyID, botID)
		bot.CreatedAt, bot.UpdatedAt = createdAt, createdAt
		if _, err := tx.Exec(ctx, insertBotSQL,
			bot.ID, bot.APIKeyID, bot.Name, bot.AvatarColor, bot.DefaultWeapon, bot.DefaultStats,
			bot.DefaultFallback, bot.CreatedAt, bot.UpdatedAt,
		); err != nil {
			t.Fatalf("insert history bot %d: %v", index, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO account_api_keys (account_id, api_key_id, linked_at)
			VALUES ($1, $2, $3)`, accountID, keyID, createdAt); err != nil {
			t.Fatalf("insert history ownership %d: %v", index, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit history seed: %v", err)
	}
}

func TestAccountAPIKeyLifecycleEnforcesDurableFiveActiveCap(t *testing.T) {
	ctx, account := setupVerifiedAccountAPIKeyTest(t, "owner@example.com", "owner")

	created := make([]*AccountAPIKey, 0, MaxActiveAccountAPIKeys)
	for i := 0; i < MaxActiveAccountAPIKeys; i++ {
		key, activeCount, err := createOwnedAccountAPIKey(ctx, account.ID, i)
		if err != nil {
			t.Fatalf("create key %d: %v", i, err)
		}
		if activeCount != i+1 {
			t.Fatalf("active count after key %d = %d, want %d", i, activeCount, i+1)
		}
		created = append(created, key)
	}

	if unlinked, err := UnlinkBotFromCustomerAccount(ctx, account.ID, created[0].BotID); err != nil || !unlinked {
		t.Fatalf("unlink first bot = (%v, %v), want (true, nil)", unlinked, err)
	}
	if _, _, err := createOwnedAccountAPIKey(ctx, account.ID, 99); !errors.Is(err, ErrCustomerAPIKeyLimit) {
		t.Fatalf("create after unlink error = %v, want %v", err, ErrCustomerAPIKeyLimit)
	}

	revoked, activeCount, err := DeactivateAccountAPIKey(ctx, account.ID, created[0].ID)
	if err != nil || revoked == nil || revoked.IsActive || activeCount != MaxActiveAccountAPIKeys-1 {
		t.Fatalf("deactivate = (%+v, %d, %v)", revoked, activeCount, err)
	}
	if _, activeCount, err := createOwnedAccountAPIKey(ctx, account.ID, 100); err != nil || activeCount != MaxActiveAccountAPIKeys {
		t.Fatalf("replacement key = active %d, error %v", activeCount, err)
	}

	keys, activeCount, err := ListAccountAPIKeys(ctx, account.ID)
	if err != nil || len(keys) != MaxActiveAccountAPIKeys+1 || activeCount != MaxActiveAccountAPIKeys {
		t.Fatalf("list keys = (%d keys, active %d, %v)", len(keys), activeCount, err)
	}
}

func TestAccountAPIKeyConcurrentCreationNeverExceedsFive(t *testing.T) {
	ctx, account := setupVerifiedAccountAPIKeyTest(t, "concurrent@example.com", "concurrent")

	const attempts = 12
	var wg sync.WaitGroup
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, _, err := createOwnedAccountAPIKey(context.Background(), account.ID, 200+n)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)

	succeeded, limited := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrCustomerAPIKeyLimit):
			limited++
		default:
			t.Fatalf("unexpected concurrent create error: %v", err)
		}
	}
	if succeeded != MaxActiveAccountAPIKeys || limited != attempts-MaxActiveAccountAPIKeys {
		t.Fatalf("concurrent results: succeeded=%d limited=%d", succeeded, limited)
	}
	_, activeCount, err := ListAccountAPIKeys(ctx, account.ID)
	if err != nil || activeCount != MaxActiveAccountAPIKeys {
		t.Fatalf("active count = %d, error %v", activeCount, err)
	}
}

func TestLinkingLegacyKeyClaimsDurableOwnershipAndPreventsTransfer(t *testing.T) {
	ctx, first := setupVerifiedAccountAPIKeyTest(t, "first@example.com", "first")
	second, err := UpsertVerifiedCustomerAccount(ctx, "second@example.com", "https://accounts.example.test", "second", "Second")
	if err != nil {
		t.Fatalf("create second account: %v", err)
	}

	keyID, botID := "legacy-key", "legacy-bot"
	if err := CreateAPIKeyAndBot(ctx, keyID, "legacy-hash", "arena_legacy", "127.0.0.1", accountAPIKeyTestBot(keyID, botID)); err != nil {
		t.Fatalf("create legacy key: %v", err)
	}
	if _, err := LinkBotToCustomerAccount(ctx, first.ID, botID); err != nil {
		t.Fatalf("claim legacy key: %v", err)
	}
	if unlinked, err := UnlinkBotFromCustomerAccount(ctx, first.ID, botID); err != nil || !unlinked {
		t.Fatalf("unlink legacy bot = (%v, %v)", unlinked, err)
	}
	if _, err := LinkBotToCustomerAccount(ctx, second.ID, botID); !errors.Is(err, ErrCustomerAPIKeyAlreadyOwned) {
		t.Fatalf("cross-account legacy claim error = %v, want %v", err, ErrCustomerAPIKeyAlreadyOwned)
	}
	keys, activeCount, err := ListAccountAPIKeys(ctx, first.ID)
	if err != nil || len(keys) != 1 || activeCount != 1 || keys[0].BotID != botID {
		t.Fatalf("first account ownership = (%+v, %d, %v)", keys, activeCount, err)
	}
}

func TestCosmeticsSchemaBackfillsOwnershipForAlreadyLinkedLegacyBots(t *testing.T) {
	ctx, account := setupVerifiedAccountAPIKeyTest(t, "backfill@example.com", "backfill")
	keyID, botID := "backfill-key", "backfill-bot"
	if err := CreateAPIKeyAndBot(ctx, keyID, "backfill-hash", "arena_backfl", "127.0.0.1", accountAPIKeyTestBot(keyID, botID)); err != nil {
		t.Fatalf("create legacy key: %v", err)
	}
	if _, err := Pool.Exec(ctx, `INSERT INTO account_bot_links (account_id, bot_id) VALUES ($1, $2)`, account.ID, botID); err != nil {
		t.Fatalf("create legacy bot link: %v", err)
	}
	if err := EnsureCosmeticsSchema(ctx); err != nil {
		t.Fatalf("rerun cosmetics schema: %v", err)
	}

	keys, activeCount, err := ListAccountAPIKeys(ctx, account.ID)
	if err != nil || len(keys) != 1 || activeCount != 1 || keys[0].ID != keyID {
		t.Fatalf("backfilled ownership = (%+v, %d, %v)", keys, activeCount, err)
	}
}

func TestAccountAPIKeyCreationRollsBackEveryRowOnBotFailure(t *testing.T) {
	ctx, account := setupVerifiedAccountAPIKeyTest(t, "rollback@example.com", "rollback")
	existingKeyID := "existing-key"
	existingBotID := "rollback-bot"
	if err := CreateAPIKeyAndBot(ctx, existingKeyID, "existing-hash", "arena_existg", "127.0.0.1",
		accountAPIKeyTestBot(existingKeyID, existingBotID)); err != nil {
		t.Fatalf("create conflicting bot: %v", err)
	}

	keyID := "rollback-key"
	// The bot references the new key correctly, so the credential insert runs,
	// then the duplicate bot ID makes the second insert fail inside the same tx.
	bot := accountAPIKeyTestBot(keyID, existingBotID)
	if _, _, err := CreateAccountAPIKeyAndBot(ctx, account.ID, keyID, "rollback-hash", "arena_rollbk", "127.0.0.1", bot); err == nil {
		t.Fatal("expected account key creation to fail")
	}

	var count int
	if err := Pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE id = $1`, keyID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled-back api key count = %d, error %v", count, err)
	}
	if err := Pool.QueryRow(ctx, `SELECT COUNT(*) FROM account_api_keys WHERE api_key_id = $1`, keyID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled-back ownership count = %d, error %v", count, err)
	}
}

func TestAccountAPIKeyHistoryLimitRejectsFurtherCreationEvenWhenAllKeysAreInactive(t *testing.T) {
	ctx, account := setupVerifiedAccountAPIKeyTest(t, "history-limit@example.com", "history-limit")
	seedAccountAPIKeyHistory(t, ctx, account.ID, MaxAccountAPIKeyHistory, func(int) bool { return false })

	keyID := "over-history-key"
	_, _, err := CreateAccountAPIKeyAndBot(ctx, account.ID, keyID, "unused-hash", "arena_overhi", "127.0.0.1",
		accountAPIKeyTestBot(keyID, "over-history-bot"))
	if !errors.Is(err, ErrCustomerAPIKeyHistoryLimit) {
		t.Fatalf("create above lifetime history error = %v, want %v", err, ErrCustomerAPIKeyHistoryLimit)
	}
	var stored int
	if queryErr := Pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE id = $1`, keyID).Scan(&stored); queryErr != nil || stored != 0 {
		t.Fatalf("key above history cap stored=%d error=%v", stored, queryErr)
	}
}

func TestListAccountAPIKeysIsBoundedNewestFirstWithAccurateActiveCount(t *testing.T) {
	ctx, account := setupVerifiedAccountAPIKeyTest(t, "bounded-list@example.com", "bounded-list")
	seedAccountAPIKeyHistory(t, ctx, account.ID, MaxAccountAPIKeyHistory+5, func(index int) bool { return index < 3 })

	keys, activeCount, err := ListAccountAPIKeys(ctx, account.ID)
	if err != nil {
		t.Fatalf("ListAccountAPIKeys: %v", err)
	}
	if len(keys) != AccountAPIKeyListLimit {
		t.Fatalf("listed history = %d, want bounded %d", len(keys), AccountAPIKeyListLimit)
	}
	if activeCount != 3 {
		t.Fatalf("active count = %d, want 3 from complete history", activeCount)
	}
	if keys[0].ID != "history-key-104" || keys[len(keys)-1].ID != "history-key-005" {
		t.Fatalf("bounded order = first %q last %q", keys[0].ID, keys[len(keys)-1].ID)
	}
}

func TestAccountAPIKeyQuotaIsAtomicAndNamespacedByAccountAndAction(t *testing.T) {
	ctx, account := setupVerifiedAccountAPIKeyTest(t, "quota@example.com", "quota")
	const limit = 3
	const attempts = 12
	results := make(chan bool, attempts)
	errs := make(chan error, attempts)
	for index := 0; index < attempts; index++ {
		go func() {
			allowed, _, err := ConsumeAccountAPIKeyQuota(context.Background(), account.ID, AccountAPIKeyQuotaCreate, limit)
			results <- allowed
			errs <- err
		}()
	}
	allowedCount := 0
	for index := 0; index < attempts; index++ {
		if err := <-errs; err != nil {
			t.Fatalf("consume quota: %v", err)
		}
		if <-results {
			allowedCount++
		}
	}
	if allowedCount != limit {
		t.Fatalf("account quota admissions = %d, want %d", allowedCount, limit)
	}
	var stored int
	if err := Pool.QueryRow(ctx, `SELECT keys_generated FROM rate_limits WHERE ip_address = $1`,
		accountAPIKeyQuotaBucket(account.ID, AccountAPIKeyQuotaCreate)).Scan(&stored); err != nil || stored != limit {
		t.Fatalf("namespaced quota stored=%d error=%v", stored, err)
	}
}
