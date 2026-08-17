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

The API health endpoint is `GET /healthz`. The development-only Java protocol check is `GET /api/v1/probe?host=example.org`; production registration will only allow VoxelLink-verified endpoints.

## Current foundation

The repository deliberately starts with the durable boundaries rather than a throwaway checker: Compose topology, PostgreSQL schema, Java STATUS protocol implementation, and the v1 incident confirmation state machine are implemented. OAuth, VoxelLink client, encrypted Tunnel credentials, persistent repositories, Discord Gateway adapter, aggregate jobs, and owner/status-page UI build on these explicit interfaces next.

