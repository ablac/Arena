package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type PlatformAgentUnlinkCommand struct {
	AccountID               string `json:"account_id"`
	AgentID                 string `json:"agent_id"`
	ExpectedAccountRevision int64  `json:"expected_account_revision"`
	Reason                  string `json:"reason"`
	IdempotencyKey          string `json:"-"`
}

// UnlinkPlatformAgent applies the exact W1a account-agent unlink command.
// Revision enforcement, assignment cleanup, history, change records, and the
// replay record commit in one PostgreSQL transaction.
func UnlinkPlatformAgent(ctx context.Context, command PlatformAgentUnlinkCommand) (*PlatformAgentLinkResult, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if command.AccountID == "" || strings.TrimSpace(command.AccountID) != command.AccountID || utf8.RuneCountInString(command.AccountID) > 128 {
		return nil, errors.New("platform agent unlink requires a valid account ID")
	}
	if command.AgentID == "" || strings.TrimSpace(command.AgentID) != command.AgentID || utf8.RuneCountInString(command.AgentID) > 128 {
		return nil, errors.New("platform agent unlink requires a valid agent ID")
	}
	if command.ExpectedAccountRevision < 0 {
		return nil, errors.New("platform agent unlink requires a nonnegative expected account revision")
	}
	if command.Reason == "" || strings.TrimSpace(command.Reason) != command.Reason || utf8.RuneCountInString(command.Reason) > 256 {
		return nil, errors.New("platform agent unlink requires a 1-256 character reason without surrounding whitespace")
	}
	idempotencyKeyLength := utf8.RuneCountInString(command.IdempotencyKey)
	if strings.TrimSpace(command.IdempotencyKey) != command.IdempotencyKey || idempotencyKeyLength < platformIdempotencyKeyMinimum || idempotencyKeyLength > platformIdempotencyKeyMaximum {
		return nil, errors.New("platform agent unlink requires an 8-128 character idempotency key without surrounding whitespace")
	}

	requestJSON, err := json.Marshal(struct {
		AccountID               string `json:"account_id"`
		AgentID                 string `json:"agent_id"`
		ExpectedAccountRevision int64  `json:"expected_account_revision"`
		Reason                  string `json:"reason"`
	}{
		AccountID:               command.AccountID,
		AgentID:                 command.AgentID,
		ExpectedAccountRevision: command.ExpectedAccountRevision,
		Reason:                  command.Reason,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal platform agent unlink request: %w", err)
	}
	requestHash := sha256.Sum256(requestJSON)
	scopedKeyHash := sha256.Sum256([]byte(command.AccountID + "\x1f" + command.IdempotencyKey))
	scopedIdempotencyKey := hex.EncodeToString(scopedKeyHash[:])
	const operation = "unlink_platform_agent"

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("UnlinkPlatformAgent begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := lockCustomerAccount(ctx, tx, command.AccountID, true); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, operation+":"+command.AccountID+":"+command.IdempotencyKey); err != nil {
		return nil, fmt.Errorf("UnlinkPlatformAgent idempotency lock: %w", err)
	}
	capacity, err := lockPlatformAccountCapacityTx(ctx, tx, command.AccountID)
	if err != nil {
		return nil, err
	}

	var storedHash, storedResponse []byte
	err = tx.QueryRow(ctx, `
		SELECT request_hash, response
		FROM platform_idempotency_records
		WHERE operation = $1 AND idempotency_key = $2`, operation, scopedIdempotencyKey).Scan(&storedHash, &storedResponse)
	if err == nil {
		if !bytes.Equal(storedHash, requestHash[:]) {
			return nil, ErrPlatformIdempotencyConflict
		}
		var replayed PlatformAgentLinkResult
		if err := json.Unmarshal(storedResponse, &replayed); err != nil {
			return nil, fmt.Errorf("UnlinkPlatformAgent decode replay: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("UnlinkPlatformAgent replay commit: %w", err)
		}
		return &replayed, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("UnlinkPlatformAgent load idempotency: %w", err)
	}
	if capacity.Revision != command.ExpectedAccountRevision {
		return nil, fmt.Errorf("%w: expected %d, current %d", ErrPlatformRevisionConflict, command.ExpectedAccountRevision, capacity.Revision)
	}

	result, err := unlinkBotFromCustomerAccountTx(ctx, tx, command.AccountID, command.AgentID, command.Reason)
	if err != nil {
		return nil, err
	}
	responseJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("UnlinkPlatformAgent marshal response: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO platform_idempotency_records (
			operation, idempotency_key, request_hash, response,
			subject_kind, subject_id, revision, created_at
		) VALUES ($1, $2, $3, $4, 'agent_link', $5, $6, $7)`,
		operation,
		scopedIdempotencyKey,
		requestHash[:],
		responseJSON,
		command.AgentID,
		result.Revision,
		time.Now().UTC().Truncate(time.Microsecond),
	); err != nil {
		return nil, fmt.Errorf("UnlinkPlatformAgent store idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("UnlinkPlatformAgent commit: %w", err)
	}
	return result, nil
}
