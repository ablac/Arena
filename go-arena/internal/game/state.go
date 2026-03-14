package game

import (
	"encoding/json"
	"math"
	"sync"

	"github.com/gorilla/websocket"
)

// Vec2 is a 2D vector that serializes as [x, y] for protocol compatibility.
type Vec2 [2]float64

func NewVec2(x, y float64) Vec2    { return Vec2{x, y} }
func (v Vec2) X() float64          { return v[0] }
func (v Vec2) Y() float64          { return v[1] }
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
	var arr [2]float64
	if err := json.Unmarshal(data, &arr); err != nil {
		// Try object form {"x":..,"y":..}
		var obj struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		}
		if err2 := json.Unmarshal(data, &obj); err2 != nil {
			return err
		}
		v[0] = obj.X
		v[1] = obj.Y
		return nil
	}
	*v = arr
	return nil
}

// ActionType enumerates bot action types.
type ActionType string

const (
	ActionMove    ActionType = "move"
	ActionMoveTo  ActionType = "move_to"
	ActionAttack  ActionType = "attack"
	ActionDodge   ActionType = "dodge"
	ActionShove   ActionType = "shove"
	ActionUseItem ActionType = "use_item"
	ActionIdle    ActionType = "idle"
)

// Action represents a bot's pending action for the current tick.
type Action struct {
	Type           ActionType
	TargetID       string // bot_id for attacks
	Direction      Vec2   // (dx, dy) for move/dodge
	ItemID         string // pickup_id for use_item
	TargetPosition *Vec2  // for move_to and staff area attacks
}

// Effect represents an active buff on a bot.
type Effect struct {
	Name           string  `json:"name"`
	RemainingTicks int     `json:"remaining_ticks"`
	Value          float64 `json:"value"`
}

// PickupType enumerates pickup types.
type PickupType string

const (
	PickupHealthPack  PickupType = "health_pack"
	PickupSpeedBoost  PickupType = "speed_boost"
	PickupDamageBoost PickupType = "damage_boost"
	PickupShieldBubble PickupType = "shield_bubble"
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
	Position  Vec2
	Direction Vec2
	Speed     float64
	Damage    float64
	Weapon    string
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
	OwnerID    string
	Position   Vec2
	Radius     float64
	Damage     float64
	TicksLeft  int
	AttackMult float64
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
	BotID      string
	APIKeyID   string
	Name       string
	AvatarColor string

	// Position & movement
	Position Vec2
	Speed    float64

	// Health & combat
	HP               float64
	MaxHP            float64
	AttackMultiplier float64
	DefenseReduction float64
	Weapon           string
	CooldownRemaining float64
	IsAlive          bool
	KillStreak       int
	ActiveEffects    []Effect

	// AI
	FallbackBehavior string
	PendingAction    *Action
	LastActionTick   int

	// Stats allocation
	Stats map[string]int // {hp, speed, attack, defense}
	Elo   int

	// Dodge / stun / invuln
	DodgeCooldown int
	InvulnTicks   int
	StunTicks     int

	// Shove cooldown (separate from weapon cooldown)
	ShoveCooldown float64

	// Shield
	ShieldAbsorb float64

	// Pathfinding
	CurrentPath []Vec2
	PathTarget  *Vec2

	// Round stats
	RoundKills       int
	RoundDeaths      int
	RoundDamageDealt float64
	RoundDamageTaken float64
	RoundDistance     float64
	RoundShotsFired  int
	RoundShotsHit    int
	RoundLongestLife  int
	RoundPickups     int
	RoundLifeStartTick int

	// Persistence snapshot — tracks what was already synced to DB
	PersistedKills       int
	PersistedDeaths      int
	PersistedDamageDealt float64
	PersistedDamageTaken float64
	PersistedDistance    float64
	PersistedPickups     int

	// Kill attribution
	LastDamagedBy    string
	LastDamageTick   int

	// Per-tick feedback (cleared each tick)
	HitsReceived    []HitRecord
	LastActionResult *ActionResult

	// WebSocket (nil for AI-only bots)
	Conn     *websocket.Conn
	SendChan chan []byte
	mu       sync.Mutex // protects Conn writes if needed
}

