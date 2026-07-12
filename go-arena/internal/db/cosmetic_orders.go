package db

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	CosmeticOrderStatusCreated       = "created"
	CosmeticOrderStatusCheckout      = "checkout_pending"
	CosmeticOrderStatusProcessing    = "processing"
	CosmeticOrderStatusPaid          = "paid"
	CosmeticOrderStatusPaymentFailed = "payment_failed"
	CosmeticOrderStatusExpired       = "expired"
	CosmeticOrderStatusRefundReview  = "refund_review"
	CosmeticOrderStatusRefunded      = "refunded"
	CosmeticOrderStatusDisputed      = "disputed"

	CosmeticStripeCheckoutCompleted             = "checkout.session.completed"
	CosmeticStripeCheckoutAsyncPaymentSucceeded = "checkout.session.async_payment_succeeded"
	CosmeticStripeCheckoutAsyncPaymentFailed    = "checkout.session.async_payment_failed"
	CosmeticStripeCheckoutExpired               = "checkout.session.expired"
	CosmeticStripeRefundCreated                 = "refund.created"
	CosmeticStripeRefundUpdated                 = "refund.updated"
	CosmeticStripeRefundFailed                  = "refund.failed"
	CosmeticStripeChargeRefunded                = "charge.refunded"
	CosmeticStripeDisputeCreated                = "charge.dispute.created"

	CosmeticRefundStatusPending        = "pending"
	CosmeticRefundStatusRequiresAction = "requires_action"
	CosmeticRefundStatusSucceeded      = "succeeded"
	CosmeticRefundStatusFailed         = "failed"
	CosmeticRefundStatusCanceled       = "canceled"
)

var (
	ErrCosmeticOrderNotFound         = errors.New("cosmetic order not found")
	ErrCosmeticOrderQuantity         = errors.New("cosmetic order quantity must be between 1 and 10")
	ErrCosmeticOrderPackUnavailable  = errors.New("cosmetic pack is unavailable for purchase")
	ErrCosmeticOrderMismatch         = errors.New("cosmetic payment does not match the order")
	ErrCosmeticOrderTerminal         = errors.New("cosmetic order is terminal")
	ErrCosmeticPaymentEventInvalid   = errors.New("invalid cosmetic payment event")
	ErrCosmeticPaymentEventRetryable = errors.New("cosmetic payment event should be retried")
	ErrCosmeticPaymentEventConflict  = errors.New("cosmetic payment event payload conflicts with an existing event")
	ErrCosmeticPaymentEventRejected  = errors.New("cosmetic payment event was previously rejected")
)

var cosmeticPaymentHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// CosmeticOrder is a server-owned purchase snapshot. Catalog edits after this
// row is created never change the price or items presented to the processor.
type CosmeticOrder struct {
	ID                    string              `json:"id"`
	AccountID             string              `json:"account_id"`
	AccountEmail          string              `json:"account_email"`
	PackID                string              `json:"pack_id"`
	PackName              string              `json:"pack_name"`
	PackDescription       string              `json:"pack_description"`
	UnitPriceCents        int64               `json:"unit_price_cents"`
	Quantity              int                 `json:"quantity"`
	ExpectedSubtotalCents int64               `json:"expected_subtotal_cents"`
	AmountReceivedCents   int64               `json:"amount_received_cents"`
	AmountRefundedCents   int64               `json:"amount_refunded_cents"`
	Currency              string              `json:"currency"`
	Status                string              `json:"status"`
	CheckoutSessionID     string              `json:"checkout_session_id,omitempty"`
	PaymentIntentID       string              `json:"payment_intent_id,omitempty"`
	LastError             string              `json:"last_error,omitempty"`
	CreatedAt             time.Time           `json:"created_at"`
	UpdatedAt             time.Time           `json:"updated_at"`
	PaidAt                *time.Time          `json:"paid_at,omitempty"`
	TerminalAt            *time.Time          `json:"terminal_at,omitempty"`
	Items                 []CosmeticOrderItem `json:"items"`
	FulfilledLicenseCount int                 `json:"fulfilled_license_count"`
}

