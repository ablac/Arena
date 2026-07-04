package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnsureCoreSchema creates or repairs the database tables required by the
// runtime. It is intentionally idempotent so a fresh Postgres volume can be
// bootstrapped on first server start without a separate migration step.
func EnsureCoreSchema(ctx context.Context) error {
	if Pool == nil {
		return nil
	}

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("EnsureCoreSchema begin: %w", err)
	}
	defer tx.Rollback(ctx)

	statements := []string{
		`CREATE TABLE IF NOT EXISTS api_keys (
			id TEXT PRIMARY KEY,
			key_hash TEXT NOT NULL,
			key_prefix TEXT NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_seen TIMESTAMPTZ,
			is_active BOOLEAN NOT NULL DEFAULT true,
			ip_created TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_active_prefix
			ON api_keys (key_prefix) WHERE is_active = true`,
		`CREATE TABLE IF NOT EXISTS bots (
			id TEXT PRIMARY KEY,
			api_key_id TEXT NOT NULL UNIQUE REFERENCES api_keys(id) ON DELETE CASCADE,
			name TEXT NOT NULL DEFAULT 'Unnamed Bot',
			avatar_color TEXT NOT NULL DEFAULT '#888888',
			default_weapon TEXT NOT NULL DEFAULT 'sword',
			default_stats JSONB NOT NULL DEFAULT '{}'::jsonb,
			default_fallback TEXT NOT NULL DEFAULT 'aggressive',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bots_api_key_id ON bots (api_key_id)`,
		`CREATE TABLE IF NOT EXISTS bot_stats (
			bot_id TEXT PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE,
			kills INT NOT NULL DEFAULT 0,
			deaths INT NOT NULL DEFAULT 0,
			assists INT NOT NULL DEFAULT 0,
			damage_dealt BIGINT NOT NULL DEFAULT 0,
			damage_taken BIGINT NOT NULL DEFAULT 0,
			current_streak INT NOT NULL DEFAULT 0,
			best_streak INT NOT NULL DEFAULT 0,
			elo INT NOT NULL DEFAULT 1000,
			time_alive_seconds BIGINT NOT NULL DEFAULT 0,
			longest_life_secs INT NOT NULL DEFAULT 0,
			rounds_played INT NOT NULL DEFAULT 0,
			round_wins INT NOT NULL DEFAULT 0,
			pickups_collected INT NOT NULL DEFAULT 0,
			distance_traveled DOUBLE PRECISION NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bot_stats_elo ON bot_stats (elo DESC)`,
		`CREATE TABLE IF NOT EXISTS rounds (
			id TEXT PRIMARY KEY,
			round_number INT NOT NULL,
			started_at TIMESTAMPTZ NOT NULL,
			ended_at TIMESTAMPTZ,
			bots_participated INT NOT NULL DEFAULT 0,
			mvp_bot_id TEXT REFERENCES bots(id) ON DELETE SET NULL,
			status TEXT NOT NULL DEFAULT 'active'
		)`,
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1
				FROM pg_constraint
				WHERE conrelid = 'rounds'::regclass
				  AND conname = 'rounds_round_number_key'
			) THEN
				ALTER TABLE rounds DROP CONSTRAINT rounds_round_number_key;
			END IF;
		END
		$$`,
		`CREATE INDEX IF NOT EXISTS idx_rounds_round_number ON rounds (round_number DESC)`,
		`CREATE TABLE IF NOT EXISTS kill_log (
			id TEXT PRIMARY KEY,
			round_id TEXT REFERENCES rounds(id) ON DELETE SET NULL,
			killer_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
			victim_id TEXT NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
			weapon TEXT NOT NULL,
			damage INT NOT NULL DEFAULT 0,
			killer_hp INT NOT NULL DEFAULT 0,
			tick INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_kill_log_round_id ON kill_log (round_id)`,
		`CREATE INDEX IF NOT EXISTS idx_kill_log_created_at ON kill_log (created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS rate_limits (
			ip_address TEXT PRIMARY KEY,
			keys_generated INT NOT NULL DEFAULT 0,
			window_start TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_rate_limits_window_start
			ON rate_limits (window_start DESC)`,
		`CREATE TABLE IF NOT EXISTS weapon_balance (
			weapon TEXT PRIMARY KEY,
			damage_scale DOUBLE PRECISION NOT NULL DEFAULT 1.0,
			cooldown_scale DOUBLE PRECISION NOT NULL DEFAULT 1.0,
			adjustment_scale DOUBLE PRECISION NOT NULL DEFAULT 0.05,
			rounds_tracked INT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS weapon_balance_history (
			id BIGSERIAL PRIMARY KEY,
			weapon TEXT NOT NULL,
			rounds_tracked INT NOT NULL DEFAULT 0,
			damage_scale DOUBLE PRECISION NOT NULL DEFAULT 1.0,
			cooldown_scale DOUBLE PRECISION NOT NULL DEFAULT 1.0,
			adjustment_scale DOUBLE PRECISION NOT NULL DEFAULT 0.05,
			avg_score DOUBLE PRECISION NOT NULL DEFAULT 0,
			mean_score DOUBLE PRECISION NOT NULL DEFAULT 0,
			diff_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
			damage_delta DOUBLE PRECISION NOT NULL DEFAULT 0,
			cooldown_delta DOUBLE PRECISION NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_weapon_balance_history_weapon_created
			ON weapon_balance_history (weapon, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS bounty_board (
			bot_id TEXT PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE,
			name TEXT NOT NULL DEFAULT '',
			avatar_color TEXT NOT NULL DEFAULT '#888888',
			weapon TEXT NOT NULL DEFAULT 'sword',
			win_streak INT NOT NULL DEFAULT 0,
			bounty_points INT NOT NULL DEFAULT 0,
			claims INT NOT NULL DEFAULT 0,
			is_target BOOLEAN NOT NULL DEFAULT false,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("EnsureCoreSchema exec: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("EnsureCoreSchema commit: %w", err)
	}

	if err := EnsureRoundBotStatsTable(ctx); err != nil {
		return fmt.Errorf("EnsureCoreSchema round_bot_stats: %w", err)
	}
	if err := EnsureDemoBotKeysTable(ctx); err != nil {
		return fmt.Errorf("EnsureCoreSchema demo_bot_keys: %w", err)
	}
	if err := EnsureAdminTokensTable(ctx); err != nil {
		return fmt.Errorf("EnsureCoreSchema admin_tokens: %w", err)
	}

	return nil
}

// ---------- round_bot_stats (per-round per-bot performance for time-based leaderboards) ----------

func EnsureRoundBotStatsTable(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS round_bot_stats (
			id SERIAL PRIMARY KEY,
			round_number INT NOT NULL,
			bot_id TEXT NOT NULL,
			bot_name TEXT NOT NULL DEFAULT '',
			weapon TEXT NOT NULL DEFAULT '',
			kills INT NOT NULL DEFAULT 0,
			deaths INT NOT NULL DEFAULT 0,
			damage_dealt BIGINT NOT NULL DEFAULT 0,
			damage_taken BIGINT NOT NULL DEFAULT 0,
			longest_life_secs INT NOT NULL DEFAULT 0,
			shots_fired INT NOT NULL DEFAULT 0,
			shots_hit INT NOT NULL DEFAULT 0,
			pickups INT NOT NULL DEFAULT 0,
			distance DOUBLE PRECISION NOT NULL DEFAULT 0,
			elo INT NOT NULL DEFAULT 1000,
			won BOOLEAN NOT NULL DEFAULT false,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		return err
	}
	Pool.Exec(ctx, `ALTER TABLE round_bot_stats ADD COLUMN IF NOT EXISTS weapon TEXT NOT NULL DEFAULT ''`)
	Pool.Exec(ctx, `ALTER TABLE round_bot_stats ADD COLUMN IF NOT EXISTS longest_life_secs INT NOT NULL DEFAULT 0`)
	Pool.Exec(ctx, `ALTER TABLE round_bot_stats ADD COLUMN IF NOT EXISTS shots_fired INT NOT NULL DEFAULT 0`)
	Pool.Exec(ctx, `ALTER TABLE round_bot_stats ADD COLUMN IF NOT EXISTS shots_hit INT NOT NULL DEFAULT 0`)
	Pool.Exec(ctx, `UPDATE round_bot_stats AS r
		SET weapon = b.default_weapon
		FROM bots AS b
		WHERE r.weapon = ''
		  AND r.bot_id = b.id
		  AND b.default_weapon <> ''`)
	// Index for time-based queries
	Pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_rbs_created ON round_bot_stats (created_at)`)
	Pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_rbs_bot ON round_bot_stats (bot_id)`)
	Pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_rbs_weapon ON round_bot_stats (weapon)`)
	Pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_rbs_round_number ON round_bot_stats (round_number DESC)`)
	return nil
}

func InsertRoundBotStats(ctx context.Context, roundNumber int, botID, botName, weapon string,
	kills, deaths int, dmgDealt, dmgTaken int64, longestLife, shotsFired, shotsHit, pickups int, distance float64, elo int, won bool) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO round_bot_stats (round_number, bot_id, bot_name, weapon, kills, deaths, damage_dealt, damage_taken, longest_life_secs, shots_fired, shots_hit, pickups, distance, elo, won)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		roundNumber, botID, botName, weapon, kills, deaths, dmgDealt, dmgTaken, longestLife, shotsFired, shotsHit, pickups, distance, elo, won)
	return err
}

// ListRecentWeaponPerformance returns average per-weapon round score over the last N rounds.
func ListRecentWeaponPerformance(ctx context.Context, roundLimit int) ([]WeaponRecentPerformance, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `
		WITH recent_rounds AS (
			SELECT DISTINCT DATE_TRUNC('second', created_at) AS round_at
			FROM round_bot_stats
			ORDER BY round_at DESC
			LIMIT $1
		)
		SELECT
			weapon,
			COUNT(*)::INT AS bots,
			SUM(CASE WHEN won THEN 1 ELSE 0 END)::INT AS wins,
			COUNT(DISTINCT DATE_TRUNC('second', created_at))::INT AS rounds,
			AVG(
				(kills * 30)::DOUBLE PRECISION +
				(damage_dealt * 0.12)::DOUBLE PRECISION +
				(longest_life_secs * 0.35)::DOUBLE PRECISION +
				(CASE WHEN won THEN 60 ELSE 0 END)::DOUBLE PRECISION
			) AS avg_score,
			AVG(kills)::DOUBLE PRECISION AS avg_kills,
			AVG(damage_dealt)::DOUBLE PRECISION AS avg_damage,
			AVG(longest_life_secs)::DOUBLE PRECISION AS avg_life_secs,
			COALESCE(SUM(shots_fired), 0)::INT AS shots_fired,
			COALESCE(SUM(shots_hit), 0)::INT AS shots_hit,
			CASE
				WHEN COALESCE(SUM(shots_fired), 0) > 0
				THEN COALESCE(SUM(shots_hit), 0)::DOUBLE PRECISION / SUM(shots_fired)
				ELSE 0
			END AS hit_rate,
			CASE
				WHEN COALESCE(SUM(shots_fired), 0) > 0
				THEN COALESCE(SUM(damage_dealt), 0)::DOUBLE PRECISION / SUM(shots_fired)
				ELSE 0
			END AS damage_per_shot,
			CASE
				WHEN COALESCE(SUM(shots_hit), 0) > 0
				THEN COALESCE(SUM(damage_dealt), 0)::DOUBLE PRECISION / SUM(shots_hit)
				ELSE 0
			END AS damage_per_hit,
			CASE
				WHEN COALESCE(SUM(longest_life_secs), 0) > 0
				THEN COALESCE(SUM(shots_fired), 0)::DOUBLE PRECISION / SUM(longest_life_secs)
				ELSE 0
			END AS shots_per_life,
			CASE
				WHEN COALESCE(SUM(shots_hit), 0) > 0
				THEN COALESCE(SUM(kills), 0)::DOUBLE PRECISION / SUM(shots_hit)
				ELSE 0
			END AS kills_per_hit
		FROM round_bot_stats
		WHERE DATE_TRUNC('second', created_at) IN (SELECT round_at FROM recent_rounds)
		  AND weapon <> ''
		GROUP BY weapon
		ORDER BY weapon
	`, roundLimit)
	if err != nil {
		return nil, fmt.Errorf("ListRecentWeaponPerformance: %w", err)
	}
	defer rows.Close()

	var items []WeaponRecentPerformance
	for rows.Next() {
		var item WeaponRecentPerformance
		if err := rows.Scan(
			&item.Weapon,
			&item.Bots,
			&item.Wins,
			&item.Rounds,
			&item.AvgScore,
			&item.AvgKills,
			&item.AvgDamage,
			&item.AvgLifeSecs,
			&item.ShotsFired,
			&item.ShotsHit,
			&item.HitRate,
			&item.DamagePerShot,
			&item.DamagePerHit,
			&item.ShotsPerLife,
			&item.KillsPerHit,
		); err != nil {
			return nil, fmt.Errorf("ListRecentWeaponPerformance scan: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListRecentWeaponPerformance rows: %w", err)
	}
	return items, nil
}

// InsertWeaponBalanceHistory stores one balance-decision snapshot.
func InsertWeaponBalanceHistory(ctx context.Context, item *WeaponBalanceHistory) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO weapon_balance_history
			(weapon, rounds_tracked, damage_scale, cooldown_scale, adjustment_scale, avg_score, mean_score, diff_pct, damage_delta, cooldown_delta, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		item.Weapon, item.RoundsTracked, item.DamageScale, item.CooldownScale, item.AdjustmentScale,
		item.AvgScore, item.MeanScore, item.DiffPct, item.DamageDelta, item.CooldownDelta, item.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("InsertWeaponBalanceHistory: %w", err)
	}
	return nil
}

// ListWeaponBalanceHistory returns up to N most recent history points per weapon.
func ListWeaponBalanceHistory(ctx context.Context, perWeapon int) ([]WeaponBalanceHistory, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `
		WITH ranked AS (
			SELECT
				weapon, rounds_tracked, damage_scale, cooldown_scale, adjustment_scale,
				avg_score, mean_score, diff_pct, damage_delta, cooldown_delta, created_at,
				ROW_NUMBER() OVER (PARTITION BY weapon ORDER BY created_at DESC, rounds_tracked DESC) AS rn
			FROM weapon_balance_history
		)
		SELECT
			weapon, rounds_tracked, damage_scale, cooldown_scale, adjustment_scale,
			avg_score, mean_score, diff_pct, damage_delta, cooldown_delta, created_at
		FROM ranked
		WHERE rn <= $1
		ORDER BY weapon, created_at ASC, rounds_tracked ASC
	`, perWeapon)
	if err != nil {
		return nil, fmt.Errorf("ListWeaponBalanceHistory: %w", err)
	}
	defer rows.Close()

	var items []WeaponBalanceHistory
	for rows.Next() {
		var item WeaponBalanceHistory
		if err := rows.Scan(
			&item.Weapon, &item.RoundsTracked, &item.DamageScale, &item.CooldownScale, &item.AdjustmentScale,
			&item.AvgScore, &item.MeanScore, &item.DiffPct, &item.DamageDelta, &item.CooldownDelta, &item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListWeaponBalanceHistory scan: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListWeaponBalanceHistory rows: %w", err)
	}
	return items, nil
}

// GetTimeBasedLeaderboard returns aggregated stats for bots within a time window.
func GetTimeBasedLeaderboard(ctx context.Context, since time.Time, sortBy string, limit int) ([]map[string]interface{}, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	validSorts := map[string]string{
		"kills":      "SUM(r.kills) DESC",
		"elo":        "MAX(r.elo) DESC",
		"kd_ratio":   "CASE WHEN SUM(r.deaths)=0 THEN SUM(r.kills) ELSE SUM(r.kills)::float/SUM(r.deaths) END DESC",
		"best_streak": "SUM(r.kills) DESC", // approx — no per-round streak tracking
		"wins":       "SUM(CASE WHEN r.won THEN 1 ELSE 0 END) DESC",
		"damage":     "SUM(r.damage_dealt) DESC",
	}
	order, ok := validSorts[sortBy]
	if !ok {
		order = validSorts["elo"]
	}

	query := fmt.Sprintf(`
		SELECT
			r.bot_id,
			MAX(r.bot_name) AS name,
			COALESCE(MAX(b.avatar_color), '#888') AS avatar_color,
			SUM(r.kills) AS kills,
			SUM(r.deaths) AS deaths,
			MAX(r.elo) AS elo,
			SUM(r.damage_dealt) AS damage_dealt,
			COUNT(*) AS rounds_played,
			SUM(CASE WHEN r.won THEN 1 ELSE 0 END) AS round_wins
		FROM round_bot_stats r
		LEFT JOIN bots b ON b.id::text = r.bot_id
		WHERE r.created_at >= $1
		  AND r.bot_name NOT LIKE 'Legacy-%%'
		GROUP BY r.bot_id
		HAVING COUNT(*) > 0
		ORDER BY %s
		LIMIT $2
	`, order)

	rows, err := Pool.Query(ctx, query, since, limit)
	if err != nil {
		return nil, fmt.Errorf("GetTimeBasedLeaderboard: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	rank := 0
	for rows.Next() {
		rank++
		var botID, name, color string
		var kills, deaths, elo int
		var dmgDealt int64
		var roundsPlayed, roundWins int
		if err := rows.Scan(&botID, &name, &color, &kills, &deaths, &elo, &dmgDealt, &roundsPlayed, &roundWins); err != nil {
			return nil, fmt.Errorf("GetTimeBasedLeaderboard scan: %w", err)
		}
		results = append(results, map[string]interface{}{
			"rank":          rank,
			"bot_id":        botID,
			"name":          name,
			"avatar_color":  color,
			"kills":         kills,
			"deaths":        deaths,
			"elo":           elo,
			"damage_dealt":  dmgDealt,
			"rounds_played": roundsPlayed,
			"round_wins":    roundWins,
		})
	}
	return results, rows.Err()
}

// ---------- demo_bot_keys ----------

// EnsureDemoBotKeysTable creates the demo_bot_keys table if it doesn't exist.
func EnsureDemoBotKeysTable(ctx context.Context) error {
	if Pool == nil {
		return nil
	}
	_, err := Pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS demo_bot_keys (
			name TEXT PRIMARY KEY,
			api_key TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	return err
}

// GetDemoBotKey returns the stored API key for a demo bot by name, or empty if not found.
func GetDemoBotKey(ctx context.Context, name string) (string, error) {
	if Pool == nil {
		return "", ErrNoDatabase
	}
	var key string
	err := Pool.QueryRow(ctx,
		`SELECT api_key FROM demo_bot_keys WHERE name = $1`, name,
	).Scan(&key)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", nil
		}
		return "", err
	}
	return key, nil
}

// GetAllDemoBotKeys returns all demo bot name→key mappings.
func GetAllDemoBotKeys(ctx context.Context) (map[string]string, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `SELECT name, api_key FROM demo_bot_keys`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var name, key string
		if err := rows.Scan(&name, &key); err != nil {
			return nil, err
		}
		result[name] = key
	}
	return result, rows.Err()
}

// SaveDemoBotKey upserts a demo bot's API key.
func SaveDemoBotKey(ctx context.Context, name, apiKey string) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO demo_bot_keys (name, api_key) VALUES ($1, $2)
		 ON CONFLICT (name) DO UPDATE SET api_key = $2`,
		name, apiKey,
	)
	return err
}

// ---------- admin_tokens ----------

// EnsureAdminTokensTable creates the admin_tokens table if it doesn't exist.
func EnsureAdminTokensTable(ctx context.Context) error {
	if Pool == nil {
		return nil
	}
	_, err := Pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS admin_tokens (
			id TEXT PRIMARY KEY,
			label TEXT NOT NULL DEFAULT 'Admin Token',
			token_hash TEXT NOT NULL,
			token_hint TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		return fmt.Errorf("EnsureAdminTokensTable: %w", err)
	}
	return nil
}

// ListAdminTokens returns all admin tokens (without the actual token, just metadata).
func ListAdminTokens(ctx context.Context) ([]map[string]interface{}, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx,
		`SELECT id, label, token_hint, created_at FROM admin_tokens ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("ListAdminTokens: %w", err)
	}
	defer rows.Close()
	var results []map[string]interface{}
	for rows.Next() {
		var id, label, tokenHint string
		var createdAt time.Time
		if err := rows.Scan(&id, &label, &tokenHint, &createdAt); err != nil {
			return nil, fmt.Errorf("ListAdminTokens scan: %w", err)
		}
		results = append(results, map[string]interface{}{
			"id":         id,
			"label":      label,
			"token_hint": tokenHint,
			"created_at": createdAt,
		})
	}
	return results, rows.Err()
}