// RoundPhase tracks the current phase of the game.
type RoundPhase int

const (
	PhaseLobby RoundPhase = iota
	PhaseActive
	PhaseIntermission
)

// RoundState tracks current round info.
type RoundState struct {
	RoundNumber       int
	StartTick         int
	Phase             RoundPhase
	TimeRemaining     float64
	IntermissionTicks int
	LobbyCountdownTicks int
	RoundID           string // UUID for DB
}

// DeathEvent is emitted when a bot dies.
type DeathEvent struct {
	VictimID   string
	KillerID   string
	KillerName string
	Weapon     string
	Damage     float64
	VictimKills int // kills this life
}

// KillEvent is emitted when a bot gets a kill.
type KillEvent struct {
	KillerID    string
	VictimID    string
	VictimName  string
	Weapon      string
	KillStreak  int
	RoundKills  int
}

// RoundEndInfo holds data for the round_end message.
type RoundEndInfo struct {
	RoundNumber int
	WinnerID    string
	WinnerName  string
	Awards      map[string]string // award_name -> bot_name
}

// SpectatorState is the serialized arena state for spectators.
type SpectatorState struct {
	Type      string                   `json:"type"`
	Tick      int                      `json:"tick"`
	Bots      []map[string]interface{} `json:"bots"`
	SafeZone  map[string]interface{}   `json:"safe_zone"`
	Pickups   []map[string]interface{} `json:"pickups"`
	KillFeed  []map[string]interface{} `json:"kill_feed"`
	Obstacles []Obstacle               `json:"obstacles"`
}

// DerivedStats are computed from stat allocations.
type DerivedStats struct {
	MaxHP           float64 `json:"max_hp"`
	MoveSpeed       float64 `json:"move_speed"`
	AttackMult      float64 `json:"attack_mult"`
	DefenseReduction float64 `json:"defense_red"`
	AttackRange     float64 `json:"attack_range"`
	CooldownSeconds float64 `json:"cooldown_seconds"`
	WeaponDamage    float64 `json:"weapon_damage"`
}

// ComputeDerivedStats calculates derived stats from base allocations and weapon.
func ComputeDerivedStats(stats map[string]int, weapon string) DerivedStats {
	hp := stats["hp"]
	spd := stats["speed"]
	atk := stats["attack"]
	def := stats["defense"]

	wc := GetWeaponConfig(weapon)

	return DerivedStats{
		MaxHP:           100.0 + float64(hp)*10.0,
		MoveSpeed:       3.0 + float64(spd)*0.5,
		AttackMult:      1.0 + float64(atk)*0.1,
		DefenseReduction: float64(def) * 0.03,
		AttackRange:     wc.Range,
		CooldownSeconds: wc.Cooldown,
		WeaponDamage:    float64(wc.Damage),
	}
}

// ResetRoundStats zeros out all per-round stats on a bot.
func (b *BotState) ResetRoundStats() {
	b.RoundKills = 0
	b.RoundDeaths = 0
	b.RoundDamageDealt = 0
	b.RoundDamageTaken = 0
	b.RoundDistance = 0
	b.RoundShotsFired = 0
	b.RoundShotsHit = 0
	b.RoundLongestLife = 0
	b.RoundPickups = 0
	b.RoundLifeStartTick = 0
	// Reset persistence snapshots so deltas start fresh
	b.PersistedKills = 0
	b.PersistedDeaths = 0
	b.PersistedDamageDealt = 0
	b.PersistedDamageTaken = 0
	b.PersistedDistance = 0
	b.PersistedPickups = 0
}

// ClearTickFeedback resets per-tick transient data.
func (b *BotState) ClearTickFeedback() {
	b.HitsReceived = nil
	b.LastActionResult = nil
	b.PendingAction = nil
}
