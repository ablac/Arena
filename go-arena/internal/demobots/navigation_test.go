package demobots

import "testing"

func navigationTick(tick int, position [2]float64, speed float64) map[string]interface{} {
	return map[string]interface{}{
		"type": "tick",
		"tick": float64(tick),
		"your_state": map[string]interface{}{
			"position": position,
			"speed":    speed,
			"hp":       float64(100),
			"max_hp":   float64(100),
			"is_alive": true,
		},
	}
}

func TestNavigationStateEscapesRepeatedSameCellMovement(t *testing.T) {
	walls := make([][2]int, 0, 20)
	for row := 0; row < 20; row++ {
		walls = append(walls, [2]int{5, row})
	}
	setTerrain(t, 20, 20, walls)

	var navigation navigationState
	requested := moveDir([2]float64{1, 0})
	position := [2]float64{4, 10}
	var got actionResult
	for tick := 1; tick <= 4; tick++ {
		got = navigation.stabilize(navigationTick(tick, position, 5.5), requested, "wall-bot")
	}

	if got.Action != "move" || got.Direction == nil || *got.Direction == [2]float64{} {
		t.Fatalf("stalled movement recovery = %+v, want a non-zero move", got)
	}
	if *got.Direction == *requested.Direction {
		t.Fatalf("stalled movement repeated blocked direction %v", *got.Direction)
	}
}

func TestNavigationStateBreaksAlternatingCellLoop(t *testing.T) {
	setTerrain(t, 20, 20, nil)

	var navigation navigationState
	a := [2]float64{7, 10}
	b := [2]float64{8, 10}
	steps := []struct {
		position  [2]float64
		requested actionResult
	}{
		{position: a, requested: moveDir([2]float64{1, 0})},
		{position: b, requested: moveDir([2]float64{-1, 0})},
		{position: a, requested: moveDir([2]float64{1, 0})},
		{position: b, requested: moveDir([2]float64{-1, 0})},
	}

	var got actionResult
	for i, step := range steps {
		got = navigation.stabilize(navigationTick(i+1, step.position, 5.5), step.requested, "loop-bot")
	}

	if got.Action != "move" || got.Direction == nil || *got.Direction == [2]float64{} {
		t.Fatalf("oscillation recovery = %+v, want a non-zero move", got)
	}
	if *got.Direction == [2]float64{-1, 0} {
		t.Fatalf("oscillation recovery returned directly to the previous cell: %+v", got)
	}
}

func TestNavigationStateBreaksAlternatingLoopAcrossFractionalPacing(t *testing.T) {
	setTerrain(t, 20, 20, nil)

	var navigation navigationState
	a := [2]float64{7, 10}
	b := [2]float64{8, 9}
	towardB := moveDir([2]float64{1, -1})
	towardA := moveDir([2]float64{-1, 1})
	steps := []struct {
		position  [2]float64
		requested actionResult
	}{
		{a, towardB}, {a, towardB},
		{b, towardA}, {b, towardA},
		{a, towardB}, {a, towardB},
		{b, towardA},
	}

	var got actionResult
	for i, step := range steps {
		got = navigation.stabilize(navigationTick(i+1, step.position, 5.5), step.requested, "paced-loop-bot")
	}

	if navigation.recovery == nil {
		t.Fatal("fractional A,A,B,B loop did not arm bounded navigation recovery")
	}
	if got.Action != "move" || got.Direction == nil || *got.Direction == *towardA.Direction {
		t.Fatalf("fractional loop recovery = %+v, want a third/lateral route", got)
	}
}

func TestNavigationStateAllowsFractionalMovementPacing(t *testing.T) {
	setTerrain(t, 20, 20, nil)

	var navigation navigationState
	requested := moveDir([2]float64{1, 0})
	position := [2]float64{5, 5}
	for tick := 1; tick <= 3; tick++ {
		got := navigation.stabilize(navigationTick(tick, position, 4.5), requested, "paced-bot")
		if got.Direction == nil || *got.Direction != *requested.Direction {
			t.Fatalf("tick %d treated normal fractional movement pacing as a stall: %+v", tick, got)
		}
	}
}

