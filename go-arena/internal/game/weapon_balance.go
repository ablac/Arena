package game

import (
	"context"
	"log/slog"
	"math"
	"sort"
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
	bots               int
	score              float64
	totalKills         float64
	totalDamage        float64
	totalLifeSecs      float64
	totalShotsFired    float64
	totalShotsHit      float64
	botIDs             []string
	engagedOpponentIDs []string
}

func (p *weaponRoundPerformance) avgDamage() float64 {
	if p == nil || p.bots == 0 {
		return 0
	}
	return p.totalDamage / float64(p.bots)
}

func (p *weaponRoundPerformance) avgScore() float64 {
	if p == nil || p.bots == 0 {
		return 0
	}
	return p.score / float64(p.bots)
}

func (p *weaponRoundPerformance) hitRate() float64 {
	if p == nil || p.totalShotsFired <= 0 {
		return 0
	}
	return clampFloat(p.totalShotsHit/p.totalShotsFired, 0, 1)
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
	return p.totalShotsFired / p.totalLifeSecs
}

func (p *weaponRoundPerformance) killsPerHit() float64 {
	if p == nil || p.totalShotsHit <= 0 {
		return 0
	}
	return p.totalKills / p.totalShotsHit
}

func (p *weaponRoundPerformance) damagePerLife() float64 {
	if p == nil || p.totalLifeSecs <= 0 {
		return 0
	}
	return p.totalDamage / p.totalLifeSecs
}

func (p *weaponRoundPerformance) subtract(other *weaponRoundPerformance) *weaponRoundPerformance {
	if p == nil || other == nil {
		return nil
	}
	return &weaponRoundPerformance{
		bots:            p.bots - other.bots,
		score:           p.score - other.score,
		totalKills:      p.totalKills - other.totalKills,
		totalDamage:     p.totalDamage - other.totalDamage,
		totalLifeSecs:   p.totalLifeSecs - other.totalLifeSecs,
		totalShotsFired: p.totalShotsFired - other.totalShotsFired,
		totalShotsHit:   p.totalShotsHit - other.totalShotsHit,
	}
}

type runningBalanceStat struct {
	count int
	mean  float64
	m2    float64
}

func (s *runningBalanceStat) add(value float64) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	s.count++
	delta := value - s.mean
	s.mean += delta / float64(s.count)
	s.m2 += delta * (value - s.mean)
}

func (s runningBalanceStat) standardError() float64 {
	if s.count < 2 {
		return math.Inf(1)
	}
	variance := math.Max(0, s.m2/float64(s.count-1))
	return math.Sqrt(variance / float64(s.count))
}

// directionOutside returns -1 or 1 only when the full confidence interval is
// outside the practical deadzone. A zero result means the evidence is either
// too small, too noisy, or too close to balanced to justify changing gameplay.
func (s runningBalanceStat) directionOutside(deadzone, confidenceZ float64) int {
	margin := confidenceZ * s.standardError()
	if s.mean-margin > deadzone {
		return 1
	}
	if s.mean+margin < -deadzone {
		return -1
	}
	return 0
}

func (s runningBalanceStat) conservativeMagnitude(confidenceZ float64) float64 {
	return math.Max(0, math.Abs(s.mean)-confidenceZ*s.standardError())
}

type weaponBalanceEvidence struct {
	rounds               int
	botSamples           int
	opponentSamples      int
	distinctWeaponBots   map[string]struct{}
	distinctOpponentBots map[string]struct{}
	hitRateParityRounds  int
	scoreDiff            runningBalanceStat
	damagePressure       runningBalanceStat
	cooldownPressure     runningBalanceStat
}

