package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	CosmeticSubscriptionPriceCents = 1999
	CosmeticSubscriptionCurrency   = "USD"
	CosmeticSubscriptionInterval   = "month"
	CosmeticSubscriptionMaxAPIKeys = 5

	CosmeticSubscriptionStatusCreated         = "created"
	CosmeticSubscriptionStatusCheckoutPending = "checkout_pending"
	CosmeticSubscriptionStatusIncomplete      = "incomplete"
	CosmeticSubscriptionStatusTrialing        = "trialing"
	CosmeticSubscriptionStatusActive          = "active"
	CosmeticSubscriptionStatusPastDue         = "past_due"
	CosmeticSubscriptionStatusPaused          = "paused"
	CosmeticSubscriptionStatusUnpaid          = "unpaid"
	CosmeticSubscriptionStatusBillingMismatch = "billing_mismatch"
	CosmeticSubscriptionStatusCanceled        = "canceled"
	CosmeticSubscriptionStatusExpired         = "expired"

	CosmeticStripeSubscriptionCheckoutCompleted = "checkout.session.completed"
	CosmeticStripeSubscriptionCheckoutExpired   = "checkout.session.expired"
	CosmeticStripeSubscriptionCreated           = "customer.subscription.created"
	CosmeticStripeSubscriptionUpdated           = "customer.subscription.updated"
	CosmeticStripeSubscriptionDeleted           = "customer.subscription.deleted"
)

var (
	ErrCosmeticSubscriptionNotFound      = errors.New("cosmetic subscription not found")
	ErrCosmeticSubscriptionExists        = errors.New("an unfinished cosmetic subscription already exists")
	ErrCosmeticSubscriptionMismatch      = errors.New("cosmetic subscription event does not match the account")
	ErrCosmeticSubscriptionEventInvalid  = errors.New("invalid cosmetic subscription event")
	ErrCosmeticSubscriptionEventConflict = errors.New(
		"cosmetic subscription event payload conflicts with an existing event",
	)
)

type CosmeticSubscriptionOffer struct {
	Enabled            bool   `json:"enabled"`
	PriceCents         int64  `json:"price_cents"`
	Currency           string `json:"currency"`
	Interval           string `json:"interval"`
	IncludesFutureSets bool   `json:"includes_future_sets"`
	MaxAPIKeys         int    `json:"max_api_keys"`
}

type CosmeticSubscription struct {
	ID                          string     `json:"id"`
	AccountID                   string     `json:"-"`
	AccountEmail                string     `json:"-"`
	Status                      string     `json:"status"`
	HasAccess                   bool       `json:"has_access"`
	Terminal                    bool       `json:"terminal"`
	CanManage                   bool       `json:"can_manage"`
	CancelAtPeriodEnd           bool       `json:"cancel_at_period_end"`
	CurrentPeriodEnd            *time.Time `json:"current_period_end,omitempty"`
	PriceCents                  int64      `json:"price_cents"`
	Currency                    string     `json:"currency"`
	Interval                    string     `json:"interval"`
	IncludesFutureSets          bool       `json:"includes_future_sets"`
	MaxAPIKeys                  int        `json:"max_api_keys"`
	CheckoutSessionID           string     `json:"-"`
	CustomerID                  string     `json:"-"`
	ProviderSubscriptionID      string     `json:"-"`
	LastProviderEventCreatedAt  *time.Time `json:"-"`
	LastProviderStateObservedAt *time.Time `json:"-"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`
	TerminalAt                  *time.Time `json:"terminal_at,omitempty"`
}

type CosmeticSubscriptionEventInput struct {
	Provider                string
	EventID                 string
	EventType               string
	PayloadHash             string
	SubscriptionID          string
	AccountID               string
	CheckoutSessionID       string
	ProviderSubscriptionID  string
	CustomerID              string
	Status                  string
	CancelAtPeriodEnd       bool
	CurrentPeriodEnd        *time.Time
	ProviderCreatedAt       time.Time
	ProviderStateObservedAt time.Time
	Terminal                bool
}

