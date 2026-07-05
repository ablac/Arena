# Local dev server: builds and runs arena-server against the docker-compose
# arena-db / arena-redis containers (host ports 5433 / 6380), with demo bots
# on and short caves-only rounds for fast iteration.
#
#   docker compose up -d arena-db arena-redis
#   powershell -File scripts/dev-server.ps1
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot

# Source the compose .env so we connect with the same credentials and host
# ports the containers were created with.
$dotenv = @{}
$envFile = Join-Path $repo '.env'
if (Test-Path $envFile) {
  Get-Content $envFile | ForEach-Object {
    if ($_ -match '^\s*([A-Z0-9_]+)\s*=\s*(.*)\s*$') { $dotenv[$Matches[1]] = $Matches[2] }
  }
}
function DotEnvOr($key, $fallback) { if ($dotenv.ContainsKey($key) -and $dotenv[$key]) { $dotenv[$key] } else { $fallback } }

$env:ARENA_DB_HOST = 'localhost'
$env:ARENA_DB_PORT = DotEnvOr 'ARENA_DB_HOST_PORT' '5433'
$env:ARENA_DB_USER = DotEnvOr 'ARENA_DB_USER' 'arena_user'
$env:ARENA_DB_NAME = DotEnvOr 'ARENA_DB_NAME' 'arena'
$env:ARENA_DB_PASSWORD = DotEnvOr 'ARENA_DB_PASSWORD' 'changeme_arena_2026'
$env:ARENA_REDIS_HOST = 'localhost'
$env:ARENA_REDIS_PORT = DotEnvOr 'ARENA_REDIS_HOST_PORT' '6380'

# Fast map rotation for testing; force caves every round.
if (-not $env:ARENA_MAP_SHAPE) { $env:ARENA_MAP_SHAPE = 'caves' }
if (-not $env:ARENA_ROUND_DURATION) { $env:ARENA_ROUND_DURATION = '60' }
if (-not $env:ARENA_INTERMISSION_TIME) { $env:ARENA_INTERMISSION_TIME = '5' }

# Report the working-tree commit through the runtime fallback path.
$env:ARENA_GIT_COMMIT = (git -C $repo rev-parse HEAD)

Set-Location (Join-Path $repo 'go-arena')
go build -o arena-server-dev.exe ./cmd/arena-server
if ($LASTEXITCODE -ne 0) { exit 1 }
& .\arena-server-dev.exe
