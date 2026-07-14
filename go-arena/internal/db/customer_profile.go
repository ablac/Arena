package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PublicProfileBot is the public-safe subset of AccountBot shown on a
// profile card: no key prefix/active flag (those are account-management
// details, not public information), plus the handful of stats that make a
// bot card meaningful to a stranger.
type PublicProfileBot struct {
	BotID         string `json:"bot_id"`
	Name          string `json:"name"`
	AvatarColor   string `json:"avatar_color"`
	DefaultWeapon string `json:"default_weapon"`
	Elo           int    `json:"elo"`
	Kills         int    `json:"kills"`
	Deaths        int    `json:"deaths"`
	RoundWins     int    `json:"round_wins"`
}

// PublicProfile is what GET /api/v1/profile/{account_id} returns. It
// deliberately omits email: the account id and chat handle are the only
// identifiers exposed publicly.
type PublicProfile struct {
	AccountID   string             `json:"account_id"`
	DisplayName string             `json:"display_name"`
	Bio         string             `json:"bio"`
	AvatarColor string             `json:"avatar_color"`
	JoinedAt    time.Time          `json:"joined_at"`
	Bots        []PublicProfileBot `json:"bots"`
	ShowsBots   bool               `json:"shows_bots"`
}

// GetPublicProfile loads the public-facing profile for an account. Returns
// (nil, nil) for an unknown account (matching the "missing is not an error"
// convention already used for chat bans) so the caller renders a 404.
func GetPublicProfile(ctx context.Context, accountID string) (*PublicProfile, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	var profile PublicProfile
	var showBots bool
	err := Pool.QueryRow(ctx,
		`SELECT id, display_name, bio, avatar_color, show_bots_public, created_at
		 FROM customer_accounts WHERE id = $1`, accountID).
		Scan(&profile.AccountID, &profile.DisplayName, &profile.Bio, &profile.AvatarColor, &showBots, &profile.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetPublicProfile: %w", err)
	}
	profile.ShowsBots = showBots
	profile.Bots = []PublicProfileBot{}
	if !showBots {
		return &profile, nil
	}

	rows, err := Pool.Query(ctx,
		`SELECT b.id, b.name, b.avatar_color, b.default_weapon,
		        COALESCE(s.elo, 1000), COALESCE(s.kills, 0), COALESCE(s.deaths, 0), COALESCE(s.round_wins, 0)
		 FROM account_bot_links links
		 JOIN bots b ON b.id = links.bot_id
		 LEFT JOIN bot_stats s ON s.bot_id = b.id
		 WHERE links.account_id = $1
		 ORDER BY links.linked_at ASC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("GetPublicProfile bots: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b PublicProfileBot
		if err := rows.Scan(&b.BotID, &b.Name, &b.AvatarColor, &b.DefaultWeapon, &b.Elo, &b.Kills, &b.Deaths, &b.RoundWins); err != nil {
			return nil, fmt.Errorf("GetPublicProfile bots scan: %w", err)
		}
		profile.Bots = append(profile.Bots, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetPublicProfile bots rows: %w", err)
	}
	return &profile, nil
}

// CustomerProfileUpdate carries the partial-update fields for
// UpdateCustomerProfile; a nil pointer leaves that column unchanged.
type CustomerProfileUpdate struct {
	DisplayName    *string
	Bio            *string
	AvatarColor    *string
	ShowBotsPublic *bool
}

// UpdateCustomerProfile applies a partial update to an account's
// dashboard-editable profile fields and returns the resulting public view.
func UpdateCustomerProfile(ctx context.Context, accountID string, update CustomerProfileUpdate) (*PublicProfile, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	sets := []string{"updated_at = NOW()"}
	args := []interface{}{accountID}
	addSet := func(column string, value interface{}) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if update.DisplayName != nil {
		addSet("display_name", *update.DisplayName)
	}
	if update.Bio != nil {
		addSet("bio", *update.Bio)
	}
	if update.AvatarColor != nil {
		addSet("avatar_color", *update.AvatarColor)
	}
	if update.ShowBotsPublic != nil {
		addSet("show_bots_public", *update.ShowBotsPublic)
	}
	tag, err := Pool.Exec(ctx,
		fmt.Sprintf(`UPDATE customer_accounts SET %s WHERE id = $1`, strings.Join(sets, ", ")),
		args...)
	if err != nil {
		return nil, fmt.Errorf("UpdateCustomerProfile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrCustomerAccountNotFound
	}
	return GetPublicProfile(ctx, accountID)
}
