package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	DefaultPlatformChangePageSize = 50
	MaxPlatformChangePageSize     = 100
	DefaultPlatformLinkPageSize   = 50
	MaxPlatformLinkPageSize       = 100
	DefaultPlatformMaximumAgents  = 10
)

type PlatformChange struct {
	ChangeID    int64     `json:"change_id"`
	SubjectKind string    `json:"subject_kind"`
	SubjectID   string    `json:"subject_id"`
	Transition  string    `json:"transition"`
	Revision    int64     `json:"revision"`
	ChangedAt   time.Time `json:"changed_at"`
}

type PlatformAccountCapacity struct {
	AccountID     string    `json:"account_id"`
	Status        string    `json:"status"`
	MaximumAgents int       `json:"maximum_agents"`
	CurrentAgents int       `json:"current_agents"`
	Revision      int64     `json:"revision"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type PlatformAgentLimitError struct {
	CurrentAgents int
	MaximumAgents int
}

func (err *PlatformAgentLimitError) Error() string {
	return fmt.Sprintf(
		"%s: %d current agents and maximum_agents %d",
		ErrPlatformAgentLimit,
		err.CurrentAgents,
		err.MaximumAgents,
	)
}

func (err *PlatformAgentLimitError) Unwrap() error { return ErrPlatformAgentLimit }

type PlatformAgentLinkEvent struct {
	EventID    int64     `json:"event_id"`
	AccountID  string    `json:"account_id"`
	AgentID    string    `json:"agent_id"`
	Status     string    `json:"status"`
	Revision   int64     `json:"revision"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}

type PlatformProfileTransition struct {
	AgentID          string `json:"agent_id"`
	Game             string `json:"game"`
	Status           string `json:"status"`
	ExpectedRevision int64  `json:"expected_revision"`
	IdempotencyKey   string `json:"-"`
}

type PlatformProfileTransitionResult struct {
	ProfileID       string    `json:"profile_id"`
	AgentID         string    `json:"agent_id"`
	Game            string    `json:"game"`
	Status          string    `json:"status"`
	AgentRevision   int64     `json:"agent_revision"`
	ProfileRevision int64     `json:"profile_revision"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type PlatformAgentLinkCommand struct {
	AccountID               string `json:"account_id"`
	AgentID                 string `json:"agent_id"`
	ControlProof            string `json:"-"`
	ExpectedAccountRevision int64  `json:"expected_account_revision"`
	IdempotencyKey          string `json:"-"`
}

type PlatformAgentLinkResult struct {
	AccountID  string     `json:"account_id"`
	AgentID    string     `json:"agent_id"`
	Status     string     `json:"status"`
	Revision   int64      `json:"revision"`
	LinkedAt   time.Time  `json:"linked_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	UnlinkedAt *time.Time `json:"unlinked_at,omitempty"`
}

var (
	ErrPlatformIdempotencyConflict  = errors.New("platform idempotency key was already used for a different request")
	ErrPlatformRevisionConflict     = errors.New("platform resource revision changed")
	ErrPlatformProfileNotFound      = errors.New("platform game profile was not found")
	ErrPlatformAccountNotFound      = errors.New("platform account metadata was not found")
	ErrPlatformAccountInactive      = errors.New("platform account is not active")
	ErrPlatformAgentInactive        = errors.New("platform agent is not active")
	ErrPlatformAgentLimit           = errors.New("platform account maximum_agents reached")
	ErrPlatformControlProofRejected = errors.New("platform agent control proof was rejected")
	ErrPlatformAgentAlreadyLinked   = errors.New("platform agent is already linked")
)

const insertPlatformAgentSQL = `
	INSERT INTO platform_agents (
		agent_id, registration_source, status, revision, created_at, updated_at
	) VALUES ($1, $2, 'active', 1, $3, $4)`

const insertPlatformArenaProfileSQL = `
	INSERT INTO platform_game_profiles (
		profile_id, agent_id, game, status, revision, enrolled_at, updated_at
	) VALUES (md5($1 || chr(31) || 'arena')::UUID, $1, 'arena', 'active', 1, $2, $3)`

const insertPlatformChangeSQL = `
	INSERT INTO platform_changes (subject_kind, subject_id, transition, revision, changed_at)
	VALUES ($1, $2, $3, $4, $5)`

