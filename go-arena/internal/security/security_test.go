package security

import (
	"strings"
	"testing"

	"arena-server/internal/config"

	"golang.org/x/crypto/bcrypt"
)

// --- API Key Generation ---

func TestGenerateAPIKeyFormat(t *testing.T) {
	config.Load()
	fullKey, keyHash, keyPrefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey error: %v", err)
	}
	if fullKey == "" {
		t.Error("fullKey should not be empty")
	}
	if keyHash == "" {
		t.Error("keyHash should not be empty")
	}
	if keyPrefix == "" {
		t.Error("keyPrefix should not be empty")
	}
	if !strings.HasPrefix(fullKey, config.C.APIKeyPrefix) {
		t.Errorf("fullKey should start with prefix %q, got %q", config.C.APIKeyPrefix, fullKey)
	}
	if !strings.HasPrefix(fullKey, keyPrefix) {
		t.Errorf("fullKey should start with keyPrefix %q", keyPrefix)
	}
}

func TestGenerateAPIKeyLength(t *testing.T) {
	config.Load()
	fullKey, _, keyPrefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(keyPrefix) < 4 {
		t.Errorf("keyPrefix too short: %q", keyPrefix)
	}
	if len(fullKey) < 16 {
		t.Errorf("fullKey too short: %q", fullKey)
	}
}

func TestGenerateAPIKeyUniqueness(t *testing.T) {
	config.Load()
	key1, _, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	key2, _, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if key1 == key2 {
		t.Error("two generated keys should not be identical")
	}
}

func TestGenerateAPIKeyBcryptHash(t *testing.T) {
	config.Load()
	fullKey, keyHash, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	// The hash should verify against the full key
	if err := bcrypt.CompareHashAndPassword([]byte(keyHash), []byte(fullKey)); err != nil {
		t.Errorf("hash does not verify against full key: %v", err)
	}
}

func TestGenerateAPIKeyHashDoesNotMatchOther(t *testing.T) {
	config.Load()
	_, keyHash, _, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	otherKey, _, _, _ := GenerateAPIKey()
	// Other key should not match this hash
	if err := bcrypt.CompareHashAndPassword([]byte(keyHash), []byte(otherKey)); err == nil {
		t.Error("hash should not verify against a different key")
	}
}

// --- Input Validator ---

func TestSanitizeBotNameClean(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"AlphaBot", "AlphaBot"},
		{"Bot 123", "Bot 123"},
		{"my-bot_v2", "my-bot_v2"},
		{"", "Unnamed Bot"},
		{"   ", "Unnamed Bot"},
	}
	for _, tc := range tests {
		got := SanitizeBotName(tc.input)
		if got != tc.want {
			t.Errorf("SanitizeBotName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSanitizeBotNameHTMLStripped(t *testing.T) {
	got := SanitizeBotName("<script>alert('xss')</script>Bot")
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Errorf("HTML tags not stripped: %q", got)
	}
}

func TestSanitizeBotNameSpecialCharsStripped(t *testing.T) {
	got := SanitizeBotName("Bot!@#$%^&*()")
	// Should only contain a-z A-Z 0-9 space _ -
	for _, ch := range got {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == ' ' || ch == '_' || ch == '-') {
			t.Errorf("disallowed char %q in sanitized name %q", ch, got)
		}
	}
}

func TestSanitizeBotNameMaxLength(t *testing.T) {
	long := strings.Repeat("a", 50)
	got := SanitizeBotName(long)
	if len(got) > 20 {
		t.Errorf("name too long: %d chars", len(got))
	}
}

func TestSanitizeBotNameUnicode(t *testing.T) {
	got := SanitizeBotName("Bøt Ñame 🤖")
	// Should be sanitized, not panic
	if got == "" {
		t.Log("unicode fully stripped, got 'Unnamed Bot' fallback")
	}
}

func TestValidateColorValid(t *testing.T) {
	valid := []string{"#000000", "#ffffff", "#FF0000", "#1a2B3c", "#abcdef"}
	for _, c := range valid {
		if !ValidateColor(c) {
			t.Errorf("expected %q to be valid", c)
		}
	}
}

func TestValidateColorInvalid(t *testing.T) {
	invalid := []string{
		"",
		"#fff",
		"#GGGGGG",
		"000000",
		"#0000000",
		"#00000g",
		"red",
		"rgb(0,0,0)",
	}
	for _, c := range invalid {
		if ValidateColor(c) {
			t.Errorf("expected %q to be invalid", c)
		}
	}
}

