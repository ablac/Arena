package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Clear any env vars that might be set
	Load()

	if C.TickRate <= 0 {
		t.Errorf("TickRate=%d, want > 0", C.TickRate)
	}
	if C.MaxBots <= 0 {
		t.Errorf("MaxBots=%d, want > 0", C.MaxBots)
	}
	if C.ArenaWidth <= 0 {
		t.Errorf("ArenaWidth=%v, want > 0", C.ArenaWidth)
	}
	if C.ArenaHeight <= 0 {
		t.Errorf("ArenaHeight=%v, want > 0", C.ArenaHeight)
	}
	if C.BotRadius <= 0 {
		t.Errorf("BotRadius=%v, want > 0", C.BotRadius)
	}
}

func TestDefaultServerConfig(t *testing.T) {
	Load()
	if C.ServerHost == "" {
		t.Error("ServerHost should have default")
	}
	if C.ServerPort <= 0 {
		t.Errorf("ServerPort=%d, want > 0", C.ServerPort)
	}
}

func TestDefaultDBConfig(t *testing.T) {
	Load()
	if C.DBHost == "" {
		t.Error("DBHost should have default")
	}
	if C.DBPort <= 0 {
		t.Errorf("DBPort=%d, want > 0", C.DBPort)
	}
	if C.DBName == "" {
		t.Error("DBName should have default")
	}
}

func TestDefaultRedisConfig(t *testing.T) {
	Load()
	if C.RedisHost == "" {
		t.Error("RedisHost should have default")
	}
	if C.RedisPort <= 0 {
		t.Errorf("RedisPort=%d, want > 0", C.RedisPort)
	}
}

func TestDefaultSecurityConfig(t *testing.T) {
	Load()
	if C.APIKeyPrefix == "" {
		t.Error("APIKeyPrefix should have default")
	}
	if C.BcryptRounds <= 0 {
		t.Errorf("BcryptRounds=%d, want > 0", C.BcryptRounds)
	}
	if C.StatBudget <= 0 {
		t.Errorf("StatBudget=%d, want > 0", C.StatBudget)
	}
	if C.StatMin <= 0 {
		t.Errorf("StatMin=%d, want > 0", C.StatMin)
	}
	if C.StatMax <= 0 {
		t.Errorf("StatMax=%d, want > 0", C.StatMax)
	}
	if C.StatMax < C.StatMin {
		t.Errorf("StatMax=%d < StatMin=%d", C.StatMax, C.StatMin)
	}
}

func TestDefaultGameConfig(t *testing.T) {
	Load()
	if C.RoundDuration <= 0 {
		t.Errorf("RoundDuration=%v, want > 0", C.RoundDuration)
	}
	if C.MinBotsToStart <= 0 {
		t.Errorf("MinBotsToStart=%d, want > 0", C.MinBotsToStart)
	}
	if C.KillFeedSize <= 0 {
		t.Errorf("KillFeedSize=%d, want > 0", C.KillFeedSize)
	}
}

func TestDefaultZoneConfig(t *testing.T) {
	Load()
	if C.ZoneInitialRadius <= 0 {
		t.Errorf("ZoneInitialRadius=%v, want > 0", C.ZoneInitialRadius)
	}
	if C.ZoneMinRadius <= 0 {
		t.Errorf("ZoneMinRadius=%v, want > 0", C.ZoneMinRadius)
	}
	if C.ZoneMinRadius >= C.ZoneInitialRadius {
		t.Errorf("ZoneMinRadius=%v >= ZoneInitialRadius=%v", C.ZoneMinRadius, C.ZoneInitialRadius)
	}
	if C.ZoneShrinkPercent <= 0 || C.ZoneShrinkPercent >= 1 {
		t.Errorf("ZoneShrinkPercent=%v, want (0,1)", C.ZoneShrinkPercent)
	}
}

func TestDefaultEloConfig(t *testing.T) {
	Load()
	if C.EloKFactor <= 0 {
		t.Errorf("EloKFactor=%v, want > 0", C.EloKFactor)
	}
	if C.EloStarting <= 0 {
		t.Errorf("EloStarting=%d, want > 0", C.EloStarting)
	}
	if C.EloMin <= 0 {
		t.Errorf("EloMin=%d, want > 0", C.EloMin)
	}
	if C.EloMin >= C.EloStarting {
		t.Errorf("EloMin=%d >= EloStarting=%d", C.EloMin, C.EloStarting)
	}
}

