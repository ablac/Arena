# Contributing to AI Battle Arena ⚔️

Thanks for contributing! Here's how we work.

## Workflow

1. **Never push directly to `main`** — it's our production branch
2. Create a feature branch from `develop`:
   ```bash
   git checkout develop
   git pull origin develop
   git checkout -b feature/your-feature-name
   ```
3. Make your changes, commit with clear messages
4. Push your branch and open a PR to `develop`:
   ```bash
   git push origin feature/your-feature-name
   ```
5. Get at least 1 review approval
6. Squash-merge into `develop`
7. `develop` → `main` merges happen for releases

## Branch Naming

- `feature/description` — new features
- `fix/description` — bug fixes
- `docs/description` — documentation only
- `refactor/description` — code restructuring

## Commit Messages

Use clear, concise messages:
```
feat: add bow weapon type
fix: bot disconnect during loadout phase
docs: update API examples
refactor: extract combat logic into module
```

## Project Structure

```
go-arena/           # Go server (production)
├── cmd/            # Entry points
├── internal/
│   ├── api/        # REST endpoints
│   ├── config/     # Configuration
│   ├── db/         # Database layer
│   ├── game/       # Game engine, combat, movement
│   ├── security/   # Auth, rate limiting
│   └── ws/         # WebSocket handlers
frontend/           # Spectator UI (HTML/JS/BabylonJS)
sdk/python/         # Python bot SDK
server/             # Legacy Python server (reference only)
```

## Running Locally

```bash
docker compose up -d
# Server runs at http://localhost:8700
# Generate an API key: POST /api/v1/keys/generate
```

## Questions?

Open an issue or ask in the team chat!