func TestValidateStatsValid(t *testing.T) {
	config.Load()
	budget := config.C.StatBudget
	perStat := budget / 4
	// If budget is not divisible by 4, we need to be careful
	rem := budget - perStat*3
	stats := map[string]int{
		"hp":      perStat,
		"speed":   perStat,
		"attack":  perStat,
		"defense": rem,
	}
	// Adjust to pass stat range check
	for k, v := range stats {
		if v < config.C.StatMin {
			stats[k] = config.C.StatMin
		}
		if v > config.C.StatMax {
			stats[k] = config.C.StatMax
		}
	}
	// Calculate the sum, then build a valid map
	validStats := buildValidStats(config.C.StatBudget, config.C.StatMin, config.C.StatMax)
	if validStats == nil {
		t.Skip("cannot construct valid stats with current config")
	}
	if err := ValidateStats(validStats); err != nil {
		t.Errorf("valid stats failed: %v", err)
	}
}

// buildValidStats constructs a valid stats map summing to budget with each
// value in [min, max].
func buildValidStats(budget, min, max int) map[string]int {
	// Try to distribute budget evenly across 4 stats
	base := budget / 4
	if base < min || base > max {
		return nil
	}
	rem := budget - base*4
	return map[string]int{
		"hp":      base,
		"speed":   base,
		"attack":  base + rem,
		"defense": base,
	}
}

func TestValidateStatsMissingKey(t *testing.T) {
	config.Load()
	stats := map[string]int{
		"hp":    5,
		"speed": 5,
		// missing attack, defense
	}
	if err := ValidateStats(stats); err == nil {
		t.Error("expected error for missing keys")
	}
}

func TestValidateStatsExtraKey(t *testing.T) {
	config.Load()
	base := config.C.StatBudget / 4
	stats := map[string]int{
		"hp":      base,
		"speed":   base,
		"attack":  base,
		"defense": base,
		"extra":   0, // extra key
	}
	if err := ValidateStats(stats); err == nil {
		t.Error("expected error for extra keys")
	}
}

func TestValidateStatsBudgetMismatch(t *testing.T) {
	config.Load()
	// All zeros — won't equal budget
	stats := map[string]int{
		"hp":      config.C.StatMin,
		"speed":   config.C.StatMin,
		"attack":  config.C.StatMin,
		"defense": config.C.StatMin,
	}
	// Only fail if sum != budget
	sum := stats["hp"] + stats["speed"] + stats["attack"] + stats["defense"]
	if sum == config.C.StatBudget {
		t.Skip("min*4 happens to equal budget")
	}
	if err := ValidateStats(stats); err == nil {
		t.Error("expected error for budget mismatch")
	}
}

func TestValidateStatsBelowMin(t *testing.T) {
	config.Load()
	base := config.C.StatBudget / 4
	stats := map[string]int{
		"hp":      config.C.StatMin - 1, // below minimum
		"speed":   base,
		"attack":  base,
		"defense": base,
	}
	if err := ValidateStats(stats); err == nil {
		t.Error("expected error for value below minimum")
	}
}

func TestValidateStatsAboveMax(t *testing.T) {
	config.Load()
	stats := map[string]int{
		"hp":      config.C.StatMax + 1, // above maximum
		"speed":   1,
		"attack":  1,
		"defense": 1,
	}
	if err := ValidateStats(stats); err == nil {
		t.Error("expected error for value above maximum")
	}
}

func TestValidateFallbackBehavior(t *testing.T) {
	valid := []string{"aggressive", "defensive", "opportunistic", "territorial", "hunter"}
	for _, b := range valid {
		if !ValidateFallbackBehavior(b) {
			t.Errorf("expected %q to be valid behavior", b)
		}
	}
	if ValidateFallbackBehavior("unknown") {
		t.Error("unknown behavior should not be valid")
	}
	if ValidateFallbackBehavior("") {
		t.Error("empty behavior should not be valid")
	}
}

func TestValidateWeapon(t *testing.T) {
	valid := []string{"sword", "bow", "daggers", "shield", "spear", "staff", "grapple"}
	for _, w := range valid {
		if !ValidateWeapon(w) {
			t.Errorf("expected %q to be valid weapon", w)
		}
	}
	if ValidateWeapon("axe") {
		t.Error("axe should not be valid")
	}
	if ValidateWeapon("") {
		t.Error("empty weapon should not be valid")
	}
	if ValidateWeapon("Sword") {
		t.Error("capitalized weapon should not be valid (case-sensitive)")
	}
}
