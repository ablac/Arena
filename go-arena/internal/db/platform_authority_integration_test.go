package db

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresPlatformAuthorityBackfillsStableArenaAgentsWithoutTouchingLicenses(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	linkedBot := createCustomerCosmeticsTestBot(t, ctx, "platform-linked")
	unlinkedBot := createCustomerCosmeticsTestBot(t, ctx, "platform-unlinked")
	account, err := UpsertVerifiedCustomerAccount(
		ctx,
		"platform-owner@example.com",
		"https://id.example",
		"platform-owner",
		"Platform Owner",
	)
	if err != nil {
		t.Fatalf("UpsertVerifiedCustomerAccount: %v", err)
	}
	if _, err := LinkBotToCustomerAccount(ctx, account.ID, linkedBot.ID); err != nil {
		t.Fatalf("LinkBotToCustomerAccount: %v", err)
	}

	activeLicense, _, err := GrantCosmeticLicense(ctx, account.Email, "skin-neon-grid", "manual", "platform-active")
	if err != nil {
		t.Fatalf("GrantCosmeticLicense active: %v", err)
	}
	if _, err := AssignCosmeticLicense(ctx, account.ID, activeLicense.ID, &linkedBot.ID); err != nil {
		t.Fatalf("AssignCosmeticLicense active: %v", err)
	}
	for _, status := range []string{"refunded", "revoked", "chargeback", "expired"} {
		license, _, err := GrantCosmeticLicense(ctx, account.Email, "attachment-orbital-halo", "manual", "platform-"+status)
		if err != nil {
			t.Fatalf("GrantCosmeticLicense %s: %v", status, err)
		}
		if _, err := Pool.Exec(ctx, `UPDATE cosmetic_licenses SET status = $2 WHERE id = $1`, license.ID, status); err != nil {
			t.Fatalf("set %s license status: %v", status, err)
		}
	}
	if _, err := GrantCosmeticEntitlement(ctx, unlinkedBot.ID, "weapon-solar-flare", "legacy-test", "platform-legacy"); err != nil {
		t.Fatalf("GrantCosmeticEntitlement: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		INSERT INTO cosmetic_subscriptions (
			id, account_id, account_email, status, terminal_at
		) VALUES ('platform-expired-subscription', $1, $2, 'expired', NOW())`, account.ID, account.Email); err != nil {
		t.Fatalf("seed expired subscription: %v", err)
	}

	licenseBefore := snapshotPlatformMigrationTable(t, ctx, "cosmetic_licenses", "id")
	assignmentBefore := snapshotPlatformMigrationTable(t, ctx, "cosmetic_license_assignments", "license_id")
	entitlementBefore := snapshotPlatformMigrationTable(t, ctx, "cosmetic_entitlements", "bot_id, cosmetic_id")
	linkBefore := snapshotPlatformMigrationTable(t, ctx, "account_bot_links", "account_id, bot_id")
	subscriptionBefore := snapshotPlatformMigrationTable(t, ctx, "cosmetic_subscriptions", "id")

	// Recreate an authentic pre-W1b.2 database: all existing Arena tables and
	// data are present, but the new platform metadata has not been installed.
	if _, err := Pool.Exec(ctx, `
		DROP TABLE IF EXISTS
			platform_idempotency_records,
			platform_agent_link_events,
			platform_changes,
			platform_game_profiles,
			platform_agents`); err != nil {
		t.Fatalf("drop platform metadata fixture: %v", err)
	}
	if err := EnsurePlatformAuthoritySchema(ctx); err != nil {
		t.Fatalf("EnsurePlatformAuthoritySchema: %v", err)
	}

	type importedProfile struct {
		agentID            string
		registrationSource string
		agentStatus        string
		agentRevision      int64
		game               string
		profileStatus      string
		profileRevision    int64
	}
	rows, err := Pool.Query(ctx, `
		SELECT agents.agent_id, agents.registration_source, agents.status, agents.revision,
		       profiles.game, profiles.status, profiles.revision
		FROM platform_agents agents
		JOIN platform_game_profiles profiles ON profiles.agent_id = agents.agent_id
		ORDER BY agents.agent_id`)
	if err != nil {
		t.Fatalf("load imported profiles: %v", err)
	}
	defer rows.Close()
	var imported []importedProfile
	for rows.Next() {
		var profile importedProfile
		if err := rows.Scan(
			&profile.agentID,
			&profile.registrationSource,
			&profile.agentStatus,
			&profile.agentRevision,
			&profile.game,
			&profile.profileStatus,
			&profile.profileRevision,
		); err != nil {
			t.Fatalf("scan imported profile: %v", err)
		}
		imported = append(imported, profile)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate imported profiles: %v", err)
	}
	if len(imported) != 2 {
		t.Fatalf("imported profiles = %+v, want one per existing bot", imported)
	}
	for _, profile := range imported {
		if profile.agentID != linkedBot.ID && profile.agentID != unlinkedBot.ID {
			t.Fatalf("agent ID %q was not preserved from Arena", profile.agentID)
		}
		wantAgentRevision := int64(1)
		if profile.agentID == linkedBot.ID {
			wantAgentRevision = 2
		}
		if profile.registrationSource != "arena_import" || profile.agentStatus != "active" || profile.agentRevision != wantAgentRevision ||
			profile.game != "arena" || profile.profileStatus != "active" || profile.profileRevision != 1 {
			t.Fatalf("imported profile = %+v, want active revision-1 Arena metadata", profile)
		}
	}

	var profileColumns []string
	if err := Pool.QueryRow(ctx, `
		SELECT array_agg(column_name ORDER BY ordinal_position)
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'platform_game_profiles'`).Scan(&profileColumns); err != nil {
		t.Fatalf("load platform profile columns: %v", err)
	}
	wantColumns := []string{"profile_id", "agent_id", "game", "status", "revision", "enrolled_at", "updated_at"}
	if strings.Join(profileColumns, ",") != strings.Join(wantColumns, ",") {
		t.Fatalf("platform profile columns = %v, want metadata only %v", profileColumns, wantColumns)
	}

	// A repeat repair and a replacement connection pool model process restart.
	// Existing authority-owned metadata must not be reset by either path.
	if _, err := Pool.Exec(ctx, `
		UPDATE platform_agents SET status = 'suspended', revision = 7 WHERE agent_id = $1`, linkedBot.ID); err != nil {
		t.Fatalf("mutate imported metadata: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		DELETE FROM platform_game_profiles WHERE agent_id = $1 AND game = 'arena'`, linkedBot.ID); err != nil {
		t.Fatalf("remove profile for repair fixture: %v", err)
	}
	if err := EnsurePlatformAuthoritySchema(ctx); err != nil {
		t.Fatalf("repeat EnsurePlatformAuthoritySchema: %v", err)
	}
	restartPool, err := pgxpool.NewWithConfig(ctx, Pool.Config().Copy())
	if err != nil {
		t.Fatalf("create replacement pool: %v", err)
	}
	defer restartPool.Close()
	originalPool := Pool
	Pool = restartPool
	if err := EnsurePlatformAuthoritySchema(ctx); err != nil {
		Pool = originalPool
		t.Fatalf("restart EnsurePlatformAuthoritySchema: %v", err)
	}
	Pool = originalPool

	var status, profileStatus string
	var revision int64
	if err := Pool.QueryRow(ctx, `
		SELECT agents.status, agents.revision, profiles.status
		FROM platform_agents agents
		JOIN platform_game_profiles profiles ON profiles.agent_id = agents.agent_id AND profiles.game = 'arena'
		WHERE agents.agent_id = $1`, linkedBot.ID).Scan(&status, &revision, &profileStatus); err != nil {
		t.Fatalf("load preserved agent metadata: %v", err)
	}
	if status != "suspended" || revision != 7 || profileStatus != "suspended" {
		t.Fatalf("metadata after restart = (%q, %d, %q), want suspended agent/profile at revision 7",
			status, revision, profileStatus)
	}

	for table, before := range map[string]string{
		"cosmetic_licenses":            licenseBefore,
		"cosmetic_license_assignments": assignmentBefore,
		"cosmetic_entitlements":        entitlementBefore,
		"account_bot_links":            linkBefore,
		"cosmetic_subscriptions":       subscriptionBefore,
	} {
		orderBy := "id"
		switch table {
		case "cosmetic_license_assignments":
			orderBy = "license_id"
		case "cosmetic_entitlements":
			orderBy = "bot_id, cosmetic_id"
		case "account_bot_links":
			orderBy = "account_id, bot_id"
		case "cosmetic_subscriptions":
			orderBy = "id"
		}
		if after := snapshotPlatformMigrationTable(t, ctx, table, orderBy); after != before {
			t.Fatalf("%s changed during platform metadata migration\nbefore: %s\nafter:  %s", table, before, after)
		}
	}
}

