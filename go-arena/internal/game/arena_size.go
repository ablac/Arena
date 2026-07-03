package game

import (
	"sync"

	"arena-server/internal/config"
)

// arenaSizeBase captures the configured arena dimensions and zone centre
// before any dynamic scaling mutates them, so repeated scaling never
// compounds and disabling dynamic sizing restores the configured values.
var arenaSizeBase struct {
	once       sync.Once
	w, h       float64
	cx, cy     float64
}

// arenaSizeMinBots anchors the small end of the down-scale curve: a 2-bot
// duel gets the smallest arena (GitHub issue #12's own example). Counts below
// it clamp to the same minimum scale.
const arenaSizeMinBots = 2

// ApplyDynamicArenaSize resizes the arena for the upcoming round based on
// how many bots are expected to play, and returns the applied linear scale.
// The map grows linearly from 1x at ArenaSizeBaseBots up to ArenaSizeMaxScale
// at >= ArenaSizeMaxBots, and shrinks linearly from 1x at ArenaSizeBaseBots
// down to ArenaSizeMinScale at <= arenaSizeMinBots (2); the zone centre
// scales with it so the zone still covers the whole map. ArenaSizeMinScale
// outside (0, 1] disables shrinking. With dynamic sizing disabled the base
// dimensions are (re)applied and the scale is 1.
//
// Must only be called from round-lifecycle code (terrain generation), never
// mid-round: gameplay systems read the dimensions live from config.
func ApplyDynamicArenaSize(botCount int) float64 {
	c := &config.C
	arenaSizeBase.once.Do(func() {
		arenaSizeBase.w, arenaSizeBase.h = c.ArenaWidth, c.ArenaHeight
		arenaSizeBase.cx, arenaSizeBase.cy = c.ZoneCenterX, c.ZoneCenterY
	})

	scale := 1.0
	if c.ArenaSizeDynamic {
		base := c.ArenaSizeBaseBots
		if botCount >= base {
			maxBots := c.ArenaSizeMaxBots
			if maxBots <= base {
				maxBots = base + 1
			}
			maxScale := c.ArenaSizeMaxScale
			if maxScale < 1 {
				maxScale = 1
			}
			t := clampFloat(float64(botCount-base)/float64(maxBots-base), 0, 1)
			scale = 1 + t*(maxScale-1)
		} else {
			minScale := c.ArenaSizeMinScale
			if minScale <= 0 || minScale > 1 {
				minScale = 1 // degenerate config: shrink disabled
			}
			span := float64(base - arenaSizeMinBots)
			if span < 1 {
				span = 1
			}
			t := clampFloat(float64(botCount-arenaSizeMinBots)/span, 0, 1)
			scale = minScale + t*(1-minScale)
		}
	}

	c.ArenaWidth = arenaSizeBase.w * scale
	c.ArenaHeight = arenaSizeBase.h * scale
	c.ZoneCenterX = arenaSizeBase.cx * scale
	c.ZoneCenterY = arenaSizeBase.cy * scale
	return scale
}
