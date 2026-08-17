# VoxelLink Monitor

An independently deployable availability-monitoring platform for Minecraft Java servers listed on VoxelLink.

It observes the player-facing Minecraft STATUS protocol, records incident evidence, provides a player-oriented Discord interface and public status surface, and provides Discord OAuth2 web management for server owners. It is intentionally decoupled from VoxelLink at runtime: the VoxelLink API imports and verifies data, but an API outage never stops existing checks.

The frozen v1 product contract is in [docs/v1-requirements.md](docs/v1-requirements.md). Architecture decisions and component boundaries are kept in [docs/architecture.md](docs/architecture.md).

## Components

- `api`: health endpoint and HTTP foundation for status pages, OAuth, and owner console.
- `worker`: scheduler, Java probes, state machine, incidents, retention, and notifications.
- `bot`: Discord player commands and per-server status-channel updates.
- `postgres`: monitor-owned source of truth.

## Quick start

```sh
cp .env.example .env
# Set POSTGRES_PASSWORD and optional integration secrets in .env
docker compose up --build
```

The API health endpoint is `GET /healthz`. Monitoring targets are imported only from VoxelLink through the trusted integration endpoint; arbitrary-host probing is intentionally unavailable.

To enable Discord, set `DISCORD_BOT_TOKEN` and invite the application with `applications.commands` and bot permissions to send messages in each server's status channel. The bot registers player-facing `/status`, `/uptime`, and `/incidents` commands; their `server` option is the VoxelLink server ID until the owner console provides a friendly server picker. The Worker sends only state-change notices to the configured per-server channels.

## Current foundation

The Worker now persists checks in PostgreSQL and monitors every enabled server at the configured interval. `DIRECT` and `CLOUDFLARE_SPECTRUM` endpoints use external Java STATUS Ping; tunnel transport deliberately records `PROBE_ERROR` / `UNKNOWN` until its `cloudflared` adapter is configured. The database transaction opens an Incident on the third consecutive external failure and resolves it after the second consecutive success.

VoxelLink import and ownership snapshotting are available through the contract in [docs/voxellink-integration.md](docs/voxellink-integration.md). Next: Discord Gateway notifications, encrypted Tunnel credentials, retention aggregation, and the OAuth-backed owner/status-page UI.
