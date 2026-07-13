package security

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"

	"golang.org/x/crypto/bcrypt"
)

// TestVerifyAPIKey_NilPool_ReturnsErrorNotPanic is the end-to-end regression
// for the 2026-05-29 bot-join outage. VerifyAPIKey is called from the
// /ws/bot handler before the WebSocket upgrade; when db.Pool was nil it
// panicked inside GetAPIKeyByPrefix. It must instead return an error so the
// handler can respond with a clean auth failure.
func TestVerifyAPIKey_NilPool_ReturnsErrorNotPanic(t *testing.T) {
	orig := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = orig })

	if _, err := VerifyAPIKey(context.Background(), "arena_abcd1234efgh"); err == nil {
		t.Fatal("expected an error when db.Pool is nil, got nil")
	}
}

func TestGenerateAPIKeyUsesRollbackSafeCompositeCredential(t *testing.T) {
	previousPrefix := config.C.APIKeyPrefix
	previousRounds := config.C.BcryptRounds
	config.C.APIKeyPrefix = "arena_"
	config.C.BcryptRounds = bcrypt.MinCost
	t.Cleanup(func() {
		config.C.APIKeyPrefix = previousPrefix
		config.C.BcryptRounds = previousRounds
	})

	fullKey, storedHash, keyPrefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if len(fullKey) != len(config.C.APIKeyPrefix)+32 {
		t.Fatalf("full key length = %d, want %d", len(fullKey), len(config.C.APIKeyPrefix)+32)
	}
	if keyPrefix != fullKey[:12] {
		t.Fatalf("key prefix = %q, want %q", keyPrefix, fullKey[:12])
	}
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(fullKey)); err != nil {
		t.Fatalf("origin/main bcrypt reader rejected generated credential: %v", err)
	}
	if len(storedHash) <= bcryptHashLength || !strings.HasPrefix(storedHash[bcryptHashLength:], apiKeyDigestPrefix) {
		t.Fatalf("stored hash = %q, want bcrypt hash followed by %q digest", storedHash, apiKeyDigestPrefix)
	}
	if strings.Contains(storedHash, fullKey) {
		t.Fatal("stored hash contains the plaintext API key")
	}
	replacement, err := verifyAPIKeyCredential(storedHash, fullKey)
	if err != nil {
		t.Fatalf("verify generated credential: %v", err)
	}
	if replacement != "" {
		t.Fatalf("current digest requested replacement %q", replacement)
	}
}

func TestVerifyAPIKeyMigratesLegacyBcryptAfterSuccessfulVerification(t *testing.T) {
	fullKey := "arena_0123456789abcdefghijklmnopqrstuv"
	legacyHash, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate legacy bcrypt fixture: %v", err)
	}
	recentLastSeen := time.Now()
	key := &db.ApiKey{
		ID: "key-1", KeyHash: string(legacyHash), KeyPrefix: fullKey[:12], LastSeen: &recentLastSeen,
	}
	bot := &db.Bot{ID: "bot-1", APIKeyID: key.ID}

	lookupCalls := 0
	recordCalls := 0
	recordedHash := ""
	got, err := verifyAPIKey(
		context.Background(), fullKey,
		func(_ context.Context, prefix string) (*db.ApiKey, *db.Bot, error) {
			lookupCalls++
			if prefix != key.KeyPrefix {
				t.Fatalf("lookup prefix = %q, want %q", prefix, key.KeyPrefix)
			}
			return key, bot, nil
		},
		func(_ context.Context, keyID, replacementHash string) error {
			recordCalls++
			if keyID != key.ID {
				t.Fatalf("record key ID = %q, want %q", keyID, key.ID)
			}
			recordedHash = replacementHash
			return nil
		},
	)
	if err != nil {
		t.Fatalf("verify legacy API key: %v", err)
	}
	if got != bot {
		t.Fatalf("bot = %#v, want %#v", got, bot)
	}
	if lookupCalls != 1 || recordCalls != 1 {
		t.Fatalf("calls = lookup %d, record %d; want one each", lookupCalls, recordCalls)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(recordedHash), []byte(fullKey)); err != nil {
		t.Fatalf("origin/main bcrypt reader rejected migrated credential: %v", err)
	}
	if len(recordedHash) <= bcryptHashLength || !strings.HasPrefix(recordedHash[bcryptHashLength:], apiKeyDigestPrefix) {
		t.Fatalf("replacement hash = %q, want bcrypt hash followed by versioned digest", recordedHash)
	}
	if _, err := verifyAPIKeyCredential(recordedHash, fullKey); err != nil {
		t.Fatalf("verify migrated credential: %v", err)
	}
}

