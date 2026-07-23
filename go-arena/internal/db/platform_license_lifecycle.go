package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	DefaultPlatformLicenseHistoryPageSize = 50
	MaxPlatformLicenseHistoryPageSize     = 100
)

type PlatformLicenseTransitionCommand struct {
	LicenseID         string `json:"license_id"`
	TargetStatus      string `json:"target_status"`
	ExpectedRevision  int64  `json:"expected_license_revision"`
	Reason            string `json:"reason"`
	ProviderReference string `json:"provider_reference,omitempty"`
	IdempotencyKey    string `json:"-"`
}

type PlatformLicenseAssignmentCommand struct {
	LicenseID        string `json:"license_id"`
	AgentID          string `json:"agent_id"`
	ExpectedRevision int64  `json:"expected_license_revision"`
	IdempotencyKey   string `json:"-"`
}

type PlatformLicenseUnassignmentCommand struct {
	LicenseID        string `json:"license_id"`
	ExpectedRevision int64  `json:"expected_license_revision"`
	Reason           string `json:"reason"`
	IdempotencyKey   string `json:"-"`
}

type PlatformCosmeticLicense struct {
	LicenseID       string    `json:"license_id"`
	AccountID       string    `json:"account_id"`
	CosmeticID      string    `json:"cosmetic_id"`
	AssignedAgentID *string   `json:"assigned_agent_id"`
	Status          string    `json:"status"`
	Revision        int64     `json:"revision"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type PlatformLicenseLifecycleEvent struct {
	EventID           int64     `json:"event_id,string"`
	LicenseID         string    `json:"license_id"`
	AccountID         *string   `json:"account_id,omitempty"`
	Status            string    `json:"status"`
	PreviousStatus    *string   `json:"previous_status,omitempty"`
	CurrentStatus     string    `json:"current_status"`
	PreviousAgentID   *string   `json:"previous_agent_id,omitempty"`
	CurrentAgentID    *string   `json:"current_agent_id,omitempty"`
	Transition        string    `json:"transition"`
	Revision          int64     `json:"revision"`
	Source            string    `json:"source"`
	Reason            string    `json:"reason"`
	ProviderReference *string   `json:"provider_reference,omitempty"`
	OccurredAt        time.Time `json:"occurred_at"`
}

func ListPlatformLicenseLifecycleEvents(
	ctx context.Context,
	licenseID string,
	afterEventID int64,
	limit int,
) ([]PlatformLicenseLifecycleEvent, int64, error) {
	if Pool == nil {
		return nil, 0, ErrNoDatabase
	}
	if licenseID == "" || strings.TrimSpace(licenseID) != licenseID || utf8.RuneCountInString(licenseID) > 128 {
		return nil, 0, errors.New("platform license history requires a valid license ID")
	}
	if afterEventID < 0 {
		afterEventID = 0
	}
	if limit <= 0 {
		limit = DefaultPlatformLicenseHistoryPageSize
	}
	if limit > MaxPlatformLicenseHistoryPageSize {
		limit = MaxPlatformLicenseHistoryPageSize
	}
	rows, err := Pool.Query(ctx, `
		SELECT event_id, license_id, account_id, status, previous_status, current_status,
		       previous_agent_id, current_agent_id,
		       transition, revision, source, reason, provider_reference, occurred_at
		FROM platform_license_lifecycle_events
		WHERE license_id = $1 AND (license_id, event_id) > ($1, $2)
		ORDER BY license_id, event_id
		LIMIT $3`, licenseID, afterEventID, limit+1)
	if err != nil {
		return nil, 0, fmt.Errorf("ListPlatformLicenseLifecycleEvents: %w", err)
	}
	defer rows.Close()
	events := make([]PlatformLicenseLifecycleEvent, 0, limit+1)
	for rows.Next() {
		var event PlatformLicenseLifecycleEvent
		if err := rows.Scan(
			&event.EventID, &event.LicenseID, &event.AccountID, &event.Status,
			&event.PreviousStatus, &event.CurrentStatus,
			&event.PreviousAgentID, &event.CurrentAgentID, &event.Transition, &event.Revision,
			&event.Source, &event.Reason, &event.ProviderReference, &event.OccurredAt,
		); err != nil {
			return nil, 0, fmt.Errorf("ListPlatformLicenseLifecycleEvents scan: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("ListPlatformLicenseLifecycleEvents rows: %w", err)
	}
	var nextCursor int64
	if len(events) > limit {
		events = events[:limit]
		nextCursor = events[len(events)-1].EventID
	}
	return events, nextCursor, nil
}

func AssignPlatformLicense(ctx context.Context, command PlatformLicenseAssignmentCommand) (*PlatformCosmeticLicense, error) {
	if command.AgentID == "" || strings.TrimSpace(command.AgentID) != command.AgentID || utf8.RuneCountInString(command.AgentID) > 128 {
		return nil, errors.New("platform license assignment requires a valid agent ID")
	}
	requestJSON, err := json.Marshal(struct {
		LicenseID        string `json:"license_id"`
		AgentID          string `json:"agent_id"`
		ExpectedRevision int64  `json:"expected_license_revision"`
	}{command.LicenseID, command.AgentID, command.ExpectedRevision})
	if err != nil {
		return nil, fmt.Errorf("marshal platform license assignment request: %w", err)
	}
	return applyPlatformLicenseAssignment(ctx, platformLicenseAssignmentRequest{
		licenseID: command.LicenseID, agentID: &command.AgentID, expectedRevision: command.ExpectedRevision,
		idempotencyKey: command.IdempotencyKey, operation: "assign_license", transition: "assigned",
		source: "platform", reason: "assigned", requestJSON: requestJSON,
	})
}

func UnassignPlatformLicense(ctx context.Context, command PlatformLicenseUnassignmentCommand) (*PlatformCosmeticLicense, error) {
	requestJSON, err := json.Marshal(struct {
		LicenseID        string `json:"license_id"`
		ExpectedRevision int64  `json:"expected_license_revision"`
		Reason           string `json:"reason"`
	}{command.LicenseID, command.ExpectedRevision, command.Reason})
	if err != nil {
		return nil, fmt.Errorf("marshal platform license unassignment request: %w", err)
	}
	return applyPlatformLicenseAssignment(ctx, platformLicenseAssignmentRequest{
		licenseID: command.LicenseID, expectedRevision: command.ExpectedRevision,
		idempotencyKey: command.IdempotencyKey, operation: "unassign_license", transition: "unassigned",
		source: "platform", reason: command.Reason, requestJSON: requestJSON,
	})
}

type platformLicenseAssignmentRequest struct {
	licenseID        string
	agentID          *string
	expectedRevision int64
	idempotencyKey   string
	operation        string
	transition       string
	source           string
	reason           string
	requestJSON      []byte
}

func applyPlatformLicenseAssignment(ctx context.Context, request platformLicenseAssignmentRequest) (*PlatformCosmeticLicense, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if request.licenseID == "" || strings.TrimSpace(request.licenseID) != request.licenseID || utf8.RuneCountInString(request.licenseID) > 128 {
		return nil, errors.New("platform license assignment requires a valid license ID")
	}
	if request.expectedRevision < 0 {
		return nil, errors.New("platform license assignment requires a nonnegative expected revision")
	}
	if request.reason == "" || strings.TrimSpace(request.reason) != request.reason || utf8.RuneCountInString(request.reason) > 256 {
		return nil, errors.New("platform license assignment requires a 1-256 character reason without surrounding whitespace")
	}
	keyLength := utf8.RuneCountInString(request.idempotencyKey)
	if strings.TrimSpace(request.idempotencyKey) != request.idempotencyKey || keyLength < platformIdempotencyKeyMinimum || keyLength > platformIdempotencyKeyMaximum {
		return nil, errors.New("platform license assignment requires an 8-128 character idempotency key without surrounding whitespace")
	}
	requestHash := sha256.Sum256(request.requestJSON)
	scopedKeyHash := sha256.Sum256([]byte(request.licenseID + "\x1f" + request.idempotencyKey))
	scopedKey := hex.EncodeToString(scopedKeyHash[:])
	for attempt := 0; attempt < 3; attempt++ {
		result, retry, err := applyPlatformLicenseAssignmentAttempt(ctx, request, requestHash[:], scopedKey)
		if retry {
			continue
		}
		return result, err
	}
	return nil, fmt.Errorf("platform license assignment: account ownership changed repeatedly")
}

func applyPlatformLicenseAssignmentAttempt(
	ctx context.Context,
	request platformLicenseAssignmentRequest,
	requestHash []byte,
	scopedKey string,
) (*PlatformCosmeticLicense, bool, error) {
	var observedOwner *string
	if err := Pool.QueryRow(ctx, `SELECT account_id FROM cosmetic_licenses WHERE id = $1`, request.licenseID).Scan(&observedOwner); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, ErrCosmeticLicenseNotFound
		}
		return nil, false, fmt.Errorf("platform license assignment owner: %w", err)
	}
	if observedOwner == nil {
		return nil, false, ErrCosmeticLicenseNotOwned
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("platform license assignment begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, request.operation+":"+request.licenseID+":"+request.idempotencyKey); err != nil {
		return nil, false, fmt.Errorf("platform license assignment idempotency lock: %w", err)
	}
	if _, err := lockCustomerAccount(ctx, tx, *observedOwner, false); err != nil {
		return nil, false, err
	}
	result, actualOwner, err := lockPlatformCosmeticLicenseTx(ctx, tx, request.licenseID)
	if err != nil {
		return nil, false, err
	}
	if actualOwner == nil || *actualOwner != *observedOwner {
		return nil, true, nil
	}

	replayed, err := loadPlatformLicenseIdempotencyTx(ctx, tx, request.operation, scopedKey, requestHash)
	if err != nil {
		return nil, false, err
	}
	if replayed != nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, false, fmt.Errorf("platform license assignment replay commit: %w", err)
		}
		return replayed, false, nil
	}
	if result.Revision != request.expectedRevision {
		return nil, false, fmt.Errorf("%w: expected %d, current %d", ErrPlatformRevisionConflict, request.expectedRevision, result.Revision)
	}
	if result.Status != "active" {
		return nil, false, fmt.Errorf("%w: license %q is %s", ErrCosmeticInactive, request.licenseID, result.Status)
	}
	if request.agentID != nil && result.AssignedAgentID != nil && *request.agentID != *result.AssignedAgentID {
		return nil, false, ErrCosmeticLicenseAlreadyAssigned
	}
	if blocked, err := cosmeticLicenseBlockedByAdminMembership(ctx, tx, request.licenseID); err != nil {
		return nil, false, fmt.Errorf("platform license assignment membership: %w", err)
	} else if blocked {
		return nil, false, ErrCosmeticInactive
	}
	if request.agentID != nil {
		var agentStatus string
		if err := tx.QueryRow(ctx, `
			SELECT agents.status
			FROM account_bot_links AS links
			JOIN platform_agents AS agents ON agents.agent_id = links.bot_id
			WHERE links.account_id = $1 AND links.bot_id = $2
			FOR SHARE OF links, agents`, result.AccountID, *request.agentID).Scan(&agentStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, false, ErrCustomerBotNotLinked
			}
			return nil, false, fmt.Errorf("platform license assignment agent: %w", err)
		}
		if agentStatus != "active" {
			return nil, false, ErrPlatformAgentInactive
		}
	}

	if _, err := applyCosmeticLicenseAssignmentTx(ctx, tx, result, request.agentID, request.transition, request.source, request.reason); err != nil {
		return nil, false, err
	}
	if err := storePlatformLicenseIdempotencyTx(ctx, tx, request.operation, scopedKey, requestHash, result); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("platform license assignment commit: %w", err)
	}
	return result, false, nil
}

func applyCosmeticLicenseAssignmentTx(
	ctx context.Context,
	tx pgx.Tx,
	result *PlatformCosmeticLicense,
	agentID *string,
	transition, source, reason string,
) (bool, error) {
	sameAssignment := (result.AssignedAgentID == nil) == (agentID == nil)
	if sameAssignment && result.AssignedAgentID != nil {
		sameAssignment = *result.AssignedAgentID == *agentID
	}
	if sameAssignment {
		return false, nil
	}
	previousAgentID := result.AssignedAgentID
	if _, err := tx.Exec(ctx, `DELETE FROM cosmetic_license_assignments WHERE license_id = $1`, result.LicenseID); err != nil {
		return false, fmt.Errorf("cosmetic license lifecycle clear assignment: %w", err)
	}
	if agentID != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO cosmetic_license_assignments (license_id, account_id, bot_id, assigned_at)
			VALUES ($1, $2, $3, NOW())`, result.LicenseID, result.AccountID, *agentID); err != nil {
			return false, fmt.Errorf("cosmetic license lifecycle insert assignment: %w", err)
		}
	}
	changedAt := time.Now().UTC().Truncate(time.Microsecond)
	result.AssignedAgentID = agentID
	result.Revision++
	result.UpdatedAt = changedAt
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_licenses SET assigned_bot_id = NULL, revision = $2, updated_at = $3 WHERE id = $1`,
		result.LicenseID, result.Revision, changedAt); err != nil {
		return false, fmt.Errorf("cosmetic license lifecycle assignment revision: %w", err)
	}
	accountID := any(nil)
	if result.AccountID != "" {
		accountID = result.AccountID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_license_lifecycle_events (
			license_id, account_id, status, previous_status, current_status,
			assigned_agent_id, previous_agent_id,
			current_agent_id, transition, revision, source, reason, occurred_at
		) VALUES ($1, $2, $3, $3, $3, $4, $5, $4, $6, $7, $8, $9, $10)`,
		result.LicenseID, accountID, result.Status, agentID, previousAgentID,
		transition, result.Revision, source, reason, changedAt,
	); err != nil {
		return false, fmt.Errorf("cosmetic license lifecycle assignment history: %w", err)
	}
	if err := appendPlatformLicenseChangeTx(ctx, tx, "license_assignment", result.LicenseID, transition, result.Revision, changedAt); err != nil {
		return false, fmt.Errorf("cosmetic license lifecycle assignment change: %w", err)
	}
	return true, nil
}

