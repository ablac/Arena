package game

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"arena-server/internal/config"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// GameEngine is the central coordinator for the arena game loop.
type GameEngine struct {
	mu sync.RWMutex

	// State
	Bots         map[string]*BotState
	WaitingBots  map[string]*BotState
	Pickups      []Pickup
	Projectiles  []Projectile
	StaffImpacts []StaffImpact
	Round        RoundState
	Arena        *ArenaMap
	Grid         *SpatialGrid
	NavGrid      *NavGrid
	KillFeed     *KillFeed

	// Anti-teaming
	AntiTeam *AntiTeamTracker

	// Spectators
	Spectators   []*SpectatorConn
	spectatorsMu sync.RWMutex

	// Tick tracking
	TickCount int
	Running   bool

	// Events (buffered, drained after each tick)
	DeathEvents   []DeathEvent
	KillEvents    []KillEvent
	// Persistence tracking
	lastPersistTick int
}

// SpectatorConn wraps a WebSocket connection for a spectator client.
type SpectatorConn struct {
	Conn     *websocket.Conn
	SendChan chan []byte
	Done     chan struct{}
}

// NewGameEngine initialises all fields and returns a ready-to-run engine.
func NewGameEngine() *GameEngine {
	return &GameEngine{
		Bots:        make(map[string]*BotState),
		WaitingBots: make(map[string]*BotState),
		Arena:       NewArenaMap(),
		Grid:        NewSpatialGrid(config.C.SpatialCellSize),
		KillFeed:    NewKillFeed(config.C.KillFeedSize),
		AntiTeam:    NewAntiTeamTracker(),
		Round: RoundState{
			Phase: PhaseLobby,
		},
	}
}

// Run starts the main game loop. It ticks at the configured TickRate and
// stops when ctx is cancelled.
func (e *GameEngine) Run(ctx context.Context) {
	interval := time.Duration(float64(time.Second) / float64(config.C.TickRate))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.Running = true
	slog.Info("game engine started", "tick_rate", config.C.TickRate)

	for {
		select {
		case <-ctx.Done():
			e.Running = false
			slog.Info("game engine stopped")
			return
		case <-ticker.C:
			e.tick()
		}
	}
}

// tick is the main per-tick update. It acquires the write lock for the
// duration of the game-state mutation, releasing it briefly for network
// sends.
func (e *GameEngine) tick() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.TickCount++
	c := &config.C
	dt := 1.0 / float64(c.TickRate)

	switch e.Round.Phase {
	case PhaseLobby:
		e.tickLobby(c)

	case PhaseIntermission:
		e.tickIntermission()

	case PhaseActive:
		e.tickActive(c, dt)
	}

	// Broadcast to spectators in ALL phases (lobby/intermission included).
	if e.Round.Phase != PhaseActive {
		if e.TickCount%c.SpectatorBroadcastInterval == 0 {
			e.sendLobbyStateUpdate()
		}
	}
}

// --------------------------------------------------------------------------
// Phase handlers
// --------------------------------------------------------------------------

func (e *GameEngine) tickLobby(c *config.Config) {
	// Merge waiting bots into active bots.
	for id, bot := range e.WaitingBots {
		e.Bots[id] = bot
	}
	e.WaitingBots = make(map[string]*BotState)

	// Count connected bots.
	connected := len(e.Bots)

	// Start or continue lobby countdown.
	if connected >= c.MinBotsToStart && e.Round.LobbyCountdownTicks == 0 {
		e.Round.LobbyCountdownTicks = int(c.LobbyCountdown * float64(c.TickRate))
	}

	if e.Round.LobbyCountdownTicks > 0 {
		e.Round.LobbyCountdownTicks--
		if e.Round.LobbyCountdownTicks <= 0 {
			e.startRound()
			return
		}
	}

	// Reset countdown if we drop below minimum.
	if connected < c.MinBotsToStart {
		e.Round.LobbyCountdownTicks = 0
	}

	// Send lobby updates every 2 ticks.
	if e.TickCount%2 == 0 {
		var countdown *int
		if e.Round.LobbyCountdownTicks > 0 {
			secs := e.Round.LobbyCountdownTicks / config.C.TickRate
			if secs < 1 {
				secs = 1
			}
			countdown = &secs
		}
		for _, bot := range e.Bots {
			SendLobbyUpdate(bot, connected, config.C.MinBotsToStart, countdown, e.Bots)
		}
	}
}