func TestVerifyAPIKeyThrottlesCurrentCredentialLastSeenWrites(t *testing.T) {
	fullKey := "arena_0123456789abcdefghijklmnopqrstuv"
	legacyHash, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate bcrypt fixture: %v", err)
	}
	composite := string(legacyHash) + digestAPIKey(fullKey)
	now := time.Now()
	recent := now.Add(-30 * time.Second)
	stale := now.Add(-2 * time.Minute)

	tests := []struct {
		name       string
		storedHash string
		lastSeen   *time.Time
		wantWrites int
	}{
		{name: "recent composite skips write", storedHash: composite, lastSeen: &recent, wantWrites: 0},
		{name: "stale raw digest records", storedHash: digestAPIKey(fullKey), lastSeen: &stale, wantWrites: 1},
		{name: "nil composite records", storedHash: composite, lastSeen: nil, wantWrites: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key := &db.ApiKey{
				ID: "key-1", KeyHash: test.storedHash, KeyPrefix: fullKey[:12], LastSeen: test.lastSeen,
			}
			bot := &db.Bot{ID: "bot-1", APIKeyID: key.ID}
			writes := 0
			got, err := verifyAPIKey(
				context.Background(), fullKey,
				func(context.Context, string) (*db.ApiKey, *db.Bot, error) { return key, bot, nil },
				func(_ context.Context, _ string, replacement string) error {
					writes++
					if replacement != "" {
						t.Fatalf("current credential requested replacement %q", replacement)
					}
					return nil
				},
			)
			if err != nil {
				t.Fatalf("verify current API key: %v", err)
			}
			if got != bot {
				t.Fatalf("bot = %#v, want %#v", got, bot)
			}
			if writes != test.wantWrites {
				t.Fatalf("last_seen writes = %d, want %d", writes, test.wantWrites)
			}
		})
	}
}

func TestShouldRecordAPIKeyAuthentication(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-59 * time.Second)
	boundary := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	tests := []struct {
		name            string
		lastSeen        *time.Time
		replacementHash string
		want            bool
	}{
		{name: "recent current credential", lastSeen: &recent, want: false},
		{name: "one minute boundary", lastSeen: &boundary, want: true},
		{name: "missing last seen", lastSeen: nil, want: true},
		{name: "clock skew does not force write", lastSeen: &future, want: false},
		{name: "migration overrides recent timestamp", lastSeen: &recent, replacementHash: "composite", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldRecordAPIKeyAuthentication(test.lastSeen, test.replacementHash, now); got != test.want {
				t.Fatalf("shouldRecordAPIKeyAuthentication() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestVerifyAPIKeyCompositeRejectsWrongOrUnknownDigest(t *testing.T) {
	fullKey := "arena_0123456789abcdefghijklmnopqrstuv"
	legacyHash, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate bcrypt fixture: %v", err)
	}

	tests := []struct {
		name   string
		suffix string
	}{
		{name: "wrong digest", suffix: digestAPIKey(fullKey + "-different")},
		{name: "unknown digest version", suffix: "sha256:v2:" + strings.Repeat("0", 64)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			storedHash := string(legacyHash) + test.suffix
			if _, err := verifyAPIKeyCredential(storedHash, fullKey); err == nil {
				t.Fatalf("verifyAPIKeyCredential(%s) succeeded, want fail-closed error", test.name)
			}
		})
	}
}

func TestVerifyAPIKeyRawDigestRemainsAcceptedWithoutHashReplacement(t *testing.T) {
	fullKey := "arena_0123456789abcdefghijklmnopqrstuv"
	key := &db.ApiKey{ID: "key-1", KeyHash: digestAPIKey(fullKey), KeyPrefix: fullKey[:12]}
	bot := &db.Bot{ID: "bot-1", APIKeyID: key.ID}

	recordCalls := 0
	replacementHash := "not-recorded"
	got, err := verifyAPIKey(
		context.Background(), fullKey,
		func(context.Context, string) (*db.ApiKey, *db.Bot, error) { return key, bot, nil },
		func(_ context.Context, _ string, replacement string) error {
			recordCalls++
			replacementHash = replacement
			return nil
		},
	)
	if err != nil {
		t.Fatalf("verify digest API key: %v", err)
	}
	if got != bot {
		t.Fatalf("bot = %#v, want %#v", got, bot)
	}
	if recordCalls != 1 || replacementHash != "" {
		t.Fatalf("record calls = %d, replacement = %q; want one last_seen-only record", recordCalls, replacementHash)
	}
}

func TestVerifyAPIKeyRejectsWrongOrUnknownVersionedDigestWithoutRecording(t *testing.T) {
	fullKey := "arena_0123456789abcdefghijklmnopqrstuv"
	tests := []struct {
		name       string
		storedHash string
	}{
		{name: "wrong key", storedHash: digestAPIKey(fullKey)},
		{name: "unknown digest version", storedHash: "sha256:v2:" + strings.Repeat("0", 64)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			presented := fullKey
			if test.name == "wrong key" {
				presented = fullKey[:len(fullKey)-1] + "w"
			}
			key := &db.ApiKey{ID: "key-1", KeyHash: test.storedHash, KeyPrefix: presented[:12]}
			recordCalls := 0
			_, err := verifyAPIKey(
				context.Background(), presented,
				func(context.Context, string) (*db.ApiKey, *db.Bot, error) {
					return key, &db.Bot{ID: "bot-1", APIKeyID: key.ID}, nil
				},
				func(context.Context, string, string) error {
					recordCalls++
					return nil
				},
			)
			if err == nil || !strings.Contains(err.Error(), "invalid API key") {
				t.Fatalf("error = %v, want invalid API key", err)
			}
			if recordCalls != 0 {
				t.Fatalf("record calls = %d, want 0", recordCalls)
			}
		})
	}
}