type CosmeticSubscriptionEventResult struct {
	Subscription    *CosmeticSubscription `json:"subscription"`
	Applied         bool                  `json:"applied"`
	Duplicate       bool                  `json:"duplicate"`
	LicensesCreated int                   `json:"licenses_created"`
	LicensesRevoked int                   `json:"licenses_revoked"`
}

func DefaultCosmeticSubscriptionOffer(enabled bool) CosmeticSubscriptionOffer {
	return CosmeticSubscriptionOffer{
		Enabled:            enabled,
		PriceCents:         CosmeticSubscriptionPriceCents,
		Currency:           CosmeticSubscriptionCurrency,
		Interval:           CosmeticSubscriptionInterval,
		IncludesFutureSets: true,
		MaxAPIKeys:         CosmeticSubscriptionMaxAPIKeys,
	}
}

func cosmeticSubscriptionGrantsAccess(status string) bool {
	return status == CosmeticSubscriptionStatusActive || status == CosmeticSubscriptionStatusTrialing
}

func cosmeticSubscriptionIsTerminal(status string) bool {
	return status == CosmeticSubscriptionStatusCanceled || status == CosmeticSubscriptionStatusExpired
}

func validCosmeticSubscriptionStatus(status string) bool {
	switch status {
	case CosmeticSubscriptionStatusCreated, CosmeticSubscriptionStatusCheckoutPending,
		CosmeticSubscriptionStatusIncomplete, CosmeticSubscriptionStatusTrialing,
		CosmeticSubscriptionStatusActive, CosmeticSubscriptionStatusPastDue,
		CosmeticSubscriptionStatusPaused, CosmeticSubscriptionStatusUnpaid, CosmeticSubscriptionStatusBillingMismatch,
		CosmeticSubscriptionStatusCanceled, CosmeticSubscriptionStatusExpired:
		return true
	default:
		return false
	}
}

