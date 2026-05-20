package api

import (
	"math"
	"net/http"
	"sort"
	"time"

	"arena-server/internal/db"
	"arena-server/internal/game"
)

type weaponMetaRaw struct {
	entry WeaponStatsEntry
	dps   float64
	reach float64
}

// GetWeaponStats handles GET /api/v1/weapon-stats.
func GetWeaponStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	killMap := map[string]db.WeaponKillStats{}
	recentMap := map[string]db.WeaponRecentPerformance{}
	historyMap := map[string][]WeaponBalanceHistoryPoint{}
	if db.Pool != nil {
		rows, err := db.ListWeaponKillStats(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get weapon stats")
			return
		}
		for _, row := range rows {
			killMap[row.Weapon] = row
		}

		recentRows, err := db.ListRecentWeaponPerformance(ctx, 10)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get recent weapon performance")
			return
		}
		for _, row := range recentRows {
			recentMap[row.Weapon] = row
		}

		historyRows, err := db.ListWeaponBalanceHistory(ctx, 50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get weapon history")
			return
		}
		for _, row := range historyRows {
			baseWC, _ := game.GetBaseWeaponConfig(row.Weapon)
			historyMap[row.Weapon] = append(historyMap[row.Weapon], WeaponBalanceHistoryPoint{
				Round:         row.RoundsTracked,
				DamageScale:   roundMetaScore(row.DamageScale),
				CooldownScale: roundMetaScore(row.CooldownScale),
				DamageExact:   roundMetaScore(float64(baseWC.Damage) * row.DamageScale),
				Cooldown:      roundMetaScore(baseWC.Cooldown * row.CooldownScale),
				DiffPct:       roundMetaScore(row.DiffPct),
				UpdatedAt:     row.CreatedAt,
			})
		}
	}

	totalKills := 0
	maxKills24h := 0
	maxKills1h := 0
	recentMean := 0.0
	recentCount := 0
	for _, row := range killMap {
		totalKills += row.Kills
		if row.Kills24h > maxKills24h {
			maxKills24h = row.Kills24h
		}
		if row.Kills1h > maxKills1h {
			maxKills1h = row.Kills1h
		}
	}
	for _, row := range recentMap {
		recentMean += row.AvgScore
		recentCount++
	}
	if recentCount > 0 {
		recentMean /= float64(recentCount)
	}

	rawEntries := make([]weaponMetaRaw, 0, len(game.GetAvailableWeapons()))
	updatedAt := time.Time{}
	minDPS := math.MaxFloat64
	maxDPS := 0.0
	minReach := math.MaxFloat64
	maxReach := 0.0

	for _, name := range game.GetAvailableWeapons() {
		wc := game.GetWeaponConfig(name)
		baseWC, _ := game.GetBaseWeaponConfig(name)
		balance, _ := game.GetWeaponBalanceState(name)
		kills := killMap[name]
		recent := recentMap[name]
		history := historyMap[name]

		dps := float64(wc.Damage) / math.Max(0.1, wc.Cooldown)
		reach := float64(wc.GridRange)
		recentDiffPct := 0.0
		if recentMean > 0 {
			recentDiffPct = ((recent.AvgScore - recentMean) / recentMean) * 100
		}
		lastDamageMove := "flat"
		lastCooldownMove := "flat"
		for i := len(history) - 1; i >= 0; i-- {
			if lastDamageMove == "flat" {
				switch {
				case i > 0 && history[i].DamageScale > history[i-1].DamageScale+0.0001:
					lastDamageMove = "up"
				case i > 0 && history[i].DamageScale < history[i-1].DamageScale-0.0001:
					lastDamageMove = "down"
				}
			}
			if lastCooldownMove == "flat" {
				switch {
				case i > 0 && history[i].CooldownScale > history[i-1].CooldownScale+0.0001:
					lastCooldownMove = "up"
				case i > 0 && history[i].CooldownScale < history[i-1].CooldownScale-0.0001:
					lastCooldownMove = "down"
				}
			}
		}
		if lastDamageMove == "flat" {
			lastDamageMove = damageTrend(balance.DamageScale)
		}
		if lastCooldownMove == "flat" {
			lastCooldownMove = cooldownTrend(balance.CooldownScale)
		}

		if dps < minDPS {
			minDPS = dps
		}
		if dps > maxDPS {
			maxDPS = dps
		}
		if reach < minReach {
			minReach = reach
		}
		if reach > maxReach {
			maxReach = reach
		}

		raw := weaponMetaRaw{
			entry: WeaponStatsEntry{
				Weapon:           name,
				RecentForm:       roundMetaScore(clampMetaScore(100+recentDiffPct*10, 0, 200)),
				RecentRoundScore: roundMetaScore(recent.AvgScore),
				RecentDiffPct:    roundMetaScore(recentDiffPct),
				RecentRounds:     recent.Rounds,
				BalanceDirection: balanceDirection(recentDiffPct),
				Kills:            kills.Kills,
				Kills24h:         kills.Kills24h,
				Kills1h:          kills.Kills1h,
				FinisherDamage:   kills.FinisherDamage,
				Damage:           wc.Damage,
				DamageExact:      roundMetaScore(float64(baseWC.Damage) * balance.DamageScale),
				Cooldown:         roundMetaScore(wc.Cooldown),
				Range:            roundMetaScore(wc.Range),
				GridRange:        wc.GridRange,
				Special:          wc.Special,
				BaseDamage:       baseWC.Damage,
				BaseCooldown:     roundMetaScore(baseWC.Cooldown),
				DamageScale:      roundMetaScore(balance.DamageScale),
				CooldownScale:    roundMetaScore(balance.CooldownScale),
				AdjustmentScale:  roundMetaScore(balance.AdjustmentScale),
				DamageTrend:      damageTrend(balance.DamageScale),
				CooldownTrend:    cooldownTrend(balance.CooldownScale),
				LastDamageMove:   lastDamageMove,
				LastCooldownMove: lastCooldownMove,
				DamageShiftPct:   roundMetaScore((balance.DamageScale - 1) * 100),
				CooldownShiftPct: roundMetaScore((balance.CooldownScale - 1) * 100),
				RoundsTracked:    balance.RoundsTracked,
				LastBalanceAt:    balance.UpdatedAt,
				History:          history,
			},
			dps:   dps,
			reach: reach,
		}
		if balance.UpdatedAt.After(updatedAt) {
			updatedAt = balance.UpdatedAt
		}
		rawEntries = append(rawEntries, raw)
	}

	entries := make([]WeaponStatsEntry, 0, len(rawEntries))
	for _, raw := range rawEntries {
		metaScore := computeWeaponMetaScore(
			raw.entry,
			totalKills,
			maxKills24h,
			maxKills1h,
			normalize(raw.dps, minDPS, maxDPS),
			normalize(raw.reach, minReach, maxReach),
		)
		raw.entry.MetaScore = roundMetaScore(metaScore)
		entries = append(entries, raw.entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].MetaScore == entries[j].MetaScore {
			if entries[i].Kills == entries[j].Kills {
				return entries[i].Weapon < entries[j].Weapon
			}
			return entries[i].Kills > entries[j].Kills
		}
		return entries[i].MetaScore > entries[j].MetaScore
	})

	for i := range entries {
		entries[i].Rank = i + 1
		entries[i].Tier = weaponTierForRank(i + 1)
	}

	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	writeJSON(w, http.StatusOK, WeaponStatsResponse{
		Entries:   entries,
		UpdatedAt: updatedAt,
	})
}

