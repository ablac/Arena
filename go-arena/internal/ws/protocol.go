package ws

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"arena-server/internal/config"
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
	Charged        bool       `json:"charged,omitempty"`
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
	if err := rejectDuplicateJSONFields(data); err != nil {
		return "", nil, fmt.Errorf("invalid JSON: %w", err)
	}
	var raw RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", nil, fmt.Errorf("invalid JSON: %w", err)
	}

	switch raw.Type {
	case "select_loadout":
		var lm LoadoutMessage
		if err := unmarshalStrict(data, &lm); err != nil {
			return raw.Type, nil, fmt.Errorf("invalid loadout message: %w", err)
		}
		return raw.Type, &lm, nil

	case "action":
		var am ActionMessage
		if err := unmarshalStrict(data, &am); err != nil {
			return raw.Type, nil, fmt.Errorf("invalid action message: %w", err)
		}
		return raw.Type, &am, nil

	case "auth":
		var auth AuthMessage
		if err := unmarshalStrict(data, &auth); err != nil {
			return raw.Type, nil, fmt.Errorf("invalid auth message: %w", err)
		}
		return raw.Type, &auth, nil

	default:
		return raw.Type, nil, fmt.Errorf("unknown message type: %q", raw.Type)
	}
}

func rejectDuplicateJSONFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("invalid JSON object key")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("invalid JSON array")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func unmarshalStrict(data []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

// ActionMessageToAction converts a parsed ActionMessage into a game.Action
// that the engine can process.
func ActionMessageToAction(msg *ActionMessage) *game.Action {
	if err := validateActionMessage(msg); err != nil {
		return nil
	}
	actionType := actionStringToType[msg.Action]

	action := &game.Action{
		Type:           actionType,
		TargetID:       msg.Target,
		ItemID:         msg.ItemID,
		TargetPosition: msg.TargetPosition,
		Charged:        msg.Charged,
	}

	if msg.Direction != nil {
		action.Direction = *msg.Direction
	}

	return action
}

func validateActionMessage(msg *ActionMessage) error {
	if msg == nil {
		return fmt.Errorf("action message is required")
	}
	if msg.Tick <= 0 {
		return fmt.Errorf("tick must be a positive server tick")
	}
	if _, ok := actionStringToType[msg.Action]; !ok {
		return fmt.Errorf("unknown action %q", msg.Action)
	}
	if msg.Direction != nil {
		if !finiteVector(*msg.Direction) || math.Abs(msg.Direction.X()) > 1 || math.Abs(msg.Direction.Y()) > 1 {
			return fmt.Errorf("direction must contain finite components between -1 and 1")
		}
	}
	if msg.TargetPosition != nil {
		if err := validateTargetPosition(*msg.TargetPosition); err != nil {
			return err
		}
	}

	switch msg.Action {
	case "move", "dodge":
		if msg.Direction == nil || msg.Direction.Length() < 1e-10 {
			return fmt.Errorf("%s requires a non-zero direction", msg.Action)
		}
	case "move_to", "use_gravity_well":
		if msg.TargetPosition == nil {
			return fmt.Errorf("%s requires target_position", msg.Action)
		}
	case "attack":
		hasTarget := msg.Target != ""
		hasPosition := msg.TargetPosition != nil
		if hasTarget == hasPosition {
			return fmt.Errorf("attack requires exactly one of target or target_position")
		}
	case "shove":
		if msg.Target == "" {
			return fmt.Errorf("shove requires target")
		}
	case "use_item":
		if msg.ItemID == "" {
			return fmt.Errorf("use_item requires item_id")
		}
	case "grapple":
		hasTarget := msg.Target != ""
		hasPosition := msg.TargetPosition != nil
		if hasTarget == hasPosition {
			return fmt.Errorf("grapple requires exactly one of target or target_position")
		}
	}
	return nil
}

func finiteVector(v game.Vec2) bool {
	return !math.IsNaN(v.X()) && !math.IsInf(v.X(), 0) &&
		!math.IsNaN(v.Y()) && !math.IsInf(v.Y(), 0)
}

func validateTargetPosition(pos game.Vec2) error {
	if !finiteVector(pos) {
		return fmt.Errorf("target_position must contain finite coordinates")
	}
	maxScale := config.C.ArenaSizeMaxScale
	if maxScale < 1 {
		maxScale = 1
	}
	maxX := config.C.ArenaWidth*maxScale + config.C.PathfindingCellSize
	maxY := config.C.ArenaHeight*maxScale + config.C.PathfindingCellSize
	if pos.X() < 0 || pos.Y() < 0 || pos.X() > maxX || pos.Y() > maxY {
		return fmt.Errorf("target_position is outside the maximum arena bounds")
	}
	return nil
}