func EnsureCosmeticSubscriptionsSchema(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("EnsureCosmeticSubscriptionsSchema begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(2026071201::BIGINT)`); err != nil {
		return fmt.Errorf("EnsureCosmeticSubscriptionsSchema migration lock: %w", err)
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cosmetic_subscriptions (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL REFERENCES customer_accounts(id) ON DELETE RESTRICT,
			account_email TEXT NOT NULL,
			price_cents BIGINT NOT NULL DEFAULT 1999 CHECK (price_cents = 1999),
			currency TEXT NOT NULL DEFAULT 'USD' CHECK (currency = 'USD'),
			billing_interval TEXT NOT NULL DEFAULT 'month' CHECK (billing_interval = 'month'),
			status TEXT NOT NULL DEFAULT 'created' CONSTRAINT cosmetic_subscriptions_status_check CHECK (status IN (
				'created','checkout_pending','incomplete','trialing','active','past_due','paused','unpaid','billing_mismatch','canceled','expired'
			)),
			stripe_checkout_session_id TEXT,
			stripe_customer_id TEXT,
			stripe_subscription_id TEXT,
			cancel_at_period_end BOOLEAN NOT NULL DEFAULT false,
			current_period_end TIMESTAMPTZ,
			last_provider_event_created_at TIMESTAMPTZ,
			last_provider_state_observed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			terminal_at TIMESTAMPTZ
		)`,
		`ALTER TABLE cosmetic_subscriptions
			ADD COLUMN IF NOT EXISTS last_provider_state_observed_at TIMESTAMPTZ`,
		`DO $$
		DECLARE status_definition TEXT;
		BEGIN
			SELECT pg_get_constraintdef(oid) INTO status_definition
			FROM pg_constraint
			WHERE conrelid = 'cosmetic_subscriptions'::regclass
			  AND conname = 'cosmetic_subscriptions_status_check';
			IF status_definition IS NULL OR POSITION('billing_mismatch' IN status_definition) = 0 THEN
				ALTER TABLE cosmetic_subscriptions DROP CONSTRAINT IF EXISTS cosmetic_subscriptions_status_check;
				ALTER TABLE cosmetic_subscriptions ADD CONSTRAINT cosmetic_subscriptions_status_check CHECK (status IN (
					'created','checkout_pending','incomplete','trialing','active','past_due','paused','unpaid','billing_mismatch','canceled','expired'
				));
			END IF;
		END $$`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cosmetic_subscriptions_checkout
			ON cosmetic_subscriptions (stripe_checkout_session_id)
			WHERE stripe_checkout_session_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cosmetic_subscriptions_provider
			ON cosmetic_subscriptions (stripe_subscription_id)
			WHERE stripe_subscription_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cosmetic_subscriptions_account_open
			ON cosmetic_subscriptions (account_id)
			WHERE status NOT IN ('canceled','expired')`,
		`CREATE TABLE IF NOT EXISTS cosmetic_subscription_licenses (
			subscription_id TEXT NOT NULL REFERENCES cosmetic_subscriptions(id) ON DELETE RESTRICT,
			item_id TEXT NOT NULL REFERENCES cosmetic_items(id) ON DELETE RESTRICT,
			license_id TEXT NOT NULL UNIQUE REFERENCES cosmetic_licenses(id) ON DELETE RESTRICT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (subscription_id, item_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cosmetic_subscription_licenses_item
			ON cosmetic_subscription_licenses (item_id, subscription_id)`,
		`CREATE TABLE IF NOT EXISTS cosmetic_subscription_events (
			provider TEXT NOT NULL,
			event_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			payload_hash TEXT NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
			subscription_id TEXT NOT NULL REFERENCES cosmetic_subscriptions(id) ON DELETE RESTRICT,
			status TEXT NOT NULL DEFAULT 'processing' CHECK (status IN ('processing','processed','rejected')),
			last_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			processed_at TIMESTAMPTZ,
			PRIMARY KEY (provider, event_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cosmetic_subscription_events_subscription
			ON cosmetic_subscription_events (subscription_id, created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("EnsureCosmeticSubscriptionsSchema exec: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("EnsureCosmeticSubscriptionsSchema commit: %w", err)
	}
	return nil
}

type cosmeticSubscriptionScanner interface {
	Scan(dest ...any) error
}

type cosmeticSubscriptionQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func cosmeticSubscriptionSelect() string {
	return `SELECT id, account_id, account_email, price_cents, currency, billing_interval, status,
		COALESCE(stripe_checkout_session_id, ''), COALESCE(stripe_customer_id, ''),
		COALESCE(stripe_subscription_id, ''), cancel_at_period_end, current_period_end,
		last_provider_event_created_at, last_provider_state_observed_at, created_at, updated_at, terminal_at
		FROM cosmetic_subscriptions`
}

func scanCosmeticSubscription(row cosmeticSubscriptionScanner) (*CosmeticSubscription, error) {
	var subscription CosmeticSubscription
	if err := row.Scan(&subscription.ID, &subscription.AccountID, &subscription.AccountEmail,
		&subscription.PriceCents, &subscription.Currency, &subscription.Interval, &subscription.Status,
		&subscription.CheckoutSessionID, &subscription.CustomerID, &subscription.ProviderSubscriptionID,
		&subscription.CancelAtPeriodEnd, &subscription.CurrentPeriodEnd,
		&subscription.LastProviderEventCreatedAt, &subscription.LastProviderStateObservedAt,
		&subscription.CreatedAt, &subscription.UpdatedAt,
		&subscription.TerminalAt); err != nil {
		return nil, err
	}
	subscription.HasAccess = cosmeticSubscriptionGrantsAccess(subscription.Status)
	subscription.Terminal = cosmeticSubscriptionIsTerminal(subscription.Status)
	subscription.CanManage = subscription.CustomerID != "" && !subscription.Terminal
	subscription.IncludesFutureSets = true
	subscription.MaxAPIKeys = CosmeticSubscriptionMaxAPIKeys
	return &subscription, nil
}

func loadCosmeticSubscription(ctx context.Context, q cosmeticSubscriptionQuerier, subscriptionID string, forUpdate bool) (*CosmeticSubscription, error) {
	query := cosmeticSubscriptionSelect() + ` WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	subscription, err := scanCosmeticSubscription(q.QueryRow(ctx, query, subscriptionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCosmeticSubscriptionNotFound
	}
	if err != nil {
		return nil, err
	}
	return subscription, nil
}

func CreateCosmeticSubscription(ctx context.Context, accountID string) (*CosmeticSubscription, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("CreateCosmeticSubscription begin: %w", err)
	}
	defer tx.Rollback(ctx)
	account, err := lockCustomerAccount(ctx, tx, strings.TrimSpace(accountID), true)
	if err != nil {
		return nil, err
	}
	activeAPIKeys, err := countActiveAccountAPIKeys(ctx, tx, account.ID)
	if err != nil {
		return nil, err
	}
	// Exactly five active keys is a valid subscription account. This guard is
	// for legacy ownership that predates the current issuance cap and may have
	// left the account with more active keys than the subscription permits.
	if activeAPIKeys > CosmeticSubscriptionMaxAPIKeys {
		return nil, ErrCustomerAPIKeyLimit
	}
	var existing, existingStatus string
	err = tx.QueryRow(ctx, `
		SELECT id, status FROM cosmetic_subscriptions
		WHERE account_id = $1 AND status NOT IN ('canceled','expired')
		FOR UPDATE`, account.ID).Scan(&existing, &existingStatus)
	if err == nil {
		if existingStatus == CosmeticSubscriptionStatusCreated || existingStatus == CosmeticSubscriptionStatusCheckoutPending {
			return loadCosmeticSubscription(ctx, tx, existing, false)
		}
		return nil, ErrCosmeticSubscriptionExists
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("CreateCosmeticSubscription existing: %w", err)
	}
	subscriptionID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO cosmetic_subscriptions
			(id, account_id, account_email, price_cents, currency, billing_interval, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,'created',NOW(),NOW())`,
		subscriptionID, account.ID, account.Email, CosmeticSubscriptionPriceCents,
		CosmeticSubscriptionCurrency, CosmeticSubscriptionInterval)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrCosmeticSubscriptionExists
		}
		return nil, fmt.Errorf("CreateCosmeticSubscription insert: %w", err)
	}
	subscription, err := loadCosmeticSubscription(ctx, tx, subscriptionID, false)
	if err != nil {
		return nil, fmt.Errorf("CreateCosmeticSubscription load: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("CreateCosmeticSubscription commit: %w", err)
	}
	return subscription, nil
}

func AttachCosmeticSubscriptionCheckout(ctx context.Context, accountID, subscriptionID, checkoutSessionID string) (*CosmeticSubscription, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	checkoutSessionID = strings.TrimSpace(checkoutSessionID)
	if checkoutSessionID == "" || len(checkoutSessionID) > 255 {
		return nil, ErrCosmeticSubscriptionMismatch
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("AttachCosmeticSubscriptionCheckout begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := lockCustomerAccount(ctx, tx, accountID, true); err != nil {
		return nil, err
	}
	subscription, err := loadCosmeticSubscription(ctx, tx, subscriptionID, true)
	if err != nil {
		return nil, err
	}
	if subscription.AccountID != accountID || cosmeticSubscriptionIsTerminal(subscription.Status) {
		return nil, ErrCosmeticSubscriptionMismatch
	}
	if subscription.CheckoutSessionID != "" && subscription.CheckoutSessionID != checkoutSessionID {
		return nil, ErrCosmeticSubscriptionMismatch
	}
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_subscriptions
		SET stripe_checkout_session_id = $2, status = CASE WHEN status = 'created' THEN 'checkout_pending' ELSE status END,
			updated_at = NOW()
		WHERE id = $1`, subscription.ID, checkoutSessionID); err != nil {
		return nil, fmt.Errorf("AttachCosmeticSubscriptionCheckout update: %w", err)
	}
	subscription, err = loadCosmeticSubscription(ctx, tx, subscription.ID, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("AttachCosmeticSubscriptionCheckout commit: %w", err)
	}
	return subscription, nil
}

