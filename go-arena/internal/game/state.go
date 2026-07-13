package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"arena-server/internal/config"

	"github.com/gorilla/websocket"
)

// Vec2 is a 2D vector that serializes as [x, y] for protocol compatibility.
type Vec2 [2]float64

func NewVec2(x, y float64) Vec2     { return Vec2{x, y} }
func (v Vec2) X() float64           { return v[0] }
func (v Vec2) Y() float64           { return v[1] }
func (v Vec2) WithX(x float64) Vec2 { return Vec2{x, v[1]} }
func (v Vec2) WithY(y float64) Vec2 { return Vec2{v[0], y} }

func (v Vec2) Add(o Vec2) Vec2      { return Vec2{v[0] + o[0], v[1] + o[1]} }
func (v Vec2) Sub(o Vec2) Vec2      { return Vec2{v[0] - o[0], v[1] - o[1]} }
func (v Vec2) Scale(s float64) Vec2 { return Vec2{v[0] * s, v[1] * s} }

func (v Vec2) Length() float64 {
	return math.Sqrt(v[0]*v[0] + v[1]*v[1])
}

func (v Vec2) DistanceTo(o Vec2) float64 {
	dx := v[0] - o[0]
	dy := v[1] - o[1]
	return math.Sqrt(dx*dx + dy*dy)
}

func (v Vec2) Normalized() Vec2 {
	l := v.Length()
	if l < 1e-10 {
		return Vec2{0, 0}
	}
	return Vec2{v[0] / l, v[1] / l}
}

func (v Vec2) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]float64{v[0], v[1]})
}

