package game

import (
	"encoding/json"
	"log/slog"
	"math"
	"sort"

	"arena-server/internal/config"

	"github.com/gorilla/websocket"
)

// marshalJSON is a package-level helper that serialises v to JSON bytes.
func marshalJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// SendToBot marshals msg to JSON and sends it to the bot's write channel.
// The send is non-blocking: if the channel is full the message is silently
// dropped.
func SendToBot(bot *BotState, msg interface{}) {
	if bot.SendChan == nil {
		return
	}
	data, err := marshalJSON(msg)
	if err != nil {
		slog.Error("failed to marshal bot message", "bot_id", bot.BotID, "error", err)
		return
	}
	safeSend(bot.SendChan, data)
}

// SafeZoneGridView is the safe_zone submap of the tick envelope, expressed in
// grid tiles. It is identical for every bot in a tick, so the engine builds
// it once per tick and shares the value across all envelopes.
type SafeZoneGridView struct {
	Center       [2]int `json:"center"`
	Radius       int    `json:"radius"`
	TargetCenter [2]int `json:"target_center"`
	TargetRadius int    `json:"target_radius"`
}

// BuildSafeZoneGridView builds the shared per-tick safe_zone view.
func BuildSafeZoneGridView(arena *ArenaMap) SafeZoneGridView {
	cellSize := config.C.PathfindingCellSize
	return SafeZoneGridView{
		Center:       posToGrid(arena.ZoneCenter),
		Radius:       int(math.Round(arena.ZoneRadius / cellSize)),
		TargetCenter: posToGrid(arena.ZoneTargetCenter),
		TargetRadius: int(math.Round(arena.ZoneTargetRadius / cellSize)),
	}
}

// HintView is a directional hint sent to bots with nothing in view radius.
// PickupType is set only on pickup hints, matching the previous map payloads
// where bot hints carried no pickup_type key.
type HintView struct {
	HintType   string     `json:"hint_type"`
	PickupType string     `json:"pickup_type,omitempty"`
	Direction  [2]float64 `json:"direction"`
	Distance   float64    `json:"distance"`
}

// TickMessage is the typed per-tick envelope sent to each bot. All fields are
// value snapshots built under the engine lock; the tick goroutine marshals
// the message after the lock is released.
//
// Presence semantics (wire parity with the previous map-based envelope):
//   - Hints is a pointer so the key appears exactly when hints were built
//     (non-nil), never for an inapplicable tick.
//   - VoidTiles is a pointer-to-slice: present (possibly []) whenever sudden
//     death is active, absent otherwise. Bare omitempty would wrongly drop
//     the authoritative empty list.
//   - ServiceStatus appears only while a maintenance notice is active.
//   - TeamScores/Flags appear only in team modes; Flags is a pointer so team
//     modes with zero flags still serialize "flags":[].
type TickMessage struct {
	Type           string           `json:"type"`
	Tick           int              `json:"tick"`
	TickNumber     int              `json:"tick_number"`
	YourState      *YourStateView   `json:"your_state"`
	NearbyEntities []any            `json:"nearby_entities"`
	FogRadius      int              `json:"fog_radius"`
	SafeZone       SafeZoneGridView `json:"safe_zone"`
	Hints          *[]HintView      `json:"hints,omitempty"`

	// Per-tick extras (previously the variadic extra map).
	SuddenDeath      bool           `json:"sudden_death"`
	SuddenDeathStall bool           `json:"sudden_death_stall"`
	BountyTarget     string         `json:"bounty_target"`
	NearbyMines      int            `json:"nearby_mines"`
	RoundTick        int            `json:"round_tick"`
	RoundModifier    string         `json:"round_modifier"`
	ServiceStatus    *ServiceStatus `json:"service_status,omitempty"`
	VoidTiles        *[][2]int      `json:"void_tiles,omitempty"`

	// Game-mode fields (previously merged by AddModeTickExtra).
	GameMode   string         `json:"game_mode"`
	TeamScores map[string]int `json:"team_scores,omitempty"`
	Flags      *[]FlagView    `json:"flags,omitempty"`
}

// NewTickMessage assembles the base tick envelope. hints is optional — when
// non-nil it provides directional hints to far-away bots and pickups (only
// sent when no bots are within view radius). safeZone is passed in prebuilt
// because it is identical for every bot in a tick.
func NewTickMessage(yourState *YourStateView, nearbyEntities []any, tickCount int, safeZone SafeZoneGridView, hints []HintView, fogRadius int) *TickMessage {
	msg := &TickMessage{
		Type:           "tick",
		Tick:           tickCount,
		TickNumber:     tickCount,
		YourState:      yourState,
		NearbyEntities: nearbyEntities,
		FogRadius:      fogRadius,
		SafeZone:       safeZone,
	}
	if hints != nil {
		msg.Hints = &hints
	}
	return msg
}