// CreateAdminToken inserts a new admin token.
func CreateAdminToken(ctx context.Context, id, label, tokenHash, tokenHint string) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO admin_tokens (id, label, token_hash, token_hint) VALUES ($1, $2, $3, $4)`,
		id, label, tokenHash, tokenHint)
	if err != nil {
		return fmt.Errorf("CreateAdminToken: %w", err)
	}
	return nil
}

// DeleteAdminToken deletes an admin token by ID.
func DeleteAdminToken(ctx context.Context, id string) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	ct, err := Pool.Exec(ctx, `DELETE FROM admin_tokens WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("DeleteAdminToken: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("token not found")
	}
	return nil
}

// GetAllAdminTokenHashes returns all token hashes for auth checking.
func GetAllAdminTokenHashes(ctx context.Context) ([]string, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `SELECT token_hash FROM admin_tokens`)
	if err != nil {
		return nil, fmt.Errorf("GetAllAdminTokenHashes: %w", err)
	}
	defer rows.Close()
	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, fmt.Errorf("GetAllAdminTokenHashes scan: %w", err)
		}
		hashes = append(hashes, h)
	}
	return hashes, rows.Err()
}

// ---------- api_keys ----------

// GetAPIKeyByPrefix retrieves an active API key by its prefix.
func GetAPIKeyByPrefix(ctx context.Context, prefix string) (*ApiKey, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	k := &ApiKey{}
	err := Pool.QueryRow(ctx,
		`SELECT id, key_hash, key_prefix, created_at, last_seen, is_active, ip_created
		 FROM api_keys WHERE key_prefix = $1 AND is_active = true`, prefix,
	).Scan(&k.ID, &k.KeyHash, &k.KeyPrefix, &k.CreatedAt, &k.LastSeen, &k.IsActive, &k.IPCreated)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("GetAPIKeyByPrefix: %w", err)
	}
	return k, nil
}

