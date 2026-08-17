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