func TestNavigationStateRecoversFailedAnchorButPreservesTargetGrapple(t *testing.T) {
	setTerrain(t, 20, 20, nil)

	position := [2]float64{5, 5}
	anchor := grapplePos([2]float64{10, 5})
	var navigation navigationState
	var got actionResult
	for tick := 1; tick <= 4; tick++ {
		got = navigation.stabilize(navigationTick(tick, position, 5.5), anchor, "failed-anchor-bot")
	}
	if navigation.recovery == nil {
		t.Fatal("repeated failed positional grapple did not arm movement recovery")
	}
	if got.Action != "move" || got.Direction == nil || *got.Direction == [2]float64{} {
		t.Fatalf("failed positional grapple recovery = %+v, want safe movement", got)
	}

	targetGrapple := grapple("enemy")
	got = navigation.stabilize(navigationTick(5, position, 5.5), targetGrapple, "failed-anchor-bot")
	if got.Action != "grapple" || got.Target != "enemy" || got.TargetPosition != nil {
		t.Fatalf("movement recovery displaced target-ID combat grapple: %+v", got)
	}
	if navigation.recovery != nil {
		t.Fatal("target-ID combat grapple did not clear positional movement recovery")
	}
}

func TestNavigationRecoveryYieldsToTacticalAction(t *testing.T) {
	walls := make([][2]int, 0, 20)
	for row := 0; row < 20; row++ {
		walls = append(walls, [2]int{5, row})
	}
	setTerrain(t, 20, 20, walls)

	var navigation navigationState
	position := [2]float64{4, 10}
	blockedMove := moveDir([2]float64{1, 0})
	for tick := 1; tick <= 4; tick++ {
		_ = navigation.stabilize(navigationTick(tick, position, 5.5), blockedMove, "combat-bot")
	}
	if navigation.recovery == nil {
		t.Fatal("test setup did not enter movement recovery")
	}

	attack := actionResult{Action: "attack", Target: "enemy"}
	got := navigation.stabilize(navigationTick(5, position, 5.5), attack, "combat-bot")
	if got.Action != "attack" || got.Target != "enemy" {
		t.Fatalf("movement recovery displaced a tactical action: %+v", got)
	}
	if navigation.recovery != nil {
		t.Fatal("tactical action did not clear stale movement recovery")
	}

	dodgeDirection := [2]float64{0, -1}
	dodge := actionResult{Action: "dodge", Direction: &dodgeDirection}
	got = navigation.stabilize(navigationTick(6, position, 5.5), dodge, "combat-bot")
	if got.Action != "dodge" || got.Direction == nil || *got.Direction != dodgeDirection {
		t.Fatalf("navigation rewrote an emergency dodge: %+v", got)
	}
}

func TestNavigationRecoveryYieldsToIntentionalIdle(t *testing.T) {
	walls := make([][2]int, 0, 20)
	for row := 0; row < 20; row++ {
		walls = append(walls, [2]int{5, row})
	}
	setTerrain(t, 20, 20, walls)

	var navigation navigationState
	position := [2]float64{4, 10}
	blockedMove := moveDir([2]float64{1, 0})
	for tick := 1; tick <= 4; tick++ {
		_ = navigation.stabilize(navigationTick(tick, position, 5.5), blockedMove, "brace-bot")
	}
	if navigation.recovery == nil {
		t.Fatal("test setup did not enter movement recovery")
	}

	got := navigation.stabilize(navigationTick(5, position, 5.5), idle(), "brace-bot")
	if got.Action != "idle" {
		t.Fatalf("movement recovery displaced an intentional tactical idle: %+v", got)
	}
	if navigation.recovery != nil {
		t.Fatal("intentional idle did not clear stale movement recovery")
	}
}

func TestNavigationTacticalActionClearsPendingStallEvidence(t *testing.T) {
	setTerrain(t, 20, 20, nil)

	var navigation navigationState
	position := [2]float64{4, 10}
	east := moveDir([2]float64{1, 0})
	_ = navigation.stabilize(navigationTick(1, position, 5.5), east, "retarget-bot")
	_ = navigation.stabilize(navigationTick(2, position, 5.5), east, "retarget-bot")
	_ = navigation.stabilize(navigationTick(3, position, 5.5), actionResult{Action: "attack", Target: "enemy"}, "retarget-bot")

	west := moveDir([2]float64{-1, 0})
	got := navigation.stabilize(navigationTick(4, position, 5.5), west, "retarget-bot")
	if got.Action != "move" || got.Direction == nil || *got.Direction != *west.Direction {
		t.Fatalf("stale wall evidence displaced a new movement objective: %+v", got)
	}
	if navigation.recovery != nil {
		t.Fatal("stale wall evidence started recovery after a tactical action reset")
	}
}