// CreateAPIKey inserts a new API key row.
func CreateAPIKey(ctx context.Context, id, keyHash, keyPrefix, ipCreated string) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, key_prefix, created_at, is_active, ip_created)
		 VALUES ($1, $2, $3, NOW(), true, $4)`,
		id, keyHash, keyPrefix, ipCreated,
	)
	if err != nil {
		return fmt.Errorf("CreateAPIKey: %w", err)
	}
	return nil
}

// DeactivateAPIKey sets is_active = false for the given key.
func DeactivateAPIKey(ctx context.Context, id string) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`UPDATE api_keys SET is_active = false WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("DeactivateAPIKey: %w", err)
	}
	return nil
}

// UpdateAPIKeyLastSeen sets last_seen to NOW() for the given key.
func UpdateAPIKeyLastSeen(ctx context.Context, id string) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`UPDATE api_keys SET last_seen = NOW() WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("UpdateAPIKeyLastSeen: %w", err)
	}
	return nil
}

// ListAllAPIKeys returns all API keys with their associated bot info.
func ListAllAPIKeys(ctx context.Context) ([]map[string]interface{}, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx,
		`SELECT k.id, k.key_prefix, k.created_at, k.last_seen, k.is_active, k.ip_created,
		        b.id AS bot_id, b.name AS bot_name, b.avatar_color,
		        COALESCE(s.kills, 0) AS kills, COALESCE(s.deaths, 0) AS deaths,
		        COALESCE(s.elo, 1000) AS elo, COALESCE(s.rounds_played, 0) AS rounds_played
		 FROM api_keys k
		 LEFT JOIN bots b ON b.api_key_id = k.id
		 LEFT JOIN bot_stats s ON s.bot_id = b.id
		 ORDER BY k.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("ListAllAPIKeys: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var (
			keyID, keyPrefix                    string
			createdAt                           time.Time
			lastSeen                            *time.Time
			isActive                            bool
			ipCreated, botID, botName, avatarColor *string
			kills, deaths, elo, roundsPlayed    int
		)
		if err := rows.Scan(&keyID, &keyPrefix, &createdAt, &lastSeen, &isActive, &ipCreated,
			&botID, &botName, &avatarColor, &kills, &deaths, &elo, &roundsPlayed); err != nil {
			return nil, fmt.Errorf("ListAllAPIKeys scan: %w", err)
		}
		entry := map[string]interface{}{
			"key_id":        keyID,
			"key_prefix":    keyPrefix,
			"created_at":    createdAt,
			"last_seen":     lastSeen,
			"is_active":     isActive,
			"ip_created":    ipCreated,
			"bot_id":        botID,
			"bot_name":      botName,
			"avatar_color":  avatarColor,
			"kills":         kills,
			"deaths":        deaths,
			"elo":           elo,
			"rounds_played": roundsPlayed,
		}
		results = append(results, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListAllAPIKeys rows: %w", err)
	}
	return results, nil
}

