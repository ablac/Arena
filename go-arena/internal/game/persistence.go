package game

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"

	"github.com/google/uuid"
)

var (
	botStatsPersistenceMu    sync.Mutex
	botStatsPersistenceEpoch atomic.Uint64
	pendingBotStatsDeltas    = make(map[string]db.BotStatsDelta)
	applyBotStatsDelta       = db.ApplyBotStatsDelta
	insertRoundBotStats      = db.InsertRoundBotStats
)

// PersistBotStatsFromSnapshot saves accumulated round stats using pre-copied
// stat snapshots. This avoids data races because the snapshot values are
// copied under the engine lock before the goroutine starts.
func PersistBotStatsFromSnapshot(ctx context.Context, snaps []BotStatsSnapshot, winnerID string, finalizeRound bool) {
	botStatsPersistenceMu.Lock()
	defer botStatsPersistenceMu.Unlock()

	for _, snap := range snaps {
		if snap.PersistenceEpoch != botStatsPersistenceEpoch.Load() {
			continue
		}
		queueBotStatsDeltaLocked(botStatsDeltaFromSnapshot(snap, winnerID, finalizeRound))
	}
	flushBotStatsDeltasLocked(ctx)
}

func botStatsDeltaFromSnapshot(snap BotStatsSnapshot, winnerID string, finalizeRound bool) db.BotStatsDelta {
	roundsPlayed := 0
	roundWins := 0
	if finalizeRound {
		roundsPlayed = 1
		if snap.BotID == winnerID {
			roundWins = 1
		}
	}
	tickRate := max(1, snap.TickRate)
	lifeSecs := int(math.Round(float64(snap.RoundLongestLife) / float64(tickRate)))
	capturedAt := snap.CapturedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now()
	}
	// takeBotStatsSnapshot clamps Elo before asynchronous persistence. Do not
	// reread mutable runtime config from this goroutine.
	return db.BotStatsDelta{
		BotID:            snap.BotID,
		Kills:            snap.KillsDelta,
		Deaths:           snap.DeathsDelta,
		DamageDealt:      snap.DamageDealtDelta,
		DamageTaken:      snap.DamageTakenDelta,
		CurrentStreak:    snap.KillStreak,
		BestStreak:       snap.BestStreak,
		Elo:              snap.Elo,
		LongestLifeSecs:  lifeSecs,
		RoundsPlayed:     roundsPlayed,
		RoundWins:        roundWins,
		PickupsCollected: snap.PickupsDelta,
		DistanceTraveled: snap.DistanceDelta,
		CapturedAt:       capturedAt,
	}
}

func queueBotStatsDeltaLocked(delta db.BotStatsDelta) {
	pending, exists := pendingBotStatsDeltas[delta.BotID]
	if !exists {
		pendingBotStatsDeltas[delta.BotID] = delta
		return
	}
	pending.Kills += delta.Kills
	pending.Deaths += delta.Deaths
	pending.DamageDealt += delta.DamageDealt
	pending.DamageTaken += delta.DamageTaken
	pending.RoundsPlayed += delta.RoundsPlayed
	pending.RoundWins += delta.RoundWins
	pending.PickupsCollected += delta.PickupsCollected
	pending.DistanceTraveled += delta.DistanceTraveled
	if delta.BestStreak > pending.BestStreak {
		pending.BestStreak = delta.BestStreak
	}
	if delta.LongestLifeSecs > pending.LongestLifeSecs {
		pending.LongestLifeSecs = delta.LongestLifeSecs
	}
	if !delta.CapturedAt.Before(pending.CapturedAt) {
		pending.CurrentStreak = delta.CurrentStreak
		pending.Elo = delta.Elo
		pending.CapturedAt = delta.CapturedAt
	}
	pendingBotStatsDeltas[delta.BotID] = pending
}

func flushBotStatsDeltasLocked(ctx context.Context) {
	botIDs := make([]string, 0, len(pendingBotStatsDeltas))
	for botID := range pendingBotStatsDeltas {
		botIDs = append(botIDs, botID)
	}
	sort.Strings(botIDs)
	for _, botID := range botIDs {
		delta := pendingBotStatsDeltas[botID]
		if err := applyBotStatsDelta(ctx, &delta); err != nil {
			slog.Error("persist: failed to apply bot stats delta", "bot_id", botID, "error", err)
			continue
		}
		delete(pendingBotStatsDeltas, botID)
	}
}

// PersistSingleBot saves a single bot's stats, typically called on
// disconnect.
func PersistSingleBot(ctx context.Context, snapshot BotStatsSnapshot) {
	PersistBotStatsFromSnapshot(ctx, []BotStatsSnapshot{snapshot}, "", false)
}

// PersistRoundBotStats serializes the time-window source with leaderboard
// resets. An epoch captured before a successful reset is stale even if its
// goroutine was scheduled afterward, so it must not recreate truncated rows.
func PersistRoundBotStats(ctx context.Context, epoch uint64, roundID string, roundNumber int, bots map[string]*BotState, winnerID string) {
	botStatsPersistenceMu.Lock()
	defer botStatsPersistenceMu.Unlock()
	if epoch != botStatsPersistenceEpoch.Load() {
		return
	}
	if roundID == "" {
		slog.Error("persist: refusing round stats without a durable round identity", "round", roundNumber)
		return
	}

	botIDs := make([]string, 0, len(bots))
	for botID := range bots {
		botIDs = append(botIDs, botID)
	}
	sort.Strings(botIDs)
	for _, botID := range botIDs {
		bot := bots[botID]
		if bot == nil {
			continue
		}
		won := bot.BotID == winnerID
		lifeSecs := int(math.Round(float64(bot.RoundLongestLife) / math.Max(1, float64(config.C.TickRate))))
		if err := insertRoundBotStats(ctx, roundID, roundNumber, bot.BotID, bot.Name, bot.Weapon,
			bot.RoundKills, bot.RoundDeaths,
			int64(bot.RoundDamageDealt), int64(bot.RoundDamageTaken),
			lifeSecs, bot.RoundShotsFired, bot.RoundShotsHit, bot.RoundPickups,
			bot.RoundDistance, ClampElo(bot.Elo), won); err != nil {
			slog.Error("persist: failed to insert round bot stats", "bot_id", bot.BotID, "round_id", roundID, "round", roundNumber, "error", err)
		}
	}
}

// InsertKillLog records a kill event in the database.
func InsertKillLog(ctx context.Context, roundID, killerID, victimID, weapon string, damage float64, killerHP, tick int) {
	var rID *string
	if roundID != "" {
		rID = &roundID
	}

	entry := &db.KillLog{
		ID:        uuid.New().String(),
		RoundID:   rID,
		KillerID:  killerID,
		VictimID:  victimID,
		Weapon:    weapon,
		Damage:    int(damage),
		KillerHP:  killerHP,
		Tick:      tick,
		CreatedAt: time.Now(),
	}

	if err := db.InsertKillLog(ctx, entry); err != nil {
		slog.Error("persist: failed to insert kill log", "error", err)
	}
}