// SendTickUpdate marshals a prepared tick envelope and queues it on the bot's
// coalescing tick channel. The engine calls this after releasing the engine
// lock; the message must therefore already be a self-contained snapshot.
func SendTickUpdate(bot *BotState, msg *TickMessage) {
	if bot.TickChan == nil {
		// Compatibility for tests and non-network callers that construct a
		// BotState without the production transport split.
		SendToBot(bot, msg)
		return
	}
	data, err := marshalJSON(msg)
	if err != nil {
		slog.Error("failed to marshal bot tick", "bot_id", bot.BotID, "error", err)
		return
	}
	safeReplaceLatest(bot.TickChan, data)
}

// SendDeathMessage notifies a bot that it has died.
func SendDeathMessage(bot *BotState, event DeathEvent) {
	msg := map[string]interface{}{
		"type":                 "death",
		"killed_by":            event.KillerID,
		"killer_name":          event.KillerName,
		"weapon_used":          event.Weapon,
		"damage":               event.Damage,
		"your_kills_this_life": event.VictimKills,
		"respawn":              false,
	}
	SendToBot(bot, msg)
}

// SendKillMessage notifies a bot that it scored a kill.
func SendKillMessage(bot *BotState, event KillEvent) {
	msg := map[string]interface{}{
		"type":             "kill",
		"victim_name":      event.VictimName,
		"victim_id":        event.VictimID,
		"weapon_used":      event.Weapon,
		"damage":           event.Damage,
		"your_kill_streak": event.KillStreak,
		"your_round_kills": event.RoundKills,
	}
	SendToBot(bot, msg)
}

// SendRoundEnd notifies a bot that the round has ended.
func SendRoundEnd(bot *BotState, info RoundEndInfo, nextRoundIn float64) {
	// The active tick is queued before the engine evaluates the round-ending
	// condition. Drop any snapshot the writer has not already claimed so a
	// control-priority writer can never deliver round_end followed by stale
	// alive state from the completed round.
	discardPendingTick(bot.TickChan)
	msg := map[string]interface{}{
		"type":         "round_end",
		"round_number": info.RoundNumber,
		"your_stats": map[string]interface{}{
			"kills":  bot.RoundKills,
			"deaths": bot.RoundDeaths,
			"damage": bot.RoundDamageDealt,
		},
		"round_winner":  info.WinnerName,
		"next_round_in": nextRoundIn,
	}
	SendToBot(bot, msg)
}

func discardPendingTick(ch chan []byte) {
	if ch == nil {
		return
	}
	select {
	case <-ch:
	default:
	}
}

// SendRoundStart notifies a bot that a new round has begun.
// Terrain is available via GET /api/v1/arena/map (pre-generated during intermission).
func SendRoundStart(bot *BotState, round RoundState, bots map[string]*BotState, arena *ArenaMap) {
	gridPos := posToGrid(bot.Position)
	cellSize := config.C.PathfindingCellSize
	zoneCenter := posToGrid(arena.ZoneCenter)
	zoneTargetCenter := posToGrid(arena.ZoneTargetCenter)

	msg := map[string]interface{}{
		"type":                 "round_start",
		"round_number":         round.RoundNumber,
		"round_modifier":       string(round.Modifier),
		"round_modifier_label": round.Modifier.Label(),
		"position":             [2]int{gridPos[0], gridPos[1]},
		"bots_in_round":        len(bots),
		// Older clients expect this object to exist. Expose only the receiver's
		// already-known position; restoring opponent entries would recreate the
		// round-start radar leak that the fairness boundary removed.
		"all_positions": map[string]interface{}{
			bot.BotID: [2]int{gridPos[0], gridPos[1]},
		},
		"safe_zone": map[string]interface{}{
			"center":        [2]int{zoneCenter[0], zoneCenter[1]},
			"radius":        int(math.Round(arena.ZoneRadius / cellSize)),
			"target_center": [2]int{zoneTargetCenter[0], zoneTargetCenter[1]},
			"target_radius": int(math.Round(arena.ZoneTargetRadius / cellSize)),
		},
	}
	SendToBot(bot, msg)
}