// ---------- bots ----------

// GetBotByAPIKeyID retrieves a bot by its associated API key ID.
func GetBotByAPIKeyID(ctx context.Context, apiKeyID string) (*Bot, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	b := &Bot{}
	err := Pool.QueryRow(ctx,
		`SELECT id, api_key_id, name, avatar_color, default_weapon, default_stats,
		        default_fallback, created_at, updated_at
		 FROM bots WHERE api_key_id = $1`, apiKeyID,
	).Scan(&b.ID, &b.APIKeyID, &b.Name, &b.AvatarColor, &b.DefaultWeapon, &b.DefaultStats,
		&b.DefaultFallback, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("GetBotByAPIKeyID: %w", err)
	}
	return b, nil
}

// CreateBot inserts a new bot row.
func CreateBot(ctx context.Context, bot *Bot) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO bots (id, api_key_id, name, avatar_color, default_weapon, default_stats,
		                    default_fallback, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		bot.ID, bot.APIKeyID, bot.Name, bot.AvatarColor, bot.DefaultWeapon, bot.DefaultStats,
		bot.DefaultFallback, bot.CreatedAt, bot.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("CreateBot: %w", err)
	}
	return nil
}

// UpdateBot updates mutable fields on a bot row.
func UpdateBot(ctx context.Context, bot *Bot) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`UPDATE bots SET name = $1, avatar_color = $2, default_weapon = $3,
		                 default_stats = $4, default_fallback = $5, updated_at = $6
		 WHERE id = $7`,
		bot.Name, bot.AvatarColor, bot.DefaultWeapon, bot.DefaultStats,
		bot.DefaultFallback, bot.UpdatedAt, bot.ID,
	)
	if err != nil {
		return fmt.Errorf("UpdateBot: %w", err)
	}
	return nil
}

