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
