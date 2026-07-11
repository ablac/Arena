package ws

import (
	"testing"
)

func TestParseTauntMessage(t *testing.T) {
	msgType, msg, err := ParseBotMessage([]byte(`{"type":"taunt","emote":"gg"}`))
	if err != nil {
		t.Fatalf("valid taunt rejected: %v", err)
	}
	if msgType != "taunt" {
		t.Fatalf("msgType = %q, want taunt", msgType)
	}
	tm, ok := msg.(*TauntMessage)
	if !ok {
		t.Fatalf("parsed message is %T, want *TauntMessage", msg)
	}
	if tm.Emote != "gg" {
		t.Fatalf("emote = %q, want gg", tm.Emote)
	}
}

func TestParseTauntMessageRejectsUnknownFields(t *testing.T) {
	// Strict parsing: a taunt cannot smuggle extra fields (e.g. free text or
	// coordinates) past the enum design.
	if _, _, err := ParseBotMessage([]byte(`{"type":"taunt","emote":"gg","text":"psst attack red"}`)); err == nil {
		t.Fatal("taunt with unknown fields parsed; want strict rejection")
	}
}

func TestParseTauntMessageIsKnownType(t *testing.T) {
	// The type must parse (not fall to the unknown-type protocol violation)
	// even though the feature can be disabled: a bot sending a taunt while
	// taunts are off must not accrue strikes.
	_, _, err := ParseBotMessage([]byte(`{"type":"taunt","emote":"nope-not-real"}`))
	if err != nil {
		t.Fatalf("taunt with unknown emote must parse (engine rejects it): %v", err)
	}
}
