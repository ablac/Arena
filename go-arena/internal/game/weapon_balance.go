package game

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
)

type WeaponBalanceState struct {
	Weapon          string
	DamageScale     float64
	CooldownScale   float64
	AdjustmentScale float64
	RoundsTracked   int
	UpdatedAt       time.Time
}

type weaponRoundPerformance struct {
	weapon           string
	bots             int
	wins             int
	score            float64
	totalKills       int
	totalDamage      float64
	totalStreak      int
	totalLifeSecs    float64
	totalShotsFired  int
	totalShotsHit    int
	totalDamageTaken float64
}

func (p *weaponRoundPerformance) avgDamage() float64 {
	if p == nil || p.bots == 0 {
		return 0
	}
	return p.totalDamage / float64(p.bots)
}

func (p *weaponRoundPerformance) avgLifeSecs() float64 {
	if p == nil || p.bots == 0 {
		return 0
	}
	return p.totalLifeSecs / float64(p.bots)
}

func (p *weaponRoundPerformance) hitRate() float64 {
	if p == nil || p.totalShotsFired <= 0 {
		return 0
	}
	return float64(p.totalShotsHit) / float64(p.totalShotsFired)
}

func (p *weaponRoundPerformance) damagePerShot() float64 {
	if p == nil || p.totalShotsFired <= 0 {
		return 0
	}
	return p.totalDamage / float64(p.totalShotsFired)
}

func (p *weaponRoundPerformance) damagePerHit() float64 {
	if p == nil || p.totalShotsHit <= 0 {
		return 0
	}
	return p.totalDamage / float64(p.totalShotsHit)
}

func (p *weaponRoundPerformance) shotsPerLife() float64 {
	if p == nil || p.totalLifeSecs <= 0 {
		return 0
	}
	return float64(p.totalShotsFired) / p.totalLifeSecs
}

func (p *weaponRoundPerformance) killsPerHit() float64 {
	if p == nil || p.totalShotsHit <= 0 {
		return 0
	}
	return float64(p.totalKills) / float64(p.totalShotsHit)
}

func (p *weaponRoundPerformance) damagePerLife() float64 {
	if p == nil || p.totalLifeSecs <= 0 {
		return 0
	}
	return p.totalDamage / p.totalLifeSecs
}

func (p *weaponRoundPerformance) confidence() float64 {
	if p == nil {
		return 0.35
	}
	botFactor := clampFloat(float64(p.bots)/2.0, 0.35, 1.0)
	volumeFactor := clampFloat(float64(p.totalShotsFired)/18.0, 0.2, 1.0)
	damageFactor := clampFloat(p.totalDamage/160.0, 0.2, 1.0)
	return clampFloat(botFactor*0.35+volumeFactor*0.4+damageFactor*0.25, 0.25, 1.0)
}

var (
	baseWeaponConfigs map[string]WeaponConfig
	weaponBalance     map[string]WeaponBalanceState
	weaponBalanceMu   sync.RWMutex
)

func init() {
	baseWeaponConfigs = map[string]WeaponConfig{
		"sword": {
			Name:      "sword",
			Damage:    21,
			GridRange: 1,
			Cooldown:  0.55,
			Special:   "cleave",
		},
		"bow": {
			Name:      "bow",
			Damage:    16,
			GridRange: 8,
			Cooldown:  1.15,
			Special:   "projectile",
		},
		"daggers": {
			Name:      "daggers",
			Damage:    11,
			GridRange: 1,
			Cooldown:  0.35,
			Special:   "double_strike",
			Param:     0.25,
		},
		"shield": {
			Name:      "shield",
			Damage:    14,
			GridRange: 1,
			Cooldown:  0.8,
			Special:   "block",
			Param:     0.5,
		},
		"spear": {
			Name:      "spear",
			Damage:    17,
			GridRange: 2,
			Cooldown:  0.75,
			Special:   "knockback",
			Param:     2.0,
		},
		"staff": {
			Name:      "staff",
			Damage:    17,
			GridRange: 5,
			Cooldown:  1.65,
			Special:   "area",
			GridParam: 2,
		},
		"grapple": {
			Name:      "grapple",
			Damage:    14,
			GridRange: 5,
			Cooldown:  1.05,
			Special:   "grapple",
		},
	}
	WeaponConfigs = cloneWeaponConfigs(baseWeaponConfigs)
	weaponBalance = make(map[string]WeaponBalanceState, len(baseWeaponConfigs))
}