func GetPlatformAccountCapacity(ctx context.Context, accountID string) (*PlatformAccountCapacity, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if strings.TrimSpace(accountID) == "" {
		return nil, errors.New("platform account capacity requires an account")
	}
	capacity := &PlatformAccountCapacity{}
	if err := Pool.QueryRow(ctx, `
		SELECT metadata.account_id, metadata.status, metadata.maximum_agents,
		       COUNT(links.bot_id)::INTEGER, metadata.revision, metadata.updated_at
		FROM platform_account_metadata AS metadata
		LEFT JOIN account_bot_links AS links ON links.account_id = metadata.account_id
		WHERE metadata.account_id = $1
		GROUP BY metadata.account_id, metadata.status, metadata.maximum_agents,
		         metadata.revision, metadata.updated_at`, accountID).Scan(
		&capacity.AccountID,
		&capacity.Status,
		&capacity.MaximumAgents,
		&capacity.CurrentAgents,
		&capacity.Revision,
		&capacity.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlatformAccountNotFound
		}
		return nil, fmt.Errorf("GetPlatformAccountCapacity: %w", err)
	}
	return capacity, nil
}

func lockPlatformAccountCapacityTx(ctx context.Context, tx pgx.Tx, accountID string) (*PlatformAccountCapacity, error) {
	capacity := &PlatformAccountCapacity{AccountID: accountID}
	if err := tx.QueryRow(ctx, `
		SELECT status, maximum_agents, revision, updated_at
		FROM platform_account_metadata
		WHERE account_id = $1
		FOR UPDATE`, accountID).Scan(
		&capacity.Status,
		&capacity.MaximumAgents,
		&capacity.Revision,
		&capacity.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlatformAccountNotFound
		}
		return nil, fmt.Errorf("lock platform account capacity: %w", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::INTEGER
		FROM account_bot_links
		WHERE account_id = $1`, accountID).Scan(&capacity.CurrentAgents); err != nil {
		return nil, fmt.Errorf("count platform account agents: %w", err)
	}
	return capacity, nil
}

func enforcePlatformAgentCapacityTx(ctx context.Context, tx pgx.Tx, accountID string) error {
	capacity, err := lockPlatformAccountCapacityTx(ctx, tx, accountID)
	if err != nil {
		return err
	}
	if capacity.Status != "active" {
		return fmt.Errorf("%w: account %q is %s", ErrPlatformAccountInactive, accountID, capacity.Status)
	}
	if capacity.CurrentAgents >= capacity.MaximumAgents {
		return &PlatformAgentLimitError{
			CurrentAgents: capacity.CurrentAgents,
			MaximumAgents: capacity.MaximumAgents,
		}
	}
	return nil
}

func appendPlatformAccountLinkChangeTx(
	ctx context.Context,
	tx pgx.Tx,
	accountID, status string,
	changedAt time.Time,
) error {
	capacity, err := lockPlatformAccountCapacityTx(ctx, tx, accountID)
	if err != nil {
		return err
	}
	capacity.Revision++
	if _, err := tx.Exec(ctx, `
		UPDATE platform_account_metadata
		SET revision = $2, updated_at = $3
		WHERE account_id = $1`, accountID, capacity.Revision, changedAt); err != nil {
		return fmt.Errorf("platform account revision update: %w", err)
	}
	if _, err := tx.Exec(ctx, insertPlatformChangeSQL,
		"account", accountID, "agent_"+status, capacity.Revision, changedAt,
	); err != nil {
		return fmt.Errorf("platform account link change: %w", err)
	}
	return nil
}

func enrollArenaAgentTx(ctx context.Context, tx pgx.Tx, bot *Bot, registrationSource string) error {
	if _, err := tx.Exec(ctx, insertPlatformAgentSQL,
		bot.ID, registrationSource, bot.CreatedAt, bot.UpdatedAt,
	); err != nil {
		return fmt.Errorf("platform agent insert: %w", err)
	}
	if _, err := tx.Exec(ctx, insertPlatformArenaProfileSQL,
		bot.ID, bot.CreatedAt, bot.UpdatedAt,
	); err != nil {
		return fmt.Errorf("Arena platform profile insert: %w", err)
	}
	if _, err := tx.Exec(ctx, insertPlatformChangeSQL,
		"agent", bot.ID, "registered", 1, bot.UpdatedAt,
	); err != nil {
		return fmt.Errorf("platform agent change insert: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_changes (subject_kind, subject_id, transition, revision, changed_at)
		VALUES ('game_profile', md5($1 || chr(31) || 'arena')::UUID::TEXT, 'enrolled', 1, $2)`,
		bot.ID, bot.UpdatedAt,
	); err != nil {
		return fmt.Errorf("Arena platform profile change insert: %w", err)
	}
	return nil
}