func (e *weaponBalanceEvidence) add(entry, opponents *weaponRoundPerformance, scoreDiff, damagePressure, cooldownPressure float64) {
	if e.distinctWeaponBots == nil {
		e.distinctWeaponBots = make(map[string]struct{})
	}
	if e.distinctOpponentBots == nil {
		e.distinctOpponentBots = make(map[string]struct{})
	}
	e.rounds++
	e.botSamples += entry.bots
	e.opponentSamples += opponents.bots
	for _, id := range entry.botIDs {
		if id != "" {
			e.distinctWeaponBots[id] = struct{}{}
		}
	}
	for _, id := range entry.engagedOpponentIDs {
		if id != "" {
			e.distinctOpponentBots[id] = struct{}{}
		}
	}
	if opponents.hitRate() <= 0 || entry.hitRate() >= opponents.hitRate()*0.75 {
		e.hitRateParityRounds++
	}
	e.scoreDiff.add(scoreDiff)
	e.damagePressure.add(damagePressure)
	e.cooldownPressure.add(cooldownPressure)
}

func (e *weaponBalanceEvidence) reset() {
	*e = weaponBalanceEvidence{}
}

var (
	baseWeaponConfigs map[string]WeaponConfig
	weaponBalance     map[string]WeaponBalanceState
	weaponEvidence    map[string]*weaponBalanceEvidence
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
			Cooldown:  1.05,
			Special:   "projectile",
		},
		"daggers": {
			Name:      "daggers",
			Damage:    11,
			GridRange: 1,
			Cooldown:  0.35,
			Special:   "backstab",
		},
		"shield": {
			Name:      "shield",
			Damage:    14,
			GridRange: 1,
			Cooldown:  0.8,
			Special:   "bash",
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
			GridRange: 6,
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
	weaponEvidence = make(map[string]*weaponBalanceEvidence, len(baseWeaponConfigs))
}

func cloneWeaponConfigs(src map[string]WeaponConfig) map[string]WeaponConfig {
	cloned := make(map[string]WeaponConfig, len(src))
	for name, wc := range src {
		cloned[name] = wc
	}
	return cloned
}

func defaultWeaponBalanceState(weapon string) WeaponBalanceState {
	_, startStep := weaponBalanceStepBounds()
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
	if !finitePositive(state.DamageScale) {
		state.DamageScale = 1.0
	}
	if !finitePositive(state.CooldownScale) {
		state.CooldownScale = 1.0
	}
	minStep, startStep := weaponBalanceStepBounds()
	if !finitePositive(state.AdjustmentScale) {
		state.AdjustmentScale = startStep
	} else {
		state.AdjustmentScale = clampFloat(state.AdjustmentScale, minStep, startStep)
	}
	return state
}

func weaponBalanceStepBounds() (float64, float64) {
	minStep := config.C.WeaponAutoBalanceMinStep
	if !finitePositive(minStep) {
		minStep = 0.005
	}
	minStep = math.Min(minStep, 0.05)
	startStep := config.C.WeaponAutoBalanceStartStep
	if !finitePositive(startStep) {
		startStep = 0.05
	}
	startStep = clampFloat(startStep, minStep, 0.10)
	return minStep, startStep
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
	if !finitePositive(start) {
		start = 0.02
	}
	minDeadzone := config.C.WeaponAutoBalanceDeadzoneMin
	if !finitePositive(minDeadzone) {
		minDeadzone = 0.003
	}
	_, startStep := weaponBalanceStepBounds()
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
	if !finitePositive(minV) {
		minV = 0.8
	}
	if !finitePositive(maxV) || maxV <= minV {
		maxV = 1.3
	}
	return minV, maxV
}

func cooldownScaleBounds() (float64, float64) {
	minV := config.C.WeaponAutoBalanceMinCooldownScale
	maxV := config.C.WeaponAutoBalanceMaxCooldownScale
	if !finitePositive(minV) {
		minV = 0.85
	}
	if !finitePositive(maxV) || maxV <= minV {
		maxV = 1.2
	}
	return minV, maxV
}