func (e *GameEngine) tickIntermission() {
	e.Round.IntermissionTicks--
	if e.Round.IntermissionTicks <= 0 {
		e.Round.Phase = PhaseLobby
		e.Round.LobbyCountdownTicks = 0
	}
}

func (e *GameEngine) tickActive(c *config.Config, dt float64) {
	// Apply fallback AI actions for bots without pending actions.
	e.applyFallbacks()

	// Process USE_ITEM actions.
	e.processUseItems()

	// Movement.
	ProcessMovement(e.Bots, e.Arena.Obstacles, e.Grid, e.NavGrid, dt)

	// Shoves (before combat so shoved bots can't attack this tick).
	ProcessShoves(e.Bots, e.Arena.Obstacles)

	// Combat.
	ProcessCombat(e.Bots, e.Arena.Obstacles, &e.Projectiles, &e.StaffImpacts, e.Grid, e.TickCount, dt)

	// Record attacks for anti-teaming (reset proximity for fighting pairs).
	for _, bot := range e.Bots {
		if bot.PendingAction != nil && bot.PendingAction.Type == ActionAttack && bot.PendingAction.TargetID != "" {
			e.AntiTeam.RecordAttack(bot.BotID, bot.PendingAction.TargetID)
		}
	}

	// Projectiles.
	UpdateProjectiles(&e.Projectiles, e.Bots, e.Arena.Obstacles, e.TickCount, dt)

	// Staff area impacts.
	ProcessStaffImpacts(&e.StaffImpacts, e.Bots, e.TickCount)

	// Zone shrink.
	e.Arena.UpdateZone(e.TickCount, e.Round.StartTick)

	// Zone damage.
	e.applyZoneDamage()

	// Anti-teaming: penalise bots that stay near each other without fighting.
	penalised := e.AntiTeam.Update(e.Bots, e.Grid)
	for _, botID := range penalised {
		if bot, ok := e.Bots[botID]; ok && bot.IsAlive {
			bot.HP -= config.C.AntiTeamDamagePerTick
		}
	}

	// Bot separation.
	SeparateBots(e.Bots, e.Arena.Obstacles, e.Grid)

	// Check deaths.
	deaths := CheckDeaths(e.Bots, e.Grid)
	e.DeathEvents = append(e.DeathEvents, deaths...)

	// Handle kill credits.
	e.handleKillCredits(deaths)

	// No respawns — dead bots stay dead until next round.

	// Pickups.
	MaybeSpawnPickup(&e.Pickups, e.Arena, e.TickCount)
	CheckAutoCollect(e.Bots, &e.Pickups)

	// Tick effects and timers for all bots.
	for _, bot := range e.Bots {
		TickEffects(bot)
		TickTimers(bot, dt)
	}

	// Update longest life tracking.
	for _, bot := range e.Bots {
		if bot.IsAlive {
			lifeLen := e.TickCount - bot.RoundLifeStartTick
			if lifeLen > bot.RoundLongestLife {
				bot.RoundLongestLife = lifeLen
			}
		}
	}

	// Check AFK bots.
	e.checkAFK()

	// Send per-bot tick updates.
	e.sendBotTickUpdates()

	// Spectator broadcasts at configured interval.
	if e.TickCount%c.SpectatorBroadcastInterval == 0 {
		e.sendSpectatorUpdate()
	}

	// Send buffered event messages.
	e.sendEventMessages()

	// Periodic persistence.
	persistInterval := int(c.PersistIntervalSecs * float64(c.TickRate))
	if persistInterval > 0 && e.TickCount-e.lastPersistTick >= persistInterval {
		e.lastPersistTick = e.TickCount
		go PersistBotStatsFromSnapshot(context.Background(), e.snapshotBotStats())
	}

	// Check round end.
	if ShouldEndRound(e.Bots, &e.Round, e.TickCount) {
		e.endRound()
	}

	// Clear per-tick feedback.
	for _, bot := range e.Bots {
		bot.ClearTickFeedback()
	}
}