type CosmeticOrderItem struct {
	Position int    `json:"position"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slot     string `json:"slot"`
	AssetKey string `json:"asset_key"`
	Rarity   string `json:"rarity"`
}

// CosmeticPaymentEventInput is the already signature-verified, minimal event
// projection accepted from the HTTP layer. The raw webhook body is never
// passed to or retained by the database package; PayloadHash is its SHA-256.
type CosmeticPaymentEventInput struct {
	Provider                string `json:"provider"`
	EventID                 string `json:"event_id"`
	EventType               string `json:"event_type"`
	PayloadHash             string `json:"payload_hash"`
	OrderID                 string `json:"order_id"`
	AccountID               string `json:"account_id"`
	CheckoutSessionID       string `json:"checkout_session_id,omitempty"`
	PaymentIntentID         string `json:"payment_intent_id,omitempty"`
	Currency                string `json:"currency,omitempty"`
	Paid                    bool   `json:"paid"`
	AmountReceivedCents     int64  `json:"amount_received_cents,omitempty"`
	RefundID                string `json:"refund_id,omitempty"`
	RefundStatus            string `json:"refund_status,omitempty"`
	RefundAmountCents       int64  `json:"refund_amount_cents,omitempty"`
	CumulativeRefundedCents int64  `json:"cumulative_refunded_cents,omitempty"`
	FailureMessage          string `json:"failure_message,omitempty"`
}

type CosmeticPaymentEventResult struct {
	Order           *CosmeticOrder `json:"order"`
	Applied         bool           `json:"applied"`
	Duplicate       bool           `json:"duplicate"`
	LicensesCreated int            `json:"licenses_created"`
}

// EnsureCosmeticOrdersSchema creates the payment ledger after the base
// cosmetics schema. Every table is provider-neutral even though the launch
// event adapter is Stripe-specific.
func EnsureCosmeticOrdersSchema(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("EnsureCosmeticOrdersSchema begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(2026071101::BIGINT)`); err != nil {
		return fmt.Errorf("EnsureCosmeticOrdersSchema migration lock: %w", err)
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS cosmetic_orders (
			id TEXT PRIMARY KEY,
			account_id TEXT NOT NULL REFERENCES customer_accounts(id) ON DELETE RESTRICT,
			account_email TEXT NOT NULL,
			pack_id TEXT NOT NULL REFERENCES cosmetic_packs(id) ON DELETE RESTRICT,
			pack_name TEXT NOT NULL,
			pack_description TEXT NOT NULL DEFAULT '',
			unit_price_cents BIGINT NOT NULL CHECK (unit_price_cents > 0),
			quantity SMALLINT NOT NULL CHECK (quantity BETWEEN 1 AND 10),
			expected_subtotal_cents BIGINT NOT NULL CHECK (expected_subtotal_cents = unit_price_cents * quantity),
			amount_received_cents BIGINT NOT NULL DEFAULT 0 CHECK (amount_received_cents >= 0),
			amount_refunded_cents BIGINT NOT NULL DEFAULT 0 CHECK (amount_refunded_cents >= 0),
			cumulative_charge_refunded_cents BIGINT NOT NULL DEFAULT 0 CHECK (cumulative_charge_refunded_cents >= 0),
			currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
			status TEXT NOT NULL DEFAULT 'created' CHECK (status IN (
				'created','checkout_pending','processing','paid','payment_failed','expired','refund_review','refunded','disputed'
			)),
			stripe_checkout_session_id TEXT,
			stripe_payment_intent_id TEXT,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			paid_at TIMESTAMPTZ,
			terminal_at TIMESTAMPTZ
		)`,
		`ALTER TABLE cosmetic_orders ADD COLUMN IF NOT EXISTS cumulative_charge_refunded_cents BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE cosmetic_orders ADD COLUMN IF NOT EXISTS pack_description TEXT NOT NULL DEFAULT ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cosmetic_orders_checkout_session
			ON cosmetic_orders (stripe_checkout_session_id) WHERE stripe_checkout_session_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cosmetic_orders_payment_intent
			ON cosmetic_orders (stripe_payment_intent_id) WHERE stripe_payment_intent_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_cosmetic_orders_account_created
			ON cosmetic_orders (account_id, created_at DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_cosmetic_orders_status_created
			ON cosmetic_orders (status, created_at DESC, id DESC)`,
		`CREATE TABLE IF NOT EXISTS cosmetic_order_items (
			order_id TEXT NOT NULL REFERENCES cosmetic_orders(id) ON DELETE CASCADE,
			position INT NOT NULL CHECK (position >= 0),
			item_id TEXT NOT NULL REFERENCES cosmetic_items(id) ON DELETE RESTRICT,
			item_name TEXT NOT NULL,
			slot TEXT NOT NULL CHECK (slot IN ('bot_skin','weapon_skin','attachment')),
			asset_key TEXT NOT NULL,
			rarity TEXT NOT NULL,
			PRIMARY KEY (order_id, position),
			UNIQUE (order_id, item_id),
			UNIQUE (order_id, position, item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS cosmetic_order_licenses (
			order_id TEXT NOT NULL REFERENCES cosmetic_orders(id) ON DELETE RESTRICT,
			copy_index SMALLINT NOT NULL CHECK (copy_index BETWEEN 1 AND 10),
			item_position INT NOT NULL CHECK (item_position >= 0),
			item_id TEXT NOT NULL,
			license_id TEXT NOT NULL UNIQUE REFERENCES cosmetic_licenses(id) ON DELETE RESTRICT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (order_id, copy_index, item_position),
			FOREIGN KEY (order_id, item_position, item_id)
				REFERENCES cosmetic_order_items(order_id, position, item_id) ON DELETE RESTRICT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cosmetic_order_licenses_item
			ON cosmetic_order_licenses (item_id, order_id)`,
		`CREATE TABLE IF NOT EXISTS cosmetic_payment_events (
			provider TEXT NOT NULL,
			event_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			payload_hash TEXT NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
			order_id TEXT NOT NULL REFERENCES cosmetic_orders(id) ON DELETE RESTRICT,
			status TEXT NOT NULL DEFAULT 'processing' CHECK (status IN ('processing','processed','rejected')),
			last_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			processed_at TIMESTAMPTZ,
			PRIMARY KEY (provider, event_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cosmetic_payment_events_order
			ON cosmetic_payment_events (order_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS cosmetic_order_refunds (
			provider TEXT NOT NULL,
			refund_id TEXT NOT NULL,
			order_id TEXT NOT NULL REFERENCES cosmetic_orders(id) ON DELETE RESTRICT,
			status TEXT NOT NULL CHECK (status IN ('pending','requires_action','succeeded','failed','canceled')),
			amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
			currency TEXT NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
			last_event_id TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (provider, refund_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cosmetic_order_refunds_order
			ON cosmetic_order_refunds (order_id, status, created_at)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("EnsureCosmeticOrdersSchema exec: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("EnsureCosmeticOrdersSchema commit: %w", err)
	}
	return nil
}

type cosmeticOrderScanner interface {
	Scan(dest ...any) error
}

type cosmeticOrderQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func cosmeticOrderSelect() string {
	return `SELECT o.id, o.account_id, o.account_email, o.pack_id, o.pack_name, o.pack_description,
		o.unit_price_cents, o.quantity, o.expected_subtotal_cents,
		o.amount_received_cents, o.amount_refunded_cents, o.currency, o.status,
		COALESCE(o.stripe_checkout_session_id, ''), COALESCE(o.stripe_payment_intent_id, ''),
		o.last_error, o.created_at, o.updated_at, o.paid_at, o.terminal_at,
		(SELECT COUNT(*) FROM cosmetic_order_licenses ol WHERE ol.order_id = o.id)
		FROM cosmetic_orders o`
}

func scanCosmeticOrder(row cosmeticOrderScanner) (*CosmeticOrder, error) {
	var order CosmeticOrder
	err := row.Scan(&order.ID, &order.AccountID, &order.AccountEmail, &order.PackID, &order.PackName, &order.PackDescription,
		&order.UnitPriceCents, &order.Quantity, &order.ExpectedSubtotalCents,
		&order.AmountReceivedCents, &order.AmountRefundedCents, &order.Currency, &order.Status,
		&order.CheckoutSessionID, &order.PaymentIntentID, &order.LastError,
		&order.CreatedAt, &order.UpdatedAt, &order.PaidAt, &order.TerminalAt,
		&order.FulfilledLicenseCount)
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func loadCosmeticOrderItems(ctx context.Context, q cosmeticOrderQuerier, order *CosmeticOrder) error {
	rows, err := q.Query(ctx, `
		SELECT position, item_id, item_name, slot, asset_key, rarity
		FROM cosmetic_order_items WHERE order_id = $1 ORDER BY position`, order.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	order.Items = make([]CosmeticOrderItem, 0)
	for rows.Next() {
		var item CosmeticOrderItem
		if err := rows.Scan(&item.Position, &item.ID, &item.Name, &item.Slot, &item.AssetKey, &item.Rarity); err != nil {
			return err
		}
		order.Items = append(order.Items, item)
	}
	return rows.Err()
}

func loadCosmeticOrder(ctx context.Context, q cosmeticOrderQuerier, orderID string, forUpdate bool) (*CosmeticOrder, error) {
	query := cosmeticOrderSelect() + ` WHERE o.id = $1`
	if forUpdate {
		query += ` FOR UPDATE OF o`
	}
	order, err := scanCosmeticOrder(q.QueryRow(ctx, query, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCosmeticOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := loadCosmeticOrderItems(ctx, q, order); err != nil {
		return nil, err
	}
	return order, nil
}

// CreateCosmeticOrder snapshots a verified account, an available non-free
// pack, its server-side price, and every active member item in one transaction.
func CreateCosmeticOrder(ctx context.Context, accountID, packID string, quantity int) (*CosmeticOrder, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if quantity < 1 || quantity > 10 {
		return nil, ErrCosmeticOrderQuantity
	}
	packID = strings.TrimSpace(packID)
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("CreateCosmeticOrder begin: %w", err)
	}
	defer tx.Rollback(ctx)
	account, err := lockCustomerAccount(ctx, tx, strings.TrimSpace(accountID), true)
	if err != nil {
		return nil, err
	}

	var packName, packDescription, currency string
	var price int64
	var packFree, packPurchasable, packActive, categoryActive bool
	err = tx.QueryRow(ctx, `
		SELECT p.name, p.description, p.price_cents, p.currency, p.is_free, p.is_purchasable, p.is_active, c.is_active
		FROM cosmetic_packs p
		JOIN cosmetic_categories c ON c.id = p.category_id
		WHERE p.id = $1
		FOR SHARE OF p, c`, packID).
		Scan(&packName, &packDescription, &price, &currency, &packFree, &packPurchasable, &packActive, &categoryActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCosmeticOrderPackUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("CreateCosmeticOrder pack: %w", err)
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if packFree || !packPurchasable || !packActive || !categoryActive || price <= 0 || currency != "USD" {
		return nil, ErrCosmeticOrderPackUnavailable
	}

	rows, err := tx.Query(ctx, `
		SELECT i.id, i.name, i.slot, i.asset_key, i.rarity, i.is_active, c.is_active
		FROM cosmetic_pack_items pi
		JOIN cosmetic_items i ON i.id = pi.item_id
		JOIN cosmetic_categories c ON c.id = i.category_id
		WHERE pi.pack_id = $1
		ORDER BY pi.sort_order, i.sort_order, i.id
		FOR SHARE OF pi, i, c`, packID)
	if err != nil {
		return nil, fmt.Errorf("CreateCosmeticOrder items: %w", err)
	}
	items := make([]CosmeticOrderItem, 0)
	for rows.Next() {
		var item CosmeticOrderItem
		var itemActive, itemCategoryActive bool
		item.Position = len(items)
		if err := rows.Scan(&item.ID, &item.Name, &item.Slot, &item.AssetKey, &item.Rarity, &itemActive, &itemCategoryActive); err != nil {
			rows.Close()
			return nil, fmt.Errorf("CreateCosmeticOrder item scan: %w", err)
		}
		if !itemActive || !itemCategoryActive || !IsValidCosmeticSlot(item.Slot) {
			rows.Close()
			return nil, ErrCosmeticOrderPackUnavailable
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("CreateCosmeticOrder item rows: %w", err)
	}
	if len(items) == 0 {
		return nil, ErrCosmeticOrderPackUnavailable
	}

	orderID := uuid.NewString()
	subtotal := price * int64(quantity)
	_, err = tx.Exec(ctx, `
		INSERT INTO cosmetic_orders
			(id, account_id, account_email, pack_id, pack_name, pack_description, unit_price_cents, quantity,
			 expected_subtotal_cents, currency, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW(),NOW())`,
		orderID, account.ID, account.Email, packID, packName, packDescription, price, quantity, subtotal, currency, CosmeticOrderStatusCreated)
	if err != nil {
		return nil, fmt.Errorf("CreateCosmeticOrder insert: %w", err)
	}
	for _, item := range items {
		_, err = tx.Exec(ctx, `
			INSERT INTO cosmetic_order_items
				(order_id, position, item_id, item_name, slot, asset_key, rarity)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			orderID, item.Position, item.ID, item.Name, item.Slot, item.AssetKey, item.Rarity)
		if err != nil {
			return nil, fmt.Errorf("CreateCosmeticOrder snapshot item %s: %w", item.ID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("CreateCosmeticOrder commit: %w", err)
	}
	order, err := loadCosmeticOrder(ctx, Pool, orderID, false)
	if err != nil {
		return nil, fmt.Errorf("CreateCosmeticOrder load: %w", err)
	}
	return order, nil
}

func AttachCosmeticOrderCheckout(ctx context.Context, accountID, orderID, checkoutSessionID string) (*CosmeticOrder, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	checkoutSessionID = strings.TrimSpace(checkoutSessionID)
	if checkoutSessionID == "" {
		return nil, ErrCosmeticOrderMismatch
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("AttachCosmeticOrderCheckout begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := lockCustomerAccount(ctx, tx, accountID, true); err != nil {
		return nil, err
	}
	order, err := loadCosmeticOrder(ctx, tx, orderID, true)
	if err != nil {
		return nil, err
	}
	if order.AccountID != accountID {
		return nil, ErrCosmeticOrderMismatch
	}
	if order.Status == CosmeticOrderStatusRefunded || order.Status == CosmeticOrderStatusDisputed {
		return nil, ErrCosmeticOrderTerminal
	}
	if order.CheckoutSessionID != "" && order.CheckoutSessionID != checkoutSessionID {
		return nil, ErrCosmeticOrderMismatch
	}
	if order.CheckoutSessionID == "" {
		_, err = tx.Exec(ctx, `
			UPDATE cosmetic_orders SET stripe_checkout_session_id = $2,
				status = 'checkout_pending', last_error = '', updated_at = NOW()
			WHERE id = $1`, orderID, checkoutSessionID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return nil, ErrCosmeticOrderMismatch
			}
			return nil, fmt.Errorf("AttachCosmeticOrderCheckout update: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("AttachCosmeticOrderCheckout commit: %w", err)
	}
	return loadCosmeticOrder(ctx, Pool, orderID, false)
}

func MarkCosmeticOrderCheckoutFailed(ctx context.Context, accountID, orderID, message string) (*CosmeticOrder, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("MarkCosmeticOrderCheckoutFailed begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := lockCustomerAccount(ctx, tx, accountID, true); err != nil {
		return nil, err
	}
	order, err := loadCosmeticOrder(ctx, tx, orderID, true)
	if err != nil {
		return nil, err
	}
	if order.AccountID != accountID {
		return nil, ErrCosmeticOrderMismatch
	}
	if order.Status != CosmeticOrderStatusPaid && order.Status != CosmeticOrderStatusRefundReview &&
		order.Status != CosmeticOrderStatusRefunded && order.Status != CosmeticOrderStatusDisputed {
		_, err = tx.Exec(ctx, `UPDATE cosmetic_orders
			SET status = 'payment_failed', last_error = $2, updated_at = NOW() WHERE id = $1`,
			orderID, truncateCosmeticOrderError(message))
		if err != nil {
			return nil, fmt.Errorf("MarkCosmeticOrderCheckoutFailed update: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("MarkCosmeticOrderCheckoutFailed commit: %w", err)
	}
	return loadCosmeticOrder(ctx, Pool, orderID, false)
}

func truncateCosmeticOrderError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 1000 {
		return message[:1000]
	}
	return message
}

func ListCustomerCosmeticOrders(ctx context.Context, accountID string, limit int) ([]CosmeticOrder, error) {
	return listCosmeticOrders(ctx, `o.account_id = $1`, []any{strings.TrimSpace(accountID)}, limit)
}

func ListAdminCosmeticOrders(ctx context.Context, query, status string, limit int) ([]CosmeticOrder, error) {
	query = strings.TrimSpace(query)
	status = strings.TrimSpace(status)
	return listCosmeticOrders(ctx, `
		($1 = '' OR o.id ILIKE '%' || $1 || '%' OR o.account_email ILIKE '%' || $1 || '%'
		 OR o.pack_id ILIKE '%' || $1 || '%' OR o.pack_name ILIKE '%' || $1 || '%'
		 OR COALESCE(o.stripe_checkout_session_id, '') ILIKE '%' || $1 || '%'
		 OR COALESCE(o.stripe_payment_intent_id, '') ILIKE '%' || $1 || '%')
		AND ($2 = '' OR o.status = $2)`, []any{query, status}, limit)
}

func listCosmeticOrders(ctx context.Context, predicate string, args []any, limit int) ([]CosmeticOrder, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	args = append(args, limit)
	rows, err := Pool.Query(ctx, cosmeticOrderSelect()+` WHERE `+predicate+
		` ORDER BY o.created_at DESC, o.id DESC LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("listCosmeticOrders query: %w", err)
	}
	orders := make([]CosmeticOrder, 0)
	for rows.Next() {
		order, err := scanCosmeticOrder(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("listCosmeticOrders scan: %w", err)
		}
		orders = append(orders, *order)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("listCosmeticOrders rows: %w", err)
	}
	for index := range orders {
		if err := loadCosmeticOrderItems(ctx, Pool, &orders[index]); err != nil {
			return nil, fmt.Errorf("listCosmeticOrders items: %w", err)
		}
	}
	return orders, nil
}

// ProcessCosmeticPaymentEvent applies one signature-verified provider event.
// Event insertion, order transition, license mutation, and idempotency marker
// commit together, so a crash can neither lose a paid grant nor double-grant it.
func ProcessCosmeticPaymentEvent(ctx context.Context, raw CosmeticPaymentEventInput) (*CosmeticPaymentEventResult, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	input := normalizeCosmeticPaymentEvent(raw)
	if err := validateCosmeticPaymentEventEnvelope(input); err != nil {
		return nil, err
	}
	var err error
	input, err = resolveCosmeticReversalOrder(ctx, input)
	if err != nil {
		return nil, err
	}

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("ProcessCosmeticPaymentEvent begin: %w", err)
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		INSERT INTO cosmetic_payment_events
			(provider, event_id, event_type, payload_hash, order_id, status, created_at)
		VALUES ($1,$2,$3,$4,$5,'processing',NOW())
		ON CONFLICT (provider, event_id) DO NOTHING`,
		input.Provider, input.EventID, input.EventType, input.PayloadHash, input.OrderID)
	if err != nil {
		return nil, fmt.Errorf("ProcessCosmeticPaymentEvent record: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var eventType, payloadHash, orderID, status, lastError string
		err := tx.QueryRow(ctx, `
			SELECT event_type, payload_hash, order_id, status, last_error
			FROM cosmetic_payment_events WHERE provider = $1 AND event_id = $2`,
			input.Provider, input.EventID).
			Scan(&eventType, &payloadHash, &orderID, &status, &lastError)
		if err != nil {
			return nil, fmt.Errorf("ProcessCosmeticPaymentEvent duplicate: %w", err)
		}
		if eventType != input.EventType || payloadHash != input.PayloadHash || orderID != input.OrderID {
			return nil, ErrCosmeticPaymentEventConflict
		}
		if status == "rejected" {
			return nil, fmt.Errorf("%w: %s", ErrCosmeticPaymentEventRejected, lastError)
		}
		if status != "processed" {
			return nil, fmt.Errorf("%w: event is still processing", ErrCosmeticPaymentEventInvalid)
		}
		if err := tx.Rollback(ctx); err != nil {
			return nil, fmt.Errorf("ProcessCosmeticPaymentEvent duplicate rollback: %w", err)
		}
		order, err := loadCosmeticOrder(ctx, Pool, orderID, false)
		if err != nil {
			return nil, fmt.Errorf("ProcessCosmeticPaymentEvent duplicate order: %w", err)
		}
		return &CosmeticPaymentEventResult{Order: order, Duplicate: true}, nil
	}

	if _, err := lockCustomerAccount(ctx, tx, input.AccountID, false); err != nil {
		if errors.Is(err, ErrCustomerAccountNotFound) {
			return rejectCosmeticPaymentEvent(ctx, tx, input, ErrCosmeticOrderMismatch)
		}
		return nil, err
	}
	order, err := loadCosmeticOrder(ctx, tx, input.OrderID, true)
	if err != nil {
		if errors.Is(err, ErrCosmeticOrderNotFound) {
			return rejectCosmeticPaymentEvent(ctx, tx, input, err)
		}
		return nil, fmt.Errorf("ProcessCosmeticPaymentEvent order: %w", err)
	}
	if order.AccountID != input.AccountID {
		return rejectCosmeticPaymentEvent(ctx, tx, input, ErrCosmeticOrderMismatch)
	}

	result := &CosmeticPaymentEventResult{}
	switch input.EventType {
	case CosmeticStripeCheckoutCompleted, CosmeticStripeCheckoutAsyncPaymentSucceeded:
		result.Applied, result.LicensesCreated, err = applyCosmeticPaidEvent(ctx, tx, order, input)
	case CosmeticStripeCheckoutAsyncPaymentFailed, CosmeticStripeCheckoutExpired:
		result.Applied, err = applyCosmeticCheckoutFailure(ctx, tx, order, input)
	case CosmeticStripeRefundCreated, CosmeticStripeRefundUpdated, CosmeticStripeRefundFailed:
		result.Applied, err = applyCosmeticRefundEvent(ctx, tx, order, input)
	case CosmeticStripeChargeRefunded:
		result.Applied, err = applyCosmeticChargeRefundEvent(ctx, tx, order, input)
	case CosmeticStripeDisputeCreated:
		result.Applied, err = applyCosmeticDisputeEvent(ctx, tx, order, input)
	default:
		err = ErrCosmeticPaymentEventInvalid
	}
	if err != nil {
		if errors.Is(err, ErrCosmeticPaymentEventRetryable) {
			return nil, err
		}
		if isCosmeticPaymentDomainError(err) {
			return rejectCosmeticPaymentEvent(ctx, tx, input, err)
		}
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_payment_events
		SET status = 'processed', last_error = '', processed_at = NOW()
		WHERE provider = $1 AND event_id = $2`, input.Provider, input.EventID); err != nil {
		return nil, fmt.Errorf("ProcessCosmeticPaymentEvent complete event: %w", err)
	}
	result.Order, err = loadCosmeticOrder(ctx, tx, input.OrderID, false)
	if err != nil {
		return nil, fmt.Errorf("ProcessCosmeticPaymentEvent reload: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("ProcessCosmeticPaymentEvent commit: %w", err)
	}
	return result, nil
}

func normalizeCosmeticPaymentEvent(input CosmeticPaymentEventInput) CosmeticPaymentEventInput {
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.EventID = strings.TrimSpace(input.EventID)
	input.EventType = strings.TrimSpace(input.EventType)
	input.PayloadHash = strings.ToLower(strings.TrimSpace(input.PayloadHash))
	input.OrderID = strings.TrimSpace(input.OrderID)
	input.AccountID = strings.TrimSpace(input.AccountID)
	input.CheckoutSessionID = strings.TrimSpace(input.CheckoutSessionID)
	input.PaymentIntentID = strings.TrimSpace(input.PaymentIntentID)
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	input.RefundID = strings.TrimSpace(input.RefundID)
	input.RefundStatus = strings.ToLower(strings.TrimSpace(input.RefundStatus))
	input.FailureMessage = truncateCosmeticOrderError(input.FailureMessage)
	return input
}

func validateCosmeticPaymentEventEnvelope(input CosmeticPaymentEventInput) error {
	if input.Provider != "stripe" || input.EventID == "" || len(input.EventID) > 255 ||
		!cosmeticPaymentHashPattern.MatchString(input.PayloadHash) {
		return ErrCosmeticPaymentEventInvalid
	}
	switch input.EventType {
	case CosmeticStripeCheckoutCompleted, CosmeticStripeCheckoutAsyncPaymentSucceeded,
		CosmeticStripeCheckoutAsyncPaymentFailed, CosmeticStripeCheckoutExpired,
		CosmeticStripeRefundCreated, CosmeticStripeRefundUpdated, CosmeticStripeRefundFailed,
		CosmeticStripeChargeRefunded, CosmeticStripeDisputeCreated:
		return nil
	default:
		return ErrCosmeticPaymentEventInvalid
	}
}

func isCosmeticReversalEvent(eventType string) bool {
	return eventType == CosmeticStripeRefundCreated || eventType == CosmeticStripeRefundUpdated ||
		eventType == CosmeticStripeRefundFailed || eventType == CosmeticStripeChargeRefunded ||
		eventType == CosmeticStripeDisputeCreated
}

// Stripe does not normally copy Checkout metadata onto Refund objects. A
// reversal therefore resolves by the unique PaymentIntent first; any supplied
// order/account metadata is treated as an additional fail-closed assertion.
func resolveCosmeticReversalOrder(ctx context.Context, input CosmeticPaymentEventInput) (CosmeticPaymentEventInput, error) {
	if !isCosmeticReversalEvent(input.EventType) {
		if input.OrderID == "" || input.AccountID == "" {
			return input, ErrCosmeticOrderMismatch
		}
		return input, nil
	}
	if input.PaymentIntentID == "" {
		return input, ErrCosmeticOrderMismatch
	}
	var orderID, accountID string
	err := Pool.QueryRow(ctx, `
		SELECT id, account_id FROM cosmetic_orders WHERE stripe_payment_intent_id = $1`,
		input.PaymentIntentID).Scan(&orderID, &accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return input, ErrCosmeticOrderMismatch
	}
	if err != nil {
		return input, fmt.Errorf("resolveCosmeticReversalOrder: %w", err)
	}
	if (input.OrderID != "" && input.OrderID != orderID) ||
		(input.AccountID != "" && input.AccountID != accountID) {
		return input, ErrCosmeticOrderMismatch
	}
	input.OrderID = orderID
	input.AccountID = accountID
	return input, nil
}

func isCosmeticPaymentDomainError(err error) bool {
	return errors.Is(err, ErrCosmeticOrderMismatch) || errors.Is(err, ErrCosmeticOrderTerminal) ||
		errors.Is(err, ErrCosmeticOrderNotFound) || errors.Is(err, ErrCosmeticPaymentEventInvalid)
}

func rejectCosmeticPaymentEvent(ctx context.Context, tx pgx.Tx, input CosmeticPaymentEventInput, rejection error) (*CosmeticPaymentEventResult, error) {
	message := truncateCosmeticOrderError(rejection.Error())
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_payment_events
		SET status = 'rejected', last_error = $3, processed_at = NOW()
		WHERE provider = $1 AND event_id = $2`, input.Provider, input.EventID, message); err != nil {
		return nil, fmt.Errorf("rejectCosmeticPaymentEvent record: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("rejectCosmeticPaymentEvent commit: %w", err)
	}
	return nil, rejection
}

func validateCosmeticCheckoutClaims(order *CosmeticOrder, input CosmeticPaymentEventInput) error {
	if input.AccountID != order.AccountID || input.CheckoutSessionID == "" ||
		input.CheckoutSessionID != order.CheckoutSessionID || input.Currency != order.Currency {
		return ErrCosmeticOrderMismatch
	}
	if order.PaymentIntentID != "" && input.PaymentIntentID != "" && input.PaymentIntentID != order.PaymentIntentID {
		return ErrCosmeticOrderMismatch
	}
	return nil
}

func validateCosmeticReversalClaims(order *CosmeticOrder, input CosmeticPaymentEventInput) error {
	if input.AccountID != order.AccountID || input.OrderID != order.ID || input.Currency != order.Currency ||
		input.PaymentIntentID == "" || order.PaymentIntentID == "" || input.PaymentIntentID != order.PaymentIntentID {
		return ErrCosmeticOrderMismatch
	}
	if order.AmountReceivedCents <= 0 {
		return ErrCosmeticPaymentEventRetryable
	}
	return nil
}

func applyCosmeticPaidEvent(ctx context.Context, tx pgx.Tx, order *CosmeticOrder, input CosmeticPaymentEventInput) (bool, int, error) {
	if order.Status == CosmeticOrderStatusRefunded || order.Status == CosmeticOrderStatusDisputed {
		return false, 0, ErrCosmeticOrderTerminal
	}
	if err := validateCosmeticCheckoutClaims(order, input); err != nil {
		return false, 0, err
	}

	// Delayed methods legitimately send checkout.session.completed while still
	// unpaid. Record processing without granting; async_payment_succeeded is the
	// only later unpaid-capable event and must itself carry a paid assertion.
	if input.EventType == CosmeticStripeCheckoutCompleted && !input.Paid {
		if input.AmountReceivedCents != 0 {
			return false, 0, ErrCosmeticOrderMismatch
		}
		if order.Status == CosmeticOrderStatusPaid || order.Status == CosmeticOrderStatusRefundReview {
			return false, 0, nil
		}
		_, err := tx.Exec(ctx, `
			UPDATE cosmetic_orders
			SET status = 'processing',
				stripe_payment_intent_id = COALESCE(stripe_payment_intent_id, NULLIF($2, '')),
				last_error = '', updated_at = NOW()
			WHERE id = $1`, order.ID, input.PaymentIntentID)
		if err != nil {
			return false, 0, fmt.Errorf("applyCosmeticPaidEvent processing: %w", err)
		}
		return order.Status != CosmeticOrderStatusProcessing ||
			(order.PaymentIntentID == "" && input.PaymentIntentID != ""), 0, nil
	}
	if !input.Paid || input.PaymentIntentID == "" || input.AmountReceivedCents < order.ExpectedSubtotalCents {
		return false, 0, ErrCosmeticOrderMismatch
	}
	if order.PaymentIntentID != "" && order.PaymentIntentID != input.PaymentIntentID {
		return false, 0, ErrCosmeticOrderMismatch
	}
	var otherOrder string
	err := tx.QueryRow(ctx, `
		SELECT id FROM cosmetic_orders
		WHERE stripe_payment_intent_id = $1 AND id <> $2`, input.PaymentIntentID, order.ID).Scan(&otherOrder)
	if err == nil || (!errors.Is(err, pgx.ErrNoRows) && err != nil) {
		if err == nil {
			return false, 0, ErrCosmeticOrderMismatch
		}
		return false, 0, fmt.Errorf("applyCosmeticPaidEvent payment intent: %w", err)
	}

	status := CosmeticOrderStatusPaid
	if order.Status == CosmeticOrderStatusRefundReview {
		status = CosmeticOrderStatusRefundReview
	}
	_, err = tx.Exec(ctx, `
		UPDATE cosmetic_orders
		SET stripe_payment_intent_id = COALESCE(stripe_payment_intent_id, $2),
			amount_received_cents = GREATEST(amount_received_cents, $3),
			status = $4, paid_at = COALESCE(paid_at, NOW()), last_error = '', updated_at = NOW()
		WHERE id = $1`, order.ID, input.PaymentIntentID, input.AmountReceivedCents, status)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return false, 0, ErrCosmeticOrderMismatch
		}
		return false, 0, fmt.Errorf("applyCosmeticPaidEvent update: %w", err)
	}
	created, err := fulfillCosmeticOrderLicenses(ctx, tx, order)
	if err != nil {
		return false, 0, err
	}
	applied := created > 0 || order.Status != status || order.PaymentIntentID == "" ||
		input.AmountReceivedCents > order.AmountReceivedCents
	return applied, created, nil
}

func fulfillCosmeticOrderLicenses(ctx context.Context, tx pgx.Tx, order *CosmeticOrder) (int, error) {
	expected := len(order.Items) * order.Quantity
	if expected <= 0 {
		return 0, ErrCosmeticOrderMismatch
	}
	var existing int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM cosmetic_order_licenses WHERE order_id = $1`, order.ID).Scan(&existing); err != nil {
		return 0, fmt.Errorf("fulfillCosmeticOrderLicenses count: %w", err)
	}
	if existing > expected {
		return 0, ErrCosmeticOrderMismatch
	}
	created := 0
	for copyIndex := 1; copyIndex <= order.Quantity; copyIndex++ {
		for _, item := range order.Items {
			var mappedID string
			err := tx.QueryRow(ctx, `
				SELECT license_id FROM cosmetic_order_licenses
				WHERE order_id = $1 AND copy_index = $2 AND item_position = $3`,
				order.ID, copyIndex, item.Position).Scan(&mappedID)
			if err == nil {
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return 0, fmt.Errorf("fulfillCosmeticOrderLicenses mapping: %w", err)
			}
			licenseID := uuid.NewString()
			externalReference := fmt.Sprintf("cosmetic-order:%s:copy:%02d:item:%03d", order.ID, copyIndex, item.Position)
			_, err = tx.Exec(ctx, `
				INSERT INTO cosmetic_licenses
					(id, account_id, cosmetic_id, status, source, external_reference, granted_at, updated_at)
				VALUES ($1,$2,$3,'active','stripe',$4,NOW(),NOW())`,
				licenseID, order.AccountID, item.ID, externalReference)
			if err != nil {
				return 0, fmt.Errorf("fulfillCosmeticOrderLicenses license: %w", err)
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO cosmetic_order_licenses
					(order_id, copy_index, item_position, item_id, license_id, created_at)
				VALUES ($1,$2,$3,$4,$5,NOW())`,
				order.ID, copyIndex, item.Position, item.ID, licenseID)
			if err != nil {
				return 0, fmt.Errorf("fulfillCosmeticOrderLicenses order mapping: %w", err)
			}
			created++
		}
	}
	return created, nil
}

func applyCosmeticCheckoutFailure(ctx context.Context, tx pgx.Tx, order *CosmeticOrder, input CosmeticPaymentEventInput) (bool, error) {
	if err := validateCosmeticCheckoutClaims(order, input); err != nil {
		return false, err
	}
	if order.Status == CosmeticOrderStatusPaid || order.Status == CosmeticOrderStatusRefundReview ||
		order.Status == CosmeticOrderStatusRefunded || order.Status == CosmeticOrderStatusDisputed {
		return false, nil
	}
	status := CosmeticOrderStatusPaymentFailed
	if input.EventType == CosmeticStripeCheckoutExpired {
		status = CosmeticOrderStatusExpired
	}
	message := input.FailureMessage
	if message == "" && status == CosmeticOrderStatusExpired {
		message = "checkout session expired"
	}
	_, err := tx.Exec(ctx, `
		UPDATE cosmetic_orders SET status = $2, last_error = $3, updated_at = NOW()
		WHERE id = $1`, order.ID, status, message)
	if err != nil {
		return false, fmt.Errorf("applyCosmeticCheckoutFailure: %w", err)
	}
	return order.Status != status || order.LastError != message, nil
}

func applyCosmeticRefundEvent(ctx context.Context, tx pgx.Tx, order *CosmeticOrder, input CosmeticPaymentEventInput) (bool, error) {
	if order.Status == CosmeticOrderStatusDisputed {
		return false, ErrCosmeticOrderTerminal
	}
	if err := validateCosmeticReversalClaims(order, input); err != nil {
		return false, err
	}
	if input.RefundID == "" || input.RefundAmountCents <= 0 || input.RefundAmountCents > order.AmountReceivedCents ||
		(input.RefundStatus != CosmeticRefundStatusPending && input.RefundStatus != CosmeticRefundStatusRequiresAction &&
			input.RefundStatus != CosmeticRefundStatusSucceeded &&
			input.RefundStatus != CosmeticRefundStatusFailed && input.RefundStatus != CosmeticRefundStatusCanceled) {
		return false, ErrCosmeticPaymentEventInvalid
	}
	var otherSucceeded int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount_cents), 0) FROM cosmetic_order_refunds
		WHERE order_id = $1 AND status = 'succeeded' AND NOT (provider = $2 AND refund_id = $3)`,
		order.ID, input.Provider, input.RefundID).Scan(&otherSucceeded); err != nil {
		return false, fmt.Errorf("applyCosmeticRefundEvent sum: %w", err)
	}
	prospective := otherSucceeded
	if input.RefundStatus == CosmeticRefundStatusSucceeded {
		prospective += input.RefundAmountCents
	}
	if prospective > order.AmountReceivedCents {
		return false, ErrCosmeticOrderMismatch
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO cosmetic_order_refunds
			(provider, refund_id, order_id, status, amount_cents, currency, last_event_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW(),NOW())
		ON CONFLICT (provider, refund_id) DO UPDATE
		SET status = CASE
				WHEN cosmetic_order_refunds.status IN ('succeeded', 'failed', 'canceled')
					THEN cosmetic_order_refunds.status
				ELSE EXCLUDED.status
			END,
			last_event_id = EXCLUDED.last_event_id,
			updated_at = NOW()
		WHERE cosmetic_order_refunds.order_id = EXCLUDED.order_id
		  AND cosmetic_order_refunds.amount_cents = EXCLUDED.amount_cents
		  AND cosmetic_order_refunds.currency = EXCLUDED.currency`,
		input.Provider, input.RefundID, order.ID, input.RefundStatus, input.RefundAmountCents,
		input.Currency, input.EventID)
	if err != nil {
		return false, fmt.Errorf("applyCosmeticRefundEvent upsert: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return false, ErrCosmeticOrderMismatch
	}
	if err := recomputeCosmeticRefundState(ctx, tx, order); err != nil {
		return false, err
	}
	return true, nil
}

func applyCosmeticChargeRefundEvent(ctx context.Context, tx pgx.Tx, order *CosmeticOrder, input CosmeticPaymentEventInput) (bool, error) {
	if order.Status == CosmeticOrderStatusDisputed {
		return false, ErrCosmeticOrderTerminal
	}
	if err := validateCosmeticReversalClaims(order, input); err != nil {
		return false, err
	}
	if input.CumulativeRefundedCents < 0 || input.CumulativeRefundedCents > order.AmountReceivedCents {
		return false, ErrCosmeticOrderMismatch
	}
	var previous int64
	if err := tx.QueryRow(ctx, `SELECT cumulative_charge_refunded_cents FROM cosmetic_orders WHERE id = $1`, order.ID).Scan(&previous); err != nil {
		return false, fmt.Errorf("applyCosmeticChargeRefundEvent current: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_orders
		SET cumulative_charge_refunded_cents = GREATEST(cumulative_charge_refunded_cents, $2), updated_at = NOW()
		WHERE id = $1`, order.ID, input.CumulativeRefundedCents); err != nil {
		return false, fmt.Errorf("applyCosmeticChargeRefundEvent update: %w", err)
	}
	if err := recomputeCosmeticRefundState(ctx, tx, order); err != nil {
		return false, err
	}
	return input.CumulativeRefundedCents > previous, nil
}

func recomputeCosmeticRefundState(ctx context.Context, tx pgx.Tx, order *CosmeticOrder) error {
	var individual, cumulative, previous int64
	var previousStatus string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT SUM(amount_cents) FROM cosmetic_order_refunds
			WHERE order_id = o.id AND status = 'succeeded'), 0),
			o.cumulative_charge_refunded_cents, o.amount_refunded_cents, o.status
		FROM cosmetic_orders o WHERE o.id = $1`, order.ID).
		Scan(&individual, &cumulative, &previous, &previousStatus); err != nil {
		return fmt.Errorf("recomputeCosmeticRefundState totals: %w", err)
	}
	effective := individual
	if cumulative > effective {
		effective = cumulative
	}
	if previousStatus == CosmeticOrderStatusRefunded && previous > effective {
		effective = previous
	}
	if effective > order.AmountReceivedCents {
		return ErrCosmeticOrderMismatch
	}
	status := CosmeticOrderStatusPaid
	terminal := false
	if previousStatus == CosmeticOrderStatusRefunded || effective >= order.AmountReceivedCents {
		status = CosmeticOrderStatusRefunded
		terminal = true
	} else if effective > 0 {
		status = CosmeticOrderStatusRefundReview
	}
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_orders
		SET amount_refunded_cents = $2, status = $3,
			terminal_at = CASE WHEN $4 THEN COALESCE(terminal_at, NOW()) ELSE terminal_at END,
			updated_at = NOW()
		WHERE id = $1`, order.ID, effective, status, terminal); err != nil {
		return fmt.Errorf("recomputeCosmeticRefundState update: %w", err)
	}
	if terminal {
		return revokeCosmeticOrderLicenses(ctx, tx, order.ID, "refunded")
	}
	return nil
}

func applyCosmeticDisputeEvent(ctx context.Context, tx pgx.Tx, order *CosmeticOrder, input CosmeticPaymentEventInput) (bool, error) {
	if err := validateCosmeticReversalClaims(order, input); err != nil {
		return false, err
	}
	if order.Status == CosmeticOrderStatusDisputed {
		return false, nil
	}
	if err := revokeCosmeticOrderLicenses(ctx, tx, order.ID, "chargeback"); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_orders
		SET status = 'disputed', terminal_at = COALESCE(terminal_at, NOW()), updated_at = NOW()
		WHERE id = $1`, order.ID); err != nil {
		return false, fmt.Errorf("applyCosmeticDisputeEvent update: %w", err)
	}
	return true, nil
}

// revokeCosmeticOrderLicenses locks and mutates only rows tied to the immutable
// order mapping. Manual grants and licenses from other purchases are untouched.
func revokeCosmeticOrderLicenses(ctx context.Context, tx pgx.Tx, orderID, status string) error {
	rows, err := tx.Query(ctx, `
		SELECT l.id
		FROM cosmetic_order_licenses ol
		JOIN cosmetic_licenses l ON l.id = ol.license_id
		WHERE ol.order_id = $1
		ORDER BY l.id
		FOR UPDATE OF l`, orderID)
	if err != nil {
		return fmt.Errorf("revokeCosmeticOrderLicenses lock: %w", err)
	}
	for rows.Next() {
		var licenseID string
		if err := rows.Scan(&licenseID); err != nil {
			rows.Close()
			return fmt.Errorf("revokeCosmeticOrderLicenses scan: %w", err)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("revokeCosmeticOrderLicenses rows: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM bot_cosmetic_loadout l
		USING cosmetic_order_licenses ol
		WHERE ol.order_id = $1 AND ol.license_id = l.license_id`, orderID); err != nil {
		return fmt.Errorf("revokeCosmeticOrderLicenses loadouts: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM cosmetic_license_assignments a
		USING cosmetic_order_licenses ol
		WHERE ol.order_id = $1 AND ol.license_id = a.license_id`, orderID); err != nil {
		return fmt.Errorf("revokeCosmeticOrderLicenses assignments: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_licenses l
		SET status = $2, assigned_bot_id = NULL, updated_at = NOW()
		FROM cosmetic_order_licenses ol
		WHERE ol.order_id = $1 AND ol.license_id = l.id AND l.status <> $2`, orderID, status); err != nil {
		return fmt.Errorf("revokeCosmeticOrderLicenses licenses: %w", err)
	}
	return nil
}