// ---------- bot_stats ----------

// GetBotStats retrieves stats for a given bot.
func GetBotStats(ctx context.Context, botID string) (*BotStats, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	s := &BotStats{}
	err := Pool.QueryRow(ctx,
		`SELECT bot_id, kills, deaths, assists, damage_dealt, damage_taken,
		        current_streak, best_streak, elo, time_alive_seconds, longest_life_secs,
		        rounds_played, round_wins, pickups_collected, distance_traveled, updated_at
		 FROM bot_stats WHERE bot_id = $1`, botID,
	).Scan(&s.BotID, &s.Kills, &s.Deaths, &s.Assists, &s.DamageDealt, &s.DamageTaken,
		&s.CurrentStreak, &s.BestStreak, &s.Elo, &s.TimeAliveSecs, &s.LongestLifeSecs,
		&s.RoundsPlayed, &s.RoundWins, &s.PickupsCollected, &s.DistanceTraveled, &s.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("GetBotStats: %w", err)
	}
	return s, nil
}

// UpsertBotStats inserts or updates bot_stats using ON CONFLICT.
func UpsertBotStats(ctx context.Context, stats *BotStats) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO bot_stats (bot_id, kills, deaths, assists, damage_dealt, damage_taken,
		                        current_streak, best_streak, elo, time_alive_seconds,
		                        longest_life_secs, rounds_played, round_wins,
		                        pickups_collected, distance_traveled, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		 ON CONFLICT (bot_id) DO UPDATE SET
		   kills = EXCLUDED.kills,
		   deaths = EXCLUDED.deaths,
		   assists = EXCLUDED.assists,
		   damage_dealt = EXCLUDED.damage_dealt,
		   damage_taken = EXCLUDED.damage_taken,
		   current_streak = EXCLUDED.current_streak,
		   best_streak = EXCLUDED.best_streak,
		   elo = EXCLUDED.elo,
		   time_alive_seconds = EXCLUDED.time_alive_seconds,
		   longest_life_secs = EXCLUDED.longest_life_secs,
		   rounds_played = EXCLUDED.rounds_played,
		   round_wins = EXCLUDED.round_wins,
		   pickups_collected = EXCLUDED.pickups_collected,
		   distance_traveled = EXCLUDED.distance_traveled,
		   updated_at = EXCLUDED.updated_at`,
		stats.BotID, stats.Kills, stats.Deaths, stats.Assists, stats.DamageDealt,
		stats.DamageTaken, stats.CurrentStreak, stats.BestStreak, stats.Elo,
		stats.TimeAliveSecs, stats.LongestLifeSecs, stats.RoundsPlayed, stats.RoundWins,
		stats.PickupsCollected, stats.DistanceTraveled, stats.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("UpsertBotStats: %w", err)
	}
	return nil
}

// ---------- kill_log ----------

// InsertKillLog inserts a new kill log entry.
func InsertKillLog(ctx context.Context, log *KillLog) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO kill_log (id, round_id, killer_id, victim_id, weapon, damage,
		                       killer_hp, tick, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		log.ID, log.RoundID, log.KillerID, log.VictimID, log.Weapon, log.Damage,
		log.KillerHP, log.Tick, log.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("InsertKillLog: %w", err)
	}
	return nil
}

// ---------- rounds ----------

// CreateRound inserts a new round row.
func CreateRound(ctx context.Context, round *Round) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO rounds (id, round_number, started_at, ended_at, bots_participated,
		                     mvp_bot_id, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		round.ID, round.RoundNumber, round.StartedAt, round.EndedAt,
		round.BotsParticipated, round.MVPBotID, round.Status,
	)
	if err != nil {
		return fmt.Errorf("CreateRound: %w", err)
	}
	return nil
}

// UpdateRound updates a round's ended_at, status, and mvp_bot_id.
func UpdateRound(ctx context.Context, round *Round) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`UPDATE rounds SET ended_at = $1, status = $2, mvp_bot_id = $3 WHERE id = $4`,
		round.EndedAt, round.Status, round.MVPBotID, round.ID,
	)
	if err != nil {
		return fmt.Errorf("UpdateRound: %w", err)
	}
	return nil
}

// ---------- weapon_balance ----------

// ListWeaponBalances returns every persisted weapon balance row.
func ListWeaponBalances(ctx context.Context) ([]WeaponBalance, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx,
		`SELECT weapon, damage_scale, cooldown_scale, adjustment_scale, rounds_tracked, updated_at
		 FROM weapon_balance ORDER BY weapon`)
	if err != nil {
		return nil, fmt.Errorf("ListWeaponBalances: %w", err)
	}
	defer rows.Close()

	var balances []WeaponBalance
	for rows.Next() {
		var wb WeaponBalance
		if err := rows.Scan(
			&wb.Weapon, &wb.DamageScale, &wb.CooldownScale,
			&wb.AdjustmentScale, &wb.RoundsTracked, &wb.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListWeaponBalances scan: %w", err)
		}
		balances = append(balances, wb)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListWeaponBalances rows: %w", err)
	}
	return balances, nil
}

// UpsertWeaponBalance stores the adaptive balance state for a weapon.
func UpsertWeaponBalance(ctx context.Context, wb *WeaponBalance) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx,
		`INSERT INTO weapon_balance
			(weapon, damage_scale, cooldown_scale, adjustment_scale, rounds_tracked, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (weapon) DO UPDATE SET
			damage_scale = EXCLUDED.damage_scale,
			cooldown_scale = EXCLUDED.cooldown_scale,
			adjustment_scale = EXCLUDED.adjustment_scale,
			rounds_tracked = EXCLUDED.rounds_tracked,
			updated_at = EXCLUDED.updated_at`,
		wb.Weapon, wb.DamageScale, wb.CooldownScale, wb.AdjustmentScale, wb.RoundsTracked, wb.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("UpsertWeaponBalance: %w", err)
	}
	return nil
}

// ListWeaponKillStats returns per-weapon kill totals from the kill log.
func ListWeaponKillStats(ctx context.Context) ([]WeaponKillStats, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `
		SELECT
			weapon,
			COUNT(*)::INT AS kills,
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '24 hours')::INT AS kills_24h,
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '1 hour')::INT AS kills_1h,
			COALESCE(SUM(damage), 0)::BIGINT AS finisher_damage
		FROM kill_log
		GROUP BY weapon
		ORDER BY weapon
	`)
	if err != nil {
		return nil, fmt.Errorf("ListWeaponKillStats: %w", err)
	}
	defer rows.Close()

	var stats []WeaponKillStats
	for rows.Next() {
		var item WeaponKillStats
		if err := rows.Scan(
			&item.Weapon,
			&item.Kills,
			&item.Kills24h,
			&item.Kills1h,
			&item.FinisherDamage,
		); err != nil {
			return nil, fmt.Errorf("ListWeaponKillStats scan: %w", err)
		}
		stats = append(stats, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListWeaponKillStats rows: %w", err)
	}
	return stats, nil
}

// ---------- leaderboard ----------

// validSortColumns maps allowed sort keys to SQL ORDER BY clauses.
var validSortColumns = map[string]string{
	"kills":      "s.kills DESC",
	"elo":        "s.elo DESC",
	"streak":     "s.best_streak DESC",
	"best_streak": "s.best_streak DESC",
	"kd_ratio":   "CASE WHEN s.deaths = 0 THEN s.kills ELSE s.kills::float / s.deaths END DESC",
}

// GetLeaderboard returns a paginated leaderboard with rank, sorted by the given column.
func GetLeaderboard(ctx context.Context, sortBy string, limit, offset int) ([]LeaderboardEntry, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	orderClause, ok := validSortColumns[sortBy]
	if !ok {
		orderClause = validSortColumns["kills"]
	}

	query := fmt.Sprintf(
		`SELECT
		   ROW_NUMBER() OVER (ORDER BY %s) AS rank,
		   b.id, b.name, b.avatar_color,
		   s.kills, s.deaths, s.elo, s.best_streak,
		   s.damage_dealt, s.rounds_played, s.round_wins
		 FROM bot_stats s
		 JOIN bots b ON b.id = s.bot_id
		 WHERE b.name NOT LIKE 'Legacy-%%'
		   AND (s.rounds_played > 0 OR s.kills > 0 OR s.deaths > 0 OR s.damage_dealt > 0)
		 ORDER BY %s
		 LIMIT $1 OFFSET $2`, orderClause, orderClause,
	)

	rows, err := Pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("GetLeaderboard: %w", err)
	}
	defer rows.Close()

	var entries []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(
			&e.Rank, &e.BotID, &e.Name, &e.AvatarColor,
			&e.Kills, &e.Deaths, &e.Elo, &e.BestStreak,
			&e.DamageDealt, &e.RoundsPlayed, &e.RoundWins,
		); err != nil {
			return nil, fmt.Errorf("GetLeaderboard scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetLeaderboard rows: %w", err)
	}
	return entries, nil
}

// GetLeaderboardCount returns the total number of entries in bot_stats.
func GetLeaderboardCount(ctx context.Context) (int, error) {
	if Pool == nil {
		return 0, ErrNoDatabase
	}
	var count int
	err := Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM bot_stats s
		JOIN bots b ON b.id = s.bot_id
		WHERE b.name NOT LIKE 'Legacy-%'
		  AND (s.rounds_played > 0 OR s.kills > 0 OR s.deaths > 0 OR s.damage_dealt > 0)
	`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("GetLeaderboardCount: %w", err)
	}
	return count, nil
}