func balanceEvidenceThresholds() (minRounds, minBotSamples, minDistinctBots, minActions int) {
	minRounds = config.C.WeaponAutoBalanceMinRounds
	if minRounds < 2 {
		minRounds = 6
	}
	minBotSamples = config.C.WeaponAutoBalanceMinBotSamples
	if minBotSamples < minRounds {
		minBotSamples = minRounds * 3
	}
	minDistinctBots = config.C.WeaponAutoBalanceMinDistinctBots
	if minDistinctBots < 2 {
		minDistinctBots = 2
	}
	minActions = config.C.WeaponAutoBalanceMinActions
	if minActions < 1 {
		minActions = 5
	}
	return
}

func balanceConfidenceZ() float64 {
	z := config.C.WeaponAutoBalanceConfidenceZ
	if !finitePositive(z) || z < 1 || z > 4 {
		return 1.96
	}
	return z
}

func balanceMinEffect() float64 {
	effect := config.C.WeaponAutoBalanceMinEffect
	if !finitePositive(effect) || effect > 0.5 {
		return 0.05
	}
	return effect
}

func evidenceReady(e *weaponBalanceEvidence, minRounds, minBotSamples, minDistinctBots int) bool {
	return e != nil &&
		e.rounds >= minRounds &&
		e.botSamples >= minBotSamples &&
		e.opponentSamples >= minBotSamples &&
		len(e.distinctWeaponBots) >= minDistinctBots &&
		len(e.distinctOpponentBots) >= minDistinctBots
}

// eloSkillFactor estimates how much performance the round should expect from
// this bot relative to an average participant. The narrow clamp intentionally
// makes this a modest confounder correction, not a way for rating to erase a
// real weapon advantage. Elo is sampled at round end, so a wider correction
// would create a feedback loop between this round's result and its weighting.
func eloSkillFactor(elo int, roundMeanElo float64) float64 {
	if elo <= 0 || roundMeanElo <= 0 {
		return 1
	}
	expected := 1 / (1 + math.Pow(10, (roundMeanElo-float64(elo))/400))
	return clampFloat(expected/0.5, 0.75, 1.25)
}

func botRoundBalanceScore(bot *BotState) float64 {
	if bot == nil {
		return 0
	}
	// Only direct weapon output belongs in the headline balance signal. Wins,
	// survival, streaks, mines, objectives, and pickups describe the bot or the
	// round and would systematically bias tuning toward particular strategies.
	return float64(bot.RoundWeaponKills)*32 + bot.RoundWeaponDamageDealt*0.22
}

func validWeaponBalanceSample(bot *BotState, minActions int) bool {
	return bot != nil &&
		bot.Weapon != "" &&
		bot.RoundShotsFired >= minActions &&
		bot.RoundShotsHit >= 0 &&
		bot.RoundWeaponKills >= 0 &&
		bot.RoundLongestLife >= 0 &&
		!math.IsNaN(bot.RoundWeaponDamageDealt) &&
		!math.IsInf(bot.RoundWeaponDamageDealt, 0) &&
		bot.RoundWeaponDamageDealt >= 0
}

func addBotPerformance(entry, total *weaponRoundPerformance, bot *BotState, identity string, skillFactor float64) {
	if skillFactor <= 0 {
		skillFactor = 1
	}
	lifeSecs := float64(bot.RoundLongestLife) / math.Max(1, float64(config.C.TickRate))
	adjustedScore := botRoundBalanceScore(bot) / skillFactor
	adjustedKills := float64(bot.RoundWeaponKills) / skillFactor
	adjustedDamage := bot.RoundWeaponDamageDealt / skillFactor
	adjustedLife := lifeSecs / skillFactor
	adjustedShotsFired := float64(bot.RoundShotsFired) / skillFactor
	adjustedHits := float64(bot.RoundShotsHit) / skillFactor

	for _, target := range []*weaponRoundPerformance{entry, total} {
		target.bots++
		target.score += adjustedScore
		target.totalKills += adjustedKills
		target.totalDamage += adjustedDamage
		target.totalLifeSecs += adjustedLife
		target.totalShotsFired += adjustedShotsFired
		target.totalShotsHit += adjustedHits
	}
	if identity != "" {
		entry.botIDs = append(entry.botIDs, identity)
	}
	for opponentID := range bot.RoundWeaponOpponentIDs {
		if opponentID != "" && opponentID != identity {
			entry.engagedOpponentIDs = append(entry.engagedOpponentIDs, opponentID)
		}
	}
}

