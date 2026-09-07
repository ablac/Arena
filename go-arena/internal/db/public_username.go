package db

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

var ErrPublicUsernameRequired = errors.New("public username required")

const PublicUsernameSetupURL = "https://accounts.angel-serv.com/portal/account/details"
const PublicUsernameUnavailable = "Username unavailable"

var publicUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,24}$`)

// NormalizePublicUsername accepts only the central public-identity claim.
// Missing or malformed values never fall back to a private name or address.
func NormalizePublicUsername(value *string) *string {
	if value == nil || !publicUsernamePattern.MatchString(*value) {
		return nil
	}
	normalized := strings.ToLower(*value)
	return &normalized
}
func PublicUsernameLabel(value *string) string {
	if username := NormalizePublicUsername(value); username != nil {
		return *username
	}
	return PublicUsernameUnavailable
}

// GetPublicUsernames resolves current labels in one bounded query. Absent
// accounts and aliases both map to nil. Account IDs alone define ownership.
func GetPublicUsernames(ctx context.Context, accountIDs []string) (map[string]*string, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	out := make(map[string]*string, len(accountIDs))
	if len(accountIDs) == 0 {
		return out, nil
	}
	rows, err := Pool.Query(ctx, `SELECT id, public_username FROM customer_accounts WHERE id = ANY($1)`, accountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var username *string
		if err := rows.Scan(&id, &username); err != nil {
			return nil, err
		}
		out[id] = NormalizePublicUsername(username)
	}
	return out, rows.Err()
}
