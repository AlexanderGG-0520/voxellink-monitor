# VoxelLink Monitor v1 requirements

## Product boundary

VoxelLink Monitor is an independently deployable availability-monitoring platform for Minecraft Java servers listed on VoxelLink. VoxelLink is an integration and ownership source, never a runtime dependency of established monitoring. The monitor has its own PostgreSQL database and preserves a local copy of monitoring configuration.

## v1 scope (frozen)

- Java Edition Server List Ping: TCP connection, Minecraft handshake, STATUS request/response. A port being open is not a successful check.
- One external probe, normally every 60 seconds; a failed check triggers two confirmation retries at 10-second intervals.
- A confirmed outage is three consecutive failed checks. Recovery is two consecutive successful checks at 10-second intervals.
- Public states: `OPERATIONAL`, `DEGRADED`, `OUTAGE`, `MAINTENANCE`, `UNKNOWN`.
- `UNKNOWN` means the monitor/probe transport failed and must not be presented as a server outage. `MAINTENANCE` excludes the scheduled period from availability and incident notification.
- Incidents record first failure, confirmation, first recovery observation, confirmed resolution, and classified cause.
- `DEGRADED` is reserved for a functioning but materially abnormal service (latency deviation or flapping); its threshold policy is configurable without changing the public API.
- Raw checks: 30 days. 5/15-minute aggregate data: 90 days. Hourly aggregate data: one year. Daily availability, incidents, and maintenance history: long-term retention.
- The web console uses Discord OAuth2. Owners/managers see only VoxelLink servers for which they have verified membership. Discord bot commands are player-facing (`/status`, `/uptime`, `/incidents`) and direct management to the web console.
- Each listed server may have its own Discord status channel. State changes are posted once per server channel; messages are concise and player-facing.
- Registration imports the hostname, port, and ownership from the VoxelLink API. Existing monitoring, status pages, incidents, and notifications continue when VoxelLink is unavailable.
- Transports: `DIRECT`, `CLOUDFLARE_SPECTRUM` (public endpoint ping), and `CLOUDFLARE_TUNNEL` (a cloudflared client adapter). The first two share external TCP behavior. Tunnel credentials are treated as secrets and never exposed by the API.

## Non-goals for v1

Bedrock probing, public arbitrary-host monitoring, multi-region consensus, an on-premise agent, SMS notification, and AI incident analysis are explicitly post-v1. Arbitrary endpoints are prohibited to avoid turning the probe into a scanning service.

