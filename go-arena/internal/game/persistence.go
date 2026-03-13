package game

import (
	"context"
	"log/slog"
	"time"

	"arena-server/internal/db"

	"github.com/google/uuid"
)

// PersistBotStatsFromSnapshot saves accumulated round stats using pre-copied
// stat snapshots. This avoids data races because the snapshot values are
// copied under the engine lock before the goroutine starts.
func PersistBotStatsFromSnapshot(ctx context.Context, snaps []BotStatsSnapshot) {
	for _, snap := range snaps {
		persistOneSnapshot(ctx, snap)
	}
}

// persistOneSnapshot performs the load-merge-upsert for one bot using a
// value-copy snapshot instead of a live BotState pointer.
func persistOneSnapshot(ctx context.Context, snap BotStatsSnapshot) {
	existing, err := db.GetBotStats(ctx, snap.BotID)
	if err != nil {
		slog.Error("persist: failed to get bot stats", "bot_id", snap.BotID, "error", err)
		return
	}

	now := time.Now()

	if existing == nil {
		existing = &db.BotStats{
			BotID:     snap.BotID,
			Elo:       snap.Elo,
			UpdatedAt: now,
		}
	}

	// Accumulate only the delta since last persist to avoid double-counting.
	existing.Kills += snap.RoundKills - snap.PersistedKills
	existing.Deaths += snap.RoundDeaths - snap.PersistedDeaths
	existing.DamageDealt += int64(snap.RoundDamageDealt) - int64(snap.PersistedDamageDealt)
	existing.DamageTaken += int64(snap.RoundDamageTaken) - int64(snap.PersistedDamageTaken)
	existing.DistanceTraveled += snap.RoundDistance - snap.PersistedDistance
	existing.PickupsCollected += snap.RoundPickups - snap.PersistedPickups

	existing.Elo = snap.Elo
	existing.UpdatedAt = now

	if err := db.UpsertBotStats(ctx, existing); err != nil {
		slog.Error("persist: failed to upsert bot stats", "bot_id", snap.BotID, "error", err)
	}
}

// PersistSingleBot saves a single bot's stats, typically called on
// disconnect.
func PersistSingleBot(ctx context.Context, bot *BotState) {
	persistOne(ctx, bot)
}

// persistOne performs the actual load-merge-upsert for one bot.
func persistOne(ctx context.Context, bot *BotState) {
	existing, err := db.GetBotStats(ctx, bot.BotID)
	if err != nil {
		slog.Error("persist: failed to get bot stats", "bot_id", bot.BotID, "error", err)
		return
	}

	now := time.Now()

	if existing == nil {
		// First time: create a fresh stats row.
		existing = &db.BotStats{
			BotID:     bot.BotID,
			Elo:       bot.Elo,
			UpdatedAt: now,
		}
	}

	// Accumulate only the delta since last persist to avoid double-counting.
	existing.Kills += bot.RoundKills - bot.PersistedKills
	existing.Deaths += bot.RoundDeaths - bot.PersistedDeaths
	existing.DamageDealt += int64(bot.RoundDamageDealt) - int64(bot.PersistedDamageDealt)
	existing.DamageTaken += int64(bot.RoundDamageTaken) - int64(bot.PersistedDamageTaken)
	existing.DistanceTraveled += bot.RoundDistance - bot.PersistedDistance
	existing.PickupsCollected += bot.RoundPickups - bot.PersistedPickups

	// Update snapshot so next persist only adds new deltas.
	bot.PersistedKills = bot.RoundKills
	bot.PersistedDeaths = bot.RoundDeaths
	bot.PersistedDamageDealt = bot.RoundDamageDealt
	bot.PersistedDamageTaken = bot.RoundDamageTaken
	bot.PersistedDistance = bot.RoundDistance
	bot.PersistedPickups = bot.RoundPickups

	// Streak tracking.
	existing.CurrentStreak = bot.KillStreak
	if bot.KillStreak > existing.BestStreak {
		existing.BestStreak = bot.KillStreak
	}

	// ELO.
	existing.Elo = bot.Elo

	// Longest life (in ticks, stored as seconds).
	lifeSecs := bot.RoundLongestLife
	if lifeSecs > existing.LongestLifeSecs {
		existing.LongestLifeSecs = lifeSecs
	}

	existing.UpdatedAt = now

	if err := db.UpsertBotStats(ctx, existing); err != nil {
		slog.Error("persist: failed to upsert bot stats", "bot_id", bot.BotID, "error", err)
	}
}

// InsertKillLog records a kill event in the database.
func InsertKillLog(ctx context.Context, roundID string, killer, victim *BotState, weapon string, damage float64, tick int) {
	var rID *string
	if roundID != "" {
		rID = &roundID
	}

	entry := &db.KillLog{
		ID:        uuid.New().String(),
		RoundID:   rID,
		KillerID:  killer.BotID,
		VictimID:  victim.BotID,
		Weapon:    weapon,
		Damage:    int(damage),
		KillerHP:  int(killer.HP),
		Tick:      tick,
		CreatedAt: time.Now(),
	}

	if err := db.InsertKillLog(ctx, entry); err != nil {
		slog.Error("persist: failed to insert kill log", "error", err)
	}
}
