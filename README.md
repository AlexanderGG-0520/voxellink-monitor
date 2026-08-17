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

The `migrate` service applies each embedded SQL migration before `api`, `worker`, or `bot` starts. It records completed versions in `schema_migrations`; re-running Compose is safe. Deployments created with the earlier initialization-only layout are baselined automatically, then receive any missing later migration.

The API health endpoint is `GET /healthz`. Monitoring targets are imported only from VoxelLink through the trusted integration endpoint; arbitrary-host probing is intentionally unavailable.

To enable Discord, set `DISCORD_BOT_TOKEN` and invite the application with `applications.commands` and bot permissions to send messages in each server's status channel. The bot registers player-facing `/status`, `/uptime`, and `/incidents` commands; their `server` option is the VoxelLink server ID until the owner console provides a friendly server picker. The Worker sends only state-change notices to the configured per-server channels.

The web console is served by the `api` service. Configure a Discord OAuth2 application with the redirect URI `${PUBLIC_BASE_URL}/oauth/discord/callback`, then set `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, and a long random `SESSION_SECRET`. After Discord login, the console reads the locally synchronized VoxelLink membership snapshot and permits only `owner` and `manager` members to change monitoring or a status-channel ID.

The public home page is the Network Status page: it lists each enabled monitored server with its current public state. The owner console adds its current endpoint, last check, latency, 24-hour availability, and three most recent Incidents.

Maintenance windows are entered in the console as JST start and end times. The Worker continues collecting observations during the window, but exposes `MAINTENANCE` and excludes those observations from Incident opening, notifications, and uptime calculations.

## Data retention

The Worker rolls observations up on startup and every six hours before applying the fixed v1 retention policy:

- Raw STATUS checks: 30 days
- 15-minute aggregates: 90 days
- Hourly aggregates: one year
- Daily uptime and Incidents: long-term

Each aggregate stores check counts, successful checks, maintenance-excluded checks, and latency statistics. Retention is idempotent, so a restart cannot discard a lower-resolution record before its replacement aggregate exists.

## Cloudflare Tunnel

`CLOUDFLARE_TUNNEL` uses `cloudflared access tcp` to create a short-lived loopback TCP listener, then sends the Minecraft STATUS Ping through that listener while retaining the public hostname in the protocol handshake. The Worker image contains pinned `cloudflared` `2026.7.3` and mounts `${CLOUDFLARED_CONFIG_DIR:-./cloudflared}` read-only at its cloudflared configuration directory. Keep the Access login or service-credential material only in that untracked host directory; it is never written to PostgreSQL or returned by Monitor APIs.

If cloudflared cannot authenticate, start, or create its listener, Monitor reports `UNKNOWN`, never `OUTAGE`. Cloudflare documents `cloudflared access tcp` as the client-side route for arbitrary TCP and recommends service tokens for unattended automation. [Cloudflare CLI guide](https://developers.cloudflare.com/cloudflare-one/tutorials/cli/) · [service-token guide](https://developers.cloudflare.com/cloudflare-one/access-controls/service-credentials/service-tokens/)

## Current foundation

The Worker now persists checks in PostgreSQL and monitors every enabled server at the configured interval. `DIRECT` and `CLOUDFLARE_SPECTRUM` endpoints use external Java STATUS Ping; tunnel transport deliberately records `PROBE_ERROR` / `UNKNOWN` until its `cloudflared` adapter is configured. The database transaction opens an Incident on the third consecutive external failure and resolves it after the second consecutive success.

VoxelLink import and ownership snapshotting are available through the contract in [docs/voxellink-integration.md](docs/voxellink-integration.md). Next: Discord Gateway notifications, encrypted Tunnel credentials, retention aggregation, and the OAuth-backed owner/status-page UI.