// ListPlatformChanges returns an ascending cursor page. The query reads at
// most one row beyond the public page cap so both result size and database
// work stay bounded.
func ListPlatformChanges(ctx context.Context, afterChangeID int64, limit int) ([]PlatformChange, int64, error) {
	if Pool == nil {
		return nil, 0, ErrNoDatabase
	}
	if afterChangeID < 0 {
		afterChangeID = 0
	}
	if limit <= 0 {
		limit = DefaultPlatformChangePageSize
	}
	if limit > MaxPlatformChangePageSize {
		limit = MaxPlatformChangePageSize
	}

	rows, err := Pool.Query(ctx, `
		SELECT change_id, subject_kind, subject_id, transition, revision, changed_at
		FROM platform_changes
		WHERE change_id > $1
		ORDER BY change_id
		LIMIT $2`, afterChangeID, limit+1)
	if err != nil {
		return nil, 0, fmt.Errorf("ListPlatformChanges: %w", err)
	}
	defer rows.Close()

	changes := make([]PlatformChange, 0, limit+1)
	for rows.Next() {
		var change PlatformChange
		if err := rows.Scan(
			&change.ChangeID,
			&change.SubjectKind,
			&change.SubjectID,
			&change.Transition,
			&change.Revision,
			&change.ChangedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("ListPlatformChanges scan: %w", err)
		}
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("ListPlatformChanges rows: %w", err)
	}

	var nextCursor int64
	if len(changes) > limit {
		changes = changes[:limit]
		nextCursor = changes[len(changes)-1].ChangeID
	}
	return changes, nextCursor, nil
}

