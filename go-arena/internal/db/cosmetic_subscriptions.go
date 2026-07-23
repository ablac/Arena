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
	CheckoutPresentation        string     `json:"checkout_presentation"`
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
			checkout_presentation TEXT NOT NULL DEFAULT 'hosted' CHECK (checkout_presentation IN ('embedded','hosted')),
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
		`ALTER TABLE cosmetic_subscriptions ADD COLUMN IF NOT EXISTS checkout_presentation TEXT NOT NULL DEFAULT 'hosted'
			CHECK (checkout_presentation IN ('embedded','hosted'))`,
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
			generation BIGINT NOT NULL DEFAULT 1 CHECK (generation >= 1),
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
		COALESCE(stripe_checkout_session_id, ''), checkout_presentation, COALESCE(stripe_customer_id, ''),
		COALESCE(stripe_subscription_id, ''), cancel_at_period_end, current_period_end,
		last_provider_event_created_at, last_provider_state_observed_at, created_at, updated_at, terminal_at
		FROM cosmetic_subscriptions`
}

func scanCosmeticSubscription(row cosmeticSubscriptionScanner) (*CosmeticSubscription, error) {
	var subscription CosmeticSubscription
	if err := row.Scan(&subscription.ID, &subscription.AccountID, &subscription.AccountEmail,
		&subscription.PriceCents, &subscription.Currency, &subscription.Interval, &subscription.Status,
		&subscription.CheckoutSessionID, &subscription.CheckoutPresentation, &subscription.CustomerID, &subscription.ProviderSubscriptionID,
		&subscription.CancelAtPeriodEnd, &subscription.CurrentPeriodEnd,
		&subscription.LastProviderEventCreatedAt, &subscription.LastProviderStateObservedAt,
		&subscription.CreatedAt, &subscription.UpdatedAt,
		&subscription.TerminalAt); err != nil {
		return nil, err
	}
	subscription.HasAccess = cosmeticSubscriptionGrantsAccess(subscription.Status)
	subscription.Terminal = cosmeticSubscriptionIsTerminal(subscription.Status)
	subscription.CanManage = subscription.CustomerID != "" && subscription.ProviderSubscriptionID != "" && !subscription.Terminal
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
	subscription, _, err := ReserveCosmeticSubscriptionCheckout(ctx, accountID, CosmeticCheckoutPresentationHosted)
	return subscription, err
}

// ReserveCosmeticSubscriptionCheckout commits the requested Checkout UI mode
// before provider IO. An unfinished record is returned unchanged, and a new
// record inherits the account's latest Stripe Customer ID after termination.
func ReserveCosmeticSubscriptionCheckout(ctx context.Context, accountID, presentation string) (*CosmeticSubscription, bool, error) {
	if Pool == nil {
		return nil, false, ErrNoDatabase
	}
	presentation = strings.ToLower(strings.TrimSpace(presentation))
	if presentation != CosmeticCheckoutPresentationEmbedded && presentation != CosmeticCheckoutPresentationHosted {
		return nil, false, ErrCosmeticCheckoutPresentation
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("ReserveCosmeticSubscriptionCheckout begin: %w", err)
	}
	defer tx.Rollback(ctx)
	account, err := lockCustomerAccount(ctx, tx, strings.TrimSpace(accountID), true)
	if err != nil {
		return nil, false, err
	}
	activeAPIKeys, err := countActiveAccountAPIKeys(ctx, tx, account.ID)
	if err != nil {
		return nil, false, err
	}
	// Exactly five active keys is a valid subscription account. This guard is
	// for legacy ownership that predates the current issuance cap and may have
	// left the account with more active keys than the subscription permits.
	if activeAPIKeys > CosmeticSubscriptionMaxAPIKeys {
		return nil, false, ErrCustomerAPIKeyLimit
	}
	var existing, existingStatus string
	err = tx.QueryRow(ctx, `
		SELECT id, status FROM cosmetic_subscriptions
		WHERE account_id = $1 AND status NOT IN ('canceled','expired')
		FOR UPDATE`, account.ID).Scan(&existing, &existingStatus)
	if err == nil {
		if existingStatus == CosmeticSubscriptionStatusCreated || existingStatus == CosmeticSubscriptionStatusCheckoutPending {
			subscription, loadErr := loadCosmeticSubscription(ctx, tx, existing, false)
			if loadErr != nil {
				return nil, false, loadErr
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, false, fmt.Errorf("ReserveCosmeticSubscriptionCheckout existing commit: %w", err)
			}
			return subscription, false, nil
		}
		return nil, false, ErrCosmeticSubscriptionExists
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("ReserveCosmeticSubscriptionCheckout existing: %w", err)
	}
	var inheritedCustomerID string
	err = tx.QueryRow(ctx, `
		SELECT stripe_customer_id FROM cosmetic_subscriptions
		WHERE account_id = $1 AND stripe_customer_id IS NOT NULL AND stripe_customer_id <> ''
		ORDER BY created_at DESC, id DESC LIMIT 1`, account.ID).Scan(&inheritedCustomerID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("ReserveCosmeticSubscriptionCheckout customer: %w", err)
	}
	subscriptionID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO cosmetic_subscriptions
			(id, account_id, account_email, price_cents, currency, billing_interval, status,
			 checkout_presentation, stripe_customer_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,'created',$7,NULLIF($8,''),NOW(),NOW())`,
		subscriptionID, account.ID, account.Email, CosmeticSubscriptionPriceCents,
		CosmeticSubscriptionCurrency, CosmeticSubscriptionInterval, presentation, inheritedCustomerID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, false, ErrCosmeticSubscriptionExists
		}
		return nil, false, fmt.Errorf("ReserveCosmeticSubscriptionCheckout insert: %w", err)
	}
	subscription, err := loadCosmeticSubscription(ctx, tx, subscriptionID, false)
	if err != nil {
		return nil, false, fmt.Errorf("ReserveCosmeticSubscriptionCheckout load: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("ReserveCosmeticSubscriptionCheckout commit: %w", err)
	}
	return subscription, true, nil
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
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT i.id AS item_id, COALESCE(sl.generation, 0)
		FROM cosmetic_pack_items pi
		JOIN cosmetic_packs p ON p.id = pi.pack_id
		JOIN cosmetic_categories pc ON pc.id = p.category_id
		JOIN cosmetic_items i ON i.id = pi.item_id
		JOIN cosmetic_categories ic ON ic.id = i.category_id
		LEFT JOIN cosmetic_subscription_licenses sl
		  ON sl.subscription_id = $1 AND sl.item_id = i.id
		LEFT JOIN cosmetic_licenses existing_license ON existing_license.id = sl.license_id
		WHERE p.is_active = true AND p.is_purchasable = true AND p.is_free = false
		  AND pc.is_active = true AND i.is_active = true AND ic.is_active = true
		  AND (sl.item_id IS NULL OR existing_license.status <> 'active')
		ORDER BY i.id, COALESCE(sl.generation, 0)`, subscriptionID)
	if err != nil {
		return 0, 0, fmt.Errorf("syncCosmeticSubscriptionLicenses candidates: %w", err)
	}
	type candidate struct {
		itemID     string
		generation int64
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.itemID, &item.generation); err != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("syncCosmeticSubscriptionLicenses scan: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, 0, fmt.Errorf("syncCosmeticSubscriptionLicenses rows: %w", err)
	}
	rows.Close()

	inputs := make([]cosmeticLicenseCreate, 0, len(candidates))
	itemIDs := make([]string, 0, len(candidates))
	licenseIDs := make([]string, 0, len(candidates))
	generations := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		licenseID := uuid.NewString()
		generation := candidate.generation + 1
		if generation < 1 {
			generation = 1
		}
		externalReference := fmt.Sprintf("cosmetic-subscription:%s:item:%s:generation:%d", subscriptionID, candidate.itemID, generation)
		inputs = append(inputs, cosmeticLicenseCreate{
			LicenseID: licenseID, AccountID: &accountID, CosmeticID: candidate.itemID,
			Source: "stripe_subscription", Reason: "subscription_generation", ExternalReference: externalReference,
		})
		itemIDs = append(itemIDs, candidate.itemID)
		licenseIDs = append(licenseIDs, licenseID)
		generations = append(generations, generation)
	}
	inserted, err := createCosmeticLicensesTx(ctx, tx, inputs)
	if err != nil {
		return 0, 0, fmt.Errorf("syncCosmeticSubscriptionLicenses licenses: %w", err)
	}
	mappedItemIDs := make([]string, 0, len(itemIDs))
	mappedLicenseIDs := make([]string, 0, len(itemIDs))
	mappedGenerations := make([]int64, 0, len(itemIDs))
	for index, licenseID := range licenseIDs {
		if inserted[licenseID] {
			mappedItemIDs = append(mappedItemIDs, itemIDs[index])
			mappedLicenseIDs = append(mappedLicenseIDs, licenseID)
			mappedGenerations = append(mappedGenerations, generations[index])
		}
	}
	if len(mappedLicenseIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO cosmetic_subscription_licenses (
				subscription_id, item_id, license_id, generation, created_at
			)
			SELECT $1, item_id, license_id, generation, NOW()
			FROM UNNEST($2::TEXT[], $3::TEXT[], $4::BIGINT[])
				AS mappings(item_id, license_id, generation)
			ON CONFLICT (subscription_id, item_id) DO UPDATE
			SET license_id = EXCLUDED.license_id, generation = EXCLUDED.generation,
			    created_at = EXCLUDED.created_at`,
			subscriptionID, mappedItemIDs, mappedLicenseIDs, mappedGenerations); err != nil {
			return 0, 0, fmt.Errorf("syncCosmeticSubscriptionLicenses mappings: %w", err)
		}
	}
	return len(mappedLicenseIDs), 0, nil
}

func revokeCosmeticSubscriptionLicenses(ctx context.Context, tx pgx.Tx, subscriptionID string) (int, error) {
	rows, err := tx.Query(ctx, `
		SELECT l.id
		FROM cosmetic_subscription_licenses sl
		JOIN cosmetic_licenses l ON l.id = sl.license_id
		WHERE sl.subscription_id = $1
		ORDER BY l.id
		FOR UPDATE OF l`, subscriptionID)
	if err != nil {
		return 0, fmt.Errorf("revokeCosmeticSubscriptionLicenses lock: %w", err)
	}
	licenseIDs := make([]string, 0)
	for rows.Next() {
		var licenseID string
		if err := rows.Scan(&licenseID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("revokeCosmeticSubscriptionLicenses scan: %w", err)
		}
		licenseIDs = append(licenseIDs, licenseID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("revokeCosmeticSubscriptionLicenses rows: %w", err)
	}
	rows.Close()
	changed, err := applyCosmeticLicenseTerminalBatchTx(
		ctx, tx, licenseIDs, "revoked", "stripe_subscription", "subscription_ended", subscriptionID,
	)
	if err != nil {
		return 0, err
	}
	return changed, nil
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