func TestVerifyAPIKeyRejectsWrongLegacyBcryptWithoutMigration(t *testing.T) {
	fullKey := "arena_0123456789abcdefghijklmnopqrstuv"
	legacyHash, err := bcrypt.GenerateFromPassword([]byte(fullKey), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate legacy bcrypt fixture: %v", err)
	}
	key := &db.ApiKey{ID: "key-1", KeyHash: string(legacyHash), KeyPrefix: fullKey[:12]}
	recordCalls := 0

	_, err = verifyAPIKey(
		context.Background(), fullKey[:len(fullKey)-1]+"w",
		func(context.Context, string) (*db.ApiKey, *db.Bot, error) {
			return key, &db.Bot{ID: "bot-1", APIKeyID: key.ID}, nil
		},
		func(context.Context, string, string) error {
			recordCalls++
			return nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "invalid API key") {
		t.Fatalf("error = %v, want invalid API key", err)
	}
	if recordCalls != 0 {
		t.Fatalf("record calls = %d, want 0", recordCalls)
	}
}

func TestVerifyAPIKeyPreservesSuccessfulAuthWhenLastSeenWriteFails(t *testing.T) {
	fullKey := "arena_0123456789abcdefghijklmnopqrstuv"
	key := &db.ApiKey{ID: "key-1", KeyHash: digestAPIKey(fullKey), KeyPrefix: fullKey[:12]}
	bot := &db.Bot{ID: "bot-1", APIKeyID: key.ID}

	got, err := verifyAPIKey(
		context.Background(), fullKey,
		func(context.Context, string) (*db.ApiKey, *db.Bot, error) { return key, bot, nil },
		func(context.Context, string, string) error { return errors.New("database unavailable") },
	)
	if err != nil {
		t.Fatalf("verification failed because last_seen write failed: %v", err)
	}
	if got != bot {
		t.Fatalf("bot = %#v, want %#v", got, bot)
	}
}

var benchmarkAPIKeyCredentialErr error

func BenchmarkAPIKeyCredentialVerification(b *testing.B) {
	fullKey := "arena_0123456789abcdefghijklmnopqrstuv"
	digest := digestAPIKey(fullKey)
	legacyHash, err := bcrypt.GenerateFromPassword([]byte(fullKey), 12)
	if err != nil {
		b.Fatalf("generate legacy bcrypt fixture: %v", err)
	}

	composite := string(legacyHash) + digest

	b.Run("composite_sha256_v1", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, benchmarkAPIKeyCredentialErr = verifyAPIKeyCredential(composite, fullKey)
		}
	})
	b.Run("raw_sha256_v1", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, benchmarkAPIKeyCredentialErr = verifyAPIKeyCredential(digest, fullKey)
		}
	})
	b.Run("legacy_bcrypt_cost_12", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, benchmarkAPIKeyCredentialErr = verifyAPIKeyCredential(string(legacyHash), fullKey)
		}
	})
}
