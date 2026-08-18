CREATE TYPE user_report_type AS ENUM ('CONNECTION', 'LOGIN', 'TIMEOUT', 'LAG', 'OTHER');

-- reporter_hash is a daily, server-scoped hash. It limits duplicate reports
-- without storing an IP address, Discord identity, or other raw identifier.
CREATE TABLE user_reports (
  id bigserial PRIMARY KEY,
  server_id uuid NOT NULL REFERENCES monitored_servers(id) ON DELETE CASCADE,
  reported_at timestamptz NOT NULL DEFAULT now(),
  reported_day date NOT NULL DEFAULT CURRENT_DATE,
  report_type user_report_type NOT NULL,
  reporter_hash char(64) NOT NULL,
  detail text NOT NULL DEFAULT '' CHECK (char_length(detail) <= 280),
  UNIQUE (server_id, reporter_hash, reported_day)
);
CREATE INDEX user_reports_server_reported_at ON user_reports (server_id, reported_at DESC);

CREATE TABLE user_report_aggregates_15m (
  server_id uuid NOT NULL REFERENCES monitored_servers(id) ON DELETE CASCADE,
  bucket_start timestamptz NOT NULL,
  reports_total integer NOT NULL,
  connection_reports integer NOT NULL DEFAULT 0,
  login_reports integer NOT NULL DEFAULT 0,
  timeout_reports integer NOT NULL DEFAULT 0,
  lag_reports integer NOT NULL DEFAULT 0,
  other_reports integer NOT NULL DEFAULT 0,
  PRIMARY KEY (server_id, bucket_start)
);

-- The API records passive-report state changes while the Worker alone owns
-- delivery. This keeps Discord availability independent from the public API.
CREATE TABLE pending_state_notifications (
  id bigserial PRIMARY KEY,
  server_id uuid NOT NULL REFERENCES monitored_servers(id) ON DELETE CASCADE,
  state public_status NOT NULL,
  outcome check_outcome NOT NULL DEFAULT 'SUCCESS',
  created_at timestamptz NOT NULL DEFAULT now(),
  delivered_at timestamptz
);
CREATE INDEX pending_state_notifications_undelivered ON pending_state_notifications (id) WHERE delivered_at IS NULL;
