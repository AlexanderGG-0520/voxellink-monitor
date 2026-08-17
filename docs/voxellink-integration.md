# VoxelLink integration contract

Monitor is an independent service. It calls VoxelLink only when an owner enables monitoring or metadata is explicitly refreshed; the Worker reads only Monitor PostgreSQL.

## Required VoxelLink endpoint

`GET /api/v1/monitor/servers/{server_id}` authenticates the Monitor service with `Authorization: Bearer <service-token>`. VoxelLink must verify that its service credential is allowed to access this listing and return only verified ownership information.

```json
{
  "server": {
    "id": "voxel-server-id",
    "name": "Example Server",
    "hostname": "play.example.com",
    "port": 25565,
    "transport": "DIRECT"
  },
  "members": [
    { "discord_user_id": "123456789", "role": "owner" }
  ]
}
```

`transport` is `DIRECT`, `CLOUDFLARE_SPECTRUM`, or `CLOUDFLARE_TUNNEL`; roles are `owner`, `manager`, or `viewer`. Monitor rejects missing or invalid endpoints and roles.

## Monitor import endpoint

`POST /api/v1/integrations/voxellink/import` accepts `{ "external_server_id": "..." }` and requires `Authorization: Bearer <INTEGRATION_SYNC_TOKEN>`. It is intended for the trusted VoxelLink backend, not browsers or Discord clients. The endpoint fetches the canonical listing before applying it locally.

The transactional import updates local endpoint and membership snapshots but never deletes checks or incidents. If VoxelLink is down, import/refresh fails while existing monitoring, status pages, and Discord notifications continue.
