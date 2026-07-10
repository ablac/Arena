package game

import (
	"context"
	"errors"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
)

func isolateBotStatsPersistence(t *testing.T) uint64 {
	t.Helper()
	botStatsPersistenceMu.Lock()
	previousApply := applyBotStatsDelta
	previousInsertRound := insertRoundBotStats
	previousPending := pendingBotStatsDeltas
	previousEpoch := botStatsPersistenceEpoch.Load()
	pendingBotStatsDeltas = make(map[string]db.BotStatsDelta)
	botStatsPersistenceMu.Unlock()

	t.Cleanup(func() {
		botStatsPersistenceMu.Lock()
		applyBotStatsDelta = previousApply
		insertRoundBotStats = previousInsertRound
		pendingBotStatsDeltas = previousPending
		botStatsPersistenceEpoch.Store(previousEpoch)
		botStatsPersistenceMu.Unlock()
	})
	return previousEpoch
}

func TestSnapshotBotStatsReservesDisjointDeltas(t *testing.T) {
	bot := &BotState{
		BotID:            "bot-1",
		Elo:              1100,
		RoundKills:       2,
		RoundDeaths:      1,
		RoundDamageDealt: 10.9,
		RoundDamageTaken: 4.8,
		RoundDistance:    12.5,
		RoundPickups:     1,
		RoundLongestLife: 40,
	}
	engine := &GameEngine{Bots: map[string]*BotState{bot.BotID: bot}}

	first := engine.snapshotBotStats()[0]
	if first.KillsDelta != 2 || first.DeathsDelta != 1 || first.DamageDealtDelta != 10 ||
		first.DamageTakenDelta != 4 || first.DistanceDelta != 12.5 || first.PickupsDelta != 1 {
		t.Fatalf("unexpected first delta: %+v", first)
	}

	bot.RoundKills = 3
	bot.RoundDeaths = 2
	bot.RoundDamageDealt = 12.2
	bot.RoundDamageTaken = 8.1
	bot.RoundDistance = 20
	bot.RoundPickups = 3
	second := engine.snapshotBotStats()[0]
	if second.KillsDelta != 1 || second.DeathsDelta != 1 || second.DamageDealtDelta != 2 ||
		second.DamageTakenDelta != 4 || second.DistanceDelta != 7.5 || second.PickupsDelta != 2 {
		t.Fatalf("unexpected second delta: %+v", second)
	}

	final := engine.snapshotBotStats()[0]
	if final.KillsDelta != 0 || final.DeathsDelta != 0 || final.DamageDealtDelta != 0 ||
		final.DamageTakenDelta != 0 || final.DistanceDelta != 0 || final.PickupsDelta != 0 {
		t.Fatalf("unchanged totals were counted again: %+v", final)
	}

	if first.KillsDelta+second.KillsDelta+final.KillsDelta != bot.RoundKills {
		t.Fatalf("kill deltas do not reconstruct cumulative total")
	}
	if first.DamageDealtDelta+second.DamageDealtDelta+final.DamageDealtDelta != int64(bot.RoundDamageDealt) {
		t.Fatalf("damage deltas do not reconstruct cumulative total")
	}
}

func TestSnapshotBotStatsNeverEmitsNegativeDelta(t *testing.T) {
	bot := &BotState{
		BotID:                "bot-reset",
		PersistedKills:       5,
		PersistedDeaths:      4,
		PersistedDistance:    20,
		PersistedPickups:     3,
		RoundDamageDealt:     1,
		PersistedDamageDealt: 10,
	}

	snapshot := takeBotStatsSnapshot(bot)
	if snapshot.KillsDelta < 0 || snapshot.DeathsDelta < 0 || snapshot.DamageDealtDelta < 0 ||
		snapshot.DamageTakenDelta < 0 || snapshot.DistanceDelta < 0 || snapshot.PickupsDelta < 0 {
		t.Fatalf("counter reset emitted a negative delta: %+v", snapshot)
	}
}