// ---------- bounty board ----------

// ListBountyBoardEntries loads the persisted public bounty board.
func ListBountyBoardEntries(ctx context.Context) ([]BountyBoardEntry, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `
		SELECT bot_id, name, avatar_color, weapon, win_streak, bounty_points, claims, is_target, updated_at
		FROM bounty_board
		ORDER BY bounty_points DESC, win_streak DESC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("ListBountyBoardEntries: %w", err)
	}
	defer rows.Close()

	var entries []BountyBoardEntry
	for rows.Next() {
		var entry BountyBoardEntry
		if err := rows.Scan(
			&entry.BotID, &entry.Name, &entry.AvatarColor, &entry.Weapon,
			&entry.WinStreak, &entry.BountyPoints, &entry.Claims, &entry.IsTarget, &entry.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListBountyBoardEntries scan: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListBountyBoardEntries rows: %w", err)
	}
	return entries, nil
}

// ReplaceBountyBoardEntries rewrites the persisted bounty board to match the in-memory snapshot.
func ReplaceBountyBoardEntries(ctx context.Context, entries []BountyBoardEntry) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	tx, err := Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ReplaceBountyBoardEntries begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM bounty_board`); err != nil {
		return fmt.Errorf("ReplaceBountyBoardEntries clear: %w", err)
	}

	now := time.Now()
	for _, entry := range entries {
		if _, err := tx.Exec(ctx, `
			INSERT INTO bounty_board
				(bot_id, name, avatar_color, weapon, win_streak, bounty_points, claims, is_target, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`,
			entry.BotID, entry.Name, entry.AvatarColor, entry.Weapon,
			entry.WinStreak, entry.BountyPoints, entry.Claims, entry.IsTarget, now,
		); err != nil {
			return fmt.Errorf("ReplaceBountyBoardEntries insert: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ReplaceBountyBoardEntries commit: %w", err)
	}
	return nil
}