func TestDefaultBountyConfig(t *testing.T) {
	Load()
	if C.BountyKillStreakThreshold <= 0 {
		t.Errorf("BountyKillStreakThreshold=%d, want > 0", C.BountyKillStreakThreshold)
	}
	if C.BountyBonusPoints <= 0 {
		t.Errorf("BountyBonusPoints=%v, want > 0", C.BountyBonusPoints)
	}
}

func TestDefaultMineConfig(t *testing.T) {
	Load()
	if C.MineMaxPerBot <= 0 {
		t.Errorf("MineMaxPerBot=%d, want > 0", C.MineMaxPerBot)
	}
	if C.MineDamage <= 0 {
		t.Errorf("MineDamage=%v, want > 0", C.MineDamage)
	}
	if C.MineArmDelayTicks <= 0 {
		t.Errorf("MineArmDelayTicks=%d, want > 0", C.MineArmDelayTicks)
	}
}

func TestDefaultGravityWellConfig(t *testing.T) {
	Load()
	if C.GravityWellDurationTicks <= 0 {
		t.Errorf("GravityWellDurationTicks=%d, want > 0", C.GravityWellDurationTicks)
	}
	if C.GravityWellPullRadius <= 0 {
		t.Errorf("GravityWellPullRadius=%d, want > 0", C.GravityWellPullRadius)
	}
	if C.GravityWellPullForce <= 0 {
		t.Errorf("GravityWellPullForce=%v, want > 0", C.GravityWellPullForce)
	}
}

func TestDefaultPickupConfig(t *testing.T) {
	Load()
	if C.PickupHealthAmount <= 0 {
		t.Errorf("PickupHealthAmount=%v, want > 0", C.PickupHealthAmount)
	}
	if C.PickupMaxActive <= 0 {
		t.Errorf("PickupMaxActive=%d, want > 0", C.PickupMaxActive)
	}
	if C.PickupSpeedBoostMult <= 1 {
		t.Errorf("PickupSpeedBoostMult=%v, want > 1", C.PickupSpeedBoostMult)
	}
}

func TestEnvOverride(t *testing.T) {
	// Set a custom env var and verify it's picked up
	os.Setenv("ARENA_TICK_RATE", "20")
	defer os.Unsetenv("ARENA_TICK_RATE")

	Load()
	if C.TickRate != 20 {
		t.Errorf("TickRate=%d, want 20 (from env)", C.TickRate)
	}
}

func TestEnvOverrideString(t *testing.T) {
	os.Setenv("ARENA_API_KEY_PREFIX", "test_prefix_")
	defer os.Unsetenv("ARENA_API_KEY_PREFIX")

	Load()
	if C.APIKeyPrefix != "test_prefix_" {
		t.Errorf("APIKeyPrefix=%q, want test_prefix_", C.APIKeyPrefix)
	}
}

func TestEnvOverrideFloat(t *testing.T) {
	os.Setenv("ARENA_ROUND_DURATION", "120")
	defer os.Unsetenv("ARENA_ROUND_DURATION")

	Load()
	if C.RoundDuration != 120 {
		t.Errorf("RoundDuration=%v, want 120", C.RoundDuration)
	}
}

func TestLoadIsIdempotent(t *testing.T) {
	Load()
	rate1 := C.TickRate
	Load()
	if C.TickRate != rate1 {
		t.Errorf("Load not idempotent: %d != %d", C.TickRate, rate1)
	}
}

func TestDefaultWSConfig(t *testing.T) {
	Load()
	if C.WSMessageMaxBytes <= 0 {
		t.Errorf("WSMessageMaxBytes=%d, want > 0", C.WSMessageMaxBytes)
	}
	if C.WSMaxMessagesPerSec <= 0 {
		t.Errorf("WSMaxMessagesPerSec=%d, want > 0", C.WSMaxMessagesPerSec)
	}
}

func TestDefaultOIDCDisabled(t *testing.T) {
	Load()
	if C.OIDCEnabled {
		t.Error("OIDC should be disabled by default")
	}
}

func TestStatConsistency(t *testing.T) {
	Load()
	// Budget should be achievable with the given stat constraints:
	// 4 stats, each >= StatMin and <= StatMax.
	// Budget must be achievable: 4*StatMin <= Budget <= 4*StatMax
	minPossible := 4 * C.StatMin
	maxPossible := 4 * C.StatMax
	if C.StatBudget < minPossible || C.StatBudget > maxPossible {
		t.Errorf("StatBudget=%d is not achievable with min=%d max=%d (range [%d,%d])",
			C.StatBudget, C.StatMin, C.StatMax, minPossible, maxPossible)
	}
}