func cloneWeaponConfigs(src map[string]WeaponConfig) map[string]WeaponConfig {
	cloned := make(map[string]WeaponConfig, len(src))
	for name, wc := range src {
		cloned[name] = wc
	}
	return cloned
}

func defaultWeaponBalanceState(weapon string) WeaponBalanceState {
	startStep := config.C.WeaponAutoBalanceStartStep
	if startStep <= 0 {
		startStep = 0.05
	}
	return WeaponBalanceState{
		Weapon:          weapon,
		DamageScale:     1.0,
		CooldownScale:   1.0,
		AdjustmentScale: startStep,
	}
}

func normalizeWeaponBalanceState(state WeaponBalanceState) WeaponBalanceState {
	if state.Weapon == "" {
		return state
	}
	if state.DamageScale <= 0 {
		state.DamageScale = 1.0
	}
	if state.CooldownScale <= 0 {
		state.CooldownScale = 1.0
	}
	minStep := config.C.WeaponAutoBalanceMinStep
	if minStep <= 0 {
		minStep = 0.005
	}
	if state.AdjustmentScale < minStep {
		state.AdjustmentScale = minStep
	}
	return state
}

func effectiveWeaponConfigLocked(name string) WeaponConfig {
	wc, ok := baseWeaponConfigs[name]
	if !ok {
		return WeaponConfig{}
	}
	state := normalizeWeaponBalanceState(weaponBalance[name])
	if state.Weapon == "" {
		state = defaultWeaponBalanceState(name)
		weaponBalance[name] = state
	}

	wc.Damage = maxInt(1, int(math.Round(float64(wc.Damage)*state.DamageScale)))
	wc.Cooldown = math.Max(0.1, wc.Cooldown*state.CooldownScale)
	wc.Range = float64(wc.GridRange) * config.C.PathfindingCellSize
	return wc
}

func currentDeadzone(state WeaponBalanceState) float64 {
	start := config.C.WeaponAutoBalanceDeadzoneStart
	if start <= 0 {
		start = 0.02
	}
	minDeadzone := config.C.WeaponAutoBalanceDeadzoneMin
	if minDeadzone <= 0 {
		minDeadzone = 0.003
	}
	startStep := config.C.WeaponAutoBalanceStartStep
	if startStep <= 0 {
		startStep = 0.05
	}
	progress := state.AdjustmentScale / startStep
	if progress > 1 {
		progress = 1
	}
	if progress < 0 {
		progress = 0
	}
	return math.Max(minDeadzone, start*progress)
}

func damageScaleBounds() (float64, float64) {
	minV := config.C.WeaponAutoBalanceMinDamageScale
	maxV := config.C.WeaponAutoBalanceMaxDamageScale
	if minV <= 0 {
		minV = 0.8
	}
	if maxV <= minV {
		maxV = 1.3
	}
	return minV, maxV
}

func cooldownScaleBounds() (float64, float64) {
	minV := config.C.WeaponAutoBalanceMinCooldownScale
	maxV := config.C.WeaponAutoBalanceMaxCooldownScale
	if minV <= 0 {
		minV = 0.85
	}
	if maxV <= minV {
		maxV = 1.2
	}
	return minV, maxV
}

func persistWeaponBalanceSnapshot(ctx context.Context, state WeaponBalanceState, entry *weaponRoundPerformance, globalMean float64, damageDelta, cooldownDelta float64) {
	if db.Pool == nil {
		return
	}
	record := &db.WeaponBalance{
		Weapon:          state.Weapon,
		DamageScale:     state.DamageScale,
		CooldownScale:   state.CooldownScale,
		AdjustmentScale: state.AdjustmentScale,
		RoundsTracked:   state.RoundsTracked,
		UpdatedAt:       state.UpdatedAt,
	}
	if err := db.UpsertWeaponBalance(ctx, record); err != nil {
		slog.Error("failed to persist weapon balance", "weapon", state.Weapon, "error", err)
	}
	history := &db.WeaponBalanceHistory{
		Weapon:          state.Weapon,
		RoundsTracked:   state.RoundsTracked,
		DamageScale:     state.DamageScale,
		CooldownScale:   state.CooldownScale,
		AdjustmentScale: state.AdjustmentScale,
		AvgScore:        entry.score,
		MeanScore:       globalMean,
		DiffPct:         ((entry.score - globalMean) / math.Max(globalMean, 0.001)) * 100,
		DamageDelta:     damageDelta,
		CooldownDelta:   cooldownDelta,
		CreatedAt:       state.UpdatedAt,
	}
	if err := db.InsertWeaponBalanceHistory(ctx, history); err != nil {
		slog.Error("failed to persist weapon balance history", "weapon", state.Weapon, "error", err)
	}
}