// --------------------------------------------------------------------------
// Round lifecycle
// --------------------------------------------------------------------------

func (e *GameEngine) startRound() {
	c := &config.C
	e.Round.RoundNumber++

	// Generate new obstacles.
	obstacles := GenerateObstacles(c.ArenaWidth, c.ArenaHeight, c.ObstacleCountMin, c.ObstacleCountMax)
	e.Arena.Reset(obstacles)

	// Build navigation grid.
	e.NavGrid = NewNavGrid(c.ArenaWidth, c.ArenaHeight, obstacles, c.BotRadius)

	// Clear transient state.
	e.Pickups = nil
	e.Projectiles = nil
	e.StaffImpacts = nil
	e.DeathEvents = nil
	e.KillEvents = nil
	e.Grid.Clear()
	e.KillFeed.Clear()
	e.AntiTeam.Clear()

	// Set round state.
	e.Round.Phase = PhaseActive
	e.Round.StartTick = e.TickCount
	e.Round.RoundID = uuid.New().String()

	// Spawn bots evenly around the zone perimeter.
	botList := make([]*BotState, 0, len(e.Bots))
	for _, bot := range e.Bots {
		botList = append(botList, bot)
	}
	spawnPoints := e.Arena.GetSpawnPoints(len(botList))
	for i, bot := range botList {
		SpawnBotAt(bot, spawnPoints[i], e.Grid, e.TickCount)
		bot.ResetRoundStats()
		bot.KillStreak = 0
		bot.LastActionTick = 0 // Reset AFK timer so bots aren't kicked at round start
	}

	// Send round_start message to every bot.
	for _, bot := range e.Bots {
		SendRoundStart(bot, e.Round, e.Bots, obstacles, e.Arena)
	}

	slog.Info("round started",
		"round", e.Round.RoundNumber,
		"bots", len(e.Bots),
		"obstacles", len(obstacles),
	)
}

func (e *GameEngine) endRound() {
	winnerID, winnerName := DetermineWinner(e.Bots)
	awards := CalculateAwards(e.Bots)

	info := RoundEndInfo{
		RoundNumber: e.Round.RoundNumber,
		WinnerID:    winnerID,
		WinnerName:  winnerName,
		Awards:      awards,
	}

	nextRoundIn := config.C.IntermissionTime

	for _, bot := range e.Bots {
		SendRoundEnd(bot, info, nextRoundIn)
	}

	e.Round.Phase = PhaseIntermission
	e.Round.IntermissionTicks = int(config.C.IntermissionTime * float64(config.C.TickRate))

	// Persist final stats for the round.
	go PersistBotStatsFromSnapshot(context.Background(), e.snapshotBotStats())

	slog.Info("round ended",
		"round", e.Round.RoundNumber,
		"winner", winnerName,
	)
}

// --------------------------------------------------------------------------
// Bot management
// --------------------------------------------------------------------------

// AddBot adds a bot to the engine. If the game is in lobby phase the bot is
// added directly; otherwise it goes into the waiting list for the next round.
// Returns false if the server is at capacity (MaxBots).
func (e *GameEngine) AddBot(bot *BotState) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.Bots)+len(e.WaitingBots) >= config.C.MaxBots {
		return false
	}

	if e.Round.Phase == PhaseLobby {
		e.Bots[bot.BotID] = bot
	} else {
		e.WaitingBots[bot.BotID] = bot
	}
	return true
}

// RemoveBot removes a bot from both active and waiting maps and persists its
// stats.
func (e *GameEngine) RemoveBot(botID string) {
	e.mu.Lock()
	bot := e.Bots[botID]
	delete(e.Bots, botID)
	delete(e.WaitingBots, botID)
	e.Grid.Remove(botID)
	e.mu.Unlock()

	if bot != nil {
		go PersistSingleBot(context.Background(), bot)
	}
}