// RoundWinnerView is the winner submap of the spectator round_end envelope.
// Color is the bot's avatar color so the client can tint the winner banner
// without a roster lookup.
type RoundWinnerView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// NextMapView previews the NEXT round's pre-generated terrain for the
// spectator intermission show (issue #189). Obstacles carries exactly what
// the next round's first keyframe will carry (expanded + grid-snapped, mask
// rects appended — see ExpandObstaclesForClient) so the client can hand its
// renderers the data ahead of time and the keyframe swap is a no-op.
type NextMapView struct {
	Shape     string     `json:"shape"`
	ArenaSize [2]float64 `json:"arena_size"`
	Obstacles []Obstacle `json:"obstacles"`
	MaskRects []Obstacle `json:"mask_rects,omitempty"`
	// SafeZone previews the next round's OPENING safe-zone placement in the
	// exact SafeZoneSpectatorView encoding the round's first arena_state
	// keyframe will carry: center + initial radius follow deterministically
	// from the post-resize config, and the drift target is pre-picked at
	// endRound and reused verbatim by startRound (issue #192). The client's
	// intermission show glides the target ring to this placement so the
	// keyframe swap is a visual no-op. Pointer so pre-#192 payload shape is
	// preserved should staging ever lack it.
	SafeZone *SafeZoneSpectatorView `json:"safe_zone,omitempty"`
}

// RoundEndSpectatorMessage is the typed spectator broadcast staged by
// endRound: winner announcement plus the next map, so clients can stage the
// between-round show during intermission. Same precedent as TickMessage —
// value snapshot built under the engine lock, marshaled in flushTickOutbox
// after the lock is released.
//
// Presence semantics: Winner is a pointer so the key is absent when the
// round resolved without a winner (no bots left to rank); NextMap is a
// pointer for the same reason on the (never-expected) path where terrain
// pre-generation is unavailable.
type RoundEndSpectatorMessage struct {
	Type             string           `json:"type"`
	RoundNumber      int              `json:"round_number"`
	Winner           *RoundWinnerView `json:"winner,omitempty"`
	IntermissionSecs float64          `json:"intermission_secs"`
	NextMap          *NextMapView     `json:"next_map,omitempty"`
}

// SendLobbyUpdate sends a lobby status message to a bot.
func SendLobbyUpdate(bot *BotState, connectedCount, minBots int, countdown *int, allBots map[string]*BotState) {
	data, err := buildLobbyUpdatePayload(connectedCount, minBots, countdown, allBots)
	if err != nil {
		slog.Error("failed to marshal lobby update", "bot_id", bot.BotID, "error", err)
		return
	}
	sendLobbyPayload(bot, data)
}

func buildLobbyUpdatePayload(connectedCount, minBots int, countdown *int, allBots map[string]*BotState) ([]byte, error) {
	return marshalLobbyUpdatePayload(connectedCount, minBots, countdown, buildLobbyPlayers(allBots))
}

func buildLobbyPlayers(allBots map[string]*BotState) []map[string]interface{} {
	players := make([]map[string]interface{}, 0, len(allBots))
	for _, b := range allBots {
		players = append(players, map[string]interface{}{
			"name":         b.Name,
			"avatar_color": b.AvatarColor,
			"weapon":       b.Weapon,
		})
	}
	sort.Slice(players, func(i, j int) bool {
		return players[i]["name"].(string) < players[j]["name"].(string)
	})
	return players
}

func marshalLobbyUpdatePayload(connectedCount, minBots int, countdown *int, players []map[string]interface{}) ([]byte, error) {
	var countdownVal interface{}
	if countdown != nil {
		countdownVal = *countdown
	}

	msg := map[string]interface{}{
		"type":           "lobby",
		"bots_connected": connectedCount,
		"bots_needed":    minBots,
		"countdown":      countdownVal,
		"players":        players,
	}
	return marshalJSON(msg)
}

// sameByteSlice reports whether a and b are the identical slice (same backing
// array and length) — an O(1) identity check, not a content comparison.
func sameByteSlice(a, b []byte) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

func sendLobbyPayload(bot *BotState, data []byte) {
	if bot.SendChan == nil {
		return
	}
	safeSend(bot.SendChan, data)
}