func TestFailedBotStatsDeltaIsRetriedWithNextSnapshot(t *testing.T) {
	epoch := isolateBotStatsPersistence(t)
	calls := 0
	var applied db.BotStatsDelta
	applyBotStatsDelta = func(_ context.Context, delta *db.BotStatsDelta) error {
		calls++
		if calls == 1 {
			return errors.New("temporary database failure")
		}
		applied = *delta
		return nil
	}

	first := BotStatsSnapshot{
		BotID: "retry-bot", KillsDelta: 2, DamageDealtDelta: 10,
		CapturedAt: time.Unix(1, 0), PersistenceEpoch: epoch,
	}
	PersistBotStatsFromSnapshot(context.Background(), []BotStatsSnapshot{first}, "", false)

	second := BotStatsSnapshot{
		BotID: "retry-bot", KillsDelta: 1, DamageDealtDelta: 4,
		CapturedAt: time.Unix(2, 0), PersistenceEpoch: epoch,
	}
	PersistBotStatsFromSnapshot(context.Background(), []BotStatsSnapshot{second}, "", false)

	if calls != 2 {
		t.Fatalf("apply calls = %d, want 2", calls)
	}
	if applied.Kills != 3 || applied.DamageDealt != 14 {
		t.Fatalf("retry did not retain and merge failed delta: %+v", applied)
	}
	botStatsPersistenceMu.Lock()
	pending := len(pendingBotStatsDeltas)
	botStatsPersistenceMu.Unlock()
	if pending != 0 {
		t.Fatalf("successful retry left %d pending deltas", pending)
	}
}

func TestResetLeaderboardInvalidatesStaleSnapshots(t *testing.T) {
	epoch := isolateBotStatsPersistence(t)
	previousStartingElo := config.C.EloStarting
	config.C.EloStarting = 1000
	t.Cleanup(func() { config.C.EloStarting = previousStartingElo })

	bot := &BotState{
		BotID: "active", Elo: 1450, KillStreak: 4, BestKillStreak: 7,
		RoundKills: 5, RoundDamageDealt: 50, RoundShotsFired: 8,
		RoundLifeStartTick: 10, RoundLongestLife: 80,
	}
	waiting := &BotState{BotID: "waiting", Elo: 1300, RoundKills: 2}
	engine := &GameEngine{
		Bots:        map[string]*BotState{bot.BotID: bot},
		WaitingBots: map[string]*BotState{waiting.BotID: waiting},
		TickCount:   99,
		Round:       RoundState{Phase: PhaseActive, RoundNumber: 12},
	}
	stale := takeBotStatsSnapshot(bot)
	if stale.PersistenceEpoch != epoch {
		t.Fatalf("stale snapshot epoch = %d, want %d", stale.PersistenceEpoch, epoch)
	}

	var applied []db.BotStatsDelta
	applyBotStatsDelta = func(_ context.Context, delta *db.BotStatsDelta) error {
		applied = append(applied, *delta)
		return nil
	}
	if err := engine.ResetLeaderboard(context.Background(), func(context.Context) error {
		if bot.Elo != 1000 || bot.RoundKills != 5 || waiting.Elo != 1000 || waiting.RoundKills != 2 {
			return errors.New("leaderboard reset altered active round scoring")
		}
		if bot.PersistedKills != 5 || waiting.PersistedKills != 2 ||
			!bot.LeaderboardRebased || bot.LeaderboardKillBaseline != 4 || bot.LeaderboardLifeBaseline != 80 {
			return errors.New("leaderboard reset did not reserve the live persistence baseline")
		}
		if bot.KillStreak != 4 || bot.BestKillStreak != 7 || bot.RoundLongestLife != 80 {
			return errors.New("leaderboard reset altered live non-additive round state")
		}
		return nil
	}); err != nil {
		t.Fatalf("ResetLeaderboard: %v", err)
	}
	if bot.RoundLifeStartTick != 10 {
		t.Fatalf("leaderboard reset changed active life baseline: %d", bot.RoundLifeStartTick)
	}
	if engine.skipLeaderboardRound != 12 {
		t.Fatalf("straddling round marker = %d, want 12", engine.skipLeaderboardRound)
	}

	PersistBotStatsFromSnapshot(context.Background(), []BotStatsSnapshot{stale}, "", false)
	if len(applied) != 0 {
		t.Fatalf("stale pre-reset snapshot was applied: %+v", applied)
	}

	immediate := takeBotStatsSnapshot(bot)
	if immediate.KillStreak != 0 || immediate.BestStreak != 0 || immediate.RoundLongestLife != 0 {
		t.Fatalf("pre-reset non-additive stats leaked into fresh snapshot: %+v", immediate)
	}

	bot.RoundKills = 7
	bot.KillStreak = 6
	bot.RoundLongestLife = 110
	fresh := takeBotStatsSnapshot(bot)
	if fresh.KillStreak != 2 || fresh.BestStreak != 2 || fresh.RoundLongestLife != 30 {
		t.Fatalf("post-reset non-additive snapshot = %+v, want streaks=2 life=30", fresh)
	}
	PersistBotStatsFromSnapshot(context.Background(), []BotStatsSnapshot{fresh}, "", false)
	if len(applied) != 1 || applied[0].Kills != 2 {
		t.Fatalf("fresh post-reset snapshot was not applied exactly once: %+v", applied)
	}
}

