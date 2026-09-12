package db

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

type GamingBot struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Elo    int    `json:"elo"`
	Kills  int    `json:"kills"`
	Deaths int    `json:"deaths"`
	Wins   int    `json:"wins"`
	Public bool   `json:"public"`
}
type GamingProfile struct {
	Bots           []GamingBot `json:"bots"`
	PublicUsername *string     `json:"public_username"`
}

// Caller has verified the Gaming service, which only requests the signed-in
// owner's identity. Privacy flags accompany every bot for public projections.
func GetGamingProfile(ctx context.Context, issuer, subject string) (*GamingProfile, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	result := &GamingProfile{Bots: []GamingBot{}}
	var accountID string
	err := Pool.QueryRow(ctx, `SELECT id,public_username FROM customer_accounts WHERE oidc_issuer=$1 AND oidc_subject=$2`, issuer, subject).Scan(&accountID, &result.PublicUsername)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	result.PublicUsername = NormalizePublicUsername(result.PublicUsername)
	rows, err := Pool.Query(ctx, `SELECT b.id,b.name,COALESCE(s.elo,1000),COALESCE(s.kills,0),COALESCE(s.deaths,0),COALESCE(s.round_wins,0),a.show_bots_public
 FROM account_bot_links l JOIN bots b ON b.id=l.bot_id JOIN customer_accounts a ON a.id=l.account_id
 LEFT JOIN bot_stats s ON s.bot_id=b.id WHERE l.account_id=$1 ORDER BY l.linked_at,b.id LIMIT 100`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var bot GamingBot
		if err = rows.Scan(&bot.ID, &bot.Name, &bot.Elo, &bot.Kills, &bot.Deaths, &bot.Wins, &bot.Public); err != nil {
			return nil, err
		}
		result.Bots = append(result.Bots, bot)
	}
	return result, rows.Err()
}

func EnsureGamingSchema(ctx context.Context) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS gaming_event_outbox(
 event_id TEXT PRIMARY KEY,payload JSONB NOT NULL,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 attempts INTEGER NOT NULL DEFAULT 0,next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 delivered_at TIMESTAMPTZ,last_status INTEGER NOT NULL DEFAULT 0);
 CREATE INDEX IF NOT EXISTS idx_gaming_outbox_pending ON gaming_event_outbox(next_attempt_at,event_id) WHERE delivered_at IS NULL`)
	return err
}

type GamingEvent struct {
	ID      string
	Payload json.RawMessage
}

// Claim at most ten rows with a durable lease. A crashed sender leaves them
// eligible after one minute; concurrent senders cannot claim the same lease.
func ClaimGamingEvents(ctx context.Context) ([]GamingEvent, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	rows, err := Pool.Query(ctx, `WITH pending AS(SELECT event_id FROM gaming_event_outbox
 WHERE delivered_at IS NULL AND next_attempt_at<=NOW() ORDER BY next_attempt_at,event_id LIMIT 10 FOR UPDATE SKIP LOCKED)
 UPDATE gaming_event_outbox o SET attempts=LEAST(attempts+1,1000000),next_attempt_at=NOW()+INTERVAL '1 minute'
 FROM pending p WHERE o.event_id=p.event_id RETURNING o.event_id,o.payload`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []GamingEvent{}
	for rows.Next() {
		var e GamingEvent
		if err = rows.Scan(&e.ID, &e.Payload); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
func CompleteGamingEvent(ctx context.Context, id string, status int, success bool) error {
	if Pool == nil {
		return ErrNoDatabase
	}
	_, err := Pool.Exec(ctx, `UPDATE gaming_event_outbox SET last_status=$2,
 delivered_at=CASE WHEN $3 THEN NOW() ELSE NULL END,
 next_attempt_at=NOW()+LEAST(3600,POWER(2,LEAST(attempts,12))) * INTERVAL '1 second'
 WHERE event_id=$1 AND delivered_at IS NULL`, id, status, success)
	return err
}