// GetLatestWinnerBountySeed reconstructs a single bounty candidate from the most
// recent consecutive completed round winners. It is used to repopulate the
// bounty board after a restart if no persisted board state exists.
func GetLatestWinnerBountySeed(ctx context.Context, threshold, base, step, maxPoints int) (*BountyBoardEntry, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `
		SELECT r.mvp_bot_id, b.name, b.avatar_color, b.default_weapon
		FROM rounds r
		JOIN bots b ON b.id = r.mvp_bot_id
		WHERE r.status = 'completed'
		  AND r.mvp_bot_id IS NOT NULL
		  AND b.name NOT LIKE 'Legacy-%'
		ORDER BY r.round_number DESC
		LIMIT 32
	`)
	if err != nil {
		return nil, fmt.Errorf("GetLatestWinnerBountySeed: %w", err)
	}
	defer rows.Close()

	type winnerRow struct {
		BotID       string
		Name        string
		AvatarColor string
		Weapon      string
	}

	var winners []winnerRow
	for rows.Next() {
		var row winnerRow
		if err := rows.Scan(&row.BotID, &row.Name, &row.AvatarColor, &row.Weapon); err != nil {
			return nil, fmt.Errorf("GetLatestWinnerBountySeed scan: %w", err)
		}
		winners = append(winners, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetLatestWinnerBountySeed rows: %w", err)
	}
	if len(winners) == 0 {
		return nil, nil
	}

	seed := winners[0]
	streak := 0
	for _, row := range winners {
		if row.BotID != seed.BotID {
			break
		}
		streak++
	}
	if streak < threshold {
		return nil, nil
	}

	points := base + (streak-threshold)*step
	if points > maxPoints {
		points = maxPoints
	}

	return &BountyBoardEntry{
		BotID:        seed.BotID,
		Name:         seed.Name,
		AvatarColor:  seed.AvatarColor,
		Weapon:       seed.Weapon,
		WinStreak:    streak,
		BountyPoints: points,
		Claims:       0,
		IsTarget:     true,
		UpdatedAt:    time.Now(),
	}, nil
}