func claimCosmeticLicenseTx(
	ctx context.Context,
	tx pgx.Tx,
	licenseID, accountID string,
	preserveAgentID *string,
	reason string,
) (*PlatformCosmeticLicense, error) {
	result, owner, err := lockPlatformCosmeticLicenseTx(ctx, tx, licenseID)
	if err != nil {
		return nil, err
	}
	if owner != nil {
		if *owner == accountID {
			return result, nil
		}
		return nil, ErrCosmeticLicenseGrantConflict
	}
	if result.Status != "active" {
		preserveAgentID = nil
	}
	previousAgentID := result.AssignedAgentID
	if preserveAgentID == nil {
		if _, err := tx.Exec(ctx, `DELETE FROM bot_cosmetic_loadout WHERE license_id = $1`, licenseID); err != nil {
			return nil, fmt.Errorf("cosmetic license lifecycle claim loadout cleanup: %w", err)
		}
	}
	changedAt := time.Now().UTC().Truncate(time.Microsecond)
	result.AccountID = accountID
	result.AssignedAgentID = preserveAgentID
	result.Revision++
	result.UpdatedAt = changedAt
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_licenses
		SET account_id = $2, legacy_bot_id = NULL, assigned_bot_id = NULL,
		    revision = $3, updated_at = $4
		WHERE id = $1 AND account_id IS NULL`, licenseID, accountID, result.Revision, changedAt); err != nil {
		return nil, fmt.Errorf("cosmetic license lifecycle claim: %w", err)
	}
	if preserveAgentID != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO cosmetic_license_assignments (license_id, account_id, bot_id, assigned_at)
			VALUES ($1, $2, $3, $4)`, licenseID, accountID, *preserveAgentID, changedAt); err != nil {
			return nil, fmt.Errorf("cosmetic license lifecycle claim assignment: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE bot_cosmetic_loadout SET account_id = $2, updated_at = $4
			WHERE license_id = $1 AND bot_id = $3`, licenseID, accountID, *preserveAgentID, changedAt); err != nil {
			return nil, fmt.Errorf("cosmetic license lifecycle claim loadout: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_license_lifecycle_events (
			license_id, account_id, status, previous_status, current_status,
			assigned_agent_id, previous_agent_id,
			current_agent_id, transition, revision, source, reason, occurred_at
		) VALUES ($1, $2, $3, $3, $3, $4, $5, $4, 'claimed', $6, 'arena', $7, $8)`,
		licenseID, accountID, result.Status, preserveAgentID, previousAgentID, result.Revision, reason, changedAt,
	); err != nil {
		return nil, fmt.Errorf("cosmetic license lifecycle claim history: %w", err)
	}
	if err := appendPlatformLicenseChangeTx(ctx, tx, "license", licenseID, "updated", result.Revision, changedAt); err != nil {
		return nil, fmt.Errorf("cosmetic license lifecycle claim change: %w", err)
	}
	if preserveAgentID != nil {
		if err := appendPlatformLicenseChangeTx(ctx, tx, "license_assignment", licenseID, "assigned", result.Revision, changedAt); err != nil {
			return nil, fmt.Errorf("cosmetic license lifecycle claim assignment change: %w", err)
		}
	} else if previousAgentID != nil {
		if err := appendPlatformLicenseChangeTx(ctx, tx, "license_assignment", licenseID, "unassigned", result.Revision, changedAt); err != nil {
			return nil, fmt.Errorf("cosmetic license lifecycle claim unassignment change: %w", err)
		}
	}
	return result, nil
}