func refreshAllWeaponConfigsLocked() {
	for name := range baseWeaponConfigs {
		WeaponConfigs[name] = effectiveWeaponConfigLocked(name)
	}
}

func InitWeaponRanges(cellSize float64) {
	weaponBalanceMu.Lock()
	defer weaponBalanceMu.Unlock()

	for name, wc := range baseWeaponConfigs {
		wc.Range = float64(wc.GridRange) * cellSize
		baseWeaponConfigs[name] = wc
	}
	refreshAllWeaponConfigsLocked()
}

func GetBaseWeaponConfig(name string) (WeaponConfig, bool) {
	weaponBalanceMu.RLock()
	defer weaponBalanceMu.RUnlock()
	wc, ok := baseWeaponConfigs[name]
	return wc, ok
}

func UpdateBaseWeaponConfig(name string, wc WeaponConfig) bool {
	weaponBalanceMu.Lock()
	defer weaponBalanceMu.Unlock()

	if _, ok := baseWeaponConfigs[name]; !ok {
		return false
	}
	wc.Range = float64(wc.GridRange) * config.C.PathfindingCellSize
	baseWeaponConfigs[name] = wc
	WeaponConfigs[name] = effectiveWeaponConfigLocked(name)
	return true
}

func GetWeaponBalanceState(name string) (WeaponBalanceState, bool) {
	weaponBalanceMu.RLock()
	defer weaponBalanceMu.RUnlock()

	state, ok := weaponBalance[name]
	if !ok {
		if _, exists := baseWeaponConfigs[name]; !exists {
			return WeaponBalanceState{}, false
		}
		return defaultWeaponBalanceState(name), true
	}
	return normalizeWeaponBalanceState(state), true
}

func GetWeaponConfig(name string) WeaponConfig {
	weaponBalanceMu.RLock()
	defer weaponBalanceMu.RUnlock()
	if wc, ok := WeaponConfigs[name]; ok {
		return wc
	}
	slog.Warn("unknown weapon, falling back to sword", "weapon", name)
	return WeaponConfigs["sword"]
}

func LoadWeaponBalance(ctx context.Context) error {
	weaponBalanceMu.Lock()
	for name := range baseWeaponConfigs {
		weaponBalance[name] = defaultWeaponBalanceState(name)
	}
	refreshAllWeaponConfigsLocked()
	weaponBalanceMu.Unlock()

	if db.Pool == nil {
		return nil
	}

	rows, err := db.ListWeaponBalances(ctx)
	if err != nil {
		return err
	}

	weaponBalanceMu.Lock()
	defer weaponBalanceMu.Unlock()
	for _, row := range rows {
		if _, ok := baseWeaponConfigs[row.Weapon]; !ok {
			continue
		}
		weaponBalance[row.Weapon] = normalizeWeaponBalanceState(WeaponBalanceState{
			Weapon:          row.Weapon,
			DamageScale:     row.DamageScale,
			CooldownScale:   row.CooldownScale,
			AdjustmentScale: row.AdjustmentScale,
			RoundsTracked:   row.RoundsTracked,
			UpdatedAt:       row.UpdatedAt,
		})
	}
	refreshAllWeaponConfigsLocked()
	return nil
}