// SetBotAction sets the pending action for a bot and updates its last-action
// tick for AFK tracking.
func (e *GameEngine) SetBotAction(botID string, action *Action) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if bot, ok := e.Bots[botID]; ok {
		bot.PendingAction = action
		bot.LastActionTick = e.TickCount
	}
}

// --------------------------------------------------------------------------
// Spectator management
// --------------------------------------------------------------------------

// AddSpectator registers a spectator connection.
func (e *GameEngine) AddSpectator(conn *SpectatorConn) {
	e.spectatorsMu.Lock()
	defer e.spectatorsMu.Unlock()
	e.Spectators = append(e.Spectators, conn)
}

// SpectatorCount returns the current number of connected spectators.
func (e *GameEngine) SpectatorCount() int {
	e.spectatorsMu.RLock()
	defer e.spectatorsMu.RUnlock()
	return len(e.Spectators)
}

// RemoveSpectator unregisters a spectator connection.
func (e *GameEngine) RemoveSpectator(conn *SpectatorConn) {
	e.spectatorsMu.Lock()
	defer e.spectatorsMu.Unlock()

	for i, s := range e.Spectators {
		if s == conn {
			e.Spectators = append(e.Spectators[:i], e.Spectators[i+1:]...)
			return
		}
	}
}

// GetState returns a read-locked snapshot of the game state for spectators.
func (e *GameEngine) GetState() SpectatorState {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return BuildSpectatorState(e.Bots, e.Arena, e.Pickups, e.KillFeed, e.TickCount)
}

// ArenaSnapshot holds read-only arena state for the REST API.
type ArenaSnapshot struct {
	Phase              RoundPhase
	BotsConnected      int
	BotsAlive          int
	RoundNumber        int
	RoundTimeRemaining float64
	SafeZoneRadius     float64
	TopBotName         string
}

// GetArenaSnapshot returns a point-in-time snapshot of arena state under the
// read lock. It is safe to call from HTTP handlers.
func (e *GameEngine) GetArenaSnapshot() ArenaSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	snap := ArenaSnapshot{
		Phase:              e.Round.Phase,
		BotsConnected:      len(e.Bots),
		RoundNumber:        e.Round.RoundNumber,
		RoundTimeRemaining: e.Round.TimeRemaining,
		SafeZoneRadius:     e.Arena.ZoneRadius,
	}

	bestKills := -1
	for _, bot := range e.Bots {
		if bot.IsAlive {
			snap.BotsAlive++
		}
		if bot.RoundKills > bestKills {
			bestKills = bot.RoundKills
			snap.TopBotName = bot.Name
		}
	}

	return snap
}

// ConnectedBotCount returns the number of connected bots (active + waiting)
// under the read lock.
func (e *GameEngine) ConnectedBotCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.Bots) + len(e.WaitingBots)
}

// --------------------------------------------------------------------------
// Internal helpers
// --------------------------------------------------------------------------

// applyFallbacks assigns AI fallback actions to bots that have no pending
// action and are alive and not stunned.
func (e *GameEngine) applyFallbacks() {
	for _, bot := range e.Bots {
		if bot.PendingAction != nil || !bot.IsAlive || bot.StunTicks > 0 {
			continue
		}

		nearbyIDs := e.Grid.QueryRadius(bot.Position, config.C.ViewRadius)
		var nearbyBots []*BotState
		for _, id := range nearbyIDs {
			if id == bot.BotID {
				continue
			}
			if other, ok := e.Bots[id]; ok && other.IsAlive {
				nearbyBots = append(nearbyBots, other)
			}
		}

		fb := GetFallbackAction(bot, nearbyBots, bot.FallbackBehavior, e.Arena)
		if fb != nil {
			bot.PendingAction = fb
		}
	}
}