func (v *Vec2) UnmarshalJSON(data []byte) error {
	var values []float64
	if err := json.Unmarshal(data, &values); err == nil {
		if len(values) != 2 {
			return fmt.Errorf("vector array must contain exactly 2 numbers")
		}
		*v = Vec2{values[0], values[1]}
		return nil
	}

	// Object form is retained for SDK compatibility, but both coordinates are
	// required and unknown fields are rejected so malformed payloads cannot
	// silently turn into the valid-looking origin [0, 0].
	var obj struct {
		X *float64 `json:"x"`
		Y *float64 `json:"y"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&obj); err != nil {
		return fmt.Errorf("invalid vector: %w", err)
	}
	if obj.X == nil || obj.Y == nil {
		return fmt.Errorf("vector object requires x and y")
	}
	*v = Vec2{*obj.X, *obj.Y}
	return nil
}

// ActionType enumerates bot action types.
type ActionType string

const (
	ActionMove           ActionType = "move"
	ActionMoveTo         ActionType = "move_to"
	ActionAttack         ActionType = "attack"
	ActionDodge          ActionType = "dodge"
	ActionShove          ActionType = "shove"
	ActionUseItem        ActionType = "use_item"
	ActionIdle           ActionType = "idle"
	ActionPlaceMine      ActionType = "place_mine"
	ActionUseGravityWell ActionType = "use_gravity_well"
	ActionGrapple        ActionType = "grapple"
)

// Action represents a bot's pending action for the current tick.
type Action struct {
	Type           ActionType
	TargetID       string // bot_id for attacks
	Direction      Vec2   // (dx, dy) for move/dodge
	ItemID         string // pickup_id for use_item
	TargetPosition *Vec2  // for move_to and staff area attacks
	Charged        bool   // optional charged bow shot
}

// Effect represents an active buff on a bot.
type Effect struct {
	Name           string  `json:"name"`
	RemainingTicks int     `json:"remaining_ticks"`
	Value          float64 `json:"value"`
	AuxValue       float64 `json:"aux_value,omitempty"`
}

// PickupType enumerates pickup types.
type PickupType string

const (
	PickupHealthPack    PickupType = "health_pack"
	PickupSpeedBoost    PickupType = "speed_boost"
	PickupDamageBoost   PickupType = "damage_boost"
	PickupShieldBubble  PickupType = "shield_bubble"
	PickupGravityWell   PickupType = "gravity_well"
	PickupCooldownShard PickupType = "cooldown_shard"
	PickupBountyToken   PickupType = "bounty_token"
	PickupHazardKey     PickupType = "hazard_key"
	PickupOverdriveCore PickupType = "overdrive_core"
	PickupGrappleCharge PickupType = "grapple_charge"
	PickupRelayBattery  PickupType = "relay_battery"
)

// Pickup represents a collectible item on the map.
type Pickup struct {
	ID       string     `json:"pickup_id"`
	Type     PickupType `json:"pickup_type"`
	Position Vec2       `json:"position"`
	Value    float64    `json:"value"`
}

// Projectile represents an in-flight arrow.
type Projectile struct {
	ID        string
	OwnerID   string
	Color     string
	Position  Vec2
	Direction Vec2
	Speed     float64
	HitRadius float64
	Damage    float64
	Weapon    string
	Intensity float64
	AgeTicks  int
	MaxAge    int
}

// Obstacle is an axis-aligned rectangle.
type Obstacle struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// StaffImpact represents a delayed area-of-effect attack.
type StaffImpact struct {
	OwnerID     string
	Position    Vec2
	Radius      float64
	Damage      float64
	DamageScale float64
	TicksLeft   int
	AttackMult  float64
}

// BurnField is a lingering damage zone left behind by a staff detonation.
type BurnField struct {
	ID           string
	OwnerID      string
	Position     Vec2
	Radius       float64
	Damage       float64
	AttackMult   float64
	TicksLeft    int
	TickInterval int
	PulseTick    int
	HitRecorded  bool
}

// HitRecord tracks a hit received this tick (for feedback).
type HitRecord struct {
	AttackerID string  `json:"attacker_id"`
	Damage     float64 `json:"damage"`
	Weapon     string  `json:"weapon"`
}

// ActionResult records the outcome of the bot's last action.
type ActionResult struct {
	Action  string  `json:"action"`
	Success bool    `json:"success"`
	Target  string  `json:"target,omitempty"`
	Damage  float64 `json:"damage,omitempty"`
	Message string  `json:"message,omitempty"`
}

// BotState holds all state for a connected bot.
type BotState struct {
	// Identity
	BotID       string
	APIKeyID    string
	Name        string
	AvatarColor string
	Cosmetics   map[string]string

	// Position & movement
	Position          Vec2
	LastValidPosition Vec2 // last position in an unblocked cell — for wall rejection
	Facing            Vec2
	Speed             float64

	// Health & combat
	HP                float64
	MaxHP             float64
	AttackMultiplier  float64
	DefenseReduction  float64
	Weapon            string
	CooldownRemaining float64
	IsAlive           bool
	KillStreak        int
	BestKillStreak    int
	RoundWinStreak    int
	ActiveEffects     []Effect

	// AI
	FallbackBehavior string
	PendingAction    *Action
	LastActionTick   int
	// ReconnectActionGraceUntilTick gives a restored transport time to submit
	// its first fresh action without rewriting LastActionTick, which is also a
	// spectator animation edge. Fallback AI stays disabled during this grace.
	ReconnectActionGraceUntilTick int
	// Client action ticks are monotonic sequence numbers supplied by the bot.
	// Tracking them separately from LastActionTick prevents replayed messages
	// and same-tick last-write-wins overrides without weakening AFK tracking.
	LastClientActionTick int
	HasClientActionTick  bool
	// Network submissions are also limited to the first accepted action in
	// each authoritative server tick. Client ticks alone cannot enforce this:
	// two increasing but delayed client ticks may arrive during one server tick.
	LastAcceptedServerTick int
	HasAcceptedServerTick  bool
	// LastTauntTick gates the cosmetic taunt cooldown against the engine's
	// monotonic TickCount. Deliberately NOT part of the one-action-per-tick
	// budget: a taunt must never cost a gameplay action.
	LastTauntTick int

	// Stats allocation
	Stats map[string]int // {hp, speed, attack, defense}
	Elo   int

	// Dodge / stun / invuln / freeze
	DodgeCooldown          int
	InvulnTicks            int
	StunTicks              int
	RecentlyDisruptedTicks int
	Frozen                 bool // admin freeze — cannot move or attack

	// Shove cooldown (separate from weapon cooldown)
	ShoveCooldown float64

	// Shield
	ShieldAbsorb float64

	// Movement pacing. MoveProgress carries fractional grid-cell movement
	// between ticks so the configured speed stat still matters on terrain maps.
	// MovementTrace records every entered position for same-tick arena effects.
	// MoveCooldown is retained for the legacy non-terrain movement path.
	MoveProgress  float64
	MovementTrace []Vec2
	MoveCooldown  int

	// Stuck detection: counts consecutive ticks at the same grid cell.
	// PrevTickCell is the grid cell at the END of the previous tick; comparing
	// against LastValidPosition is wrong because movement syncs it to the
	// current position within the same tick (making every bot look stuck).
	StuckTicks      int
	StillTicks      int
	PrevTickCell    [2]int
	PrevTickCellSet bool

	// Pathfinding
	CurrentPath []Vec2
	PathTarget  *Vec2

	// Team (game modes): 0 = no team (FFA), 1..N in team modes.
	Team int

	// Round stats
	RoundKills             int
	RoundDeaths            int
	RoundDamageDealt       float64
	RoundWeaponKills       int
	RoundWeaponDamageDealt float64
	RoundWeaponOpponentIDs map[string]struct{}
	RoundDamageTaken       float64
	RoundDistance          float64
	RoundShotsFired        int
	RoundShotsHit          int
	RoundLongestLife       int
	RoundPickups           int
	RoundFlagCaptures      int
	RoundLifeStartTick     int

	// Persistence snapshot — tracks what was already synced to DB
	PersistedKills       int
	PersistedDeaths      int
	PersistedDamageDealt float64
	PersistedDamageTaken float64
	PersistedDistance    float64
	PersistedPickups     int

	// A leaderboard reset can happen in the middle of a live round. These
	// baselines keep the match's visible streak/lifetime counters intact while
	// ensuring only the post-reset portion is written back to the leaderboard.
	LeaderboardRebased      bool
	LeaderboardKillBaseline int
	LeaderboardLifeBaseline int

	// Kill attribution
	LastDamagedBy    string
	LastDamageTick   int
	LastDamageSource string
	LastDamageAmount float64

	// Action history (last 100 actions for profiling)
	ActionHistory    []ActionType
	ActionHistoryMax int

	// Bounty
	IsBountyTarget   bool
	BountyTokenBonus int

	// Landmines: active mines placed by this bot
	MineCount int

	// Gravity well charge (0 or 1)
	GravityWellCharge int

	// Grapple ability (universal — 2 charges per round)
	GrappleCharges  int
	GrappleCooldown float64
	BowChargeTicks  int

	// Teleport pad cooldowns: padID -> tick when cooldown expires
	TeleportCooldowns        map[string]int
	TeleportTouchedPads      map[string]bool
	TeleportHazardGraceTicks int

	// Per-tick feedback (cleared each tick)
	HitsReceived     []HitRecord
	LastActionResult *ActionResult

	// Connection tracking
	ConnectedAt        time.Time
	ReconnectPending   bool
	DisconnectedAtTick int

	// WebSocket (nil for AI-only bots)
	Conn     *websocket.Conn
	SendChan chan []byte
	TickChan chan []byte
}

// rejectControlledAction applies the shared stun/freeze/dodge gate used by
// action processors. blockWhileInvulnerable is true for offensive actions;
// self-directed anchor grapples deliberately remain valid during dodge frames.
func rejectControlledAction(bot *BotState, action string, blockWhileInvulnerable bool) bool {
	if bot == nil {
		return true
	}
	message := ""
	switch {
	case bot.Frozen:
		message = "frozen"
	case bot.StunTicks > 0:
		message = "stunned"
	case blockWhileInvulnerable && bot.InvulnTicks > 0:
		message = "cannot use offensive actions while dodging"
	default:
		return false
	}
	target := ""
	if bot.PendingAction != nil {
		target = bot.PendingAction.TargetID
	}
	bot.LastActionResult = &ActionResult{
		Action:  action,
		Success: false,
		Target:  target,
		Message: message,
	}
	return true
}

// RoundPhase tracks the current phase of the game.
type RoundPhase int

const (
	PhaseLobby RoundPhase = iota
	PhaseActive
	PhaseIntermission
)

// RoundModifier is an occasional special ruleset applied to a round.
type RoundModifier string

const (
	RoundModifierNone          RoundModifier = ""
	RoundModifierFastZone      RoundModifier = "fast_zone"
	RoundModifierPickupSurge   RoundModifier = "pickup_surge"
	RoundModifierDoubleBounty  RoundModifier = "double_bounty"
	RoundModifierTeleportSurge RoundModifier = "teleport_surge"
	RoundModifierHazardStorm   RoundModifier = "hazard_storm"
)

func (m RoundModifier) Label() string {
	switch m {
	case RoundModifierFastZone:
		return "Fast Zone"
	case RoundModifierPickupSurge:
		return "Pickup Surge"
	case RoundModifierDoubleBounty:
		return "Double Bounty"
	case RoundModifierTeleportSurge:
		return "Teleport Surge"
	case RoundModifierHazardStorm:
		return "Hazard Storm"
	default:
		return "Normal"
	}
}

// RoundState tracks current round info.
type RoundState struct {
	RoundNumber         int
	StartTick           int
	Phase               RoundPhase
	Mode                GameMode
	Modifier            RoundModifier
	TimeRemaining       float64
	IntermissionTicks   int
	LobbyCountdownTicks int
	RoundID             string // UUID for DB
}

// DeathEvent is emitted when a bot dies.
type DeathEvent struct {
	VictimID    string
	KillerID    string
	KillerName  string
	Weapon      string
	Damage      float64
	VictimKills int // kills this life
}

// KillEvent is emitted when a bot gets a kill.
type KillEvent struct {
	KillerID   string
	VictimID   string
	VictimName string
	Weapon     string
	Damage     float64
	KillStreak int
	RoundKills int
}

// RoundEndInfo holds data for the round_end message.
type RoundEndInfo struct {
	RoundNumber int
	WinnerID    string
	WinnerName  string
	Awards      map[string]string // award_name -> bot_name
}

// ArenaEvent is a short-lived spectator event used for high-signal visual
// feedback such as teleports and mine detonations.
type ArenaEvent struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	Tick         int     `json:"tick"`
	Position     Vec2    `json:"position"`
	FromPosition *Vec2   `json:"from_position,omitempty"`
	ToPosition   *Vec2   `json:"to_position,omitempty"`
	OwnerID      string  `json:"owner_id,omitempty"`
	TargetID     string  `json:"target_id,omitempty"`
	Color        string  `json:"color,omitempty"`
	Radius       float64 `json:"radius,omitempty"`
	Intensity    float64 `json:"intensity,omitempty"`
	// Emote/Text carry bot taunts. Text is always server-authored (the
	// tauntEmotes table), never bot input.
	Emote string `json:"emote,omitempty"`
	Text  string `json:"text,omitempty"`
}

// SpectatorState is the serialized arena state for spectators.
type SpectatorState struct {
	Type         string                   `json:"type"`
	Tick         int                      `json:"tick"`
	RoundTick    int                      `json:"round_tick"`
	RoundNumber  int                      `json:"round_number,omitempty"`
	Bots         []map[string]interface{} `json:"bots"`
	SafeZone     map[string]interface{}   `json:"safe_zone"`
	Pickups      []map[string]interface{} `json:"pickups"`
	KillFeed     []map[string]interface{} `json:"kill_feed"`
	Obstacles    []Obstacle               `json:"obstacles,omitempty"`
	WaitingBots  []map[string]interface{} `json:"waiting_bots,omitempty"`
	TeleportPads []map[string]interface{} `json:"teleport_pads,omitempty"`
	CapturePads  []map[string]interface{} `json:"capture_pads,omitempty"`
	HazardZones  []map[string]interface{} `json:"hazard_zones,omitempty"`
	BurnFields   []map[string]interface{} `json:"burn_fields,omitempty"`
	Landmines    []map[string]interface{} `json:"landmines,omitempty"`
	GravityWells []map[string]interface{} `json:"gravity_wells,omitempty"`
	StaffImpacts []map[string]interface{} `json:"staff_impacts,omitempty"`
	VoidTiles    [][2]int                 `json:"void_tiles,omitempty"`
	SuddenDeath  bool                     `json:"sudden_death"`
	// SuddenDeathStall is true while the no-combat window has been exceeded
	// and every living bot is taking ramping stall damage.
	SuddenDeathStall bool `json:"sudden_death_stall,omitempty"`
	// SuddenDeathMult is the active damage multiplier ("2x DAMAGE" display).
	SuddenDeathMult float64      `json:"sudden_death_mult,omitempty"`
	BountyTarget    string       `json:"bounty_target,omitempty"`
	RoundModifier   string       `json:"round_modifier,omitempty"`
	Events          []ArenaEvent `json:"events,omitempty"`

	// Game modes (groundwork)
	GameMode   string                   `json:"game_mode,omitempty"`
	TeamScores map[string]int           `json:"team_scores,omitempty"`
	Flags      []map[string]interface{} `json:"flags,omitempty"`

	// Map shape metadata for non-square maps ("square" when absent).
	MapShape string `json:"map_shape,omitempty"`

	// Arena dimensions [w, h]; sent on keyframes (dynamic arena sizing can
	// change them between rounds). Slice so omitempty drops it off-keyframe.
	ArenaSize []float64 `json:"arena_size,omitempty"`
}

// DerivedStats are computed from stat allocations.
type DerivedStats struct {
	MaxHP            float64 `json:"max_hp"`
	MoveSpeed        float64 `json:"move_speed"`
	AttackMult       float64 `json:"attack_mult"`
	DefenseReduction float64 `json:"defense_red"`
	AttackRange      float64 `json:"attack_range"`
	CooldownSeconds  float64 `json:"cooldown_seconds"`
	WeaponDamage     float64 `json:"weapon_damage"`
}

// ComputeDerivedStats calculates derived stats from base allocations and weapon.
func ComputeDerivedStats(stats map[string]int, weapon string) DerivedStats {
	hp := stats["hp"]
	spd := stats["speed"]
	atk := stats["attack"]
	def := stats["defense"]

	c := &config.C
	wc := GetWeaponConfig(weapon)

	return DerivedStats{
		MaxHP:            c.StatHPBase + float64(hp)*c.StatHPPerPoint,
		MoveSpeed:        c.StatSpeedBase + float64(spd)*c.StatSpeedPerPoint,
		AttackMult:       c.StatAttackBase + float64(atk)*c.StatAttackPerPoint,
		DefenseReduction: float64(def) * c.StatDefensePerPoint,
		AttackRange:      float64(wc.GridRange),
		CooldownSeconds:  wc.Cooldown,
		WeaponDamage:     weaponDamage(&wc),
	}
}

// ResetRoundStats zeros out all per-round stats on a bot.
func (b *BotState) ResetRoundStats() {
	b.RoundKills = 0
	b.RoundDeaths = 0
	b.RoundDamageDealt = 0
	b.RoundWeaponKills = 0
	b.RoundWeaponDamageDealt = 0
	b.RoundWeaponOpponentIDs = nil
	b.RoundDamageTaken = 0
	b.RoundDistance = 0
	b.RoundShotsFired = 0
	b.RoundShotsHit = 0
	b.RoundLongestLife = 0
	b.RoundPickups = 0
	b.RoundFlagCaptures = 0
	b.RoundLifeStartTick = 0
	b.BestKillStreak = 0
	b.RecentlyDisruptedTicks = 0
	b.BowChargeTicks = 0
	b.StillTicks = 0
	b.PrevTickCellSet = false
	b.MoveProgress = 0
	b.MovementTrace = nil
	b.MoveCooldown = 0
	b.ResetDamageAttribution()
	// Reset persistence snapshots so deltas start fresh
	b.PersistedKills = 0
	b.PersistedDeaths = 0
	b.PersistedDamageDealt = 0
	b.PersistedDamageTaken = 0
	b.PersistedDistance = 0
	b.PersistedPickups = 0
	b.LeaderboardRebased = false
	b.LeaderboardKillBaseline = 0
	b.LeaderboardLifeBaseline = 0
}

// ResetDamageAttribution prevents a hit from a previous life or round from
// receiving credit for a later environmental death.
func (b *BotState) ResetDamageAttribution() {
	b.LastDamagedBy = ""
	b.LastDamageTick = 0
	b.LastDamageSource = ""
	b.LastDamageAmount = 0
}

// ClearTickFeedback resets per-tick transient data.
func (b *BotState) ClearTickFeedback() {
	b.HitsReceived = nil
	b.LastActionResult = nil
	b.PendingAction = nil
}