func sampleReliability(a, b int) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	// Harmonic sample size is dominated by the smaller side of the
	// comparison. A two-sample prior shrinks sparse weapon matchups toward no
	// effect instead of allowing one rare pick to dictate the next patch.
	effective := 2 * float64(a*b) / float64(a+b)
	return effective / (effective + 2)
}

func symmetricRelativeDelta(value, baseline float64) float64 {
	denominator := math.Abs(value) + math.Abs(baseline)
	if denominator <= 1e-9 {
		return 0
	}
	return clampFloat(2*(value-baseline)/denominator, -2, 2)
}

func persistWeaponBalanceSnapshot(ctx context.Context, state WeaponBalanceState, entry *weaponRoundPerformance, comparisonMean, diffRatio, damageDelta, cooldownDelta float64) {
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
		AvgScore:        entry.avgScore(),
		MeanScore:       comparisonMean,
		DiffPct:         diffRatio * 100,
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
	// A manual base-value change creates a new experiment. Do not mix samples
	// collected under the previous damage/range/cooldown values into it.
	delete(weaponEvidence, name)
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
	weaponEvidence = make(map[string]*weaponBalanceEvidence, len(baseWeaponConfigs))
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

func AutoBalanceWeapons(ctx context.Context, bots map[string]*BotState, _ string) {
	if !config.C.WeaponAutoBalanceEnabled || ActiveModeRules.HasTeams() {
		return
	}

	minRounds, minBotSamples, minDistinctBots, minActions := balanceEvidenceThresholds()
	weaponBalanceMu.RLock()
	knownWeapons := make(map[string]struct{}, len(baseWeaponConfigs))
	for weapon := range baseWeaponConfigs {
		knownWeapons[weapon] = struct{}{}
	}
	weaponBalanceMu.RUnlock()

	type eligibleBot struct {
		identity string
		bot      *BotState
	}
	eligible := make([]eligibleBot, 0, len(bots))
	meanElo := 0.0
	ratedBots := 0
	for mapID, bot := range bots {
		if !validWeaponBalanceSample(bot, minActions) {
			continue
		}
		if _, known := knownWeapons[bot.Weapon]; !known {
			continue
		}
		identity := bot.BotID
		if identity == "" {
			identity = mapID
		}
		eligible = append(eligible, eligibleBot{identity: identity, bot: bot})
		if bot.Elo > 0 {
			meanElo += float64(bot.Elo)
			ratedBots++
		}
	}
	if ratedBots > 0 {
		meanElo /= float64(ratedBots)
	}

	performance := make(map[string]*weaponRoundPerformance)
	total := &weaponRoundPerformance{}
	for _, sample := range eligible {
		bot := sample.bot
		entry := performance[bot.Weapon]
		if entry == nil {
			entry = &weaponRoundPerformance{}
			performance[bot.Weapon] = entry
		}
		addBotPerformance(entry, total, bot, sample.identity, eloSkillFactor(bot.Elo, meanElo))
	}
	if len(performance) < 2 {
		return
	}

	type weaponRoundSignal struct {
		entry            *weaponRoundPerformance
		opponents        *weaponRoundPerformance
		scoreDiff        float64
		damagePressure   float64
		cooldownPressure float64
	}
	weapons := make([]string, 0, len(performance))
	signals := make(map[string]weaponRoundSignal, len(performance))
	for weapon, entry := range performance {
		opponents := total.subtract(entry)
		if opponents == nil || opponents.bots == 0 {
			continue
		}
		reliability := sampleReliability(entry.bots, opponents.bots)
		scoreDiff := symmetricRelativeDelta(entry.avgScore(), opponents.avgScore()) * reliability
		damagePressure := weightedRelative(
			symmetricRelativeDelta(entry.damagePerHit(), opponents.damagePerHit()), 0.5,
			symmetricRelativeDelta(entry.killsPerHit(), opponents.killsPerHit()), 0.3,
			symmetricRelativeDelta(entry.avgDamage(), opponents.avgDamage()), 0.2,
		) * reliability
		cooldownPressure := weightedRelative(
			symmetricRelativeDelta(entry.shotsPerLife(), opponents.shotsPerLife()), 0.55,
			symmetricRelativeDelta(entry.hitRate(), opponents.hitRate()), 0.2,
			symmetricRelativeDelta(entry.damagePerLife(), opponents.damagePerLife()), 0.25,
		) * reliability
		signals[weapon] = weaponRoundSignal{
			entry: entry, opponents: opponents, scoreDiff: scoreDiff,
			damagePressure: damagePressure, cooldownPressure: cooldownPressure,
		}
		weapons = append(weapons, weapon)
	}
	if len(weapons) < 2 {
		return
	}
	sort.Strings(weapons)

	minStep, startStep := weaponBalanceStepBounds()
	decay := config.C.WeaponAutoBalanceDecay
	if !finitePositive(decay) || decay >= 1 {
		decay = 0.94
	}
	damageWeight := config.C.WeaponAutoBalanceDamageWeight
	if !finitePositive(damageWeight) {
		damageWeight = 0.65
	}
	damageWeight = math.Min(damageWeight, 1)
	cooldownWeight := config.C.WeaponAutoBalanceCooldownWeight
	if !finitePositive(cooldownWeight) {
		cooldownWeight = 0.45
	}
	cooldownWeight = math.Min(cooldownWeight, 1)
	confidenceZ := balanceConfidenceZ()
	minEffect := balanceMinEffect()
	minDamageScale, maxDamageScale := damageScaleBounds()
	minCooldownScale, maxCooldownScale := cooldownScaleBounds()
	now := time.Now()

	type pendingSnapshot struct {
		state                      WeaponBalanceState
		entry                      *weaponRoundPerformance
		comparisonMean, diffRatio  float64
		damageDelta, cooldownDelta float64
	}
	pending := make([]pendingSnapshot, 0, len(weapons))

	weaponBalanceMu.Lock()
	if weaponEvidence == nil {
		weaponEvidence = make(map[string]*weaponBalanceEvidence, len(baseWeaponConfigs))
	}
	for _, weapon := range weapons {
		signal := signals[weapon]
		state := normalizeWeaponBalanceState(weaponBalance[weapon])
		if state.Weapon == "" {
			state = defaultWeaponBalanceState(weapon)
		}
		previousDamageScale := state.DamageScale
		previousCooldownScale := state.CooldownScale

		evidence := weaponEvidence[weapon]
		if evidence == nil {
			evidence = &weaponBalanceEvidence{}
			weaponEvidence[weapon] = evidence
		}
		evidence.add(signal.entry, signal.opponents, signal.scoreDiff, signal.damagePressure, signal.cooldownPressure)
		state.RoundsTracked++
		state.UpdatedAt = now

		if evidenceReady(evidence, minRounds, minBotSamples, minDistinctBots) {
			deadzone := math.Max(minEffect, currentDeadzone(state))
			axisDeadzone := math.Max(minEffect*0.75, currentDeadzone(state)*0.85)
			scoreDirection := evidence.scoreDiff.directionOutside(deadzone, confidenceZ)
			damageDirection := evidence.damagePressure.directionOutside(axisDeadzone, confidenceZ)
			cooldownDirection := evidence.cooldownPressure.directionOutside(axisDeadzone, confidenceZ)
			hitRateParity := float64(evidence.hitRateParityRounds) / float64(evidence.rounds)
			adjustDamage := scoreDirection != 0 && damageDirection == scoreDirection
			adjustCooldown := scoreDirection != 0 && cooldownDirection == scoreDirection
			if scoreDirection < 0 && hitRateParity < 0.75 {
				// Missing on purpose is indistinguishable from a weak weapon in
				// raw telemetry, so a low-hit-rate batch can never earn a buff.
				adjustDamage = false
				adjustCooldown = false
			}

			if adjustDamage || adjustCooldown {
				magnitude := state.AdjustmentScale * math.Min(1, evidence.scoreDiff.conservativeMagnitude(confidenceZ))
				damageAxisScale := axisMagnitudeScale(evidence.damagePressure.mean, axisDeadzone)
				cooldownAxisScale := axisMagnitudeScale(evidence.cooldownPressure.mean, axisDeadzone)
				if adjustDamage && scoreDirection > 0 {
					state.DamageScale = clampFloat(state.DamageScale*(1-magnitude*damageWeight*damageAxisScale), minDamageScale, maxDamageScale)
				} else if adjustDamage {
					state.DamageScale = clampFloat(state.DamageScale*(1+magnitude*damageWeight*damageAxisScale), minDamageScale, maxDamageScale)
				}
				if adjustCooldown && scoreDirection > 0 {
					state.CooldownScale = clampFloat(state.CooldownScale*(1+magnitude*cooldownWeight*cooldownAxisScale), minCooldownScale, maxCooldownScale)
				} else if adjustCooldown {
					state.CooldownScale = clampFloat(state.CooldownScale*(1-magnitude*cooldownWeight*cooldownAxisScale), minCooldownScale, maxCooldownScale)
				}
				state.AdjustmentScale = clampFloat(state.AdjustmentScale*1.03, minStep, startStep)
				slog.Info("weapon auto-balance adjusted",
					"weapon", weapon,
					"evidence_rounds", evidence.rounds,
					"bot_samples", evidence.botSamples,
					"distinct_weapon_bots", len(evidence.distinctWeaponBots),
					"distinct_opponent_bots", len(evidence.distinctOpponentBots),
					"score_effect", round1(evidence.scoreDiff.mean),
					"score_margin", round1(confidenceZ*evidence.scoreDiff.standardError()),
					"damage_scale", round1(state.DamageScale),
					"cooldown_scale", round1(state.CooldownScale),
					"adjust_damage", adjustDamage,
					"adjust_cooldown", adjustCooldown,
				)
			} else if scoreDirection == 0 {
				// Only a statistically steady batch is convergence evidence.
				state.AdjustmentScale = math.Max(minStep, state.AdjustmentScale*decay)
			} else {
				slog.Debug("weapon auto-balance rejected confounded batch",
					"weapon", weapon,
					"score_direction", scoreDirection,
					"damage_direction", damageDirection,
					"cooldown_direction", cooldownDirection,
					"hit_rate_parity", round1(hitRateParity),
				)
			}
			evidence.reset()
		} else if evidence.rounds >= minRounds*3 {
			// Do not let one identity stockpile unlimited historical weight.
			evidence.reset()
		}

		weaponBalance[weapon] = state
		WeaponConfigs[weapon] = effectiveWeaponConfigLocked(weapon)
		pending = append(pending, pendingSnapshot{
			state: state, entry: signal.entry,
			comparisonMean: signal.opponents.avgScore(), diffRatio: signal.scoreDiff,
			damageDelta:   state.DamageScale - previousDamageScale,
			cooldownDelta: state.CooldownScale - previousCooldownScale,
		})
	}
	weaponBalanceMu.Unlock()

	// Database latency must never hold weaponBalanceMu: GetWeaponConfig is on
	// the combat path, and this routine performs two writes per sampled weapon.
	for _, snapshot := range pending {
		persistWeaponBalanceSnapshot(ctx, snapshot.state, snapshot.entry,
			snapshot.comparisonMean, snapshot.diffRatio,
			snapshot.damageDelta, snapshot.cooldownDelta)
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

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
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