func TestPostgresPlatformAuthorityBackfillRollsBackOnInvalidLegacyAgentID(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		DROP TABLE IF EXISTS
			platform_idempotency_records,
			platform_agent_link_events,
			platform_changes,
			platform_game_profiles,
			platform_agents`); err != nil {
		t.Fatalf("drop platform metadata fixture: %v", err)
	}

	overlongAgentID := strings.Repeat("legacy-agent-", 12)
	if _, err := Pool.Exec(ctx, `
		INSERT INTO api_keys (id, key_hash, key_prefix) VALUES ('legacy-long-key', 'hash', 'legacy_long')`); err != nil {
		t.Fatalf("seed invalid legacy key: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		INSERT INTO bots (id, api_key_id) VALUES ($1, 'legacy-long-key')`, overlongAgentID); err != nil {
		t.Fatalf("seed invalid legacy bot: %v", err)
	}

	err := EnsurePlatformAuthoritySchema(ctx)
	if err == nil || !strings.Contains(err.Error(), overlongAgentID) {
		t.Fatalf("invalid legacy agent error = %v, want offending ID", err)
	}
	var installed bool
	if err := Pool.QueryRow(ctx, `SELECT to_regclass('platform_agents') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatalf("inspect rolled-back schema: %v", err)
	}
	if installed {
		t.Fatal("platform metadata tables survived a failed transactional backfill")
	}
}

func TestPostgresArenaRegistrationCreatesPlatformEnrollmentAtomically(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	anonymousBot := accountAPIKeyTestBot("platform-anonymous-key", "platform-anonymous-agent")
	if err := CreateAPIKeyAndBot(
		ctx,
		anonymousBot.APIKeyID,
		"anonymous-hash",
		"arena_platanon",
		"127.0.0.1",
		anonymousBot,
	); err != nil {
		t.Fatalf("CreateAPIKeyAndBot: %v", err)
	}
	assertArenaPlatformEnrollment(t, ctx, anonymousBot.ID, "arena", 1)

	account, err := UpsertVerifiedCustomerAccount(
		ctx,
		"platform-registration@example.com",
		"https://id.example",
		"platform-registration",
		"Platform Registration",
	)
	if err != nil {
		t.Fatalf("UpsertVerifiedCustomerAccount: %v", err)
	}
	ownedBot := accountAPIKeyTestBot("platform-owned-key", "platform-owned-agent")
	if _, _, err := CreateAccountAPIKeyAndBot(
		ctx,
		account.ID,
		ownedBot.APIKeyID,
		"owned-hash",
		"arena_platown",
		"127.0.0.1",
		ownedBot,
	); err != nil {
		t.Fatalf("CreateAccountAPIKeyAndBot: %v", err)
	}
	assertArenaPlatformEnrollment(t, ctx, ownedBot.ID, "arena", 2)
}

func TestPostgresArenaRegistrationRollsBackCredentialWhenPlatformEnrollmentFails(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	overlongAgentID := strings.Repeat("new-agent-", 15)
	anonymousBot := accountAPIKeyTestBot("platform-rollback-key", overlongAgentID)
	err := CreateAPIKeyAndBot(
		ctx,
		anonymousBot.APIKeyID,
		"rollback-hash",
		"arena_platroll",
		"127.0.0.1",
		anonymousBot,
	)
	if err == nil {
		t.Fatal("anonymous registration accepted an agent outside the platform contract")
	}
	assertRegistrationRowsAbsent(t, ctx, anonymousBot.APIKeyID, anonymousBot.ID)

	account, err := UpsertVerifiedCustomerAccount(
		ctx,
		"platform-rollback@example.com",
		"https://id.example",
		"platform-rollback",
		"Platform Rollback",
	)
	if err != nil {
		t.Fatalf("UpsertVerifiedCustomerAccount: %v", err)
	}
	ownedBot := accountAPIKeyTestBot("platform-owned-rollback-key", overlongAgentID)
	if _, _, err := CreateAccountAPIKeyAndBot(
		ctx,
		account.ID,
		ownedBot.APIKeyID,
		"owned-rollback-hash",
		"arena_ownroll",
		"127.0.0.1",
		ownedBot,
	); err == nil {
		t.Fatal("account registration accepted an agent outside the platform contract")
	}
	assertRegistrationRowsAbsent(t, ctx, ownedBot.APIKeyID, ownedBot.ID)
}

func TestPostgresPlatformChangeFeedIsOrderedBoundedAndIndexCompatible(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}

	bot := accountAPIKeyTestBot("platform-feed-key", "platform-feed-agent")
	if err := CreateAPIKeyAndBot(
		ctx,
		bot.APIKeyID,
		"feed-hash",
		"arena_platfeed",
		"127.0.0.1",
		bot,
	); err != nil {
		t.Fatalf("CreateAPIKeyAndBot: %v", err)
	}
	changes, nextCursor, err := ListPlatformChanges(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListPlatformChanges registration: %v", err)
	}
	if nextCursor != 0 || len(changes) != 2 {
		t.Fatalf("registration change page = (%+v, %d), want two terminal-page changes", changes, nextCursor)
	}
	if changes[0].SubjectKind != "agent" || changes[0].SubjectID != bot.ID || changes[0].Transition != "registered" || changes[0].Revision != 1 {
		t.Fatalf("agent registration change = %+v", changes[0])
	}
	if changes[1].SubjectKind != "game_profile" || changes[1].SubjectID == "" || changes[1].Transition != "enrolled" || changes[1].Revision != 1 {
		t.Fatalf("profile enrollment change = %+v", changes[1])
	}

	var baseChangeID int64
	if err := Pool.QueryRow(ctx, `SELECT MAX(change_id) FROM platform_changes`).Scan(&baseChangeID); err != nil {
		t.Fatalf("load base change ID: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		INSERT INTO platform_changes (subject_kind, subject_id, transition, revision, changed_at)
		SELECT 'agent', 'irrelevant-' || sequence, 'registered', 1, NOW()
		FROM generate_series(1, 10000) AS sequence`); err != nil {
		t.Fatalf("seed irrelevant platform changes: %v", err)
	}

	firstPage, firstNext, err := ListPlatformChanges(ctx, baseChangeID, 1000)
	if err != nil {
		t.Fatalf("ListPlatformChanges clamped page: %v", err)
	}
	if len(firstPage) != MaxPlatformChangePageSize || firstNext != firstPage[len(firstPage)-1].ChangeID {
		t.Fatalf("clamped page = (%d rows, next %d), want (%d, last change ID)", len(firstPage), firstNext, MaxPlatformChangePageSize)
	}
	secondPage, _, err := ListPlatformChanges(ctx, firstNext, MaxPlatformChangePageSize)
	if err != nil {
		t.Fatalf("ListPlatformChanges second page: %v", err)
	}
	if len(secondPage) != MaxPlatformChangePageSize || secondPage[0].ChangeID <= firstPage[len(firstPage)-1].ChangeID {
		t.Fatalf("second page overlaps or is incomplete: first last=%d second=%+v", firstPage[len(firstPage)-1].ChangeID, secondPage)
	}
	for index := 1; index < len(firstPage); index++ {
		if firstPage[index].ChangeID <= firstPage[index-1].ChangeID {
			t.Fatalf("change feed is not strictly ordered at %d: %+v", index, firstPage)
		}
	}

	planRows, err := Pool.Query(ctx, `
		EXPLAIN (COSTS OFF)
		SELECT change_id, subject_kind, subject_id, transition, revision, changed_at
		FROM platform_changes
		WHERE change_id > $1
		ORDER BY change_id
		LIMIT 101`, baseChangeID+9900)
	if err != nil {
		t.Fatalf("explain bounded platform changes: %v", err)
	}
	defer planRows.Close()
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatalf("scan platform change plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := planRows.Err(); err != nil {
		t.Fatalf("iterate platform change plan: %v", err)
	}
	if planText := plan.String(); !strings.Contains(planText, "Index Scan using platform_changes_pkey") || strings.Contains(planText, "Seq Scan") {
		t.Fatalf("platform change feed plan is not index-bounded:\n%s", planText)
	}
}

