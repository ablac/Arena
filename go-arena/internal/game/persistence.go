package game

import (
	"context"
	"log/slog"
	"time"

	"arena-server/internal/db"

	"github.com/google/uuid"
)

// PersistBotStats saves accumulated round stats for every bot to the
// database. Errors are logged but do not stop the process so that a single
// bot failure does not prevent persistence for the rest.
func PersistBotStats(ctx context.Context, bots map[string]*BotState) {
	for _, bot := range bots {
		persistOne(ctx, bot)
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

	// Accumulate round totals onto lifetime stats.
	existing.Kills += bot.RoundKills
	existing.Deaths += bot.RoundDeaths
	existing.DamageDealt += int64(bot.RoundDamageDealt)
	existing.DamageTaken += int64(bot.RoundDamageTaken)
	existing.DistanceTraveled += bot.RoundDistance
	existing.PickupsCollected += bot.RoundPickups

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
