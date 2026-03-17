package game

import (
	"arena-server/internal/config"
)

// UpdateProjectiles advances all projectiles, checks for collisions with
// obstacles and bots, applies damage on hit, and removes expired or collided
// projectiles.
func UpdateProjectiles(projectiles *[]Projectile, bots map[string]*BotState, obstacles []Obstacle, tickCount int, dt float64) {
	maxAge := int(config.C.ProjectileMaxAgeSecs * float64(config.C.TickRate))

	alive := (*projectiles)[:0]

	for i := range *projectiles {
		proj := &(*projectiles)[i]

		// 1. Move the projectile.
		proj.Position = proj.Position.Add(proj.Direction.Scale(proj.Speed * dt))
		proj.AgeTicks++

		// 2. Check max age.
		if proj.AgeTicks >= maxAge {
			continue // Remove: expired
		}

		// 3. Check obstacle/terrain collision.
		//    Ray-march from previous position to current position so fast
		//    projectiles can never skip over a wall cell.
		if ActiveTerrain != nil {
			hitWall := false
			prevPos := proj.Position.Sub(proj.Direction.Scale(proj.Speed * dt))
			prevCell := ActiveTerrain.WorldToGrid(prevPos)
			curCell := ActiveTerrain.WorldToGrid(proj.Position)

			if ActiveTerrain.IsBlocked(curCell[0], curCell[1]) {
				hitWall = true
			} else if prevCell != curCell {
				// DDA ray-march: step through cells using world-space line
				// to avoid rounding into adjacent blocked cells.
				dx := curCell[0] - prevCell[0]
				dy := curCell[1] - prevCell[1]
				adx := dx
				if adx < 0 {
					adx = -adx
				}
				ady := dy
				if ady < 0 {
					ady = -ady
				}
				steps := adx
				if ady > steps {
					steps = ady
				}
				for s := 1; s < steps; s++ {
					cx := prevCell[0] + dx*s/steps
					cy := prevCell[1] + dy*s/steps
					if ActiveTerrain.IsBlocked(cx, cy) {
						hitWall = true
						break
					}
				}
			}
			if hitWall {
				continue // Remove: hit wall
			}
		} else if CollidesWithObstacle(proj.Position.X(), proj.Position.Y(), obstacles, 0.5) != nil {
			continue // Remove: hit obstacle
		}

		// 4. Check bot hits (grid-based: projectile hits bot in same cell).
		hit := false

		for _, bot := range bots {
			if !bot.IsAlive || bot.BotID == proj.OwnerID {
				continue
			}

			if !IsInRange(proj.Position, bot.Position, 0) {
				continue
			}

			// We have a hit.
			owner, ownerOk := bots[proj.OwnerID]

			if bot.InvulnTicks > 0 {
				// Invulnerable: consume projectile, no damage.
				hit = true
				break
			}

			if ownerOk {
				ApplyDamage(bot, owner, proj.Damage, proj.Weapon, tickCount)
				ApplyGridKnockback(bot, proj.Position.Sub(proj.Direction.Scale(proj.Speed*dt)), 1, obstacles)
				owner.RoundShotsHit++
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
			continue // Remove: out of arena
		}

		// Projectile survives this tick.
		alive = append(alive, *proj)
	}

	*projectiles = alive
}