func TestNavigationRecoveryYieldsToChangedMovementObjective(t *testing.T) {
	walls := make([][2]int, 0, 20)
	for row := 0; row < 20; row++ {
		walls = append(walls, [2]int{5, row})
	}
	setTerrain(t, 20, 20, walls)

	var navigation navigationState
	position := [2]float64{4, 10}
	east := moveDir([2]float64{1, 0})
	for tick := 1; tick <= 4; tick++ {
		_ = navigation.stabilize(navigationTick(tick, position, 5.5), east, "flee-bot")
	}
	if navigation.recovery == nil {
		t.Fatal("test setup did not enter movement recovery")
	}

	west := moveDir([2]float64{-1, 0})
	got := navigation.stabilize(navigationTick(5, position, 5.5), west, "flee-bot")
	if got.Action != "move" || got.Direction == nil || *got.Direction != *west.Direction {
		t.Fatalf("wall recovery displaced a changed movement objective: %+v", got)
	}
	if navigation.recovery != nil {
		t.Fatal("changed movement objective did not clear stale recovery")
	}
}

func TestNavigationOscillationRecoveryIsBriefButNotSingleTick(t *testing.T) {
	setTerrain(t, 20, 20, nil)

	var navigation navigationState
	a := [2]float64{7, 10}
	b := [2]float64{8, 10}
	east := moveDir([2]float64{1, 0})
	west := moveDir([2]float64{-1, 0})
	steps := []struct {
		position  [2]float64
		requested actionResult
	}{
		{position: a, requested: east},
		{position: b, requested: west},
		{position: a, requested: east},
		{position: b, requested: west},
	}
	for i, step := range steps {
		_ = navigation.stabilize(navigationTick(i+1, step.position, 5.5), step.requested, "loop-grace-bot")
	}
	if navigation.recovery == nil {
		t.Fatal("test setup did not enter oscillation recovery")
	}

	_ = navigation.stabilize(navigationTick(5, a, 5.5), east, "loop-grace-bot")
	if navigation.recovery == nil {
		t.Fatal("oscillation recovery yielded before it could take a second corrective step")
	}
	_ = navigation.stabilize(navigationTick(6, b, 5.5), west, "loop-grace-bot")
	got := navigation.stabilize(navigationTick(7, a, 5.5), east, "loop-grace-bot")
	if got.Action != "move" || got.Direction == nil || *got.Direction != *east.Direction {
		t.Fatalf("expired oscillation grace displaced a persistent changed direction: %+v", got)
	}
	if navigation.recovery != nil {
		t.Fatal("oscillation recovery did not yield after its bounded direction-change grace")
	}
}

func TestNavigationRecoveryClearsWhenDynamicRouteBecomesUnavailable(t *testing.T) {
	walls := make([][2]int, 0, 8)
	for dc := -1; dc <= 1; dc++ {
		for dr := -1; dr <= 1; dr++ {
			if dc != 0 || dr != 0 {
				walls = append(walls, [2]int{5 + dc, 5 + dr})
			}
		}
	}
	setTerrain(t, 20, 20, walls)

	east := moveDir([2]float64{1, 0})
	navigation := navigationState{recovery: &navigationRecovery{
		waypoint: [2]float64{10, 5}, requestedDirection: [2]int{1, 0}, ticksLeft: navigationRecoveryTicks,
	}}
	got := navigation.stabilize(navigationTick(1, [2]float64{5, 5}, 5.5), east, "dynamic-block-bot")
	if got.Action != "move" || got.Direction == nil || *got.Direction != *east.Direction {
		t.Fatalf("unreachable recovery displaced the current movement objective: %+v", got)
	}
	if navigation.recovery != nil {
		t.Fatal("unreachable recovery retained an infinite idle retry")
	}
}
