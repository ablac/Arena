package db

import (
	"errors"
	"testing"
	"time"
)

func usernamePointer(s string) *string { return &s }

func TestNormalizePublicUsername(t *testing.T) {
	for _, raw := range []string{"", "ab", "spaces here", " email@example.com", "valid ", "ééé", "<script>", "abcdefghijklmnopqrstuvwxy"} {
		if value := NormalizePublicUsername(&raw); value != nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if NormalizePublicUsername(nil) != nil {
		t.Fatal("missing claim must stay missing")
	}
	if got := NormalizePublicUsername(usernamePointer("Reef_Pilot09")); got == nil || *got != "reef_pilot09" {
		t.Fatalf("normalized=%v", got)
	}
}

func TestPostgresPublicUsernameMigrationIdentityAndHistory(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatal(err)
	}
	// A pre-feature row migrates with no alias, retaining its private name.
	if _, err := Pool.Exec(ctx, `INSERT INTO customer_accounts(id,display_name) VALUES('legacy','Private Real Name'); ALTER TABLE customer_accounts DROP COLUMN public_username`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := EnsureCoreSchema(ctx); err != nil {
			t.Fatal(err)
		}
	}
	legacy, err := GetCustomerAccount(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.PublicUsername != nil || legacy.DisplayName != "Private Real Name" {
		t.Fatalf("backfilled legacy account: %+v", legacy)
	}
	account, err := UpsertVerifiedCustomerAccount(ctx, "", "https://accounts.example", "subject", "Private Name", usernamePointer("Reef_Pilot"))
	if err != nil {
		t.Fatal(err)
	}
	if account.PublicUsername == nil || *account.PublicUsername != "reef_pilot" {
		t.Fatalf("alias=%v", account.PublicUsername)
	}
	if err := InsertCustomerSession(ctx, []byte("test-only-hash"), account.ID, "csrf", time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Seed an old persisted label; public queries must never serve it.
	if _, err := Pool.Exec(ctx, `INSERT INTO chat_messages(account_id,handle,body,ip) VALUES($1,'Private Name#legacy','original body','127.0.0.1')`, account.ID); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []*string{usernamePointer("renamed_pilot"), nil, usernamePointer("third_pilot")} {
		updated, err := UpsertVerifiedCustomerAccount(ctx, "", "https://accounts.example", "subject", "Private Name", alias)
		if err != nil {
			t.Fatal(err)
		}
		if updated.ID != account.ID || updated.DisplayName != "Private Name" {
			t.Fatal("identity or private name changed")
		}
		row, err := GetCustomerSessionByTokenHash(ctx, []byte("test-only-hash"))
		if err != nil {
			t.Fatal(err)
		}
		if row == nil || row.CSRFToken != "csrf" || PublicUsernameLabel(row.PublicUsername) != PublicUsernameLabel(alias) {
			t.Fatalf("old durable session=%+v", row)
		}
		profile, err := GetPublicProfile(ctx, account.ID)
		if err != nil {
			t.Fatal(err)
		}
		if PublicUsernameLabel(profile.PublicUsername) != PublicUsernameLabel(alias) {
			t.Fatal("stale profile")
		}
		msgs, err := ListRecentChatMessages(ctx, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) != 1 || msgs[0].Handle != PublicUsernameLabel(alias) || msgs[0].Body != "original body" || *msgs[0].AccountID != account.ID {
			t.Fatalf("history=%+v", msgs)
		}
		if alias == nil {
			err := InsertChatMessage(ctx, &ChatMessage{AccountID: &account.ID, Handle: "forged", Body: "blocked", IP: "127.0.0.1"})
			if !errors.Is(err, ErrPublicUsernameRequired) {
				t.Fatalf("missing-alias insertion=%v", err)
			}
		}
	}
	// Reassignment at Accounts is a label change, never an identity merge. The
	// consumer cache must not add a local unique constraint blocking new owners.
	other, err := UpsertVerifiedCustomerAccount(ctx, "", "https://accounts.example", "other-subject", "Another Private Name", usernamePointer("third_pilot"))
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == account.ID {
		t.Fatal("public alias reassigned account ownership")
	}
	msg := &ChatMessage{AccountID: &account.ID, Handle: "Private Forgery", Body: "new", IP: "127.0.0.1"}
	if err := InsertChatMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if msg.Handle != "third_pilot" {
		t.Fatalf("insert trusts caller handle=%q", msg.Handle)
	}
}