// processUseItems handles USE_ITEM actions by attempting to collect the
// specified pickup.
func (e *GameEngine) processUseItems() {
	for _, bot := range e.Bots {
		if bot.PendingAction == nil || bot.PendingAction.Type != ActionUseItem {
			continue
		}
		if !bot.IsAlive {
			continue
		}
		ok := CollectByAction(bot, bot.PendingAction.ItemID, &e.Pickups)
		if ok {
			bot.LastActionResult = &ActionResult{
				Action:  "use_item",
				Success: true,
				Message: "item collected",
			}
		} else {
			bot.LastActionResult = &ActionResult{
				Action:  "use_item",
				Success: false,
				Message: "item not found or out of range",
			}
		}
	}
}

// applyZoneDamage hurts alive bots that are outside the safe zone.
func (e *GameEngine) applyZoneDamage() {
	for _, bot := range e.Bots {
		if !bot.IsAlive {
			continue
		}
		if !e.Arena.IsInZone(bot.Position) {
			bot.HP -= config.C.ZoneDamagePerTick
		}
	}
}

// handleKillCredits processes death events: updates killer streaks, ELO, and
// the kill feed.
func (e *GameEngine) handleKillCredits(deaths []DeathEvent) {
	for i := range deaths {
		death := &deaths[i]
		victim, victimOk := e.Bots[death.VictimID]

		var killer *BotState
		if death.KillerID != "" {
			killer = e.Bots[death.KillerID]
		}

		killerName := ""
		weapon := ""
		damage := 0.0

		if killer != nil {
			killer.KillStreak++
			killer.RoundKills++
			killerName = killer.Name
			weapon = killer.Weapon
			death.KillerName = killerName
			death.Weapon = weapon

			if victimOk {
				ApplyEloChange(killer, victim)
			}
		}

		if victimOk {
			damage = victim.RoundDamageTaken
			death.Damage = damage
		}

		victimName := ""
		if victimOk {
			victimName = victim.Name
		}

		e.KillFeed.Add(killerName, victimName, weapon, e.TickCount)

		e.KillEvents = append(e.KillEvents, KillEvent{
			KillerID:   death.KillerID,
			VictimID:   death.VictimID,
			VictimName: victimName,
			Weapon:     weapon,
			KillStreak: 0,
			RoundKills: 0,
		})

		// Fill in kill event stats from the killer if available.
		if killer != nil {
			last := &e.KillEvents[len(e.KillEvents)-1]
			last.KillStreak = killer.KillStreak
			last.RoundKills = killer.RoundKills
		}

		// Log the kill to the database.
		if killer != nil && victimOk {
			go InsertKillLog(context.Background(), e.Round.RoundID, killer, victim, weapon, damage, e.TickCount)
		}
	}
}

// checkAFK kicks bots that haven't sent an action within the AFK timeout.
// Removes them directly from e.Bots so they stop receiving tick messages
// immediately rather than waiting for the reader goroutine to notice the
// closed connection.
func (e *GameEngine) checkAFK() {
	c := &config.C
	var toRemove []string
	for _, bot := range e.Bots {
		if bot.LastActionTick > 0 && e.TickCount-bot.LastActionTick > c.AFKTimeoutTicks {
			SendKick(bot, "AFK timeout")
			if bot.Conn != nil {
				bot.Conn.Close()
			}
			toRemove = append(toRemove, bot.BotID)
		}
	}
	for _, id := range toRemove {
		delete(e.Bots, id)
		e.Grid.Remove(id)
	}
}

