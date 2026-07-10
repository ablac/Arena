package game

import (
	"arena-server/internal/config"
	"math"
)

// UpdateProjectiles advances all projectiles, checks for collisions with
// obstacles and bots, applies damage on hit, and removes expired or collided
// projectiles.
func UpdateProjectiles(projectiles *[]Projectile, bots map[string]*BotState, obstacles []Obstacle, arenaEvents *[]ArenaEvent, tickCount int, dt float64) {
	alive := (*projectiles)[:0]

	for i := range *projectiles {
		proj := &(*projectiles)[i]
		prevPos := proj.Position

		// 1. Move the projectile.
		proj.Position = proj.Position.Add(proj.Direction.Scale(proj.Speed * dt))
		proj.AgeTicks++

		// 2. Check max age.
		if proj.AgeTicks >= proj.MaxAge {
			if proj.Weapon == "bow" && arenaEvents != nil {
				*arenaEvents = append(*arenaEvents, buildBowImpactEvent(proj.ID, proj.OwnerID, proj.Color, proj.Position, tickCount, "", proj.Intensity))
			}
			continue // Remove: expired
		}

		// 3. Check obstacle/terrain collision.
		//    Ray-march from previous position to current position so fast
		//    projectiles can never skip over a wall cell.
		if ActiveTerrain != nil {
			curCell := ActiveTerrain.WorldToGrid(proj.Position)

			// Exact voxel traversal of the swept segment so fast projectiles
			// can never skip over a wall cell, even on steep diagonals.
			hitWall := ActiveTerrain.IsBlocked(curCell[0], curCell[1]) ||
				ActiveTerrain.SegmentBlocked(prevPos, proj.Position)
			if hitWall {
				if proj.Weapon == "bow" && arenaEvents != nil {
					*arenaEvents = append(*arenaEvents, buildBowImpactEvent(proj.ID, proj.OwnerID, proj.Color, proj.Position, tickCount, "", proj.Intensity))
				}
				continue // Remove: hit wall
			}
		} else if CollidesWithObstacle(proj.Position.X(), proj.Position.Y(), obstacles, 0.5) != nil {
			if proj.Weapon == "bow" && arenaEvents != nil {
				*arenaEvents = append(*arenaEvents, buildBowImpactEvent(proj.ID, proj.OwnerID, proj.Color, proj.Position, tickCount, "", proj.Intensity))
			}
			continue // Remove: hit obstacle
		}

		// 4. Check bot hits using a swept segment so fast projectiles can still
		// connect without landing in the exact same grid cell.
		hit := false
		projectileHitRadius := proj.HitRadius
		if projectileHitRadius <= 0 {
			projectileHitRadius = config.C.ProjectileHitRadius
		}
		hitRadius := config.C.BotRadius + projectileHitRadius

		for _, bot := range bots {
			if !bot.IsAlive || bot.BotID == proj.OwnerID {
				continue
			}

			if distancePointToSegment(bot.Position, prevPos, proj.Position) > hitRadius {
				continue
			}

			// We have a hit.
			owner, ownerOk := bots[proj.OwnerID]

			if bot.InvulnTicks > 0 {
				// Invulnerable: consume projectile, no damage.
				if proj.Weapon == "bow" && arenaEvents != nil {
					*arenaEvents = append(*arenaEvents, buildBowImpactEvent(proj.ID, proj.OwnerID, proj.Color, bot.Position, tickCount, bot.BotID, proj.Intensity))
				}
				hit = true
				break
			}

			if ownerOk {
				ApplyDamage(bot, owner, proj.Damage, proj.Weapon, tickCount)
				ApplyAttributedGridKnockback(bot, owner, proj.Position.Sub(proj.Direction.Scale(proj.Speed*dt)), 1, obstacles, proj.Weapon, tickCount)
				owner.RoundShotsHit++
			}
			if proj.Weapon == "bow" && arenaEvents != nil {
				*arenaEvents = append(*arenaEvents, buildBowImpactEvent(proj.ID, proj.OwnerID, proj.Color, bot.Position, tickCount, bot.BotID, proj.Intensity))
			}

			hit = true
			break // One hit per projectile.
		}

		if hit {
			continue // Remove: hit a bot
		}

		// 5. Check out of bounds.
		if proj.Position.X() < 0 || proj.Position.X() > config.C.ArenaWidth ||
			proj.Position.Y() < 0 || proj.Position.Y() > config.C.ArenaHeight {
			if proj.Weapon == "bow" && arenaEvents != nil {
				*arenaEvents = append(*arenaEvents, buildBowImpactEvent(proj.ID, proj.OwnerID, proj.Color, proj.Position, tickCount, "", proj.Intensity))
			}
			continue // Remove: out of arena
		}

		// Projectile survives this tick.
		alive = append(alive, *proj)
	}

	*projectiles = alive
}

func distancePointToSegment(point, segStart, segEnd Vec2) float64 {
	seg := segEnd.Sub(segStart)
	segLenSq := seg.X()*seg.X() + seg.Y()*seg.Y()
	if segLenSq <= 1e-9 {
		return point.DistanceTo(segStart)
	}

	toPoint := point.Sub(segStart)
	t := (toPoint.X()*seg.X() + toPoint.Y()*seg.Y()) / segLenSq
	t = math.Max(0, math.Min(1, t))

	closest := segStart.Add(seg.Scale(t))
	return point.DistanceTo(closest)
}