func appendPlatformLicenseChangeTx(
	ctx context.Context,
	tx pgx.Tx,
	subjectKind, subjectID, transition string,
	revision int64,
	changedAt time.Time,
) error {
	if _, err := tx.Exec(ctx, insertPlatformChangeSQL,
		subjectKind, subjectID, transition, revision, changedAt,
	); err != nil {
		return fmt.Errorf("append platform license change: %w", err)
	}
	return nil
}

func lockPlatformCosmeticLicenseTx(ctx context.Context, tx pgx.Tx, licenseID string) (*PlatformCosmeticLicense, *string, error) {
	var result PlatformCosmeticLicense
	var owner *string
	if err := tx.QueryRow(ctx, `
		SELECT licenses.account_id, licenses.cosmetic_id,
		       COALESCE(assignments.bot_id, licenses.assigned_bot_id),
		       licenses.status, licenses.revision, licenses.granted_at, licenses.updated_at
		FROM cosmetic_licenses AS licenses
		LEFT JOIN cosmetic_license_assignments AS assignments ON assignments.license_id = licenses.id
		WHERE licenses.id = $1
		FOR UPDATE OF licenses`, licenseID).Scan(
		&owner, &result.CosmeticID, &result.AssignedAgentID, &result.Status,
		&result.Revision, &result.CreatedAt, &result.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrCosmeticLicenseNotFound
		}
		return nil, nil, fmt.Errorf("lock platform cosmetic license: %w", err)
	}
	result.LicenseID = licenseID
	if owner != nil {
		result.AccountID = *owner
	}
	return &result, owner, nil
}