// sendBotTickUpdates sends the per-tick state update to each connected bot.
func (e *GameEngine) sendBotTickUpdates() {
	for _, bot := range e.Bots {
		if bot.SendChan == nil {
			continue
		}

		yourState := BuildYourState(bot, e.Arena, e.KillFeed, e.TickCount)

		// Build nearby entities.
		nearbyIDs := e.Grid.QueryRadius(bot.Position, config.C.ViewRadius)
		var nearby []map[string]interface{}

		for _, id := range nearbyIDs {
			if id == bot.BotID {
				continue
			}
			if other, ok := e.Bots[id]; ok {
				nearby = append(nearby, BuildBotNearbyView(other))
			}
		}

		// Include nearby pickups.
		for _, p := range e.Pickups {
			if bot.Position.DistanceTo(p.Position) <= config.C.ViewRadius {
				nearby = append(nearby, BuildPickupNearbyView(p))
			}
		}

		// Include nearby obstacles.
		for _, obs := range e.Arena.Obstacles {
			if obstacleInRange(obs, bot.Position, config.C.ViewRadius) {
				nearby = append(nearby, BuildObstacleNearbyView(obs))
			}
		}

		// Build directional hints when no bots are within view radius.
		var hints []map[string]interface{}
		nearbyBotCount := 0
		for _, id := range nearbyIDs {
			if id != bot.BotID {
				nearbyBotCount++
			}
		}
		if nearbyBotCount == 0 {
			hints = buildHints(bot, e.Bots, e.Pickups)
		}

		SendTickUpdate(bot, yourState, nearby, e.TickCount, e.Arena, hints)
	}
}

// sendLobbyStateUpdate broadcasts lobby/intermission state to spectators.
// During active rounds, waiting bots are included so the lobby tab stays populated.
func (e *GameEngine) sendLobbyStateUpdate() {
	c := &config.C

	// During lobby phase, all bots are in e.Bots. During active/intermission,
	// mid-round joiners sit in e.WaitingBots.
	lobbyBots := e.Bots
	if e.Round.Phase != PhaseLobby {
		lobbyBots = e.WaitingBots
	}

	players := make([]map[string]interface{}, 0, len(lobbyBots))
	for _, bot := range lobbyBots {
		players = append(players, map[string]interface{}{
			"name":         bot.Name,
			"avatar_color": bot.AvatarColor,
			"weapon":       bot.Weapon,
		})
	}
	sort.Slice(players, func(i, j int) bool {
		return players[i]["name"].(string) < players[j]["name"].(string)
	})

	var countdown interface{}
	if e.Round.LobbyCountdownTicks > 0 {
		secs := e.Round.LobbyCountdownTicks / c.TickRate
		if secs < 1 {
			secs = 1
		}
		countdown = secs
	}

	state := map[string]interface{}{
		"type":           "lobby_state",
		"tick":           e.TickCount,
		"bots_connected": len(lobbyBots),
		"bots_needed":    c.MinBotsToStart,
		"countdown":      countdown,
		"players":        players,
	}

	data, err := marshalJSON(state)
	if err != nil {
		slog.Error("failed to marshal lobby state", "error", err)
		return
	}

	e.spectatorsMu.RLock()
	specs := make([]*SpectatorConn, len(e.Spectators))
	copy(specs, e.Spectators)
	e.spectatorsMu.RUnlock()

	BroadcastToSpectators(specs, data)

	// Also send lobby updates to waiting bots so they know they're queued.
	for _, bot := range e.WaitingBots {
		SendLobbyUpdate(bot, len(lobbyBots), c.MinBotsToStart, nil, lobbyBots)
	}
}

// sendSpectatorUpdate broadcasts the full arena state to all spectators.
func (e *GameEngine) sendSpectatorUpdate() {
	state := BuildSpectatorState(e.Bots, e.Arena, e.Pickups, e.KillFeed, e.TickCount)
	data, err := marshalJSON(state)
	if err != nil {
		slog.Error("failed to marshal spectator state", "error", err)
		return
	}

	e.spectatorsMu.RLock()
	specs := make([]*SpectatorConn, len(e.Spectators))
	copy(specs, e.Spectators)
	e.spectatorsMu.RUnlock()

	BroadcastToSpectators(specs, data)
}

// sendEventMessages delivers buffered death and kill events to the relevant
// bots, then drains the event buffers.
func (e *GameEngine) sendEventMessages() {
	for _, ev := range e.DeathEvents {
		if victim, ok := e.Bots[ev.VictimID]; ok {
			SendDeathMessage(victim, ev)
		}
	}
	for _, ev := range e.KillEvents {
		if killer, ok := e.Bots[ev.KillerID]; ok {
			SendKillMessage(killer, ev)
		}
	}
	// No respawns - dead bots stay dead until next round.

	e.DeathEvents = e.DeathEvents[:0]
	e.KillEvents = e.KillEvents[:0]
}

