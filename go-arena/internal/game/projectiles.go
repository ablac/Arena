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

		// 3. Check obstacle collision.
		if CollidesWithObstacle(proj.Position.X(), proj.Position.Y(), obstacles, 0.5) != nil {
			continue // Remove: hit obstacle
		}

		// 4. Check bot hits.
		hitRadius := config.C.ProjectileHitRadius + config.C.BotRadius
		hit := false

		for _, bot := range bots {
			if !bot.IsAlive || bot.BotID == proj.OwnerID {
				continue
			}

			dist := proj.Position.DistanceTo(bot.Position)
			if dist > hitRadius {
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
				ApplyHitKnockback(bot, proj.Position.Sub(proj.Direction.Scale(proj.Speed*dt)), 2.5, obstacles)
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