func TestResetLeaderboardFailureRestoresMemoryAndEpoch(t *testing.T) {
	epoch := isolateBotStatsPersistence(t)
	bot := &BotState{
		BotID: "restore", Elo: 1400, KillStreak: 3, BestKillStreak: 6,
		RoundKills: 4, RoundDeaths: 2, RoundDamageDealt: 44,
		PersistedKills: 3, PersistedDamageDealt: 30, RoundLifeStartTick: 12,
	}
	engine := &GameEngine{
		Bots: map[string]*BotState{bot.BotID: bot}, TickCount: 50,
		Round:                RoundState{Phase: PhaseActive, RoundNumber: 9},
		skipLeaderboardRound: 4,
	}

	err := engine.ResetLeaderboard(context.Background(), func(context.Context) error {
		return errors.New("truncate failed")
	})
	if err == nil {
		t.Fatal("expected reset failure")
	}
	if bot.Elo != 1400 || bot.RoundKills != 4 || bot.PersistedKills != 3 ||
		bot.RoundDamageDealt != 44 || bot.PersistedDamageDealt != 30 || bot.RoundLifeStartTick != 12 {
		t.Fatalf("failed reset did not restore bot counters: %+v", bot)
	}
	if got := botStatsPersistenceEpoch.Load(); got != epoch {
		t.Fatalf("failed reset advanced epoch: %d != %d", got, epoch)
	}
	if engine.skipLeaderboardRound != 4 {
		t.Fatalf("failed reset changed straddling round marker: %d", engine.skipLeaderboardRound)
	}
}

func TestRoundBotStatsRejectsPreResetEpoch(t *testing.T) {
	epoch := isolateBotStatsPersistence(t)
	inserted := 0
	insertRoundBotStats = func(_ context.Context, _ int, _, _, _ string,
		_, _ int, _, _ int64, _, _, _, _ int, _ float64, _ int, _ bool) error {
		inserted++
		return nil
	}
	bot := &BotState{BotID: "round-bot", Name: "Round Bot", Weapon: "sword", RoundKills: 2}
	engine := &GameEngine{Bots: map[string]*BotState{bot.BotID: bot}}

	if err := engine.ResetLeaderboard(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("ResetLeaderboard: %v", err)
	}
	PersistRoundBotStats(context.Background(), epoch, 1, map[string]*BotState{bot.BotID: bot}, "")
	if inserted != 0 {
		t.Fatal("pre-reset round stats recreated a truncated row")
	}

	PersistRoundBotStats(context.Background(), botStatsPersistenceEpoch.Load(), 2,
		map[string]*BotState{bot.BotID: bot}, "")
	if inserted != 1 {
		t.Fatalf("fresh round stats inserts = %d, want 1", inserted)
	}
}