func TestPostgresPlatformProfileTransitionEnforcesRevisionAndIdempotency(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	bot := accountAPIKeyTestBot("platform-transition-key", "platform-transition-agent")
	if err := CreateAPIKeyAndBot(
		ctx,
		bot.APIKeyID,
		"transition-hash",
		"arena_plattrans",
		"127.0.0.1",
		bot,
	); err != nil {
		t.Fatalf("CreateAPIKeyAndBot: %v", err)
	}

	suspend := PlatformProfileTransition{
		AgentID:          bot.ID,
		Game:             "arena",
		Status:           "suspended",
		ExpectedRevision: 1,
		IdempotencyKey:   "suspend-once",
	}
	first, err := TransitionPlatformProfile(ctx, suspend)
	if err != nil {
		t.Fatalf("TransitionPlatformProfile suspend: %v", err)
	}
	if first.AgentRevision != 2 || first.ProfileRevision != 2 || first.Status != "suspended" {
		t.Fatalf("suspend result = %+v, want both revisions 2", first)
	}
	replayed, err := TransitionPlatformProfile(ctx, suspend)
	if err != nil {
		t.Fatalf("TransitionPlatformProfile replay: %v", err)
	}
	if !reflect.DeepEqual(replayed, first) {
		t.Fatalf("idempotent replay = %+v, want exact %+v", replayed, first)
	}

	conflictingReplay := suspend
	conflictingReplay.Status = "active"
	conflictingReplay.ExpectedRevision = 2
	if _, err := TransitionPlatformProfile(ctx, conflictingReplay); !errors.Is(err, ErrPlatformIdempotencyConflict) {
		t.Fatalf("conflicting idempotency replay error = %v, want %v", err, ErrPlatformIdempotencyConflict)
	}
	stale := PlatformProfileTransition{
		AgentID:          bot.ID,
		Game:             "arena",
		Status:           "active",
		ExpectedRevision: 1,
		IdempotencyKey:   "stale-transition",
	}
	if _, err := TransitionPlatformProfile(ctx, stale); !errors.Is(err, ErrPlatformRevisionConflict) {
		t.Fatalf("stale transition error = %v, want %v", err, ErrPlatformRevisionConflict)
	}
	var staleRecords int
	if err := Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM platform_idempotency_records
		WHERE operation = 'transition_platform_profile' AND idempotency_key = $1`, stale.IdempotencyKey).Scan(&staleRecords); err != nil {
		t.Fatalf("count stale idempotency records: %v", err)
	}
	if staleRecords != 0 {
		t.Fatalf("stale transition stored %d idempotency records, want 0", staleRecords)
	}

	activate := PlatformProfileTransition{
		AgentID:          bot.ID,
		Game:             "arena",
		Status:           "active",
		ExpectedRevision: 2,
		IdempotencyKey:   "concurrent-activate",
	}
	const attempts = 8
	results := make(chan *PlatformProfileTransitionResult, attempts)
	errs := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := TransitionPlatformProfile(context.Background(), activate)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent transition: %v", err)
		}
	}
	var canonical *PlatformProfileTransitionResult
	for result := range results {
		if canonical == nil {
			canonical = result
			continue
		}
		if !reflect.DeepEqual(result, canonical) {
			t.Fatalf("concurrent replay = %+v, want exact %+v", result, canonical)
		}
	}
	if canonical == nil || canonical.AgentRevision != 3 || canonical.ProfileRevision != 3 || canonical.Status != "active" {
		t.Fatalf("concurrent transition result = %+v, want one revision increment", canonical)
	}

	var idempotencyRecords, transitionChanges int
	if err := Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM platform_idempotency_records
		WHERE operation = 'transition_platform_profile' AND idempotency_key = $1`, activate.IdempotencyKey).Scan(&idempotencyRecords); err != nil {
		t.Fatalf("count concurrent idempotency records: %v", err)
	}
	if err := Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM platform_changes
		WHERE transition IN ('status_active', 'profile_status_active')
		  AND revision = 3`).Scan(&transitionChanges); err != nil {
		t.Fatalf("count concurrent transition changes: %v", err)
	}
	if idempotencyRecords != 1 || transitionChanges != 2 {
		t.Fatalf("concurrent persistence = %d idempotency records, %d changes; want 1 and 2", idempotencyRecords, transitionChanges)
	}
}

func TestPostgresPlatformAgentLinkHistorySurvivesRelinkAndUsesAccountIndex(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatalf("EnsureCoreSchema: %v", err)
	}
	account, err := UpsertVerifiedCustomerAccount(
		ctx,
		"platform-links@example.com",
		"https://id.example",
		"platform-links",
		"Platform Links",
	)
	if err != nil {
		t.Fatalf("UpsertVerifiedCustomerAccount: %v", err)
	}
	bot := accountAPIKeyTestBot("platform-links-key", "platform-links-agent")
	if _, _, err := CreateAccountAPIKeyAndBot(
		ctx,
		account.ID,
		bot.APIKeyID,
		"links-hash",
		"arena_platlinks",
		"127.0.0.1",
		bot,
	); err != nil {
		t.Fatalf("CreateAccountAPIKeyAndBot: %v", err)
	}
	if unlinked, err := UnlinkBotFromCustomerAccount(ctx, account.ID, bot.ID); err != nil || !unlinked {
		t.Fatalf("UnlinkBotFromCustomerAccount = (%v, %v)", unlinked, err)
	}
	if _, err := LinkBotToCustomerAccount(ctx, account.ID, bot.ID); err != nil {
		t.Fatalf("LinkBotToCustomerAccount relink: %v", err)
	}

	events, nextCursor, err := ListPlatformAgentLinkEvents(ctx, account.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListPlatformAgentLinkEvents: %v", err)
	}
	if nextCursor != 0 || len(events) != 3 {
		t.Fatalf("link history = (%+v, %d), want three terminal-page events", events, nextCursor)
	}
	for index, wantStatus := range []string{"linked", "unlinked", "linked"} {
		if events[index].AccountID != account.ID || events[index].AgentID != bot.ID ||
			events[index].Status != wantStatus || events[index].Revision != int64(index+1) {
			t.Fatalf("link event %d = %+v, want %s revision %d", index, events[index], wantStatus, index+1)
		}
	}
	var agentRevision int64
	if err := Pool.QueryRow(ctx, `SELECT revision FROM platform_agents WHERE agent_id = $1`, bot.ID).Scan(&agentRevision); err != nil {
		t.Fatalf("load linked agent revision: %v", err)
	}
	if agentRevision != 4 {
		t.Fatalf("agent revision after register/link/unlink/relink = %d, want 4", agentRevision)
	}

	// If an older writer moves the compatibility projection without history,
	// reconciliation emits the missing prior-account unlink exactly once, then
	// the current-account link. A repeat repair must remain idempotent.
	otherAccount, err := UpsertVerifiedCustomerAccount(
		ctx,
		"platform-links-other@example.com",
		"https://id.example",
		"platform-links-other",
		"Platform Links Other",
	)
	if err != nil {
		t.Fatalf("create other link account: %v", err)
	}
	if _, err := Pool.Exec(ctx, `
		UPDATE account_bot_links SET account_id = $2 WHERE account_id = $1 AND bot_id = $3`,
		account.ID, otherAccount.ID, bot.ID); err != nil {
		t.Fatalf("seed compatibility-link divergence: %v", err)
	}
	if err := EnsurePlatformAuthoritySchema(ctx); err != nil {
		t.Fatalf("reconcile compatibility-link divergence: %v", err)
	}
	if err := EnsurePlatformAuthoritySchema(ctx); err != nil {
		t.Fatalf("repeat reconciled schema: %v", err)
	}
	var latestAccount, latestStatus string
	var latestRevision int64
	if err := Pool.QueryRow(ctx, `
		SELECT account_id, status, revision
		FROM platform_agent_link_events
		WHERE agent_id = $1
		ORDER BY event_id DESC
		LIMIT 1`, bot.ID).Scan(&latestAccount, &latestStatus, &latestRevision); err != nil {
		t.Fatalf("load reconciled latest link: %v", err)
	}
	if latestAccount != otherAccount.ID || latestStatus != "linked" || latestRevision != 5 {
		t.Fatalf("reconciled latest link = (%q, %q, %d), want other account linked revision 5",
			latestAccount, latestStatus, latestRevision)
	}
	priorHistory, priorNext, err := ListPlatformAgentLinkEvents(ctx, account.ID, 0, 10)
	if err != nil {
		t.Fatalf("load prior account reconciled history: %v", err)
	}
	if priorNext != 0 || len(priorHistory) != 4 || priorHistory[3].Status != "unlinked" || priorHistory[3].Revision != 4 {
		t.Fatalf("prior account reconciled history = (%+v, %d), want terminal unlinked revision 4", priorHistory, priorNext)
	}
	currentHistory, currentNext, err := ListPlatformAgentLinkEvents(ctx, otherAccount.ID, 0, 10)
	if err != nil {
		t.Fatalf("load current account reconciled history: %v", err)
	}
	if currentNext != 0 || len(currentHistory) != 1 || currentHistory[0].Status != "linked" || currentHistory[0].Revision != 5 {
		t.Fatalf("current account reconciled history = (%+v, %d), want linked revision 5", currentHistory, currentNext)
	}

	if _, err := Pool.Exec(ctx, `
		INSERT INTO platform_agent_link_events (
			account_id, agent_id, status, revision, reason, occurred_at
		)
		SELECT 'irrelevant-account', 'irrelevant-agent-' || sequence,
		       'linked', 1, 'large-history-test', NOW()
		FROM generate_series(1, 10000) AS sequence`); err != nil {
		t.Fatalf("seed irrelevant link history: %v", err)
	}
	planRows, err := Pool.Query(ctx, `
		EXPLAIN (COSTS OFF)
		SELECT event_id, account_id, agent_id, status, revision, reason, occurred_at
		FROM platform_agent_link_events
		WHERE account_id = $1
		  AND (account_id, event_id) > ($1, $2)
		ORDER BY account_id, event_id
		LIMIT 101`, account.ID, 0)
	if err != nil {
		t.Fatalf("explain bounded link history: %v", err)
	}
	defer planRows.Close()
	var plan strings.Builder
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			t.Fatalf("scan link history plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := planRows.Err(); err != nil {
		t.Fatalf("iterate link history plan: %v", err)
	}
	if planText := plan.String(); (!strings.Contains(planText, "Index Scan using idx_platform_agent_link_events_account") &&
		!strings.Contains(planText, "Index Only Scan using idx_platform_agent_link_events_account") &&
		!strings.Contains(planText, "Bitmap Index Scan on idx_platform_agent_link_events_account")) ||
		strings.Contains(planText, "Seq Scan") {
		t.Fatalf("platform link history plan is not account-index-bounded:\n%s", planText)
	}
}

func assertArenaPlatformEnrollment(t *testing.T, ctx context.Context, agentID, wantSource string, wantAgentRevision int64) {
	t.Helper()
	var source, agentStatus, game, profileStatus string
	var agentRevision, profileRevision int64
	if err := Pool.QueryRow(ctx, `
		SELECT agents.registration_source, agents.status, agents.revision,
		       profiles.game, profiles.status, profiles.revision
		FROM platform_agents agents
		JOIN platform_game_profiles profiles ON profiles.agent_id = agents.agent_id
		WHERE agents.agent_id = $1 AND profiles.game = 'arena'`, agentID).Scan(
		&source,
		&agentStatus,
		&agentRevision,
		&game,
		&profileStatus,
		&profileRevision,
	); err != nil {
		t.Fatalf("load platform enrollment for %s: %v", agentID, err)
	}
	if source != wantSource || agentStatus != "active" || agentRevision != wantAgentRevision ||
		game != "arena" || profileStatus != "active" || profileRevision != 1 {
		t.Fatalf(
			"platform enrollment for %s = (%q, %q, %d, %q, %q, %d)",
			agentID,
			source,
			agentStatus,
			agentRevision,
			game,
			profileStatus,
			profileRevision,
		)
	}
}

func assertRegistrationRowsAbsent(t *testing.T, ctx context.Context, keyID, agentID string) {
	t.Helper()
	for table, column := range map[string]string{
		"api_keys":               "id",
		"bots":                   "id",
		"platform_agents":        "agent_id",
		"platform_game_profiles": "agent_id",
		"account_api_keys":       "api_key_id",
		"account_bot_links":      "bot_id",
	} {
		value := agentID
		if table == "api_keys" || table == "account_api_keys" {
			value = keyID
		}
		var count int
		if err := Pool.QueryRow(ctx, `SELECT COUNT(*) FROM `+table+` WHERE `+column+` = $1`, value).Scan(&count); err != nil {
			t.Fatalf("count rolled-back %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("rolled-back registration left %d row(s) in %s", count, table)
		}
	}
}

func snapshotPlatformMigrationTable(t *testing.T, ctx context.Context, table, orderBy string) string {
	t.Helper()
	var snapshot string
	query := `SELECT COALESCE(jsonb_agg(to_jsonb(rows) ORDER BY ` + orderBy + `), '[]'::jsonb)::text FROM ` + table + ` rows`
	if err := Pool.QueryRow(ctx, query).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot %s: %v", table, err)
	}
	return snapshot
}