func ListPlatformAgentLinkEvents(ctx context.Context, accountID string, afterEventID int64, limit int) ([]PlatformAgentLinkEvent, int64, error) {
	if Pool == nil {
		return nil, 0, ErrNoDatabase
	}
	if strings.TrimSpace(accountID) == "" {
		return nil, 0, errors.New("platform link history requires an account")
	}
	if afterEventID < 0 {
		afterEventID = 0
	}
	if limit <= 0 {
		limit = DefaultPlatformLinkPageSize
	}
	if limit > MaxPlatformLinkPageSize {
		limit = MaxPlatformLinkPageSize
	}

	rows, err := Pool.Query(ctx, `
		SELECT event_id, account_id, agent_id, status, revision, reason, occurred_at
		FROM platform_agent_link_events
		WHERE account_id = $1
		  AND (account_id, event_id) > ($1, $2)
		ORDER BY account_id, event_id
		LIMIT $3`, accountID, afterEventID, limit+1)
	if err != nil {
		return nil, 0, fmt.Errorf("ListPlatformAgentLinkEvents: %w", err)
	}
	defer rows.Close()

	events := make([]PlatformAgentLinkEvent, 0, limit+1)
	for rows.Next() {
		var event PlatformAgentLinkEvent
		if err := rows.Scan(
			&event.EventID,
			&event.AccountID,
			&event.AgentID,
			&event.Status,
			&event.Revision,
			&event.Reason,
			&event.OccurredAt,
		); err != nil {
			return nil, 0, fmt.Errorf("ListPlatformAgentLinkEvents scan: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("ListPlatformAgentLinkEvents rows: %w", err)
	}

	var nextCursor int64
	if len(events) > limit {
		events = events[:limit]
		nextCursor = events[len(events)-1].EventID
	}
	return events, nextCursor, nil
}

func appendPlatformAgentLinkEventTx(
	ctx context.Context,
	tx pgx.Tx,
	accountID, agentID, status, reason string,
	occurredAt time.Time,
) error {
	if status != "linked" && status != "unlinked" {
		return fmt.Errorf("unsupported platform agent link status %q", status)
	}
	if reason == "" {
		reason = "arena_account"
	}
	occurredAt = occurredAt.UTC().Truncate(time.Microsecond)
	if err := appendPlatformAccountLinkChangeTx(ctx, tx, accountID, status, occurredAt); err != nil {
		return err
	}

	var agentRevision int64
	if err := tx.QueryRow(ctx, `
		SELECT revision FROM platform_agents WHERE agent_id = $1 FOR UPDATE`, agentID).Scan(&agentRevision); err != nil {
		return fmt.Errorf("platform agent link lock: %w", err)
	}
	var linkRevision int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE((
			SELECT revision
			FROM platform_agent_link_events
			WHERE agent_id = $1
			ORDER BY event_id DESC
			LIMIT 1
		), 0) + 1`, agentID).Scan(&linkRevision); err != nil {
		return fmt.Errorf("platform agent link revision: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_agent_link_events (
			account_id, agent_id, status, revision, reason, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6)`,
		accountID, agentID, status, linkRevision, reason, occurredAt,
	); err != nil {
		return fmt.Errorf("platform agent link event insert: %w", err)
	}
	agentRevision++
	if _, err := tx.Exec(ctx, `
		UPDATE platform_agents SET revision = $2, updated_at = $3 WHERE agent_id = $1`,
		agentID, agentRevision, occurredAt,
	); err != nil {
		return fmt.Errorf("platform linked agent revision: %w", err)
	}
	if _, err := tx.Exec(ctx, insertPlatformChangeSQL,
		"agent_link", agentID, status, linkRevision, occurredAt,
	); err != nil {
		return fmt.Errorf("platform agent link change: %w", err)
	}
	if _, err := tx.Exec(ctx, insertPlatformChangeSQL,
		"agent", agentID, "link_"+status, agentRevision, occurredAt,
	); err != nil {
		return fmt.Errorf("platform linked agent change: %w", err)
	}
	return nil
}

// TransitionPlatformProfile applies one revision-guarded game-profile status
// change. The idempotency record, resource revisions, and change-feed entries
// commit in the same transaction.
func TransitionPlatformProfile(ctx context.Context, command PlatformProfileTransition) (*PlatformProfileTransitionResult, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if strings.TrimSpace(command.AgentID) == "" || utf8.RuneCountInString(command.AgentID) > 128 {
		return nil, errors.New("platform profile transition requires a valid agent ID")
	}
	if command.Game != "arena" && command.Game != "kingdom_grid" {
		return nil, fmt.Errorf("unsupported platform game %q", command.Game)
	}
	if command.Status != "active" && command.Status != "suspended" && command.Status != "retired" {
		return nil, fmt.Errorf("unsupported platform profile status %q", command.Status)
	}
	if command.ExpectedRevision < 1 {
		return nil, errors.New("platform profile transition requires a positive expected revision")
	}
	if command.IdempotencyKey == "" || strings.TrimSpace(command.IdempotencyKey) != command.IdempotencyKey || utf8.RuneCountInString(command.IdempotencyKey) > 128 {
		return nil, errors.New("platform profile transition requires a 1-128 character idempotency key without surrounding whitespace")
	}

	requestJSON, err := json.Marshal(struct {
		AgentID          string `json:"agent_id"`
		Game             string `json:"game"`
		Status           string `json:"status"`
		ExpectedRevision int64  `json:"expected_revision"`
	}{
		AgentID:          command.AgentID,
		Game:             command.Game,
		Status:           command.Status,
		ExpectedRevision: command.ExpectedRevision,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal platform profile transition: %w", err)
	}
	requestHash := sha256.Sum256(requestJSON)
	const operation = "transition_platform_profile"

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("TransitionPlatformProfile begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Serialize identical keys before reading their durable record. This makes
	// concurrent replays return the first committed response instead of racing
	// on a uniqueness violation.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, operation+":"+command.IdempotencyKey); err != nil {
		return nil, fmt.Errorf("TransitionPlatformProfile idempotency lock: %w", err)
	}
	var storedHash, storedResponse []byte
	err = tx.QueryRow(ctx, `
		SELECT request_hash, response
		FROM platform_idempotency_records
		WHERE operation = $1 AND idempotency_key = $2`, operation, command.IdempotencyKey).Scan(&storedHash, &storedResponse)
	if err == nil {
		if !bytes.Equal(storedHash, requestHash[:]) {
			return nil, ErrPlatformIdempotencyConflict
		}
		var replayed PlatformProfileTransitionResult
		if err := json.Unmarshal(storedResponse, &replayed); err != nil {
			return nil, fmt.Errorf("TransitionPlatformProfile decode replay: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("TransitionPlatformProfile replay commit: %w", err)
		}
		return &replayed, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("TransitionPlatformProfile load idempotency: %w", err)
	}

	result := &PlatformProfileTransitionResult{
		AgentID: command.AgentID,
		Game:    command.Game,
	}
	if err := tx.QueryRow(ctx, `
		SELECT profiles.profile_id::TEXT, profiles.status, profiles.revision,
		       profiles.updated_at, agents.revision
		FROM platform_game_profiles AS profiles
		JOIN platform_agents AS agents ON agents.agent_id = profiles.agent_id
		WHERE profiles.agent_id = $1 AND profiles.game = $2
		FOR UPDATE OF profiles, agents`, command.AgentID, command.Game).Scan(
		&result.ProfileID,
		&result.Status,
		&result.ProfileRevision,
		&result.UpdatedAt,
		&result.AgentRevision,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlatformProfileNotFound
		}
		return nil, fmt.Errorf("TransitionPlatformProfile load profile: %w", err)
	}
	if result.ProfileRevision != command.ExpectedRevision {
		return nil, fmt.Errorf("%w: expected %d, current %d", ErrPlatformRevisionConflict, command.ExpectedRevision, result.ProfileRevision)
	}

	if result.Status != command.Status {
		result.Status = command.Status
		result.ProfileRevision++
		result.AgentRevision++
		result.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
		if _, err := tx.Exec(ctx, `
			UPDATE platform_game_profiles
			SET status = $3, revision = $4, updated_at = $5
			WHERE agent_id = $1 AND game = $2`,
			command.AgentID, command.Game, result.Status, result.ProfileRevision, result.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("TransitionPlatformProfile update profile: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE platform_agents
			SET revision = $2, updated_at = $3
			WHERE agent_id = $1`, command.AgentID, result.AgentRevision, result.UpdatedAt); err != nil {
			return nil, fmt.Errorf("TransitionPlatformProfile update agent: %w", err)
		}
		if _, err := tx.Exec(ctx, insertPlatformChangeSQL,
			"game_profile", result.ProfileID, "status_"+result.Status, result.ProfileRevision, result.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("TransitionPlatformProfile profile change: %w", err)
		}
		if _, err := tx.Exec(ctx, insertPlatformChangeSQL,
			"agent", command.AgentID, "profile_status_"+result.Status, result.AgentRevision, result.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("TransitionPlatformProfile agent change: %w", err)
		}
	}

	responseJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("TransitionPlatformProfile marshal response: %w", err)
	}
	idempotencyRecordedAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_idempotency_records (
			operation, idempotency_key, request_hash, response,
			subject_kind, subject_id, revision, created_at
		) VALUES ($1, $2, $3, $4, 'game_profile', $5, $6, $7)`,
		operation,
		command.IdempotencyKey,
		requestHash[:],
		responseJSON,
		result.ProfileID,
		result.ProfileRevision,
		idempotencyRecordedAt,
	); err != nil {
		return nil, fmt.Errorf("TransitionPlatformProfile store idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("TransitionPlatformProfile commit: %w", err)
	}
	return result, nil
}

// EnsurePlatformAuthoritySchema installs the stable, game-neutral agent
// metadata introduced by the W1b.2 authority checkpoint. Existing Arena bot
// IDs are imported verbatim; reconciliation is insert-only so a restart can
// never reset authority-owned status or revision state.
func EnsurePlatformAuthoritySchema(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("EnsurePlatformAuthoritySchema begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(2026071901::BIGINT)`); err != nil {
		return fmt.Errorf("EnsurePlatformAuthoritySchema migration lock: %w", err)
	}

	var invalidAgentID string
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM bots
		WHERE char_length(id) NOT BETWEEN 1 AND 128
		ORDER BY id
		LIMIT 1`).Scan(&invalidAgentID)
	if err == nil {
		return fmt.Errorf("EnsurePlatformAuthoritySchema legacy agent ID %q exceeds the 1-128 character platform contract", invalidAgentID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("EnsurePlatformAuthoritySchema validate legacy agent IDs: %w", err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS platform_account_metadata (
		account_id TEXT PRIMARY KEY REFERENCES customer_accounts(id) ON DELETE RESTRICT,
		status TEXT NOT NULL DEFAULT 'active'
			CHECK (status IN ('active', 'suspended', 'closed')),
		maximum_agents INTEGER NOT NULL DEFAULT %d
			CHECK (maximum_agents BETWEEN 1 AND 1000),
		revision BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`, DefaultPlatformMaximumAgents)); err != nil {
		return fmt.Errorf("EnsurePlatformAuthoritySchema account metadata table: %w", err)
	}

	var overLimitAccountID string
	var overLimitCurrent, overLimitMaximum int
	err = tx.QueryRow(ctx, `
		SELECT accounts.id,
		       COUNT(links.bot_id)::INTEGER,
		       COALESCE(metadata.maximum_agents, $1)::INTEGER
		FROM customer_accounts AS accounts
		LEFT JOIN platform_account_metadata AS metadata ON metadata.account_id = accounts.id
		LEFT JOIN account_bot_links AS links ON links.account_id = accounts.id
		GROUP BY accounts.id, metadata.maximum_agents
		HAVING COUNT(links.bot_id) > COALESCE(metadata.maximum_agents, $1)
		ORDER BY accounts.id
		LIMIT 1`, DefaultPlatformMaximumAgents).Scan(
		&overLimitAccountID,
		&overLimitCurrent,
		&overLimitMaximum,
	)
	if err == nil {
		return fmt.Errorf(
			"EnsurePlatformAuthoritySchema account %q has %d current agents, above maximum_agents %d",
			overLimitAccountID,
			overLimitCurrent,
			overLimitMaximum,
		)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("EnsurePlatformAuthoritySchema validate account agent capacity: %w", err)
	}
	var accountStatusConstraint string
	if err := tx.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'platform_account_metadata'::regclass
		  AND conname = 'platform_account_metadata_status_check'`).Scan(&accountStatusConstraint); err != nil {
		return fmt.Errorf("EnsurePlatformAuthoritySchema inspect account status constraint: %w", err)
	}
	switch normalizedPlatformAccountStatusConstraint(accountStatusConstraint) {
	case "active,suspended,closed":
		// The final contract is already installed. Avoid repeated ACCESS
		// EXCLUSIVE locks and full-table CHECK validation on later starts.
	case "active,suspended,retired":
		if _, err := tx.Exec(ctx, `ALTER TABLE platform_account_metadata
			DROP CONSTRAINT platform_account_metadata_status_check`); err != nil {
			return fmt.Errorf("EnsurePlatformAuthoritySchema drop legacy account status constraint: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE platform_account_metadata
			SET status = 'closed'
			WHERE status = 'retired'`); err != nil {
			return fmt.Errorf("EnsurePlatformAuthoritySchema upgrade account status: %w", err)
		}
		if _, err := tx.Exec(ctx, `ALTER TABLE platform_account_metadata
			ADD CONSTRAINT platform_account_metadata_status_check
			CHECK (status IN ('active', 'suspended', 'closed'))`); err != nil {
			return fmt.Errorf("EnsurePlatformAuthoritySchema install account status constraint: %w", err)
		}
	default:
		return fmt.Errorf(
			"EnsurePlatformAuthoritySchema unexpected account status constraint %q",
			accountStatusConstraint,
		)
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS platform_agents (
			agent_id TEXT PRIMARY KEY CHECK (char_length(agent_id) BETWEEN 1 AND 128),
			registration_source TEXT NOT NULL
				CHECK (registration_source IN ('arena', 'arena_import', 'kingdom_grid')),
			status TEXT NOT NULL DEFAULT 'active'
				CHECK (status IN ('active', 'suspended', 'retired')),
			revision BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS platform_game_profiles (
			profile_id UUID NOT NULL UNIQUE,
			agent_id TEXT NOT NULL REFERENCES platform_agents(agent_id) ON DELETE RESTRICT,
			game TEXT NOT NULL CHECK (game IN ('arena', 'kingdom_grid')),
			status TEXT NOT NULL DEFAULT 'active'
				CHECK (status IN ('active', 'suspended', 'retired')),
			revision BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
			enrolled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (agent_id, game)
		)`,
		`CREATE TABLE IF NOT EXISTS platform_changes (
			change_id BIGSERIAL PRIMARY KEY,
			subject_kind TEXT NOT NULL
				CHECK (subject_kind IN ('account', 'agent', 'game_profile', 'agent_link')),
			subject_id TEXT NOT NULL CHECK (char_length(subject_id) BETWEEN 1 AND 256),
			transition TEXT NOT NULL CHECK (char_length(transition) BETWEEN 1 AND 64),
			revision BIGINT NOT NULL CHECK (revision >= 1),
			changed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (subject_kind, subject_id, revision)
		)`,
		fmt.Sprintf(`INSERT INTO platform_account_metadata (
			account_id, status, maximum_agents, revision, created_at, updated_at
		)
		SELECT accounts.id, 'active', %d, 1, accounts.created_at, accounts.updated_at
		FROM customer_accounts AS accounts
		ORDER BY accounts.id
		ON CONFLICT (account_id) DO NOTHING`, DefaultPlatformMaximumAgents),
		`INSERT INTO platform_changes (
			subject_kind, subject_id, transition, revision, changed_at
		)
		SELECT 'account', metadata.account_id, 'registered', 1, metadata.updated_at
		FROM platform_account_metadata AS metadata
		ORDER BY metadata.account_id
		ON CONFLICT (subject_kind, subject_id, revision) DO NOTHING`,
		fmt.Sprintf(`CREATE OR REPLACE FUNCTION insert_platform_account_metadata()
		RETURNS TRIGGER
		LANGUAGE plpgsql
		AS $$
		BEGIN
			INSERT INTO platform_account_metadata (
				account_id, status, maximum_agents, revision, created_at, updated_at
			) VALUES (NEW.id, 'active', %d, 1, NEW.created_at, NEW.updated_at)
			ON CONFLICT (account_id) DO NOTHING;
			INSERT INTO platform_changes (
				subject_kind, subject_id, transition, revision, changed_at
			) VALUES ('account', NEW.id, 'registered', 1, NEW.updated_at)
			ON CONFLICT (subject_kind, subject_id, revision) DO NOTHING;
			RETURN NEW;
		END
		$$`, DefaultPlatformMaximumAgents),
		`DO $$
		BEGIN
			CREATE TRIGGER customer_accounts_platform_metadata
			AFTER INSERT ON customer_accounts
			FOR EACH ROW EXECUTE FUNCTION insert_platform_account_metadata();
		EXCEPTION
			WHEN duplicate_object THEN NULL;
		END
		$$`,
		`CREATE TABLE IF NOT EXISTS platform_idempotency_records (
			operation TEXT NOT NULL CHECK (char_length(operation) BETWEEN 1 AND 64),
			idempotency_key TEXT NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 128),
			request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
			response JSONB NOT NULL CHECK (octet_length(response::TEXT) <= 65536),
			subject_kind TEXT NOT NULL CHECK (char_length(subject_kind) BETWEEN 1 AND 64),
			subject_id TEXT NOT NULL CHECK (char_length(subject_id) BETWEEN 1 AND 256),
			revision BIGINT NOT NULL CHECK (revision >= 1),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (operation, idempotency_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_platform_idempotency_created
			ON platform_idempotency_records (created_at, operation, idempotency_key)`,
		`CREATE TABLE IF NOT EXISTS platform_agent_link_events (
			event_id BIGSERIAL PRIMARY KEY,
			account_id TEXT NOT NULL CHECK (char_length(account_id) BETWEEN 1 AND 128),
			agent_id TEXT NOT NULL CHECK (char_length(agent_id) BETWEEN 1 AND 128),
			status TEXT NOT NULL CHECK (status IN ('linked', 'unlinked')),
			revision BIGINT NOT NULL CHECK (revision >= 1),
			reason TEXT NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 256),
			occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (agent_id, revision)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_platform_agent_link_events_account
			ON platform_agent_link_events (account_id, event_id)
			INCLUDE (agent_id, status, revision, reason, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_platform_agent_link_events_agent
			ON platform_agent_link_events (agent_id, event_id DESC)`,
		`INSERT INTO platform_agents (
			agent_id, registration_source, status, revision, created_at, updated_at
		)
		SELECT b.id, 'arena_import', 'active', 1, b.created_at, b.updated_at
		FROM bots AS b
		ORDER BY b.id
		ON CONFLICT (agent_id) DO NOTHING`,
		`INSERT INTO platform_game_profiles (
			profile_id, agent_id, game, status, revision, enrolled_at, updated_at
		)
		SELECT md5(b.id || chr(31) || 'arena')::UUID,
		       b.id, 'arena', agents.status, 1, b.created_at, b.updated_at
		FROM bots AS b
		JOIN platform_agents AS agents ON agents.agent_id = b.id
		ORDER BY b.id
		ON CONFLICT (agent_id, game) DO NOTHING`,
		`INSERT INTO platform_changes (
			subject_kind, subject_id, transition, revision, changed_at
		)
		SELECT 'agent', agents.agent_id, 'registered', 1, agents.updated_at
		FROM platform_agents AS agents
		JOIN bots AS b ON b.id = agents.agent_id
		WHERE agents.registration_source = 'arena_import'
		ORDER BY agents.agent_id
		ON CONFLICT (subject_kind, subject_id, revision) DO NOTHING`,
		`INSERT INTO platform_changes (
			subject_kind, subject_id, transition, revision, changed_at
		)
		SELECT 'game_profile', profiles.profile_id::TEXT, 'enrolled', 1, profiles.updated_at
		FROM platform_game_profiles AS profiles
		JOIN bots AS b ON b.id = profiles.agent_id
		WHERE profiles.game = 'arena'
		ORDER BY profiles.agent_id
		ON CONFLICT (subject_kind, subject_id, revision) DO NOTHING`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("EnsurePlatformAuthoritySchema exec: %w", err)
		}
	}
	if err := reconcilePlatformAgentLinksTx(ctx, tx); err != nil {
		return fmt.Errorf("EnsurePlatformAuthoritySchema reconcile agent links: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("EnsurePlatformAuthoritySchema commit: %w", err)
	}
	return nil
}

func normalizedPlatformAccountStatusConstraint(definition string) string {
	normalized := strings.NewReplacer(
		"::text", "",
		" ", "",
		"\n", "",
		"\r", "",
		"\t", "",
	).Replace(definition)

	switch normalized {
	case "CHECK((status=ANY(ARRAY['active','suspended','closed'])))":
		return "active,suspended,closed"
	case "CHECK((status=ANY(ARRAY['active','suspended','retired'])))":
		return "active,suspended,retired"
	default:
		return ""
	}
}

func reconcilePlatformAgentLinksTx(ctx context.Context, tx pgx.Tx) error {
	type repair struct {
		accountID  string
		agentID    string
		status     string
		reason     string
		occurredAt time.Time
	}
	repairs := make([]repair, 0)
	rows, err := tx.Query(ctx, `
		SELECT latest.account_id, links.bot_id, 'unlinked',
		       'arena_reconciliation_transfer', NOW()
		FROM account_bot_links AS links
		JOIN LATERAL (
			SELECT account_id, status
			FROM platform_agent_link_events
			WHERE agent_id = links.bot_id
			ORDER BY event_id DESC
			LIMIT 1
		) AS latest ON latest.status = 'linked'
		WHERE latest.account_id <> links.account_id
		ORDER BY links.bot_id`)
	if err != nil {
		return fmt.Errorf("load transferred prior links: %w", err)
	}
	for rows.Next() {
		var item repair
		if err := rows.Scan(&item.accountID, &item.agentID, &item.status, &item.reason, &item.occurredAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan transferred prior link: %w", err)
		}
		repairs = append(repairs, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate transferred prior links: %w", err)
	}
	rows.Close()

	rows, err = tx.Query(ctx, `
		SELECT links.account_id, links.bot_id, 'linked', 'arena_import', links.linked_at
		FROM account_bot_links AS links
		LEFT JOIN LATERAL (
			SELECT account_id, status
			FROM platform_agent_link_events
			WHERE agent_id = links.bot_id
			ORDER BY event_id DESC
			LIMIT 1
		) AS latest ON true
		WHERE latest.status IS DISTINCT FROM 'linked'
		   OR latest.account_id IS DISTINCT FROM links.account_id
		ORDER BY links.bot_id`)
	if err != nil {
		return fmt.Errorf("load missing current links: %w", err)
	}
	for rows.Next() {
		var item repair
		if err := rows.Scan(&item.accountID, &item.agentID, &item.status, &item.reason, &item.occurredAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan missing current link: %w", err)
		}
		repairs = append(repairs, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate missing current links: %w", err)
	}
	rows.Close()

	rows, err = tx.Query(ctx, `
		SELECT latest.account_id, agents.agent_id, 'unlinked',
		       'arena_reconciliation', NOW()
		FROM platform_agents AS agents
		JOIN LATERAL (
			SELECT account_id, status
			FROM platform_agent_link_events
			WHERE agent_id = agents.agent_id
			ORDER BY event_id DESC
			LIMIT 1
		) AS latest ON latest.status = 'linked'
		WHERE NOT EXISTS (
			SELECT 1 FROM account_bot_links AS links
			WHERE links.bot_id = agents.agent_id
		  )
		ORDER BY agents.agent_id`)
	if err != nil {
		return fmt.Errorf("load stale durable links: %w", err)
	}
	for rows.Next() {
		var item repair
		if err := rows.Scan(&item.accountID, &item.agentID, &item.status, &item.reason, &item.occurredAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan stale durable link: %w", err)
		}
		repairs = append(repairs, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate stale durable links: %w", err)
	}
	rows.Close()

	for _, item := range repairs {
		if err := appendPlatformAgentLinkEventTx(
			ctx, tx, item.accountID, item.agentID, item.status, item.reason, item.occurredAt,
		); err != nil {
			return err
		}
	}
	return nil
}
