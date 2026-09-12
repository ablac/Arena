package db

import (
	"arena-server/internal/config"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPostgresGamingSnapshotRetryAndAtomicRollback(t *testing.T) {
	ctx := useFreshPostgresSchema(t)
	previous := config.C
	t.Cleanup(func() { config.C = previous })
	config.C.GamingOrigin = "https://angel-gaming.com"
	config.C.GamingServiceKey = strings.Repeat("a", 64)
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCoreSchema(ctx); err != nil {
		t.Fatal(err)
	}
	alias := "public_handle"
	account, err := UpsertVerifiedCustomerAccount(ctx, "", "https://accounts.angel-serv.com", "subject-one", "Private Person", &alias)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"owned-bot", "guest-bot"} {
		_, err = Pool.Exec(ctx, `INSERT INTO api_keys(id,key_hash,key_prefix) VALUES($1,'unused',$1);`, id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Pool.Exec(ctx, `INSERT INTO bots(id,api_key_id,name) VALUES($1,$1,$1)`, id)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = Pool.Exec(ctx, `INSERT INTO account_bot_links(account_id,bot_id) VALUES($1,'owned-bot')`, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = CreateRound(ctx, &Round{ID: "round-one", RoundNumber: 1, StartedAt: time.Now(), Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err = InsertRoundBotStatsBatch(ctx, "round-one", 1, []RoundBotStatsRow{{BotID: "owned-bot", Kills: 3, Deaths: 1, Won: true}, {BotID: "guest-bot", Kills: 2}}); err != nil {
		t.Fatal(err)
	}
	// A commit may succeed even when its acknowledgment is lost. Replaying the
	// same production batch must not count the bots or recreate events twice.
	if err = InsertRoundBotStatsBatch(ctx, "round-one", 1, []RoundBotStatsRow{{BotID: "owned-bot", Kills: 3, Deaths: 1, Won: true}, {BotID: "guest-bot", Kills: 2}}); err != nil {
		t.Fatal(err)
	}
	var roundRows int
	if err = Pool.QueryRow(ctx, `SELECT COUNT(*) FROM round_bot_stats WHERE round_id='round-one'`).Scan(&roundRows); err != nil || roundRows != 2 {
		t.Fatalf("ambiguous commit duplicated stats: %d %v", roundRows, err)
	}
	profile, err := GetGamingProfile(ctx, "https://accounts.angel-serv.com", "subject-one")
	if err != nil || len(profile.Bots) != 1 || profile.PublicUsername == nil || *profile.PublicUsername != alias {
		t.Fatalf("profile: %+v %v", profile, err)
	}
	if _, err = Pool.Exec(ctx, `UPDATE customer_accounts SET show_bots_public=false WHERE id=$1`, account.ID); err != nil {
		t.Fatal(err)
	}
	privateProfile, err := GetGamingProfile(ctx, "https://accounts.angel-serv.com", "subject-one")
	if err != nil || len(privateProfile.Bots) != 1 || privateProfile.Bots[0].Public {
		t.Fatalf("private owner projection: %+v %v", privateProfile, err)
	}
	encoded, _ := json.Marshal(profile)
	if strings.Contains(string(encoded), "Private Person") || strings.Contains(string(encoded), "subject-one") || strings.Contains(string(encoded), "key") {
		t.Fatalf("unsafe projection: %s", encoded)
	}
	pending, err := ClaimGamingEvents(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("outbox: %+v %v", pending, err)
	}
	if pending[0].ID != "round-one:owned-bot" {
		t.Fatalf("event id: %s", pending[0].ID)
	}
	var event map[string]interface{}
	if err = json.Unmarshal(pending[0].Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event["subject"] != "subject-one" || event["type"] != "round.completed" {
		t.Fatalf("event: %+v", event)
	}
	if _, err = time.Parse("2006-01-02T15:04:05.000Z", event["occurredAt"].(string)); err != nil {
		t.Fatal(err)
	}
	_, err = Pool.Exec(ctx, `DELETE FROM account_bot_links WHERE bot_id='owned-bot'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = CompleteGamingEvent(ctx, pending[0].ID, 503, false); err != nil {
		t.Fatal(err)
	}
	// Expire the durable retry lease as if time passed across a process restart.
	if _, err = Pool.Exec(ctx, `UPDATE gaming_event_outbox SET next_attempt_at=NOW()-INTERVAL '1 second'`); err != nil {
		t.Fatal(err)
	}
	retry, err := ClaimGamingEvents(ctx)
	if err != nil || len(retry) != 1 || string(retry[0].Payload) != string(pending[0].Payload) {
		t.Fatalf("retry lost ownership: %+v %v", retry, err)
	}
	if err = CompleteGamingEvent(ctx, retry[0].ID, 200, true); err != nil {
		t.Fatal(err)
	}
	if done, err := ClaimGamingEvents(ctx); err != nil || len(done) != 0 {
		t.Fatalf("ack not durable: %+v %v", done, err)
	}
	_, err = Pool.Exec(ctx, `INSERT INTO account_bot_links(account_id,bot_id) VALUES($1,'owned-bot')`, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = CreateRound(ctx, &Round{ID: "round-two", RoundNumber: 2, StartedAt: time.Now(), Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	_, err = Pool.Exec(ctx, `CREATE FUNCTION fail_gaming_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'outbox storage failed'; END $$;
 CREATE TRIGGER fail_gaming_event BEFORE INSERT ON gaming_event_outbox FOR EACH ROW EXECUTE FUNCTION fail_gaming_event()`)
	if err != nil {
		t.Fatal(err)
	}
	if err = InsertRoundBotStatsBatch(ctx, "round-two", 2, []RoundBotStatsRow{{BotID: "owned-bot", Kills: 2}}); err == nil {
		t.Fatal("outbox failure accepted")
	}
	var count int
	if err = Pool.QueryRow(ctx, `SELECT COUNT(*) FROM round_bot_stats WHERE round_id='round-two'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial stats commit: %d %v", count, err)
	}
	if _, err = Pool.Exec(ctx, `DROP TRIGGER fail_gaming_event ON gaming_event_outbox`); err != nil {
		t.Fatal(err)
	}
	config.C.GamingOrigin = ""
	config.C.GamingServiceKey = ""
	if err = InsertRoundBotStatsBatch(ctx, "round-two", 2, []RoundBotStatsRow{{BotID: "owned-bot", Kills: 2}}); err != nil {
		t.Fatal(err)
	}
	if err = Pool.QueryRow(ctx, `SELECT COUNT(*) FROM gaming_event_outbox WHERE event_id='round-two:owned-bot'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("disabled integration emitted event: %d %v", count, err)
	}
}
