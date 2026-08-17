CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE public_status AS ENUM ('OPERATIONAL', 'DEGRADED', 'OUTAGE', 'MAINTENANCE', 'UNKNOWN');
CREATE TYPE transport_kind AS ENUM ('DIRECT', 'CLOUDFLARE_TUNNEL', 'CLOUDFLARE_SPECTRUM');
CREATE TYPE check_outcome AS ENUM ('SUCCESS', 'DNS_FAILURE', 'CONNECTION_REFUSED', 'CONNECT_TIMEOUT', 'STATUS_TIMEOUT', 'CONNECTION_RESET', 'INVALID_STATUS_RESPONSE', 'PROBE_ERROR');

CREATE TABLE monitored_servers (
  id uuid PRIMARY KEY,
  external_source text NOT NULL DEFAULT 'voxellink',
  external_server_id text NOT NULL,
  name text NOT NULL,
  hostname text NOT NULL,
  port integer NOT NULL DEFAULT 25565 CHECK (port BETWEEN 1 AND 65535),
  transport transport_kind NOT NULL DEFAULT 'DIRECT',
  status public_status NOT NULL DEFAULT 'UNKNOWN',
  enabled boolean NOT NULL DEFAULT true,
  status_changed_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (external_source, external_server_id)
);

CREATE TABLE checks (
  id bigserial PRIMARY KEY,
  server_id uuid NOT NULL REFERENCES monitored_servers(id) ON DELETE CASCADE,
  checked_at timestamptz NOT NULL DEFAULT now(),
  outcome check_outcome NOT NULL,
  latency_ms integer,
  players_online integer,
  players_max integer,
  error_detail text
);
CREATE INDEX checks_server_checked_at ON checks (server_id, checked_at DESC);

CREATE TABLE incidents (
  id uuid PRIMARY KEY,
  server_id uuid NOT NULL REFERENCES monitored_servers(id) ON DELETE CASCADE,
  state text NOT NULL CHECK (state IN ('OPEN', 'RESOLVED')),
  reason check_outcome NOT NULL,
  started_at timestamptz NOT NULL,
  confirmed_at timestamptz NOT NULL,
  recovery_started_at timestamptz,
  resolved_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX incidents_server_started_at ON incidents (server_id, started_at DESC);

CREATE TABLE server_members (
  server_id uuid NOT NULL REFERENCES monitored_servers(id) ON DELETE CASCADE,
  discord_user_id text NOT NULL,
  role text NOT NULL CHECK (role IN ('owner', 'manager', 'viewer')),
  source text NOT NULL DEFAULT 'voxellink',
  PRIMARY KEY (server_id, discord_user_id)
);

CREATE TABLE discord_notification_channels (
  server_id uuid PRIMARY KEY REFERENCES monitored_servers(id) ON DELETE CASCADE,
  channel_id text NOT NULL UNIQUE,
  enabled boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE maintenance_windows (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  server_id uuid NOT NULL REFERENCES monitored_servers(id) ON DELETE CASCADE,
  starts_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL CHECK (ends_at > starts_at),
  created_by_discord_user_id text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX maintenance_windows_server_time ON maintenance_windows (server_id, starts_at, ends_at);

CREATE TABLE check_aggregates_15m (
  server_id uuid NOT NULL REFERENCES monitored_servers(id) ON DELETE CASCADE,
  bucket_start timestamptz NOT NULL,
  checks_total integer NOT NULL,
  checks_successful integer NOT NULL,
  checks_excluded integer NOT NULL DEFAULT 0,
  latency_avg_ms integer,
  latency_max_ms integer,
  PRIMARY KEY (server_id, bucket_start)
);

CREATE TABLE check_aggregates_hourly (
  server_id uuid NOT NULL REFERENCES monitored_servers(id) ON DELETE CASCADE,
  bucket_start timestamptz NOT NULL,
  checks_total integer NOT NULL,
  checks_successful integer NOT NULL,
  checks_excluded integer NOT NULL DEFAULT 0,
  latency_avg_ms integer,
  latency_max_ms integer,
  PRIMARY KEY (server_id, bucket_start)
);

CREATE TABLE daily_uptime (
  server_id uuid NOT NULL REFERENCES monitored_servers(id) ON DELETE CASCADE,
  day date NOT NULL,
  checks_total integer NOT NULL,
  checks_successful integer NOT NULL,
  checks_excluded integer NOT NULL DEFAULT 0,
  uptime_percent numeric(5,2),
  PRIMARY KEY (server_id, day)
);
