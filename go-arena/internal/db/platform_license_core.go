package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type cosmeticLicenseCreate struct {
	LicenseID         string
	AccountID         *string
	LegacyBotID       *string
	CosmeticID        string
	AssignedAgentID   *string
	Source            string
	Reason            string
	ExternalReference string
	GrantedAt         time.Time
}

func createCosmeticLicenseTx(ctx context.Context, tx pgx.Tx, input cosmeticLicenseCreate) (bool, error) {
	inserted, err := createCosmeticLicensesTx(ctx, tx, []cosmeticLicenseCreate{input})
	return inserted[input.LicenseID], err
}

func createCosmeticLicensesTx(ctx context.Context, tx pgx.Tx, inputs []cosmeticLicenseCreate) (map[string]bool, error) {
	inserted := make(map[string]bool, len(inputs))
	if len(inputs) == 0 {
		return inserted, nil
	}
	licenseIDs := make([]string, 0, len(inputs))
	accountIDs := make([]string, 0, len(inputs))
	legacyBotIDs := make([]string, 0, len(inputs))
	cosmeticIDs := make([]string, 0, len(inputs))
	assignedAgentIDs := make([]string, 0, len(inputs))
	sources := make([]string, 0, len(inputs))
	reasons := make([]string, 0, len(inputs))
	externalReferences := make([]string, 0, len(inputs))
	grantedTimes := make([]time.Time, 0, len(inputs))
	now := time.Now().UTC().Truncate(time.Microsecond)
	seen := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if _, duplicate := seen[input.LicenseID]; duplicate {
			return nil, fmt.Errorf("cosmetic license lifecycle create duplicate license ID %q", input.LicenseID)
		}
		seen[input.LicenseID] = struct{}{}
		accountID := ""
		if input.AccountID != nil {
			accountID = *input.AccountID
		}
		legacyBotID := ""
		if input.LegacyBotID != nil {
			legacyBotID = *input.LegacyBotID
		}
		assignedAgentID := ""
		if input.AssignedAgentID != nil {
			assignedAgentID = *input.AssignedAgentID
		}
		grantedAt := input.GrantedAt.UTC().Truncate(time.Microsecond)
		if grantedAt.IsZero() {
			grantedAt = now
		}
		licenseIDs = append(licenseIDs, input.LicenseID)
		accountIDs = append(accountIDs, accountID)
		legacyBotIDs = append(legacyBotIDs, legacyBotID)
		cosmeticIDs = append(cosmeticIDs, input.CosmeticID)
		assignedAgentIDs = append(assignedAgentIDs, assignedAgentID)
		sources = append(sources, input.Source)
		reason := input.Reason
		if reason == "" {
			reason = "created"
		}
		reasons = append(reasons, reason)
		externalReferences = append(externalReferences, input.ExternalReference)
		grantedTimes = append(grantedTimes, grantedAt)
	}
	rows, err := tx.Query(ctx, `
		WITH input AS MATERIALIZED (
			SELECT license_id, NULLIF(account_id, '') AS account_id,
			       NULLIF(legacy_bot_id, '') AS legacy_bot_id, cosmetic_id,
			       NULLIF(assigned_agent_id, '') AS assigned_agent_id,
			       source, reason, NULLIF(external_reference, '') AS external_reference, granted_at
			FROM UNNEST(
				$1::TEXT[], $2::TEXT[], $3::TEXT[], $4::TEXT[],
				$5::TEXT[], $6::TEXT[], $7::TEXT[], $8::TEXT[], $9::TIMESTAMPTZ[]
			) AS candidates(
				license_id, account_id, legacy_bot_id, cosmetic_id,
				assigned_agent_id, source, reason, external_reference, granted_at
			)
		), inserted AS MATERIALIZED (
			INSERT INTO cosmetic_licenses (
				id, account_id, legacy_bot_id, cosmetic_id, assigned_bot_id,
				status, source, external_reference, revision, granted_at, updated_at
			)
			SELECT license_id, account_id, legacy_bot_id, cosmetic_id, assigned_agent_id,
			       'active', source, external_reference, 1, granted_at, granted_at
			FROM input
			ORDER BY license_id
			ON CONFLICT (id) DO NOTHING
			RETURNING id, account_id, assigned_bot_id, source, external_reference, granted_at
		), history AS (
			INSERT INTO platform_license_lifecycle_events (
				license_id, account_id, status, previous_status, current_status,
				assigned_agent_id, previous_agent_id,
				current_agent_id, transition, revision,
				source, reason, provider_reference, occurred_at
			)
			SELECT inserted.id, inserted.account_id, 'active', NULL, 'active', inserted.assigned_bot_id,
			       NULL, inserted.assigned_bot_id, 'created', 1,
			       inserted.source, input.reason, inserted.external_reference, inserted.granted_at
			FROM inserted
			JOIN input ON input.license_id = inserted.id
			RETURNING license_id
		), changes AS (
			INSERT INTO platform_changes (
				subject_kind, subject_id, transition, revision, changed_at
			)
			SELECT 'license', id, 'created', 1, granted_at FROM inserted
			UNION ALL
			SELECT 'license_assignment', id, 'assigned', 1, granted_at
			FROM inserted WHERE assigned_bot_id IS NOT NULL
			ON CONFLICT (subject_kind, subject_id, revision) DO NOTHING
			RETURNING subject_id
		)
		SELECT inserted.id
		FROM inserted
		JOIN history ON history.license_id = inserted.id
		ORDER BY inserted.id`,
		licenseIDs, accountIDs, legacyBotIDs, cosmeticIDs,
		assignedAgentIDs, sources, reasons, externalReferences, grantedTimes,
	)
	if err != nil {
		return nil, fmt.Errorf("cosmetic license lifecycle batch create: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var licenseID string
		if err := rows.Scan(&licenseID); err != nil {
			return nil, fmt.Errorf("cosmetic license lifecycle batch create scan: %w", err)
		}
		inserted[licenseID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cosmetic license lifecycle batch create rows: %w", err)
	}
	return inserted, nil
}

func applyCosmeticLicenseTerminalTransitionTx(
	ctx context.Context,
	tx pgx.Tx,
	result *PlatformCosmeticLicense,
	targetStatus, source, reason, providerReference string,
) (bool, error) {
	currentPrecedence, currentKnown := cosmeticLicenseStatusPrecedence(result.Status)
	targetPrecedence, targetKnown := cosmeticLicenseStatusPrecedence(targetStatus)
	if !currentKnown || !targetKnown || targetStatus == "active" {
		return false, fmt.Errorf("cosmetic license lifecycle invalid transition %q -> %q", result.Status, targetStatus)
	}
	if targetPrecedence <= currentPrecedence {
		return false, nil
	}
	previousAgentID := result.AssignedAgentID
	previousStatus := result.Status
	changedAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := tx.Exec(ctx, `DELETE FROM bot_cosmetic_loadout WHERE license_id = $1`, result.LicenseID); err != nil {
		return false, fmt.Errorf("cosmetic license lifecycle terminal loadout cleanup: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cosmetic_license_assignments WHERE license_id = $1`, result.LicenseID); err != nil {
		return false, fmt.Errorf("cosmetic license lifecycle terminal assignment cleanup: %w", err)
	}
	result.Status = targetStatus
	result.AssignedAgentID = nil
	result.Revision++
	result.UpdatedAt = changedAt
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_licenses
		SET status = $2, assigned_bot_id = NULL, revision = $3,
		    terminal_at = COALESCE(terminal_at, $4), updated_at = $4
		WHERE id = $1`, result.LicenseID, result.Status, result.Revision, changedAt); err != nil {
		return false, fmt.Errorf("cosmetic license lifecycle terminal update: %w", err)
	}
	accountID := any(nil)
	if result.AccountID != "" {
		accountID = result.AccountID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_license_lifecycle_events (
			license_id, account_id, status, previous_status, current_status,
			assigned_agent_id, previous_agent_id,
			current_agent_id, transition, revision,
			source, reason, provider_reference, occurred_at
		) VALUES ($1, $2, $3, $4, $3, NULL, $5, NULL, $3, $6, $7, $8, NULLIF($9, ''), $10)`,
		result.LicenseID, accountID, result.Status, previousStatus, previousAgentID, result.Revision,
		source, reason, providerReference, changedAt,
	); err != nil {
		return false, fmt.Errorf("cosmetic license lifecycle terminal history: %w", err)
	}
	if err := appendPlatformLicenseChangeTx(ctx, tx, "license", result.LicenseID, result.Status, result.Revision, changedAt); err != nil {
		return false, fmt.Errorf("cosmetic license lifecycle terminal change: %w", err)
	}
	if previousAgentID != nil {
		if err := appendPlatformLicenseChangeTx(ctx, tx, "license_assignment", result.LicenseID, "unassigned", result.Revision, changedAt); err != nil {
			return false, fmt.Errorf("cosmetic license lifecycle terminal assignment change: %w", err)
		}
	}
	return true, nil
}

func applyCosmeticLicenseTerminalBatchTx(
	ctx context.Context,
	tx pgx.Tx,
	licenseIDs []string,
	targetStatus, source, reason, providerReference string,
) (int, error) {
	if len(licenseIDs) == 0 {
		return 0, nil
	}
	targetPrecedence, targetKnown := cosmeticLicenseStatusPrecedence(targetStatus)
	if !targetKnown || targetStatus == "active" {
		return 0, fmt.Errorf("cosmetic license lifecycle invalid batch target %q", targetStatus)
	}
	rows, err := tx.Query(ctx, `
		SELECT licenses.id, COALESCE(licenses.account_id, ''), licenses.status,
		       COALESCE(assignments.bot_id, licenses.assigned_bot_id, ''), licenses.revision
		FROM cosmetic_licenses AS licenses
		LEFT JOIN cosmetic_license_assignments AS assignments ON assignments.license_id = licenses.id
		WHERE licenses.id = ANY($1)
		ORDER BY licenses.id
		FOR UPDATE OF licenses`, licenseIDs)
	if err != nil {
		return 0, fmt.Errorf("cosmetic license lifecycle batch lock: %w", err)
	}
	changedIDs := make([]string, 0, len(licenseIDs))
	accountIDs := make([]string, 0, len(licenseIDs))
	previousAgentIDs := make([]string, 0, len(licenseIDs))
	previousStatuses := make([]string, 0, len(licenseIDs))
	revisions := make([]int64, 0, len(licenseIDs))
	seen := 0
	for rows.Next() {
		var licenseID, accountID, status, previousAgentID string
		var revision int64
		if err := rows.Scan(&licenseID, &accountID, &status, &previousAgentID, &revision); err != nil {
			rows.Close()
			return 0, fmt.Errorf("cosmetic license lifecycle batch scan: %w", err)
		}
		seen++
		currentPrecedence, currentKnown := cosmeticLicenseStatusPrecedence(status)
		if !currentKnown {
			rows.Close()
			return 0, fmt.Errorf("cosmetic license lifecycle unknown status %q for %q", status, licenseID)
		}
		if targetPrecedence <= currentPrecedence {
			continue
		}
		changedIDs = append(changedIDs, licenseID)
		accountIDs = append(accountIDs, accountID)
		previousAgentIDs = append(previousAgentIDs, previousAgentID)
		previousStatuses = append(previousStatuses, status)
		revisions = append(revisions, revision+1)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("cosmetic license lifecycle batch rows: %w", err)
	}
	rows.Close()
	if seen != len(licenseIDs) {
		return 0, ErrCosmeticLicenseNotFound
	}
	if len(changedIDs) == 0 {
		return 0, nil
	}
	changedAt := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := tx.Exec(ctx, `DELETE FROM bot_cosmetic_loadout WHERE license_id = ANY($1)`, changedIDs); err != nil {
		return 0, fmt.Errorf("cosmetic license lifecycle batch loadout cleanup: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM cosmetic_license_assignments WHERE license_id = ANY($1)`, changedIDs); err != nil {
		return 0, fmt.Errorf("cosmetic license lifecycle batch assignment cleanup: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE cosmetic_licenses
		SET status = $2, assigned_bot_id = NULL, revision = revision + 1,
		    terminal_at = COALESCE(terminal_at, $3), updated_at = $3
		WHERE id = ANY($1)`, changedIDs, targetStatus, changedAt); err != nil {
		return 0, fmt.Errorf("cosmetic license lifecycle batch update: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_license_lifecycle_events (
			license_id, account_id, status, previous_status, current_status,
			assigned_agent_id, previous_agent_id,
			current_agent_id, transition, revision, source, reason, provider_reference, occurred_at
		)
		SELECT license_id, NULLIF(account_id, ''), $6, previous_status, $6,
		       NULL, NULLIF(previous_agent_id, ''), NULL, $6, revision, $7, $8, NULLIF($9, ''), $10
		FROM UNNEST($1::TEXT[], $2::TEXT[], $3::TEXT[], $4::TEXT[], $5::BIGINT[])
			AS transitions(license_id, account_id, previous_agent_id, previous_status, revision)`,
		changedIDs, accountIDs, previousAgentIDs, previousStatuses, revisions,
		targetStatus, source, reason, providerReference, changedAt,
	); err != nil {
		return 0, fmt.Errorf("cosmetic license lifecycle batch history: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		WITH transitions AS MATERIALIZED (
			SELECT license_id, previous_agent_id, revision
			FROM UNNEST($1::TEXT[], $2::TEXT[], $3::BIGINT[])
				AS input(license_id, previous_agent_id, revision)
		)
		INSERT INTO platform_changes (
			subject_kind, subject_id, transition, revision, changed_at
		)
		SELECT 'license', license_id, $4, revision, $5::TIMESTAMPTZ FROM transitions
		UNION ALL
		SELECT 'license_assignment', license_id, 'unassigned', revision, $5::TIMESTAMPTZ
		FROM transitions WHERE previous_agent_id <> ''`,
		changedIDs, previousAgentIDs, revisions, targetStatus, changedAt,
	); err != nil {
		return 0, fmt.Errorf("cosmetic license lifecycle batch changes: %w", err)
	}
	return len(changedIDs), nil
}

func cosmeticLicenseStatusPrecedence(status string) (int, bool) {
	switch status {
	case "active":
		return 0, true
	case "expired":
		return 1, true
	case "revoked":
		return 2, true
	case "refunded":
		return 3, true
	case "chargeback":
		return 4, true
	default:
		return 0, false
	}
}
