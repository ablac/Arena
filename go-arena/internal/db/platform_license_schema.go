package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func ensurePlatformLicenseLifecycleSchemaTx(ctx context.Context, tx pgx.Tx) error {
	statements := []string{
		`DO $$
		DECLARE invalid_id TEXT;
		BEGIN
			SELECT licenses.id INTO invalid_id
			FROM cosmetic_licenses AS licenses
			WHERE char_length(licenses.id) NOT BETWEEN 1 AND 128
			   OR char_length(licenses.cosmetic_id) NOT BETWEEN 1 AND 128
			   OR (licenses.account_id IS NOT NULL AND char_length(licenses.account_id) NOT BETWEEN 1 AND 128)
			ORDER BY licenses.id
			LIMIT 1;
			IF invalid_id IS NOT NULL THEN
				RAISE EXCEPTION 'legacy cosmetic license % has an identifier outside the 1-128 character platform contract', invalid_id;
			END IF;
		END
		$$`,
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'cosmetic_licenses'::regclass
				  AND conname = 'cosmetic_licenses_status_check'
				  AND POSITION('expired' IN pg_get_constraintdef(oid)) > 0
			) THEN
				ALTER TABLE cosmetic_licenses DROP CONSTRAINT IF EXISTS cosmetic_licenses_status_check;
				ALTER TABLE cosmetic_licenses ADD CONSTRAINT cosmetic_licenses_status_check
					CHECK (status IN ('active', 'refunded', 'revoked', 'chargeback', 'expired')) NOT VALID;
			END IF;
		END
		$$`,
		`ALTER TABLE cosmetic_licenses VALIDATE CONSTRAINT cosmetic_licenses_status_check`,
		`ALTER TABLE cosmetic_licenses
			ADD COLUMN IF NOT EXISTS revision BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1)`,
		`ALTER TABLE cosmetic_licenses
			ADD COLUMN IF NOT EXISTS terminal_at TIMESTAMPTZ`,
		`ALTER TABLE cosmetic_subscription_licenses
			ADD COLUMN IF NOT EXISTS generation BIGINT NOT NULL DEFAULT 1 CHECK (generation >= 1)`,
		`ALTER TABLE cosmetic_entitlements
			ADD COLUMN IF NOT EXISTS license_id TEXT`,
		`UPDATE cosmetic_entitlements AS entitlements
			SET license_id = licenses.id
			FROM cosmetic_licenses AS licenses
			WHERE entitlements.license_id IS NULL
			  AND licenses.id = 'legacy-' || MD5(entitlements.bot_id || CHR(31) || entitlements.cosmetic_id)`,
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conrelid = 'cosmetic_entitlements'::regclass
				  AND conname = 'cosmetic_entitlements_license_fk'
			) THEN
				ALTER TABLE cosmetic_entitlements
					ADD CONSTRAINT cosmetic_entitlements_license_fk
					FOREIGN KEY (license_id) REFERENCES cosmetic_licenses(id) ON DELETE RESTRICT
					DEFERRABLE INITIALLY DEFERRED;
			END IF;
		END
		$$`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cosmetic_entitlements_license
			ON cosmetic_entitlements (license_id)`,
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1
				FROM pg_constraint
				WHERE conrelid = 'platform_changes'::regclass
				  AND conname = 'platform_changes_subject_kind_check'
				  AND pg_get_constraintdef(oid) LIKE '%license_assignment%'
			) THEN
				ALTER TABLE platform_changes DROP CONSTRAINT IF EXISTS platform_changes_subject_kind_check;
				ALTER TABLE platform_changes ADD CONSTRAINT platform_changes_subject_kind_check
					CHECK (subject_kind IN ('account', 'agent', 'game_profile', 'agent_link', 'license', 'license_assignment')) NOT VALID;
			END IF;
		END
		$$`,
		`ALTER TABLE platform_changes VALIDATE CONSTRAINT platform_changes_subject_kind_check`,
		`CREATE TABLE IF NOT EXISTS platform_license_lifecycle_events (
			event_id BIGSERIAL PRIMARY KEY,
			license_id TEXT NOT NULL REFERENCES cosmetic_licenses(id) ON DELETE RESTRICT,
			account_id TEXT REFERENCES customer_accounts(id) ON DELETE RESTRICT,
			status TEXT NOT NULL CHECK (status IN ('active', 'refunded', 'revoked', 'chargeback', 'expired')),
			previous_status TEXT CHECK (previous_status IS NULL OR previous_status IN ('active', 'refunded', 'revoked', 'chargeback', 'expired')),
			current_status TEXT NOT NULL DEFAULT 'active' CHECK (current_status IN ('active', 'refunded', 'revoked', 'chargeback', 'expired')),
			assigned_agent_id TEXT,
			previous_agent_id TEXT,
			current_agent_id TEXT,
			transition TEXT NOT NULL CHECK (transition IN ('created', 'claimed', 'assigned', 'unassigned', 'refunded', 'revoked', 'chargeback', 'expired')),
			revision BIGINT NOT NULL CHECK (revision >= 1),
			source TEXT NOT NULL DEFAULT 'arena',
			reason TEXT NOT NULL,
			provider_reference TEXT,
			occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (license_id, revision)
		)`,
		`ALTER TABLE platform_license_lifecycle_events ADD COLUMN IF NOT EXISTS previous_agent_id TEXT`,
		`ALTER TABLE platform_license_lifecycle_events ADD COLUMN IF NOT EXISTS current_agent_id TEXT`,
		`ALTER TABLE platform_license_lifecycle_events ADD COLUMN IF NOT EXISTS previous_status TEXT`,
		`ALTER TABLE platform_license_lifecycle_events ADD COLUMN IF NOT EXISTS current_status TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE platform_license_lifecycle_events ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'arena'`,
		`UPDATE platform_license_lifecycle_events SET current_status = status WHERE current_status <> status`,
		`UPDATE platform_license_lifecycle_events
			SET current_agent_id = assigned_agent_id
			WHERE current_agent_id IS NULL AND assigned_agent_id IS NOT NULL`,
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1
				FROM pg_constraint
				WHERE conrelid = 'platform_license_lifecycle_events'::regclass
				  AND conname = 'platform_license_lifecycle_events_transition_check'
				  AND pg_get_constraintdef(oid) LIKE '%claimed%'
			) THEN
				ALTER TABLE platform_license_lifecycle_events
					DROP CONSTRAINT platform_license_lifecycle_events_transition_check;
				ALTER TABLE platform_license_lifecycle_events
					ADD CONSTRAINT platform_license_lifecycle_events_transition_check
					CHECK (transition IN ('created', 'claimed', 'assigned', 'unassigned', 'refunded', 'revoked', 'chargeback', 'expired'));
			END IF;
		END
		$$`,
		`CREATE INDEX IF NOT EXISTS idx_platform_license_lifecycle_account
			ON platform_license_lifecycle_events (account_id, event_id)
			INCLUDE (license_id, status, previous_status, current_status, previous_agent_id, current_agent_id, transition, revision, source, reason, provider_reference, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_platform_license_lifecycle_license
			ON platform_license_lifecycle_events (license_id, event_id)
			INCLUDE (account_id, status, previous_status, current_status, previous_agent_id, current_agent_id, transition, revision, source, reason, provider_reference, occurred_at)`,
		`INSERT INTO platform_license_lifecycle_events (
			license_id, account_id, status, previous_status, current_status,
			assigned_agent_id, previous_agent_id,
			current_agent_id, transition, revision,
			source, reason, provider_reference, occurred_at
		)
		SELECT licenses.id, licenses.account_id, licenses.status, NULL, licenses.status,
		       CASE WHEN licenses.status = 'active'
		            THEN COALESCE(assignments.bot_id, licenses.assigned_bot_id) END,
		       CASE WHEN licenses.status <> 'active'
		            THEN COALESCE(assignments.bot_id, licenses.assigned_bot_id) END,
		       CASE WHEN licenses.status = 'active'
		            THEN COALESCE(assignments.bot_id, licenses.assigned_bot_id) END,
		       'created', licenses.revision, licenses.source, 'arena_import', licenses.external_reference,
		       licenses.granted_at
		FROM cosmetic_licenses AS licenses
		LEFT JOIN cosmetic_license_assignments AS assignments ON assignments.license_id = licenses.id
		WHERE NOT EXISTS (
			SELECT 1 FROM platform_license_lifecycle_events AS events
			WHERE events.license_id = licenses.id
		)
		ORDER BY licenses.id
		ON CONFLICT (license_id, revision) DO NOTHING`,
		`DELETE FROM bot_cosmetic_loadout AS loadouts
			USING cosmetic_licenses AS licenses
			WHERE loadouts.license_id = licenses.id AND licenses.status <> 'active'`,
		`DELETE FROM cosmetic_license_assignments AS assignments
			USING cosmetic_licenses AS licenses
			WHERE assignments.license_id = licenses.id AND licenses.status <> 'active'`,
		`UPDATE cosmetic_licenses
			SET assigned_bot_id = NULL
			WHERE status <> 'active' AND assigned_bot_id IS NOT NULL`,
		`UPDATE cosmetic_licenses
			SET terminal_at = updated_at
			WHERE status <> 'active' AND terminal_at IS NULL`,
		`CREATE OR REPLACE FUNCTION enforce_cosmetic_license_status_monotonic()
		RETURNS TRIGGER
		LANGUAGE plpgsql
		AS $$
		DECLARE
			old_rank INTEGER;
			new_rank INTEGER;
		BEGIN
			old_rank := CASE OLD.status
				WHEN 'active' THEN 0 WHEN 'expired' THEN 1 WHEN 'revoked' THEN 2
				WHEN 'refunded' THEN 3 WHEN 'chargeback' THEN 4 ELSE -1 END;
			new_rank := CASE NEW.status
				WHEN 'active' THEN 0 WHEN 'expired' THEN 1 WHEN 'revoked' THEN 2
				WHEN 'refunded' THEN 3 WHEN 'chargeback' THEN 4 ELSE -1 END;
			IF new_rank < old_rank THEN
				RAISE EXCEPTION 'cosmetic license status cannot move from % to %', OLD.status, NEW.status
					USING ERRCODE = '23514';
			END IF;
			RETURN NEW;
		END
		$$`,
		`DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_trigger
				WHERE tgrelid = 'cosmetic_licenses'::regclass
				  AND tgname = 'cosmetic_license_status_monotonic'
				  AND NOT tgisinternal
			) THEN
				CREATE TRIGGER cosmetic_license_status_monotonic
					BEFORE UPDATE OF status ON cosmetic_licenses
					FOR EACH ROW EXECUTE FUNCTION enforce_cosmetic_license_status_monotonic();
			END IF;
		END
		$$`,
		`INSERT INTO platform_changes (
			subject_kind, subject_id, transition, revision, changed_at
		)
		SELECT CASE WHEN events.transition IN ('assigned', 'unassigned')
		            THEN 'license_assignment' ELSE 'license' END,
		       events.license_id,
		       CASE WHEN events.transition = 'claimed' THEN 'updated' ELSE events.transition END,
		       events.revision,
		       events.occurred_at
		FROM platform_license_lifecycle_events AS events
		ORDER BY events.event_id
		ON CONFLICT (subject_kind, subject_id, revision) DO NOTHING`,
		`INSERT INTO platform_changes (
			subject_kind, subject_id, transition, revision, changed_at
		)
		SELECT 'license_assignment', events.license_id, 'assigned', events.revision, events.occurred_at
		FROM platform_license_lifecycle_events AS events
		WHERE events.current_agent_id IS NOT NULL
		UNION ALL
		SELECT 'license_assignment', events.license_id, 'unassigned', events.revision, events.occurred_at
		FROM platform_license_lifecycle_events AS events
		WHERE events.previous_agent_id IS NOT NULL AND events.current_agent_id IS NULL
		ON CONFLICT (subject_kind, subject_id, revision) DO NOTHING`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
