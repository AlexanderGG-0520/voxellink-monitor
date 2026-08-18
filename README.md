# VoxelLink Monitor

An independently deployable availability-monitoring platform for Minecraft Java servers listed on VoxelLink.

It observes the player-facing Minecraft STATUS protocol, records incident evidence, provides a player-oriented Discord interface and public status surface, and provides Discord OAuth2 web management for server owners. It is intentionally decoupled from VoxelLink at runtime: the VoxelLink API imports and verifies data, but an API outage never stops existing checks.

The frozen v1 product contract is in [docs/v1-requirements.md](docs/v1-requirements.md). Architecture decisions and component boundaries are kept in [docs/architecture.md](docs/architecture.md).

Discordコミュニティオーナー向けの使い方は、[日本語ガイド](docs/owner-guide-ja.md)を参照してください。

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

For a production deployment, follow [docs/deployment.md](docs/deployment.md). GitHub Actions verifies formatting, tests, service build, Compose configuration, and the full container image on every push and pull request.

The API health endpoint is `GET /healthz`. Monitoring targets are imported only from VoxelLink through the trusted integration endpoint; arbitrary-host probing is intentionally unavailable.

To enable Discord, set `DISCORD_BOT_TOKEN` and invite the application with `applications.commands` and bot permissions to send messages in each server's status channel. The bot registers player-facing `/status`, `/uptime`, and `/incidents` commands; in a configured server status channel, they resolve that server automatically, while the optional `server` value works elsewhere. `/monitor` returns an ephemeral link to the owner console. The Worker sends only state-change notices to the configured per-server channels.

The web console is served by the `api` service. Configure a Discord OAuth2 application with the redirect URI `${PUBLIC_BASE_URL}/oauth/discord/callback`, then set `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, and a long random `SESSION_SECRET`. After Discord login, the console reads the locally synchronized VoxelLink membership snapshot and permits only `owner` and `manager` members to change monitoring or a status-channel ID.

The public home page is the Network Status page: it lists each enabled monitored server with its current public state. The owner console adds its current endpoint, last check, latency, 24-hour availability, and three most recent Incidents.

After import, the Worker refreshes VoxelLink listing metadata and verified memberships on startup and every six hours. A refresh failure only logs the error: existing endpoint configuration, checks, status pages, Incidents, and Discord notices continue from Monitor PostgreSQL.

Maintenance windows are entered in the console as JST start and end times. The Worker continues collecting observations during the window, but exposes `MAINTENANCE` and excludes those observations from Incident opening, notifications, and uptime calculations.

## Data retention

The Worker rolls observations up on startup and every six hours before applying the fixed v1 retention policy:

- Raw STATUS checks: 30 days
- 15-minute aggregates: 90 days
- Hourly aggregates: one year
- Daily uptime and Incidents: long-term

Each aggregate stores check counts, successful checks, maintenance-excluded checks, and latency statistics. Retention is idempotent, so a restart cannot discard a lower-resolution record before its replacement aggregate exists.

## Cloudflare Tunnel

`CLOUDFLARE_TUNNEL` uses the same client-side pattern as modflared: Monitor's own `cloudflared access tcp` creates a short-lived loopback TCP listener, then Monitor sends the Minecraft STATUS Ping through it while retaining the public hostname in the protocol handshake. The Worker image includes pinned `cloudflared` `2026.7.3`; the listing owner supplies only the public Tunnel hostname and port, never Cloudflare credentials.

This transport works for a Tunnel that accepts normal cloudflared clients. If a specific Cloudflare Access policy requires end-user authentication, Monitor reports `UNKNOWN` rather than using or requesting the listing owner's credentials. [Modflared project](https://www.curseforge.com/minecraft/mc-mods/modflared) · [Cloudflare CLI guide](https://developers.cloudflare.com/cloudflare-one/tutorials/cli/)

## Current foundation

The Worker now persists checks in PostgreSQL and monitors every enabled server at the configured interval. `DIRECT` and `CLOUDFLARE_SPECTRUM` endpoints use external Java STATUS Ping; tunnel transport deliberately records `PROBE_ERROR` / `UNKNOWN` until its `cloudflared` adapter is configured. The database transaction opens an Incident on the third consecutive external failure and resolves it after the second consecutive success.

VoxelLink import and ownership snapshotting are available through the contract in [docs/voxellink-integration.md](docs/voxellink-integration.md). Next: Discord Gateway notifications, encrypted Tunnel credentials, retention aggregation, and the OAuth-backed owner/status-page UI.
