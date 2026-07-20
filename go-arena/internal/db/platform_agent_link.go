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

	"arena-server/internal/security/credential"

	"github.com/jackc/pgx/v5"
)

// LinkPlatformAgent applies the exact W1a account-agent link command. Control
// proof verification, revision/cap checks, link history, legacy-license
// recovery, and idempotency commit in one PostgreSQL transaction.
func LinkPlatformAgent(
	ctx context.Context,
	command PlatformAgentLinkCommand,
) (*PlatformAgentLinkResult, error) {
	if Pool == nil {
		return nil, ErrNoDatabase
	}
	if command.AccountID == "" || strings.TrimSpace(command.AccountID) != command.AccountID || utf8.RuneCountInString(command.AccountID) > 128 {
		return nil, errors.New("platform agent link requires a valid account ID")
	}
	if command.AgentID == "" || strings.TrimSpace(command.AgentID) != command.AgentID || utf8.RuneCountInString(command.AgentID) > 128 {
		return nil, errors.New("platform agent link requires a valid agent ID")
	}
	proofLength := utf8.RuneCountInString(command.ControlProof)
	if proofLength < 16 || proofLength > 4096 {
		return nil, ErrPlatformControlProofRejected
	}
	if command.ExpectedAccountRevision < 0 {
		return nil, errors.New("platform agent link requires a nonnegative expected account revision")
	}
	if strings.TrimSpace(command.IdempotencyKey) != command.IdempotencyKey || utf8.RuneCountInString(command.IdempotencyKey) < platformIdempotencyKeyMinimum || utf8.RuneCountInString(command.IdempotencyKey) > platformIdempotencyKeyMaximum {
		return nil, errors.New("platform agent link requires an 8-128 character idempotency key without surrounding whitespace")
	}

	requestJSON, err := json.Marshal(struct {
		AccountID               string `json:"account_id"`
		AgentID                 string `json:"agent_id"`
		ExpectedAccountRevision int64  `json:"expected_account_revision"`
	}{
		AccountID:               command.AccountID,
		AgentID:                 command.AgentID,
		ExpectedAccountRevision: command.ExpectedAccountRevision,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal platform agent link request: %w", err)
	}
	requestHash := sha256.Sum256(requestJSON)
	scopedKeyHash := sha256.Sum256([]byte(command.AccountID + "\x1f" + command.IdempotencyKey))
	scopedIdempotencyKey := hex.EncodeToString(scopedKeyHash[:])
	const operation = "link_platform_agent"

	tx, err := Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("LinkPlatformAgent begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := lockCustomerAccount(ctx, tx, command.AccountID, true); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, operation+":"+command.AccountID+":"+command.IdempotencyKey); err != nil {
		return nil, fmt.Errorf("LinkPlatformAgent idempotency lock: %w", err)
	}
	capacity, err := lockPlatformAccountCapacityTx(ctx, tx, command.AccountID)
	if err != nil {
		return nil, err
	}

	bot, apiKeyID, agentStatus, err := loadPlatformAgentControlProofTx(ctx, tx, command.ControlProof)
	if err != nil {
		return nil, err
	}
	if bot.BotID != command.AgentID {
		return nil, ErrPlatformControlProofRejected
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
			return nil, fmt.Errorf("LinkPlatformAgent decode replay: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("LinkPlatformAgent replay commit: %w", err)
		}
		return &replayed, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("LinkPlatformAgent load idempotency: %w", err)
	}
	if agentStatus == "retired" {
		return nil, ErrPlatformAgentInactive
	}
	if capacity.Status != "active" {
		return nil, fmt.Errorf("%w: account %q is %s", ErrPlatformAccountInactive, command.AccountID, capacity.Status)
	}
	if capacity.Revision != command.ExpectedAccountRevision {
		return nil, fmt.Errorf("%w: expected %d, current %d", ErrPlatformRevisionConflict, command.ExpectedAccountRevision, capacity.Revision)
	}

	linked, created, err := linkBotToCustomerAccountTx(ctx, tx, command.AccountID, bot, apiKeyID)
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, ErrPlatformAgentAlreadyLinked
	}
	result := &PlatformAgentLinkResult{
		AccountID: command.AccountID,
		AgentID:   command.AgentID,
		Status:    agentStatus,
		LinkedAt:  linked.LinkedAt,
	}
	if err := tx.QueryRow(ctx, `
		SELECT revision, occurred_at
		FROM platform_agent_link_events
		WHERE account_id = $1 AND agent_id = $2
		ORDER BY event_id DESC
		LIMIT 1`, command.AccountID, command.AgentID).Scan(&result.Revision, &result.UpdatedAt); err != nil {
		return nil, fmt.Errorf("LinkPlatformAgent load result revision: %w", err)
	}
	responseJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("LinkPlatformAgent marshal response: %w", err)
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
		return nil, fmt.Errorf("LinkPlatformAgent store idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("LinkPlatformAgent commit: %w", err)
	}
	return result, nil
}

func loadPlatformAgentControlProofTx(
	ctx context.Context,
	tx pgx.Tx,
	controlProof string,
) (AccountBot, string, string, error) {
	if len(controlProof) < 12 {
		return AccountBot{}, "", "", ErrPlatformControlProofRejected
	}
	var bot AccountBot
	var apiKeyID, storedCredential, agentStatus string
	if err := tx.QueryRow(ctx, `
		SELECT b.id, b.name, b.avatar_color, b.default_weapon, b.api_key_id,
		       k.key_prefix, k.is_active, NOW(), k.key_hash, agents.status
		FROM bots b
		JOIN api_keys k ON k.id = b.api_key_id
		JOIN platform_agents agents ON agents.agent_id = b.id
		WHERE k.key_prefix = $1
		FOR UPDATE OF b, k, agents`, controlProof[:12]).Scan(
		&bot.BotID, &bot.Name, &bot.AvatarColor, &bot.DefaultWeapon, &apiKeyID,
		&bot.KeyPrefix, &bot.KeyIsActive, &bot.LinkedAt, &storedCredential, &agentStatus,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AccountBot{}, "", "", ErrPlatformControlProofRejected
		}
		return AccountBot{}, "", "", fmt.Errorf("load platform agent control proof: %w", err)
	}
	if !bot.KeyIsActive {
		return AccountBot{}, "", "", ErrPlatformControlProofRejected
	}
	replacementCredential, err := credential.Verify(storedCredential, controlProof)
	if err != nil {
		return AccountBot{}, "", "", ErrPlatformControlProofRejected
	}
	if replacementCredential != "" {
		storedCredential = replacementCredential
	}
	if _, err := tx.Exec(ctx, `
		UPDATE api_keys SET key_hash = $2, last_seen = NOW()
		WHERE id = $1 AND is_active = true`, apiKeyID, storedCredential); err != nil {
		return AccountBot{}, "", "", fmt.Errorf("update platform agent control proof: %w", err)
	}
	return bot, apiKeyID, agentStatus, nil
}
