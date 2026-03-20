package security

import (
	"fmt"
	"regexp"
	"strings"

	"arena-server/internal/config"
)

var (
	htmlTagRe    = regexp.MustCompile(`<[^>]*>`)
	allowedChars = regexp.MustCompile(`[^a-zA-Z0-9 _-]`)
	colorRe      = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// requiredStatKeys is the exact set of keys that must be present in a stats map.
var requiredStatKeys = []string{"hp", "speed", "attack", "defense"}

// validFallbackBehaviors is the set of accepted fallback AI behaviors.
var validFallbackBehaviors = map[string]bool{
	"aggressive":    true,
	"defensive":     true,
	"opportunistic": true,
	"territorial":   true,
	"hunter":        true,
}

// validWeapons is the set of accepted weapon names.
var validWeapons = map[string]bool{
	"sword":   true,
	"bow":     true,
	"daggers": true,
	"shield":  true,
	"spear":   true,
	"staff":   true,
	"grapple": true,
}

// SanitizeBotName cleans a bot name by stripping HTML tags, removing
// disallowed characters, trimming whitespace, and enforcing a max length.
// Returns "Unnamed Bot" if the result is empty.
func SanitizeBotName(name string) string {
	// Strip HTML tags.
	name = htmlTagRe.ReplaceAllString(name, "")

	// Remove characters not in the allowed set.
	name = allowedChars.ReplaceAllString(name, "")

	// Trim leading/trailing whitespace.
	name = strings.TrimSpace(name)

	// Enforce max length of 20 characters.
	if len(name) > 20 {
		name = name[:20]
	}

	if name == "" {
		return "Unnamed Bot"
	}

	return name
}

// ValidateColor checks whether a color string is a valid 6-digit hex color
// (e.g. "#ff00aa").
func ValidateColor(color string) bool {
	return colorRe.MatchString(color)
}

// ValidateStats checks that a stats map contains exactly the required keys
// (hp, speed, attack, defense), that each value is within the configured
// min/max range, and that the total equals the configured stat budget.
func ValidateStats(stats map[string]int) error {
	if len(stats) != len(requiredStatKeys) {
		return fmt.Errorf("stats must have exactly %d keys: %v", len(requiredStatKeys), requiredStatKeys)
	}

	sum := 0
	for _, key := range requiredStatKeys {
		val, ok := stats[key]
		if !ok {
			return fmt.Errorf("missing required stat: %q", key)
		}
		if val < config.C.StatMin {
			return fmt.Errorf("stat %q value %d is below minimum %d", key, val, config.C.StatMin)
		}
		if val > config.C.StatMax {
			return fmt.Errorf("stat %q value %d exceeds maximum %d", key, val, config.C.StatMax)
		}
		sum += val
	}

	if sum != config.C.StatBudget {
		return fmt.Errorf("stat total %d does not equal required budget %d", sum, config.C.StatBudget)
	}

	return nil
}

// ValidateFallbackBehavior checks whether the given behavior string is one of
// the accepted fallback AI behaviors.
func ValidateFallbackBehavior(behavior string) bool {
	return validFallbackBehaviors[behavior]
}

// ValidateWeapon checks whether the given weapon string is one of the accepted
// weapon types.
func ValidateWeapon(weapon string) bool {
	return validWeapons[weapon]
}
