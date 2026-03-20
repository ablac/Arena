package ws

import (
	"encoding/json"
	"fmt"

	"arena-server/internal/game"
)

// RawMessage is used for initial type dispatch of incoming JSON messages.
type RawMessage struct {
	Type string `json:"type"`
}

// LoadoutMessage represents a bot's loadout selection.
type LoadoutMessage struct {
	Type     string         `json:"type"`
	Weapon   string         `json:"weapon"`
	Stats    map[string]int `json:"stats"`
	Fallback string         `json:"fallback_behavior"`
}

// ActionMessage represents a per-tick action submitted by a bot.
type ActionMessage struct {
	Type           string     `json:"type"`
	Tick           int        `json:"tick"`
	Action         string     `json:"action"`
	Target         string     `json:"target,omitempty"`
	Direction      *game.Vec2 `json:"direction,omitempty"`
	ItemID         string     `json:"item_id,omitempty"`
	TargetPosition *game.Vec2 `json:"target_position,omitempty"`
}

// AuthMessage represents the initial authentication message from a bot.
type AuthMessage struct {
	Type   string `json:"type"`
	APIKey string `json:"api_key"`
}

// actionStringToType maps action name strings to game.ActionType constants.
var actionStringToType = map[string]game.ActionType{
	"move":             game.ActionMove,
	"move_to":          game.ActionMoveTo,
	"attack":           game.ActionAttack,
	"dodge":            game.ActionDodge,
	"shove":            game.ActionShove,
	"use_item":         game.ActionUseItem,
	"idle":             game.ActionIdle,
	"place_mine":       game.ActionPlaceMine,
	"use_gravity_well": game.ActionUseGravityWell,
	"grapple":          game.ActionGrapple,
}

// ParseBotMessage unmarshals raw JSON data from a bot into the appropriate
// typed message. It returns the message type string, the parsed message, and
// any error encountered during parsing.
func ParseBotMessage(data []byte) (msgType string, msg interface{}, err error) {
	var raw RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", nil, fmt.Errorf("invalid JSON: %w", err)
	}

	switch raw.Type {
	case "select_loadout":
		var lm LoadoutMessage
		if err := json.Unmarshal(data, &lm); err != nil {
			return raw.Type, nil, fmt.Errorf("invalid loadout message: %w", err)
		}
		return raw.Type, &lm, nil

	case "action":
		var am ActionMessage
		if err := json.Unmarshal(data, &am); err != nil {
			return raw.Type, nil, fmt.Errorf("invalid action message: %w", err)
		}
		return raw.Type, &am, nil

	case "auth":
		var auth AuthMessage
		if err := json.Unmarshal(data, &auth); err != nil {
			return raw.Type, nil, fmt.Errorf("invalid auth message: %w", err)
		}
		return raw.Type, &auth, nil

	default:
		return raw.Type, nil, fmt.Errorf("unknown message type: %q", raw.Type)
	}
}

// ActionMessageToAction converts a parsed ActionMessage into a game.Action
// that the engine can process.
func ActionMessageToAction(msg *ActionMessage) *game.Action {
	actionType, ok := actionStringToType[msg.Action]
	if !ok {
		actionType = game.ActionIdle
	}

	action := &game.Action{
		Type:           actionType,
		TargetID:       msg.Target,
		ItemID:         msg.ItemID,
		TargetPosition: msg.TargetPosition,
	}

	if msg.Direction != nil {
		action.Direction = *msg.Direction
	}

	return action
}
