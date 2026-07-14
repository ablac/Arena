// Tick parsing: converts the server's tick JSON into the typed
// tickState consumed by every tactical decision.
package demobots

import (
	"math"
)

// === Tick Parsing ===

func parseTick(msg map[string]interface{}) tickState {
	ts := tickState{
		ZoneCenter: [2]float64{1000, 1000}, ZoneRadius: 1000,
		ZoneTargetCenter: [2]float64{1000, 1000}, ZoneTargetRadius: 500,
		MaxHP: 150, InZone: true, LastActionOK: true,
	}
	if v, ok := msg["tick"].(float64); ok {
		ts.Tick = int(v)
	}
	if v, ok := msg["round_tick"].(float64); ok {
		ts.RoundTick = int(v)
	}
	if v, ok := msg["round_modifier"].(string); ok {
		ts.RoundModifier = v
		ts.FastZone = v == "fast_zone"
		ts.PickupSurge = v == "pickup_surge"
		ts.DoubleBounty = v == "double_bounty"
		ts.TeleportSurge = v == "teleport_surge"
		ts.HazardStorm = v == "hazard_storm"
	}
	if v, ok := msg["game_mode"].(string); ok {
		ts.Mode = v
	}
	if v, ok := msg["sudden_death"].(bool); ok {
		ts.SuddenDeath = v
	}
	if v, ok := msg["sudden_death_stall"].(bool); ok {
		ts.SuddenDeathStall = v
	}
	if v, ok := msg["bounty_target"].(string); ok {
		ts.BountyTargetID = v
	}
	if vt, ok := msg["void_tiles"].([]interface{}); ok {
		for _, raw := range vt {
			if cell, ok := raw.([]interface{}); ok && len(cell) >= 2 {
				x, _ := cell[0].(float64)
				y, _ := cell[1].(float64)
				ts.VoidTiles = append(ts.VoidTiles, [2]int{int(x), int(y)})
			}
		}
	}
	if scores, ok := msg["team_scores"].(map[string]interface{}); ok {
		ts.TeamScores = make(map[string]int, len(scores))
		for k, raw := range scores {
			if v, ok := raw.(float64); ok {
				ts.TeamScores[k] = int(v)
			}
		}
	}
	if flags, ok := msg["flags"].([]interface{}); ok {
		for _, raw := range flags {
			f, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			// BuildFlagView sends spectator-style world coordinates, so
			// convert to grid tiles like everything else the AI reasons in.
			ent := entity{Type: "flag", Position: worldToGridPos(parsePos(f["position"])), IsAlive: true}
			if v, ok := f["id"].(string); ok {
				ent.ID = v
			}
			if v, ok := f["team"].(float64); ok {
				ent.Team = int(v)
			}
			if v, ok := f["base_position"]; ok {
				ent.BasePosition = worldToGridPos(parsePos(v))
			}
			if v, ok := f["status"].(string); ok {
				ent.Status = v
			}
			if v, ok := f["carrier_id"].(string); ok {
				ent.CarrierID = v
			}
			ts.Flags = append(ts.Flags, ent)
		}
	}
	if ys, ok := msg["your_state"].(map[string]interface{}); ok {
		ts.Position = parsePos(ys["position"])
		if v, ok := ys["speed"].(float64); ok {
			ts.Speed = v
		}
		if v, ok := ys["team"].(float64); ok {
			ts.Team = int(v)
		}
		if v, ok := ys["hp"].(float64); ok {
			ts.HP = v
		}
		if v, ok := ys["max_hp"].(float64); ok {
			ts.MaxHP = v
		}
		if v, ok := ys["weapon_ready"].(bool); ok {
			ts.WeaponReady = v
		}
		if v, ok := ys["cooldown_remaining"].(float64); ok {
			ts.Cooldown = v
		}
		if v, ok := ys["dodge_cooldown"].(float64); ok {
			ts.DodgeCool = int(v)
		}
		if v, ok := ys["shove_cooldown"].(float64); ok {
			ts.ShoveCool = v
		}
		if v, ok := ys["invuln_ticks"].(float64); ok {
			ts.InvulnTicks = int(v)
		}
		if v, ok := ys["stun_ticks"].(float64); ok {
			ts.StunTicks = int(v)
		}
		if v, ok := ys["shield_absorb"].(float64); ok {
			ts.ShieldHP = v
		}
		if v, ok := ys["in_safe_zone"].(bool); ok {
			ts.InZone = v
		}
		if v, ok := ys["distance_to_zone_edge"].(float64); ok {
			ts.ZoneDist = v
		}
		if v, ok := ys["zone_center"]; ok {
			ts.ZoneCenter = parsePos(v)
		}
		if v, ok := ys["zone_radius"].(float64); ok {
			ts.ZoneRadius = v
		}
		if v, ok := ys["zone_target_center"]; ok {
			ts.ZoneTargetCenter = parsePos(v)
		}
		if v, ok := ys["zone_target_radius"].(float64); ok {
			ts.ZoneTargetRadius = v
		}
		if v, ok := ys["kill_streak"].(float64); ok {
			ts.KillStreak = int(v)
		}
		if v, ok := ys["round_kills"].(float64); ok {
			ts.RoundKills = int(v)
		}
		if hits, ok := ys["hits_received"].([]interface{}); ok {
			ts.HitsThisTick = len(hits)
		}
		if lar, ok := ys["last_action_result"].(map[string]interface{}); ok {
			if v, ok := lar["success"].(bool); ok {
				ts.LastActionOK = v
			}
		}
		if effs, ok := ys["effects"].([]interface{}); ok {
			for _, raw := range effs {
				if e, ok := raw.(map[string]interface{}); ok {
					if name, ok := e["name"].(string); ok {
						if name == "speed_boost" {
							ts.HasSpeedBoost = true
						}
						if name == "damage_boost" {
							ts.HasDmgBoost = true
						}
						if name == "hazard_key" {
							ts.HasHazardKey = true
						}
						if name == "relay_battery" {
							ts.HasRelayBattery = true
						}
					}
				}
			}
		}
		if v, ok := ys["gravity_well_charge"].(float64); ok {
			ts.GravityWellCharge = int(v)
		}
		if v, ok := ys["grapple_charges"].(float64); ok {
			ts.GrappleCharges = int(v)
		}
		if v, ok := ys["grapple_cooldown"].(float64); ok {
			ts.GrappleCooldown = v
		}
		if v, ok := ys["is_bounty_target"].(bool); ok {
			ts.IsBountyTarget = v
		}
		if v, ok := ys["brace_ready"].(bool); ok {
			ts.BraceReady = v
		}
		if v, ok := ys["bow_charge_ticks"].(float64); ok {
			ts.BowChargeTicks = int(v)
		}
		if v, ok := ys["bow_charge_level"].(float64); ok {
			ts.BowChargeLevel = v
		}
		if v, ok := ys["charged_shot_ready"].(bool); ok {
			ts.ChargedShotReady = v
		}
		if v, ok := ys["mine_count"].(float64); ok {
			ts.MineCount = int(v)
		}
	}
	if v, ok := msg["nearby_mines"].(float64); ok {
		ts.NearbyMines = int(v)
	}
	if sz, ok := msg["safe_zone"].(map[string]interface{}); ok {
		if v, ok := sz["center"]; ok {
			ts.ZoneCenter = parsePos(v)
		}
		if v, ok := sz["radius"].(float64); ok {
			ts.ZoneRadius = v
		}
		if v, ok := sz["target_center"]; ok {
			ts.ZoneTargetCenter = parsePos(v)
		}
		if v, ok := sz["target_radius"].(float64); ok {
			ts.ZoneTargetRadius = v
		}
	}
	if ne, ok := msg["nearby_entities"].([]interface{}); ok {
		for _, raw := range ne {
			e, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			ent := entity{Position: parsePos(e["position"]), IsAlive: true}
			if v, ok := e["type"].(string); ok {
				ent.Type = v
			}
			if v, ok := e["id"].(string); ok {
				ent.ID = v
			}
			if v, ok := e["pickup_id"].(string); ok && ent.ID == "" {
				ent.ID = v
			}
			if v, ok := e["bot_id"].(string); ok && ent.ID == "" {
				ent.ID = v
			}
			if v, ok := e["hp"].(float64); ok {
				ent.HP = v
			}
			if v, ok := e["max_hp"].(float64); ok {
				ent.MaxHP = v
			}
			if v, ok := e["weapon"].(string); ok {
				ent.Weapon = v
			}
			if v, ok := e["is_alive"].(bool); ok {
				ent.IsAlive = v
			}
			if v, ok := e["is_stunned"].(bool); ok {
				ent.Stunned = v
			}
			if v, ok := e["is_dodging"].(bool); ok {
				ent.Dodging = v
			}
			if v, ok := e["target_id"].(string); ok {
				ent.TargetID = v
			}
			if v, ok := e["facing"]; ok {
				ent.Facing = parsePos(v)
			}
			if v, ok := e["pickup_type"].(string); ok {
				ent.SubType = v
			}
			if v, ok := e["radius"].(float64); ok {
				ent.Radius = v
			}
			if v, ok := e["linked_pad_id"].(string); ok {
				ent.LinkedID = v
			}
			if v, ok := e["color"].(string); ok {
				ent.Color = v
			}
			if v, ok := e["is_ready"].(bool); ok {
				ent.Ready = v
			} else {
				ent.Ready = true
			}
			if v, ok := e["cooldown_remaining_ticks"].(float64); ok {
				ent.Cooldown = int(v)
			}
			if v, ok := e["owner_id"].(string); ok {
				ent.OwnerID = v
			}
			if v, ok := e["capturing_bot_id"].(string); ok {
				ent.CapturingBotID = v
			}
			if v, ok := e["progress_ticks"].(float64); ok {
				ent.ProgressTicks = int(v)
			}
			if v, ok := e["capture_ticks"].(float64); ok {
				ent.CaptureTicks = int(v)
			}
			if v, ok := e["contender_count"].(float64); ok {
				ent.ContenderCount = int(v)
			}
			if v, ok := e["has_los"].(bool); ok {
				ent.HasLOS = v
			} else {
				ent.HasLOS = true
			}
			if v, ok := e["can_attack"].(bool); ok {
				ent.CanAttack = v
			}
			if v, ok := e["active"].(bool); ok {
				ent.Active = v
			}
			if v, ok := e["is_contested"].(bool); ok {
				ent.Contested = v
			}
			if v, ok := e["recently_disrupted_ticks"].(float64); ok {
				ent.DisruptedTicks = int(v)
			}
			if v, ok := e["brace_ready"].(bool); ok {
				ent.BraceReady = v
			}
			if v, ok := e["bow_charge_level"].(float64); ok {
				ent.BowChargeLevel = v
			}
			if v, ok := e["charged_shot_ready"].(bool); ok {
				ent.ChargedShotReady = v
			}
			if v, ok := e["rear_exposed"].(bool); ok {
				ent.RearExposed = v
			}
			if v, ok := e["near_impact_surface"].(bool); ok {
				ent.NearImpactSurface = v
			}
			if v, ok := e["team"].(float64); ok {
				ent.Team = int(v)
			}
			if v, ok := e["threat_score"].(float64); ok {
				ent.ThreatScore = v
			}
			if v, ok := e["width"].(float64); ok {
				ent.Width = int(v)
			}
			if v, ok := e["height"].(float64); ok {
				ent.Height = int(v)
			}
			if v, ok := e["on_ticks"].(float64); ok {
				ent.OnTicks = int(v)
			}
			if v, ok := e["off_ticks"].(float64); ok {
				ent.OffTicks = int(v)
			}
			if v, ok := e["tick_counter"].(float64); ok {
				ent.TickCounter = int(v)
			}
			if v, ok := e["damage_per_tick"].(float64); ok {
				ent.DamagePerTick = v
			}
			if v, ok := e["armed"].(bool); ok {
				ent.Armed = v
			}
			if v, ok := e["pull_radius"].(float64); ok {
				ent.PullRadius = int(v)
			}
			switch ent.Type {
			case "bot":
				if ent.IsAlive {
					// Teammates are allies, everyone else is an enemy.
					if ent.Team != 0 && ent.Team == ts.Team {
						ts.Allies = append(ts.Allies, ent)
					} else {
						ts.Enemies = append(ts.Enemies, ent)
					}
				}
			case "bounty_target":
				ts.Enemies = append(ts.Enemies, ent)
			case "pickup":
				ts.Pickups = append(ts.Pickups, ent)
			case "teleport_pad", "teleporter":
				ts.Teleporters = append(ts.Teleporters, ent)
			case "capture_pad":
				ts.CapturePads = append(ts.CapturePads, ent)
			case "hazard_zone":
				ts.HazardZones = append(ts.HazardZones, ent)
			case "burn_field":
				ts.HazardZones = append(ts.HazardZones, ent)
			case "landmine":
				ts.Mines = append(ts.Mines, ent)
			case "gravity_well":
				ts.GravityWells = append(ts.GravityWells, ent)
			}
		}
	}
	if h, ok := msg["hints"].([]interface{}); ok {
		for _, raw := range h {
			if hm, ok := raw.(map[string]interface{}); ok {
				hi := hint{}
				if v, ok := hm["hint_type"].(string); ok {
					hi.HintType = v
				}
				if v, ok := hm["direction"]; ok {
					hi.Direction = parsePos(v)
				}
				if v, ok := hm["distance"].(float64); ok {
					hi.Distance = v
				}
				if v, ok := hm["pickup_type"].(string); ok {
					hi.PickupType = v
				}
				ts.Hints = append(ts.Hints, hi)
			}
		}
	}
	return ts
}

// worldToGridPos converts a world-space position to grid tile coordinates,
// mirroring the server's TerrainGrid.WorldToGrid floor division.
func worldToGridPos(p [2]float64) [2]float64 {
	cs := 20.0
	if t := getTerrain(); t != nil && t.CellSize > 0 {
		cs = t.CellSize
	}
	return [2]float64{math.Floor(p[0] / cs), math.Floor(p[1] / cs)}
}

func parsePos(v interface{}) [2]float64 {
	switch p := v.(type) {
	case []interface{}:
		if len(p) >= 2 {
			x, _ := p[0].(float64)
			y, _ := p[1].(float64)
			return [2]float64{x, y}
		}
	case [2]float64:
		return p
	}
	return [2]float64{0, 0}
}
