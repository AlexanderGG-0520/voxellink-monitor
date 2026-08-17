# Architecture

```text
VoxelLink API ── import / ownership sync ──> Monitor PostgreSQL
                                              ↑          ↑
Discord Bot <── player status / notices ── Worker      API/Web UI
                                              │
                            direct / Spectrum ├── Java STATUS Ping
                            Tunnel adapter ───┴── cloudflared client
```

The worker owns polling and the incident state machine. The API is read/management-facing and must not be required for checks to continue. The Discord bot reads the same monitor-owned state and does not make availability decisions. A future regional probe is another worker implementation, not a redesign of the database or state model.

Cloudflare Spectrum is monitored at its public Minecraft endpoint. Cloudflare Tunnel is a separate adapter which will manage a local `cloudflared access tcp` listener and classify client/transport failures as `UNKNOWN`, rather than declaring the origin down.