// ExpireCosmeticSubscriptionCheckout terminalizes a pending local checkout
// only after the caller has authoritatively observed the matching provider
// session as expired. Replays are safe and cannot expire another session.
func ExpireCosmeticSubscriptionCheckout(ctx context.Context, accountID, subscriptionID, checkoutSessionID string) (*CosmeticSubscription, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	checkoutSessionID = strings.TrimSpace(checkoutSessionID)
	if checkoutSessionID == "" {
		return nil, ErrCosmeticSubscriptionMismatch
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("ExpireCosmeticSubscriptionCheckout begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := lockCustomerAccount(ctx, tx, accountID, false); err != nil {
		return nil, err
	}
	subscription, err := loadCosmeticSubscription(ctx, tx, subscriptionID, true)
	if err != nil {
		return nil, err
	}
	if subscription.AccountID != accountID || subscription.CheckoutSessionID != checkoutSessionID {
		return nil, ErrCosmeticSubscriptionMismatch
	}
	if subscription.Status != CosmeticSubscriptionStatusExpired {
		if subscription.Status != CosmeticSubscriptionStatusCheckoutPending {
			return nil, ErrCosmeticSubscriptionMismatch
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cosmetic_subscriptions
			SET status = 'expired', terminal_at = COALESCE(terminal_at, NOW()), updated_at = NOW()
			WHERE id = $1`, subscription.ID); err != nil {
			return nil, fmt.Errorf("ExpireCosmeticSubscriptionCheckout update: %w", err)
		}
		subscription, err = loadCosmeticSubscription(ctx, tx, subscription.ID, false)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("ExpireCosmeticSubscriptionCheckout commit: %w", err)
	}
	return subscription, nil
}

func GetCustomerCosmeticSubscription(ctx context.Context, accountID string) (*CosmeticSubscription, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	subscription, err := scanCosmeticSubscription(Pool.QueryRow(ctx, cosmeticSubscriptionSelect()+`
		WHERE account_id = $1
		ORDER BY (terminal_at IS NULL) DESC, created_at DESC, id DESC
		LIMIT 1`, accountID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetCustomerCosmeticSubscription: %w", err)
	}
	return subscription, nil
}

func SyncCustomerCosmeticSubscriptionLicenses(ctx context.Context, accountID string) (int, error) {
	if Pool == nil {
		return 0, ErrNoDatabase
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("SyncCustomerCosmeticSubscriptionLicenses begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := lockCustomerAccount(ctx, tx, accountID, true); err != nil {
		return 0, err
	}
	var subscriptionID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM cosmetic_subscriptions
		WHERE account_id = $1 AND status IN ('active','trialing')
		ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, accountID).Scan(&subscriptionID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("SyncCustomerCosmeticSubscriptionLicenses subscription: %w", err)
	}
	created, _, err := syncCosmeticSubscriptionLicenses(ctx, tx, subscriptionID, accountID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("SyncCustomerCosmeticSubscriptionLicenses commit: %w", err)
	}
	return created, nil
}

func syncCosmeticSubscriptionLicenses(ctx context.Context, tx pgx.Tx, subscriptionID, accountID string) (int, int, error) {
	var created, reactivated int
	if err := tx.QueryRow(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT DISTINCT i.id AS item_id
			FROM cosmetic_pack_items pi
			JOIN cosmetic_packs p ON p.id = pi.pack_id
			JOIN cosmetic_categories pc ON pc.id = p.category_id
			JOIN cosmetic_items i ON i.id = pi.item_id
			JOIN cosmetic_categories ic ON ic.id = i.category_id
			WHERE p.is_active = true AND p.is_purchasable = true AND p.is_free = false
			  AND pc.is_active = true AND i.is_active = true AND ic.is_active = true
		),
		reactivated AS (
			UPDATE cosmetic_licenses l
			SET status = 'active', updated_at = NOW()
			FROM cosmetic_subscription_licenses sl
			JOIN candidates c ON c.item_id = sl.item_id
			WHERE sl.subscription_id = $1 AND sl.license_id = l.id AND l.status <> 'active'
			RETURNING l.id
		),
		missing AS MATERIALIZED (
			SELECT c.item_id, gen_random_uuid()::TEXT AS license_id
			FROM candidates c
			LEFT JOIN cosmetic_subscription_licenses sl
			  ON sl.subscription_id = $1 AND sl.item_id = c.item_id
			WHERE sl.item_id IS NULL
		),
		inserted_licenses AS (
			INSERT INTO cosmetic_licenses
				(id, account_id, cosmetic_id, status, source, external_reference, granted_at, updated_at)
			SELECT license_id, $2, item_id, 'active', 'stripe_subscription',
				'cosmetic-subscription:' || $1 || ':item:' || item_id, NOW(), NOW()
			FROM missing
			RETURNING id, cosmetic_id
		),
		inserted_mappings AS (
			INSERT INTO cosmetic_subscription_licenses (subscription_id, item_id, license_id, created_at)
			SELECT $1, cosmetic_id, id, NOW()
			FROM inserted_licenses
			RETURNING license_id
		)
		SELECT
			(SELECT COUNT(*) FROM inserted_mappings),
			(SELECT COUNT(*) FROM reactivated)`, subscriptionID, accountID).Scan(&created, &reactivated); err != nil {
		return 0, 0, fmt.Errorf("syncCosmeticSubscriptionLicenses set-based sync: %w", err)
	}
	return created, reactivated, nil
}

func revokeCosmeticSubscriptionLicenses(ctx context.Context, tx pgx.Tx, subscriptionID string) (int, error) {
	var active int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM cosmetic_subscription_licenses sl
		JOIN cosmetic_licenses l ON l.id = sl.license_id
		WHERE sl.subscription_id = $1 AND l.status = 'active'`, subscriptionID).Scan(&active); err != nil {
		return 0, fmt.Errorf("revokeCosmeticSubscriptionLicenses count: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM bot_cosmetic_loadout l
		USING cosmetic_subscription_licenses sl
		WHERE sl.subscription_id = $1 AND sl.license_id = l.license_id`, subscriptionID); err != nil {
		return 0, fmt.Errorf("revokeCosmeticSubscriptionLicenses loadouts: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM cosmetic_license_assignments a
		USING cosmetic_subscription_licenses sl
		WHERE sl.subscription_id = $1 AND sl.license_id = a.license_id`, subscriptionID); err != nil {
		return 0, fmt.Errorf("revokeCosmeticSubscriptionLicenses assignments: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_licenses l
		SET status = CASE WHEN l.status = 'active' THEN 'revoked' ELSE l.status END,
			assigned_bot_id = NULL, updated_at = NOW()
		FROM cosmetic_subscription_licenses sl
		WHERE sl.subscription_id = $1 AND sl.license_id = l.id`, subscriptionID); err != nil {
		return 0, fmt.Errorf("revokeCosmeticSubscriptionLicenses licenses: %w", err)
	}
	return active, nil
}

func normalizeCosmeticSubscriptionEvent(input CosmeticSubscriptionEventInput) CosmeticSubscriptionEventInput {
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.EventID = strings.TrimSpace(input.EventID)
	input.EventType = strings.TrimSpace(input.EventType)
	input.PayloadHash = strings.ToLower(strings.TrimSpace(input.PayloadHash))
	input.SubscriptionID = strings.TrimSpace(input.SubscriptionID)
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.CheckoutSessionID = strings.TrimSpace(input.CheckoutSessionID)
	input.ProviderSubscriptionID = strings.TrimSpace(input.ProviderSubscriptionID)
	input.CustomerID = strings.TrimSpace(input.CustomerID)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "incomplete_expired" {
		input.Status = CosmeticSubscriptionStatusExpired
		input.Terminal = true
	}
	if cosmeticSubscriptionIsTerminal(input.Status) {
		input.Terminal = true
	}
	return input
}

func validCosmeticSubscriptionEventType(eventType string) bool {
	switch eventType {
	case CosmeticStripeSubscriptionCheckoutCompleted, CosmeticStripeSubscriptionCheckoutExpired,
		CosmeticStripeSubscriptionCreated, CosmeticStripeSubscriptionUpdated, CosmeticStripeSubscriptionDeleted:
		return true
	default:
		return false
	}
}

func ProcessCosmeticSubscriptionEvent(ctx context.Context, raw CosmeticSubscriptionEventInput) (*CosmeticSubscriptionEventResult, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	input := normalizeCosmeticSubscriptionEvent(raw)
	if input.Provider != "stripe" || input.EventID == "" || len(input.EventID) > 255 ||
		!cosmeticPaymentHashPattern.MatchString(input.PayloadHash) || !validCosmeticSubscriptionEventType(input.EventType) ||
		(input.SubscriptionID == "" && input.ProviderSubscriptionID == "") || input.ProviderCreatedAt.IsZero() {
		return nil, ErrCosmeticSubscriptionEventInvalid
	}
	var subscriptionID, accountID string
	query := `SELECT id, account_id FROM cosmetic_subscriptions WHERE id = $1`
	value := input.SubscriptionID
	if value == "" {
		query = `SELECT id, account_id FROM cosmetic_subscriptions WHERE stripe_subscription_id = $1`
		value = input.ProviderSubscriptionID
	}
	if err := Pool.QueryRow(ctx, query, value).Scan(&subscriptionID, &accountID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCosmeticSubscriptionNotFound
		}
		return nil, fmt.Errorf("ProcessCosmeticSubscriptionEvent resolve: %w", err)
	}
	if input.AccountID != "" && input.AccountID != accountID {
		return nil, ErrCosmeticSubscriptionMismatch
	}
	input.SubscriptionID = subscriptionID
	input.AccountID = accountID

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("ProcessCosmeticSubscriptionEvent begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := lockCustomerAccount(ctx, tx, accountID, false); err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO cosmetic_subscription_events
			(provider, event_id, event_type, payload_hash, subscription_id, status, created_at)
		VALUES ($1,$2,$3,$4,$5,'processing',NOW())
		ON CONFLICT (provider, event_id) DO NOTHING`,
		input.Provider, input.EventID, input.EventType, input.PayloadHash, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("ProcessCosmeticSubscriptionEvent record: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var eventType, payloadHash, existingSubscription, status string
		if err := tx.QueryRow(ctx, `
			SELECT event_type, payload_hash, subscription_id, status
			FROM cosmetic_subscription_events WHERE provider = $1 AND event_id = $2`,
			input.Provider, input.EventID).Scan(&eventType, &payloadHash, &existingSubscription, &status); err != nil {
			return nil, fmt.Errorf("ProcessCosmeticSubscriptionEvent duplicate: %w", err)
		}
		if eventType != input.EventType || payloadHash != input.PayloadHash || existingSubscription != subscriptionID {
			return nil, ErrCosmeticSubscriptionEventConflict
		}
		if status != "processed" {
			return nil, ErrCosmeticSubscriptionEventInvalid
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, err
		}
		subscription, err := loadCosmeticSubscription(ctx, Pool, subscriptionID, false)
		if err != nil {
			return nil, err
		}
		return &CosmeticSubscriptionEventResult{Subscription: subscription, Duplicate: true}, nil
	}

	subscription, err := loadCosmeticSubscription(ctx, tx, subscriptionID, true)
	if err != nil {
		return nil, err
	}
	if subscription.AccountID != input.AccountID ||
		(subscription.CheckoutSessionID != "" && input.CheckoutSessionID != "" && subscription.CheckoutSessionID != input.CheckoutSessionID) ||
		(subscription.ProviderSubscriptionID != "" && input.ProviderSubscriptionID != "" && subscription.ProviderSubscriptionID != input.ProviderSubscriptionID) ||
		(subscription.CustomerID != "" && input.CustomerID != "" && subscription.CustomerID != input.CustomerID) {
		return nil, ErrCosmeticSubscriptionMismatch
	}
	result := &CosmeticSubscriptionEventResult{}
	providerStateObserved := !input.ProviderStateObservedAt.IsZero()
	stale := false
	if providerStateObserved {
		stale = subscription.LastProviderStateObservedAt != nil &&
			!input.ProviderStateObservedAt.After(*subscription.LastProviderStateObservedAt)
	} else {
		stale = subscription.LastProviderEventCreatedAt != nil && input.ProviderCreatedAt.Before(*subscription.LastProviderEventCreatedAt)
	}
	terminalAlready := subscription.Terminal && !input.Terminal && !cosmeticSubscriptionIsTerminal(input.Status)
	if !stale && !terminalAlready {
		status := subscription.Status
		terminal := input.Terminal
		switch input.EventType {
		case CosmeticStripeSubscriptionCheckoutCompleted:
			if status == CosmeticSubscriptionStatusCreated {
				status = CosmeticSubscriptionStatusCheckoutPending
			}
		case CosmeticStripeSubscriptionCheckoutExpired:
			if subscription.ProviderSubscriptionID == "" && input.ProviderSubscriptionID == "" {
				status = CosmeticSubscriptionStatusExpired
				terminal = true
			}
		default:
			if !validCosmeticSubscriptionStatus(input.Status) {
				return nil, ErrCosmeticSubscriptionEventInvalid
			}
			status = input.Status
			terminal = terminal || cosmeticSubscriptionIsTerminal(status)
		}
		if terminal && status != CosmeticSubscriptionStatusExpired {
			status = CosmeticSubscriptionStatusCanceled
		}
		var observedAt interface{}
		if providerStateObserved {
			observedAt = input.ProviderStateObservedAt
		}
		if _, err := tx.Exec(ctx, `
			UPDATE cosmetic_subscriptions
			SET status = $2,
				stripe_checkout_session_id = COALESCE(stripe_checkout_session_id, NULLIF($3, '')),
				stripe_customer_id = COALESCE(stripe_customer_id, NULLIF($4, '')),
				stripe_subscription_id = COALESCE(stripe_subscription_id, NULLIF($5, '')),
				cancel_at_period_end = $6,
				current_period_end = COALESCE($7, current_period_end),
				last_provider_event_created_at = CASE
					WHEN last_provider_event_created_at IS NULL OR last_provider_event_created_at < $8 THEN $8
					ELSE last_provider_event_created_at END,
				last_provider_state_observed_at = COALESCE($9::TIMESTAMPTZ, last_provider_state_observed_at),
				terminal_at = CASE WHEN $10 THEN COALESCE(terminal_at, NOW()) ELSE terminal_at END,
				updated_at = NOW()
			WHERE id = $1`, subscriptionID, status, input.CheckoutSessionID, input.CustomerID,
			input.ProviderSubscriptionID, input.CancelAtPeriodEnd, input.CurrentPeriodEnd,
			input.ProviderCreatedAt, observedAt, terminal); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return nil, ErrCosmeticSubscriptionMismatch
			}
			return nil, fmt.Errorf("ProcessCosmeticSubscriptionEvent update: %w", err)
		}
		if cosmeticSubscriptionGrantsAccess(status) {
			result.LicensesCreated, _, err = syncCosmeticSubscriptionLicenses(ctx, tx, subscriptionID, accountID)
		} else {
			result.LicensesRevoked, err = revokeCosmeticSubscriptionLicenses(ctx, tx, subscriptionID)
		}
		if err != nil {
			return nil, err
		}
		result.Applied = true
	}
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_subscription_events
		SET status = 'processed', processed_at = NOW(), last_error = ''
		WHERE provider = $1 AND event_id = $2`, input.Provider, input.EventID); err != nil {
		return nil, fmt.Errorf("ProcessCosmeticSubscriptionEvent complete: %w", err)
	}
	result.Subscription, err = loadCosmeticSubscription(ctx, tx, subscriptionID, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("ProcessCosmeticSubscriptionEvent commit: %w", err)
	}
	return result, nil
}
