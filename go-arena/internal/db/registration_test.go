package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeRegistrationTx struct {
	pgx.Tx
	execCalls     int
	failExecCall  int
	failErr       error
	commitErr     error
	committed     bool
	rolledBack    bool
	keyExists     bool
	botExists     bool
	agentExists   bool
	profileExists bool
	changeCount   int
}

func (tx *fakeRegistrationTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	tx.execCalls++
	if tx.execCalls == tx.failExecCall {
		return pgconn.CommandTag{}, tx.failErr
	}
	if tx.execCalls == 1 {
		tx.keyExists = true
	}
	if tx.execCalls == 2 {
		tx.botExists = true
	}
	if tx.execCalls == 3 {
		tx.agentExists = true
	}
	if tx.execCalls == 4 {
		tx.profileExists = true
	}
	if tx.execCalls == 5 || tx.execCalls == 6 {
		tx.changeCount++
	}
	return pgconn.CommandTag{}, nil
}

func (tx *fakeRegistrationTx) Commit(context.Context) error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
	tx.committed = true
	return nil
}

func (tx *fakeRegistrationTx) Rollback(context.Context) error {
	if tx.committed {
		return pgx.ErrTxClosed
	}
	tx.rolledBack = true
	tx.keyExists = false
	tx.botExists = false
	tx.agentExists = false
	tx.profileExists = false
	tx.changeCount = 0
	return nil
}

func testRegistrationBot() *Bot {
	now := time.Now()
	return &Bot{
		ID:              "bot-1",
		APIKeyID:        "key-1",
		Name:            "Atomic Bot",
		AvatarColor:     "#123456",
		DefaultWeapon:   "sword",
		DefaultStats:    JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		DefaultFallback: "aggressive",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func TestCreateAPIKeyAndBotRollsBackKeyWhenBotInsertFails(t *testing.T) {
	wantErr := errors.New("bot insert rejected")
	tx := &fakeRegistrationTx{failExecCall: 2, failErr: wantErr}

	err := createAPIKeyAndBotWithBegin(
		context.Background(), "key-1", "hash", "prefix", "127.0.0.1", testRegistrationBot(),
		func(context.Context) (pgx.Tx, error) { return tx, nil },
	)

	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
	if !tx.rolledBack {
		t.Fatal("transaction was not rolled back")
	}
	if tx.keyExists || tx.botExists || tx.agentExists || tx.profileExists || tx.changeCount != 0 {
		t.Fatalf("rows survived rollback: key=%v bot=%v agent=%v profile=%v changes=%d",
			tx.keyExists, tx.botExists, tx.agentExists, tx.profileExists, tx.changeCount)
	}
	if tx.committed {
		t.Fatal("failed registration was committed")
	}
}

func TestCreateAPIKeyAndBotCommitsBothRows(t *testing.T) {
	tx := &fakeRegistrationTx{}

	err := createAPIKeyAndBotWithBegin(
		context.Background(), "key-1", "hash", "prefix", "127.0.0.1", testRegistrationBot(),
		func(context.Context) (pgx.Tx, error) { return tx, nil },
	)

	if err != nil {
		t.Fatalf("createAPIKeyAndBotWithBegin returned error: %v", err)
	}
	if !tx.committed || !tx.keyExists || !tx.botExists || !tx.agentExists || !tx.profileExists || tx.changeCount != 2 {
		t.Fatalf("commit state: committed=%v key=%v bot=%v agent=%v profile=%v changes=%d",
			tx.committed, tx.keyExists, tx.botExists, tx.agentExists, tx.profileExists, tx.changeCount)
	}
	if tx.execCalls != 6 {
		t.Fatalf("Exec calls = %d, want 6", tx.execCalls)
	}
}

func TestCreateAPIKeyAndBotWithoutPoolReturnsErrNoDatabase(t *testing.T) {
	original := Pool
	Pool = nil
	t.Cleanup(func() { Pool = original })

	err := CreateAPIKeyAndBot(context.Background(), "key-1", "hash", "prefix", "127.0.0.1", testRegistrationBot())
	if !errors.Is(err, ErrNoDatabase) {
		t.Fatalf("error = %v, want %v", err, ErrNoDatabase)
	}
}

type fixedRateLimitRow struct {
	count int
	err   error
}

func (row fixedRateLimitRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*(dest[0].(*int)) = row.count
	return nil
}

type recordingRateLimitQuerier struct {
	row   pgx.Row
	query string
	args  []any
	calls int
}

func (q *recordingRateLimitQuerier) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	q.calls++
	q.query = query
	q.args = args
	return q.row
}

func TestConsumeRegistrationRateLimitMapsAtomicStatementResult(t *testing.T) {
	now := time.Now()

	allowedQuery := &recordingRateLimitQuerier{row: fixedRateLimitRow{count: 3}}
	allowed, remaining, err := consumeRegistrationRateLimit(context.Background(), allowedQuery, "203.0.113.5", 5, now)
	if err != nil || !allowed || remaining != 2 {
		t.Fatalf("allowed result = (%v, %d, %v), want (true, 2, nil)", allowed, remaining, err)
	}
	if allowedQuery.calls != 1 || len(allowedQuery.args) != 3 {
		t.Fatalf("query calls=%d args=%#v", allowedQuery.calls, allowedQuery.args)
	}

	blockedQuery := &recordingRateLimitQuerier{row: fixedRateLimitRow{err: pgx.ErrNoRows}}
	allowed, remaining, err = consumeRegistrationRateLimit(context.Background(), blockedQuery, "203.0.113.5", 5, now)
	if err != nil || allowed || remaining != 0 {
		t.Fatalf("blocked result = (%v, %d, %v), want (false, 0, nil)", allowed, remaining, err)
	}

	wantErr := errors.New("database unavailable")
	errorQuery := &recordingRateLimitQuerier{row: fixedRateLimitRow{err: wantErr}}
	_, _, err = consumeRegistrationRateLimit(context.Background(), errorQuery, "203.0.113.5", 5, now)
	if !errors.Is(err, wantErr) {
		t.Fatalf("database error = %v, want wrapped %v", err, wantErr)
	}
}

func TestRegistrationRateLimitSQLSerializesAndGuardsCapacity(t *testing.T) {
	for _, clause := range []string{
		"ON CONFLICT (ip_address) DO UPDATE",
		"rate_limits.keys_generated + 1",
		"rate_limits.keys_generated < $3",
		"RETURNING keys_generated",
	} {
		if !strings.Contains(consumeRegistrationRateLimitSQL, clause) {
			t.Errorf("atomic registration limit SQL missing %q", clause)
		}
	}
}

func TestAPIKeyLookupExcludesRevokedKeys(t *testing.T) {
	if !strings.Contains(getActiveAPIKeyByPrefixSQL, "is_active = true") {
		t.Fatalf("API key lookup can authenticate revoked keys: %q", getActiveAPIKeyByPrefixSQL)
	}
}
