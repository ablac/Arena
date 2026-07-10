package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

const resetLeaderboardSQL = `TRUNCATE TABLE bot_stats, round_bot_stats RESTART IDENTITY`

type commandExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// ResetLeaderboard clears every table that backs a leaderboard view. The
// all-time leaderboard reads bot_stats, while time-window leaderboards read
// round_bot_stats, so both must be reset in the same database statement.
func ResetLeaderboard(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	return resetLeaderboardWith(ctx, Pool)
}

func resetLeaderboardWith(ctx context.Context, executor commandExecutor) error {
	if _, err := executor.Exec(ctx, resetLeaderboardSQL); err != nil {
		return fmt.Errorf("reset leaderboard: %w", err)
	}
	return nil
}
