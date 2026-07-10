package demobots

import (
	"fmt"
	"math"
	"testing"
)

type decisionScorecard struct {
	attackOpportunities int
	hazardEscapes       int
	survivalChoices     int
	invalidActions      int
	totalActions        int
}

func scorecardTick(position [2]float64, hp float64, weaponReady bool, dodgeCooldown int, extraEntities ...map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "tick",
		"tick": float64(100),
		"your_state": map[string]interface{}{
			"position":       []interface{}{position[0], position[1]},
			"hp":             hp,
			"max_hp":         float64(100),
			"weapon_ready":   weaponReady,
			"dodge_cooldown": float64(dodgeCooldown),
			"shove_cooldown": float64(20),
			"brace_ready":    true,
		},
		"nearby_entities": func() []interface{} {
			entities := make([]interface{}, len(extraEntities))
			for i := range extraEntities {
				entities[i] = extraEntities[i]
			}
			return entities
		}(),
	}
}

func scorecardEnemy(id, weapon string, position [2]float64) map[string]interface{} {
	return map[string]interface{}{
		"type": "bot", "id": id, "position": []interface{}{position[0], position[1]},
		"hp": float64(100), "max_hp": float64(100), "weapon": weapon,
		"is_alive": true, "has_los": true, "can_attack": true,
	}
}

func scorecardPickup(id, pickupType string, position [2]float64) map[string]interface{} {
	return map[string]interface{}{
		"type": "pickup", "id": id, "pickup_type": pickupType,
		"position": []interface{}{position[0], position[1]},
	}
}

func validateDecisionAction(action actionResult) error {
	validDirection := func(direction *[2]float64) bool {
		if direction == nil || *direction == [2]float64{} {
			return false
		}
		for _, component := range direction {
			if component < -1 || component > 1 || math.IsNaN(component) || math.IsInf(component, 0) {
				return false
			}
		}
		return true
	}

	switch action.Action {
	case "idle", "place_mine":
		return nil
	case "move", "dodge":
		if !validDirection(action.Direction) {
			return fmt.Errorf("%s requires a finite non-zero unit direction", action.Action)
		}
	case "attack":
		if action.Target == "" && action.TargetPosition == nil {
			return fmt.Errorf("attack requires a target or target_position")
		}
	case "shove":
		if action.Target == "" {
			return fmt.Errorf("shove requires a target")
		}
	case "grapple":
		if action.Target == "" && action.TargetPosition == nil {
			return fmt.Errorf("grapple requires a target or target_position")
		}
	case "use_item":
		if action.ItemID == "" {
			return fmt.Errorf("use_item requires an item_id")
		}
	case "use_gravity_well":
		if action.TargetPosition == nil {
			return fmt.Errorf("use_gravity_well requires target_position")
		}
	default:
		return fmt.Errorf("unknown action %q", action.Action)
	}
	return nil
}

// TestDemoBotDecisionScorecard runs the same deterministic tactical situations
// across every weapon. It is deliberately outcome-oriented: a branch only
// earns credit when it converts a ready hit, reduces hazard exposure, or
// chooses immediate survival, and every emitted action must satisfy the public
// bot protocol shape.
func TestDemoBotDecisionScorecard(t *testing.T) {
	setTerrain(t, 30, 30, nil)
	weapons := []string{"sword", "bow", "daggers", "shield", "spear", "staff", "grapple"}
	strategies := map[string]string{
		"sword": "defensive", "bow": "kite", "daggers": "assassin",
		"shield": "territorial", "spear": "aggressive", "staff": "kite", "grapple": "assassin",
	}
	card := decisionScorecard{}

	for _, weapon := range weapons {
		strategy := strategies[weapon]
		rangeTiles := int(WeaponRanges[weapon])

		// A harmless adjacent boost is tempting, but a ready enemy in range is
		// the time-sensitive opportunity. Emergency healing is scored below.
		attackMsg := scorecardTick([2]float64{5, 5}, 100, true, 20,
			scorecardEnemy("enemy", "sword", [2]float64{6, 5}),
			scorecardPickup("boost", "speed_boost", [2]float64{5, 5}),
		)
		attack := PickAction(strategy, attackMsg, weapon, rangeTiles, "score-"+weapon)
		card.totalActions++
		if err := validateDecisionAction(attack); err != nil {
			card.invalidActions++
			t.Errorf("%s attack scenario emitted invalid action %+v: %v", weapon, attack, err)
		}
		if attack.Action == "attack" && (attack.Target == "enemy" || attack.TargetPosition != nil) {
			card.attackOpportunities++
		} else {
			t.Errorf("%s attack scenario = %+v, want attack before optional pickup", weapon, attack)
		}

		hazardMsg := scorecardTick([2]float64{15, 15}, 100, false, 20,
			map[string]interface{}{
				"type": "hazard_zone", "position": []interface{}{float64(15), float64(15)},
				"width": float64(5), "height": float64(5), "active": true,
			},
		)
		hazard := PickAction(strategy, hazardMsg, weapon, rangeTiles, "score-"+weapon)
		card.totalActions++
		if err := validateDecisionAction(hazard); err != nil {
			card.invalidActions++
			t.Errorf("%s hazard scenario emitted invalid action %+v: %v", weapon, hazard, err)
		}
		if hazard.Action == "move" && hazard.Direction != nil {
			danger := &dangerSet{}
			danger.reset()
			danger.addRect([2]float64{15, 15}, 5, 5)
			startDistance := dangerEscapeDistance(15, 15, danger, getTerrain())
			endDistance := dangerEscapeDistance(
				15+int(hazard.Direction[0]), 15+int(hazard.Direction[1]), danger, getTerrain(),
			)
			if endDistance < startDistance {
				card.hazardEscapes++
			} else {
				t.Errorf("%s hazard move %v did not reduce escape distance %d -> %d", weapon, *hazard.Direction, startDistance, endDistance)
			}
		} else {
			t.Errorf("%s hazard scenario = %+v, want non-zero move while dodge cools down", weapon, hazard)
		}

		survivalMsg := scorecardTick([2]float64{5, 5}, 20, true, 0,
			scorecardEnemy("enemy", "sword", [2]float64{6, 5}),
			scorecardPickup("heal", "health_pack", [2]float64{5, 5}),
		)
		survival := PickAction(strategy, survivalMsg, weapon, rangeTiles, "score-"+weapon)
		card.totalActions++
		if err := validateDecisionAction(survival); err != nil {
			card.invalidActions++
			t.Errorf("%s survival scenario emitted invalid action %+v: %v", weapon, survival, err)
		}
		if survival.Action == "use_item" && survival.ItemID == "heal" {
			card.survivalChoices++
		} else {
			t.Errorf("%s survival scenario = %+v, want immediate health use", weapon, survival)
		}
	}

	t.Logf("demo AI decision scorecard: attacks=%d/%d hazards=%d/%d survival=%d/%d invalid=%d/%d",
		card.attackOpportunities, len(weapons), card.hazardEscapes, len(weapons),
		card.survivalChoices, len(weapons), card.invalidActions, card.totalActions)
	if card.attackOpportunities != len(weapons) || card.hazardEscapes != len(weapons) ||
		card.survivalChoices != len(weapons) || card.invalidActions != 0 {
		t.Fatalf("decision scorecard below required tactical floor: %+v", card)
	}
}