// BuildConnectedMessage returns the initial connection acknowledgement payload.
// Used by the bot handler to write directly before the writer goroutine starts.
func BuildConnectedMessage(bot *BotState, lastLoadout map[string]interface{}) map[string]interface{} {
	var loadout interface{}
	if lastLoadout != nil {
		loadout = lastLoadout
	}

	gridW := int(config.C.ArenaWidth / config.C.PathfindingCellSize)
	gridH := int(config.C.ArenaHeight / config.C.PathfindingCellSize)

	return map[string]interface{}{
		"type":              "connected",
		"bot_id":            bot.BotID,
		"arena_size":        [2]float64{config.C.ArenaWidth, config.C.ArenaHeight},
		"grid_size":         [2]int{gridW, gridH},
		"cell_size":         config.C.PathfindingCellSize,
		"fog_radius":        config.C.FogRadius,
		"available_weapons": GetAvailableWeapons(),
		"stat_budget":       config.C.StatBudget,
		"stat_min":          config.C.StatMin,
		"stat_max":          config.C.StatMax,
		"timeout_seconds":   config.C.LoadoutTimeoutSecs,
		"last_loadout":      loadout,
	}
}

// SendConnectedMessage sends the initial connection acknowledgement to a bot.
func SendConnectedMessage(bot *BotState, lastLoadout map[string]interface{}) {
	SendToBot(bot, BuildConnectedMessage(bot, lastLoadout))
}

// SendLoadoutConfirmed confirms a bot's loadout selection with the derived
// stats.
// BuildLoadoutConfirmed returns the loadout_confirmed payload.
func BuildLoadoutConfirmed(bot *BotState, derived DerivedStats) map[string]interface{} {
	return map[string]interface{}{
		"type":   "loadout_confirmed",
		"weapon": bot.Weapon,
		"stats": map[string]interface{}{
			"hp":      bot.Stats["hp"],
			"speed":   bot.Stats["speed"],
			"attack":  bot.Stats["attack"],
			"defense": bot.Stats["defense"],
		},
		"computed": map[string]interface{}{
			"max_hp":           derived.MaxHP,
			"move_speed":       derived.MoveSpeed,
			"attack_mult":      derived.AttackMult,
			"defense_red":      derived.DefenseReduction,
			"attack_range":     derived.AttackRange,
			"cooldown_seconds": derived.CooldownSeconds,
			"weapon_damage":    derived.WeaponDamage,
		},
		"position": bot.Position,
	}
}

// SendLoadoutConfirmed confirms a bot's loadout selection with the derived stats.
func SendLoadoutConfirmed(bot *BotState, derived DerivedStats) {
	SendToBot(bot, BuildLoadoutConfirmed(bot, derived))
}

// BroadcastToSpectators sends pre-serialised data to every spectator
// connection. Sends are non-blocking. Safe against closed channels
// (spectator may disconnect between snapshot and send).
func BroadcastToSpectators(spectators []*SpectatorConn, data []byte) {
	if len(spectators) == 0 {
		return
	}
	message, err := newSpectatorMessage(data)
	if err != nil {
		slog.Error("failed to prepare spectator message", "error", err)
		return
	}
	for _, s := range spectators {
		if s != nil && s.SendChan != nil {
			safeSendSpectator(s.SendChan, message)
		}
	}
}

func newSpectatorMessage(data []byte) (*SpectatorMessage, error) {
	prepared, err := websocket.NewPreparedMessage(websocket.TextMessage, data)
	if err != nil {
		return nil, err
	}
	return &SpectatorMessage{Payload: data, Prepared: prepared}, nil
}

func safeSendSpectator(ch chan *SpectatorMessage, message *SpectatorMessage) {
	defer func() { recover() }()
	select {
	case ch <- message:
	default:
	}
}

// safeSend performs a non-blocking send on ch, recovering gracefully if
// the channel has been closed (e.g. spectator disconnected).
func safeSend(ch chan []byte, data []byte) {
	defer func() { recover() }()
	select {
	case ch <- data:
	default:
	}
}

// safeReplaceLatest keeps at most one replaceable state snapshot. Lifecycle
// and control messages use SendChan, so a slow client never trades a death or
// round transition for a backlog of obsolete ticks.
func safeReplaceLatest(ch chan []byte, data []byte) {
	defer func() { recover() }()
	select {
	case ch <- data:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- data:
	default:
	}
}

// SendError sends an error message to a bot.
func SendError(bot *BotState, message string) {
	msg := map[string]interface{}{
		"type":    "error",
		"message": message,
	}
	SendToBot(bot, msg)
}

// SendStructuredError sends a structured error message with code and details.
func SendStructuredError(bot *BotState, message, code string, details map[string]interface{}) {
	msg := map[string]interface{}{
		"type":    "error",
		"message": message,
		"code":    code,
	}
	if details != nil {
		msg["details"] = details
	}
	SendToBot(bot, msg)
}

// SendKick sends a kick message to a bot with the reason.
func SendKick(bot *BotState, reason string) {
	msg := map[string]interface{}{
		"type":   "kick",
		"reason": reason,
	}
	SendToBot(bot, msg)
}