// buildHints generates directional hints for a bot that has no nearby bots.
// Returns directions to the nearest 3 bots and the nearest pickup of each type.
func buildHints(bot *BotState, allBots map[string]*BotState, pickups []Pickup) []map[string]interface{} {
	type botDist struct {
		dir  Vec2
		dist float64
	}

	// Find nearest 3 alive bots.
	var candidates []botDist
	for _, other := range allBots {
		if other.BotID == bot.BotID || !other.IsAlive {
			continue
		}
		d := bot.Position.DistanceTo(other.Position)
		dir := other.Position.Sub(bot.Position).Normalized()
		candidates = append(candidates, botDist{dir: dir, dist: d})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})

	var hints []map[string]interface{}
	limit := 3
	if len(candidates) < limit {
		limit = len(candidates)
	}
	for i := 0; i < limit; i++ {
		hints = append(hints, map[string]interface{}{
			"hint_type": "bot",
			"direction": [2]float64{round1(candidates[i].dir.X()), round1(candidates[i].dir.Y())},
			"distance":  round1(candidates[i].dist),
		})
	}

	// Find nearest pickup of each type.
	type pickupDist struct {
		pType PickupType
		dir   Vec2
		dist  float64
	}
	bestPickup := make(map[PickupType]*pickupDist)
	for _, p := range pickups {
		d := bot.Position.DistanceTo(p.Position)
		dir := p.Position.Sub(bot.Position).Normalized()
		if existing, ok := bestPickup[p.Type]; !ok || d < existing.dist {
			bestPickup[p.Type] = &pickupDist{pType: p.Type, dir: dir, dist: d}
		}
	}
	for _, pd := range bestPickup {
		hints = append(hints, map[string]interface{}{
			"hint_type":   "pickup",
			"pickup_type": string(pd.pType),
			"direction":   [2]float64{round1(pd.dir.X()), round1(pd.dir.Y())},
			"distance":    round1(pd.dist),
		})
	}

	return hints
}

// BotStatsSnapshot holds a copy of the stats fields needed for persistence,
// avoiding concurrent reads on the live BotState pointers.
type BotStatsSnapshot struct {
	BotID            string
	APIKeyID         string
	Elo              int
	RoundKills       int
	RoundDeaths      int
	RoundDamageDealt float64
	RoundDamageTaken float64
	RoundDistance     float64
	RoundPickups     int
	PersistedKills       int
	PersistedDeaths      int
	PersistedDamageDealt float64
	PersistedDamageTaken float64
	PersistedDistance     float64
	PersistedPickups     int
}

// snapshotBotStats returns value copies of the stats fields needed for
// persistence, safe to read from a separate goroutine without locks.
func (e *GameEngine) snapshotBotStats() []BotStatsSnapshot {
	snaps := make([]BotStatsSnapshot, 0, len(e.Bots))
	for _, bot := range e.Bots {
		snaps = append(snaps, BotStatsSnapshot{
			BotID:            bot.BotID,
			APIKeyID:         bot.APIKeyID,
			Elo:              bot.Elo,
			RoundKills:       bot.RoundKills,
			RoundDeaths:      bot.RoundDeaths,
			RoundDamageDealt: bot.RoundDamageDealt,
			RoundDamageTaken: bot.RoundDamageTaken,
			RoundDistance:     bot.RoundDistance,
			RoundPickups:     bot.RoundPickups,
			PersistedKills:       bot.PersistedKills,
			PersistedDeaths:      bot.PersistedDeaths,
			PersistedDamageDealt: bot.PersistedDamageDealt,
			PersistedDamageTaken: bot.PersistedDamageTaken,
			PersistedDistance:     bot.PersistedDistance,
			PersistedPickups:     bot.PersistedPickups,
		})
	}
	return snaps
}
