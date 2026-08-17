# Deployment guide

VoxelLink Monitor is deployed as four Compose services: `postgres`, one-shot `migrate`, `api`, `worker`, and `bot`. The `migrate` service must complete before the long-running services start.

## Prepare secrets

Create a `.env` from `.env.example` and use a secret manager or deployment environment to provide the real values. Do not commit `.env` or the `cloudflared/` directory.

| Variable | Source |
| --- | --- |
| `POSTGRES_PASSWORD` | Generate a long random password. |
| `DATABASE_URL` | Use the same database name, user, and password as the Compose PostgreSQL service. |
| `VOXELLINK_API_BASE_URL`, `VOXELLINK_API_TOKEN` | VoxelLink backend's Monitor integration endpoint and service token. |
| `INTEGRATION_SYNC_TOKEN` | Generate a separate long random shared token for VoxelLink-to-Monitor imports. |
| `DISCORD_BOT_TOKEN` | Discord Developer Portal bot token. |
| `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET` | The same Discord application's OAuth2 credentials. |
| `PUBLIC_BASE_URL` | The public HTTPS URL, without a trailing slash. |
| `SESSION_SECRET` | Generate at least 32 random bytes; rotating it logs out owner-console sessions. |

Register `${PUBLIC_BASE_URL}/oauth/discord/callback` as the Discord OAuth2 redirect URI. Invite the bot with `applications.commands` and permission to send messages in the server status channels.

## Cloudflare Tunnel targets

For a monitored server using `CLOUDFLARE_TUNNEL`, provide its Cloudflare Access credential material in the host's `cloudflared/` directory (or set `CLOUDFLARED_CONFIG_DIR`). The Worker mounts it read-only. Use a service token protected by a Service Auth policy for unattended monitoring; do not put this material into VoxelLink Monitor's database or `.env`.

## Start and verify

```sh
docker compose up -d --build
docker compose ps
curl -fsS "${PUBLIC_BASE_URL}/healthz"
```

The API returns `204 No Content` when ready. Review the `migrate` container log first if a dependent service does not start. On first launch, import a VoxelLink listing through the trusted integration endpoint, then verify the target appears on the public status page and owner console.

## Upgrades and backups

Pull the new image or source, then run `docker compose up -d --build`. Embedded migrations are recorded in `schema_migrations` and are safe to rerun. Back up the PostgreSQL volume before upgrades and periodically thereafter; it contains the monitor configuration, checks, rollups, memberships, and Incident history.