func computeWeaponMetaScore(entry WeaponStatsEntry, totalKills, maxKills24h, maxKills1h int, dpsNorm, reachNorm float64) float64 {
	killShareScore := 0.0
	if totalKills > 0 {
		killShareScore = (float64(entry.Kills) / float64(totalKills)) * 52
	}

	recent24hScore := 0.0
	if maxKills24h > 0 {
		recent24hScore = (float64(entry.Kills24h) / float64(maxKills24h)) * 18
	}

	recent1hScore := 0.0
	if maxKills1h > 0 {
		recent1hScore = (float64(entry.Kills1h) / float64(maxKills1h)) * 8
	}

	throughputScore := dpsNorm * 16
	reachScore := reachNorm * 4
	balanceStateScore := normalize(entry.DamageScale/math.Max(0.1, entry.CooldownScale), 0.6, 1.5) * 2

	return killShareScore + recent24hScore + recent1hScore + throughputScore + reachScore + balanceStateScore
}

func normalize(v, minV, maxV float64) float64 {
	if maxV <= minV {
		return 0
	}
	if v < minV {
		v = minV
	}
	if v > maxV {
		v = maxV
	}
	return (v - minV) / (maxV - minV)
}

func roundMetaScore(v float64) float64 {
	return math.Round(v*100) / 100
}

func clampMetaScore(v, minV, maxV float64) float64 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func weaponTierForRank(rank int) string {
	switch {
	case rank <= 1:
		return "S"
	case rank <= 3:
		return "A"
	case rank <= 5:
		return "B"
	default:
		return "C"
	}
}

func damageTrend(scale float64) string {
	switch {
	case scale > 1.005:
		return "up"
	case scale < 0.995:
		return "down"
	default:
		return "flat"
	}
}

func cooldownTrend(scale float64) string {
	switch {
	case scale > 1.005:
		return "up"
	case scale < 0.995:
		return "down"
	default:
		return "flat"
	}
}

func balanceDirection(diffPct float64) string {
	switch {
	case diffPct > 1:
		return "nerfing"
	case diffPct < -1:
		return "buffing"
	default:
		return "steady"
	}
}