func AutoBalanceWeapons(ctx context.Context, bots map[string]*BotState, winnerID string) {
	if !config.C.WeaponAutoBalanceEnabled {
		return
	}

	perf := make(map[string]*weaponRoundPerformance)
	for _, bot := range bots {
		if bot == nil || bot.Weapon == "" {
			continue
		}
		entry := perf[bot.Weapon]
		if entry == nil {
			entry = &weaponRoundPerformance{weapon: bot.Weapon}
			perf[bot.Weapon] = entry
		}
		entry.bots++
		if bot.BotID == winnerID {
			entry.wins++
		}
		lifeSecs := float64(bot.RoundLongestLife) / math.Max(1, float64(config.C.TickRate))
		killScore := float64(bot.RoundKills) * 28
		damageScore := bot.RoundDamageDealt * 0.18
		streakScore := float64(bot.BestKillStreak) * 14
		survivalScore := lifeSecs * 0.3
		entry.score += killScore + damageScore + streakScore + survivalScore
		entry.totalKills += bot.RoundKills
		entry.totalDamage += bot.RoundDamageDealt
		entry.totalStreak += bot.BestKillStreak
		entry.totalLifeSecs += lifeSecs
		entry.totalShotsFired += bot.RoundShotsFired
		entry.totalShotsHit += bot.RoundShotsHit
		entry.totalDamageTaken += bot.RoundDamageTaken
		if bot.BotID == winnerID {
			entry.score += 60
		}
	}
	if len(perf) < 2 {
		return
	}

	globalMean := 0.0
	meanAvgDamage := 0.0
	meanHitRate := 0.0
	meanDamagePerHit := 0.0
	meanShotsPerLife := 0.0
	meanDamagePerLife := 0.0
	meanKillsPerHit := 0.0
	participants := 0
	for _, entry := range perf {
		if entry.bots == 0 {
			continue
		}
		entry.score /= float64(entry.bots)
		globalMean += entry.score
		meanAvgDamage += entry.avgDamage()
		meanHitRate += entry.hitRate()
		meanDamagePerHit += entry.damagePerHit()
		meanShotsPerLife += entry.shotsPerLife()
		meanDamagePerLife += entry.damagePerLife()
		meanKillsPerHit += entry.killsPerHit()
		participants++
	}
	if participants < 2 {
		return
	}
	globalMean /= float64(participants)
	meanAvgDamage /= float64(participants)
	meanHitRate /= float64(participants)
	meanDamagePerHit /= float64(participants)
	meanShotsPerLife /= float64(participants)
	meanDamagePerLife /= float64(participants)
	meanKillsPerHit /= float64(participants)
	if globalMean <= 0 {
		return
	}

	minStep := config.C.WeaponAutoBalanceMinStep
	if minStep <= 0 {
		minStep = 0.005
	}
	decay := config.C.WeaponAutoBalanceDecay
	if decay <= 0 || decay >= 1 {
		decay = 0.94
	}

	weaponBalanceMu.Lock()
	defer weaponBalanceMu.Unlock()

	for weapon, entry := range perf {
		state := normalizeWeaponBalanceState(weaponBalance[weapon])
		if state.Weapon == "" {
			state = defaultWeaponBalanceState(weapon)
		}
		prevDamageScale := state.DamageScale
		prevCooldownScale := state.CooldownScale

		diffRatio := (entry.score - globalMean) / globalMean
		if math.Abs(diffRatio) < currentDeadzone(state) {
			state.RoundsTracked++
			state.AdjustmentScale = math.Max(minStep, state.AdjustmentScale*decay)
			state.UpdatedAt = time.Now()
			weaponBalance[weapon] = state
			WeaponConfigs[weapon] = effectiveWeaponConfigLocked(weapon)
			persistWeaponBalanceSnapshot(ctx, state, entry, globalMean, 0, 0)
			continue
		}

		magnitude := state.AdjustmentScale * math.Min(1, math.Abs(diffRatio)) * entry.confidence()
		if magnitude < minStep {
			magnitude = minStep
		}
		damageWeight := config.C.WeaponAutoBalanceDamageWeight
		if damageWeight <= 0 {
			damageWeight = 0.65
		}
		cooldownWeight := config.C.WeaponAutoBalanceCooldownWeight
		if cooldownWeight <= 0 {
			cooldownWeight = 0.45
		}
		minDamageScale, maxDamageScale := damageScaleBounds()
		minCooldownScale, maxCooldownScale := cooldownScaleBounds()
		axisDeadzone := math.Max(0.02, currentDeadzone(state)*0.85)

		damagePressure := weightedRelative(
			relativeDelta(entry.damagePerHit(), meanDamagePerHit), 0.5,
			relativeDelta(entry.killsPerHit(), meanKillsPerHit), 0.3,
			relativeDelta(entry.avgDamage(), meanAvgDamage), 0.2,
		)
		cooldownPressure := weightedRelative(
			relativeDelta(entry.shotsPerLife(), meanShotsPerLife), 0.55,
			relativeDelta(entry.hitRate(), meanHitRate), 0.2,
			relativeDelta(entry.damagePerLife(), meanDamagePerLife), 0.25,
		)

		adjustDamage := false
		adjustCooldown := false
		if diffRatio > 0 {
			adjustDamage = damagePressure > axisDeadzone
			adjustCooldown = cooldownPressure > axisDeadzone
		} else {
			adjustDamage = damagePressure < -axisDeadzone
			adjustCooldown = cooldownPressure < -axisDeadzone && entry.hitRate() >= meanHitRate*0.9
		}
		if !adjustDamage && !adjustCooldown {
			if math.Abs(damagePressure) >= math.Abs(cooldownPressure) {
				adjustDamage = true
			} else {
				adjustCooldown = true
			}
		}

		damageAxisScale := axisMagnitudeScale(damagePressure, axisDeadzone)
		cooldownAxisScale := axisMagnitudeScale(cooldownPressure, axisDeadzone)
		if adjustDamage && diffRatio > 0 {
			state.DamageScale = clampFloat(state.DamageScale*(1-magnitude*damageWeight*damageAxisScale), minDamageScale, maxDamageScale)
		} else if adjustDamage {
			state.DamageScale = clampFloat(state.DamageScale*(1+magnitude*damageWeight*damageAxisScale), minDamageScale, maxDamageScale)
		}
		if adjustCooldown && diffRatio > 0 {
			state.CooldownScale = clampFloat(state.CooldownScale*(1+magnitude*cooldownWeight*cooldownAxisScale), minCooldownScale, maxCooldownScale)
		} else if adjustCooldown {
			state.CooldownScale = clampFloat(state.CooldownScale*(1-magnitude*cooldownWeight*cooldownAxisScale), minCooldownScale, maxCooldownScale)
		}

		state.RoundsTracked++
		state.AdjustmentScale = math.Max(minStep, state.AdjustmentScale*decay)
		state.UpdatedAt = time.Now()
		weaponBalance[weapon] = state
		WeaponConfigs[weapon] = effectiveWeaponConfigLocked(weapon)

		slog.Info("weapon auto-balance adjusted",
			"weapon", weapon,
			"round_score", round1(entry.score),
			"mean_score", round1(globalMean),
			"damage_scale", round1(state.DamageScale),
			"cooldown_scale", round1(state.CooldownScale),
			"damage_pressure", round1(damagePressure),
			"cooldown_pressure", round1(cooldownPressure),
			"adjust_damage", adjustDamage,
			"adjust_cooldown", adjustCooldown,
			"confidence", round1(entry.confidence()),
			"next_step", round1(state.AdjustmentScale),
			"participants", entry.bots,
			"wins", entry.wins,
		)

		persistWeaponBalanceSnapshot(ctx, state, entry, globalMean, state.DamageScale-prevDamageScale, state.CooldownScale-prevCooldownScale)
	}
}

func clampFloat(v, minV, maxV float64) float64 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func relativeDelta(value, mean float64) float64 {
	if mean <= 0 {
		return 0
	}
	return (value - mean) / mean
}

func weightedRelative(parts ...float64) float64 {
	if len(parts)%2 != 0 {
		return 0
	}
	totalWeight := 0.0
	total := 0.0
	for i := 0; i < len(parts); i += 2 {
		value := parts[i]
		weight := parts[i+1]
		total += value * weight
		totalWeight += weight
	}
	if totalWeight <= 0 {
		return 0
	}
	return total / totalWeight
}

func axisMagnitudeScale(pressure, deadzone float64) float64 {
	scale := math.Abs(pressure)
	if scale <= deadzone {
		return 0.85
	}
	return clampFloat(0.85+(scale-deadzone)*1.5, 0.85, 1.35)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