// GetBotRank returns the 1-based rank of a bot for a given sort column.
func GetBotRank(ctx context.Context, botID, sortBy string) (int, error) {
	if Pool == nil {
		return 0, ErrNoDatabase
	}
	orderClause, ok := validSortColumns[sortBy]
	if !ok {
		orderClause = validSortColumns["kills"]
	}

	query := fmt.Sprintf(
		`SELECT rank FROM (
		   SELECT s.bot_id, ROW_NUMBER() OVER (ORDER BY %s) AS rank
		   FROM bot_stats s
		   JOIN bots b ON b.id = s.bot_id
		   WHERE b.name NOT LIKE 'Legacy-%%'
		     AND (s.rounds_played > 0 OR s.kills > 0 OR s.deaths > 0 OR s.damage_dealt > 0)
		 ) ranked WHERE bot_id = $1`, orderClause,
	)

	var rank int
	err := Pool.QueryRow(ctx, query, botID).Scan(&rank)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("GetBotRank: %w", err)
	}
	return rank, nil
}

// ---------- rate_limits ----------

// CheckRateLimit checks whether the given IP is allowed to generate another key.
// It returns (allowed, remaining, error). If the current window has expired (>1 hour),
// it resets the counter. If under the limit, it increments.
func CheckRateLimit(ctx context.Context, ip string, maxPerHour int) (bool, int, error) {
	if Pool == nil {
		return false, 0, ErrNoDatabase
	}
	var rl RateLimit
	err := Pool.QueryRow(ctx,
		`SELECT ip_address, keys_generated, window_start
		 FROM rate_limits WHERE ip_address = $1`, ip,
	).Scan(&rl.IPAddress, &rl.KeysGenerated, &rl.WindowStart)

	if err != nil && err != pgx.ErrNoRows {
		return false, 0, fmt.Errorf("CheckRateLimit select: %w", err)
	}

	now := time.Now()

	// No existing record -- create one and allow.
	if err == pgx.ErrNoRows {
		_, insertErr := Pool.Exec(ctx,
			`INSERT INTO rate_limits (ip_address, keys_generated, window_start)
			 VALUES ($1, 1, $2)`, ip, now,
		)
		if insertErr != nil {
			return false, 0, fmt.Errorf("CheckRateLimit insert: %w", insertErr)
		}
		return true, maxPerHour - 1, nil
	}

	// Window expired -- reset.
	if now.Sub(rl.WindowStart) > time.Hour {
		_, resetErr := Pool.Exec(ctx,
			`UPDATE rate_limits SET keys_generated = 1, window_start = $1
			 WHERE ip_address = $2`, now, ip,
		)
		if resetErr != nil {
			return false, 0, fmt.Errorf("CheckRateLimit reset: %w", resetErr)
		}
		return true, maxPerHour - 1, nil
	}

	// Under limit -- increment.
	if rl.KeysGenerated < maxPerHour {
		_, incErr := Pool.Exec(ctx,
			`UPDATE rate_limits SET keys_generated = keys_generated + 1
			 WHERE ip_address = $1`, ip,
		)
		if incErr != nil {
			return false, 0, fmt.Errorf("CheckRateLimit increment: %w", incErr)
		}
		remaining := maxPerHour - rl.KeysGenerated - 1
		return true, remaining, nil
	}

	// Over limit.
	remaining := 0
	return false, remaining, nil
}