func loadPlatformLicenseIdempotencyTx(
	ctx context.Context,
	tx pgx.Tx,
	operation, scopedKey string,
	requestHash []byte,
) (*PlatformCosmeticLicense, error) {
	var storedHash, storedResponse []byte
	err := tx.QueryRow(ctx, `
		SELECT request_hash, response
		FROM platform_idempotency_records
		WHERE operation = $1 AND idempotency_key = $2`, operation, scopedKey).Scan(&storedHash, &storedResponse)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load platform license idempotency: %w", err)
	}
	if !bytes.Equal(storedHash, requestHash) {
		return nil, ErrPlatformIdempotencyConflict
	}
	var replayed PlatformCosmeticLicense
	if err := json.Unmarshal(storedResponse, &replayed); err != nil {
		return nil, fmt.Errorf("decode platform license replay: %w", err)
	}
	return &replayed, nil
}

func storePlatformLicenseIdempotencyTx(
	ctx context.Context,
	tx pgx.Tx,
	operation, scopedKey string,
	requestHash []byte,
	result *PlatformCosmeticLicense,
) error {
	responseJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal platform license response: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_idempotency_records (
			operation, idempotency_key, request_hash, response,
			subject_kind, subject_id, revision, created_at
		) VALUES ($1, $2, $3, $4, 'license', $5, $6, $7)`,
		operation, scopedKey, requestHash, responseJSON,
		result.LicenseID, result.Revision, time.Now().UTC().Truncate(time.Microsecond),
	); err != nil {
		return fmt.Errorf("store platform license idempotency: %w", err)
	}
	return nil
}

func TransitionPlatformLicense(ctx context.Context, command PlatformLicenseTransitionCommand) (*PlatformCosmeticLicense, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if command.LicenseID == "" || strings.TrimSpace(command.LicenseID) != command.LicenseID || utf8.RuneCountInString(command.LicenseID) > 128 {
		return nil, errors.New("platform license transition requires a valid license ID")
	}
	switch command.TargetStatus {
	case "refunded", "revoked", "chargeback", "expired":
	default:
		return nil, errors.New("platform license transition requires a terminal target status")
	}
	if command.ExpectedRevision < 0 {
		return nil, errors.New("platform license transition requires a nonnegative expected revision")
	}
	if command.Reason == "" || strings.TrimSpace(command.Reason) != command.Reason || utf8.RuneCountInString(command.Reason) > 256 {
		return nil, errors.New("platform license transition requires a 1-256 character reason without surrounding whitespace")
	}
	if strings.TrimSpace(command.ProviderReference) != command.ProviderReference || utf8.RuneCountInString(command.ProviderReference) > 256 {
		return nil, errors.New("platform license transition provider reference must be at most 256 characters without surrounding whitespace")
	}
	keyLength := utf8.RuneCountInString(command.IdempotencyKey)
	if strings.TrimSpace(command.IdempotencyKey) != command.IdempotencyKey || keyLength < platformIdempotencyKeyMinimum || keyLength > platformIdempotencyKeyMaximum {
		return nil, errors.New("platform license transition requires an 8-128 character idempotency key without surrounding whitespace")
	}

	requestJSON, err := json.Marshal(struct {
		LicenseID         string `json:"license_id"`
		TargetStatus      string `json:"target_status"`
		ExpectedRevision  int64  `json:"expected_license_revision"`
		Reason            string `json:"reason"`
		ProviderReference string `json:"provider_reference,omitempty"`
	}{
		LicenseID: command.LicenseID, TargetStatus: command.TargetStatus,
		ExpectedRevision: command.ExpectedRevision, Reason: command.Reason,
		ProviderReference: command.ProviderReference,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal platform license transition request: %w", err)
	}
	requestHash := sha256.Sum256(requestJSON)
	scopedKeyHash := sha256.Sum256([]byte(command.LicenseID + "\x1f" + command.IdempotencyKey))
	scopedKey := hex.EncodeToString(scopedKeyHash[:])

	for attempt := 0; attempt < 3; attempt++ {
		result, retry, err := transitionPlatformLicenseAttempt(ctx, command, requestHash[:], scopedKey)
		if retry {
			continue
		}
		return result, err
	}
	return nil, fmt.Errorf("TransitionPlatformLicense: account ownership changed repeatedly")
}

func transitionPlatformLicenseAttempt(
	ctx context.Context,
	command PlatformLicenseTransitionCommand,
	requestHash []byte,
	scopedKey string,
) (*PlatformCosmeticLicense, bool, error) {
	var observedOwner *string
	if err := Pool.QueryRow(ctx, `SELECT account_id FROM cosmetic_licenses WHERE id = $1`, command.LicenseID).Scan(&observedOwner); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, ErrCosmeticLicenseNotFound
		}
		return nil, false, fmt.Errorf("TransitionPlatformLicense owner: %w", err)
	}
	if observedOwner == nil {
		return nil, false, ErrCosmeticLicenseNotOwned
	}

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("TransitionPlatformLicense begin: %w", err)
	}
	defer tx.Rollback(ctx)
	const operation = "transition_license_lifecycle"
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, operation+":"+command.LicenseID+":"+command.IdempotencyKey); err != nil {
		return nil, false, fmt.Errorf("TransitionPlatformLicense idempotency lock: %w", err)
	}
	if _, err := lockCustomerAccount(ctx, tx, *observedOwner, false); err != nil {
		return nil, false, err
	}

	var result PlatformCosmeticLicense
	var actualOwner *string
	if err := tx.QueryRow(ctx, `
		SELECT licenses.account_id, licenses.cosmetic_id,
		       COALESCE(assignments.bot_id, licenses.assigned_bot_id),
		       licenses.status, licenses.revision, licenses.granted_at, licenses.updated_at
		FROM cosmetic_licenses AS licenses
		LEFT JOIN cosmetic_license_assignments AS assignments ON assignments.license_id = licenses.id
		WHERE licenses.id = $1
		FOR UPDATE OF licenses`, command.LicenseID).Scan(
		&actualOwner, &result.CosmeticID, &result.AssignedAgentID, &result.Status,
		&result.Revision, &result.CreatedAt, &result.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, ErrCosmeticLicenseNotFound
		}
		return nil, false, fmt.Errorf("TransitionPlatformLicense lock: %w", err)
	}
	ownerChanged := (observedOwner == nil) != (actualOwner == nil) ||
		(observedOwner != nil && actualOwner != nil && *observedOwner != *actualOwner)
	if ownerChanged {
		return nil, true, nil
	}
	result.LicenseID = command.LicenseID
	if actualOwner != nil {
		result.AccountID = *actualOwner
	}

	var storedHash, storedResponse []byte
	err = tx.QueryRow(ctx, `
		SELECT request_hash, response
		FROM platform_idempotency_records
		WHERE operation = $1 AND idempotency_key = $2`, operation, scopedKey).Scan(&storedHash, &storedResponse)
	if err == nil {
		if !bytes.Equal(storedHash, requestHash) {
			return nil, false, ErrPlatformIdempotencyConflict
		}
		var replayed PlatformCosmeticLicense
		if err := json.Unmarshal(storedResponse, &replayed); err != nil {
			return nil, false, fmt.Errorf("TransitionPlatformLicense decode replay: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, false, fmt.Errorf("TransitionPlatformLicense replay commit: %w", err)
		}
		return &replayed, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("TransitionPlatformLicense load idempotency: %w", err)
	}
	if result.Revision != command.ExpectedRevision {
		return nil, false, fmt.Errorf("%w: expected %d, current %d", ErrPlatformRevisionConflict, command.ExpectedRevision, result.Revision)
	}
	currentPrecedence, currentKnown := cosmeticLicenseStatusPrecedence(result.Status)
	targetPrecedence, targetKnown := cosmeticLicenseStatusPrecedence(command.TargetStatus)
	if !currentKnown || !targetKnown || targetPrecedence < currentPrecedence {
		return nil, false, fmt.Errorf("%w: license %q cannot move from %s to %s", ErrCosmeticInactive, command.LicenseID, result.Status, command.TargetStatus)
	}

	if _, err := applyCosmeticLicenseTerminalTransitionTx(
		ctx, tx, &result, command.TargetStatus, "platform", command.Reason, command.ProviderReference,
	); err != nil {
		return nil, false, err
	}
	changedAt := result.UpdatedAt
	responseJSON, err := json.Marshal(result)
	if err != nil {
		return nil, false, fmt.Errorf("TransitionPlatformLicense marshal response: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_idempotency_records (
			operation, idempotency_key, request_hash, response,
			subject_kind, subject_id, revision, created_at
		) VALUES ($1, $2, $3, $4, 'license', $5, $6, $7)`,
		operation, scopedKey, requestHash, responseJSON,
		command.LicenseID, result.Revision, changedAt,
	); err != nil {
		return nil, false, fmt.Errorf("TransitionPlatformLicense store idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("TransitionPlatformLicense commit: %w", err)
	}
	return &result, false, nil
}
