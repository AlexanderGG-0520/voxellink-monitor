// Package store owns Monitor's PostgreSQL persistence boundary.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/alexandergg-0520/voxellink-monitor/internal/crowd"
	"github.com/alexandergg-0520/voxellink-monitor/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ pool *pgxpool.Pool }

func Connect(ctx context.Context, url string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (s *Postgres) Close() { s.pool.Close() }

// RunRetention implements the fixed v1 policy. It is safe to run repeatedly:
// rollups are upserts, then lower-resolution data is discarded only after it
// has been represented at the next resolution.
func (s *Postgres) RunRetention(ctx context.Context) (domain.RetentionStats, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.RetentionStats{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO check_aggregates_15m (server_id, bucket_start, checks_total, checks_successful, checks_excluded, latency_avg_ms, latency_max_ms) SELECT c.server_id, date_bin('15 minutes', c.checked_at, timestamptz '2000-01-01 00:00:00+00'), count(*), count(*) FILTER (WHERE c.outcome = 'SUCCESS'), count(*) FILTER (WHERE EXISTS (SELECT 1 FROM maintenance_windows mw WHERE mw.server_id = c.server_id AND c.checked_at >= mw.starts_at AND c.checked_at < mw.ends_at)), round(avg(c.latency_ms) FILTER (WHERE c.outcome = 'SUCCESS'))::integer, max(c.latency_ms) FILTER (WHERE c.outcome = 'SUCCESS') FROM checks c WHERE c.checked_at < date_trunc('hour', now()) GROUP BY c.server_id, date_bin('15 minutes', c.checked_at, timestamptz '2000-01-01 00:00:00+00') ON CONFLICT (server_id, bucket_start) DO UPDATE SET checks_total = EXCLUDED.checks_total, checks_successful = EXCLUDED.checks_successful, checks_excluded = EXCLUDED.checks_excluded, latency_avg_ms = EXCLUDED.latency_avg_ms, latency_max_ms = EXCLUDED.latency_max_ms`)
	if err != nil {
		return domain.RetentionStats{}, fmt.Errorf("roll up 15-minute checks: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO check_aggregates_hourly (server_id, bucket_start, checks_total, checks_successful, checks_excluded, latency_avg_ms, latency_max_ms) SELECT server_id, date_trunc('hour', bucket_start), sum(checks_total), sum(checks_successful), sum(checks_excluded), round(sum(coalesce(latency_avg_ms, 0) * checks_successful)::numeric / nullif(sum(checks_successful), 0))::integer, max(latency_max_ms) FROM check_aggregates_15m WHERE bucket_start < date_trunc('hour', now()) GROUP BY server_id, date_trunc('hour', bucket_start) ON CONFLICT (server_id, bucket_start) DO UPDATE SET checks_total = EXCLUDED.checks_total, checks_successful = EXCLUDED.checks_successful, checks_excluded = EXCLUDED.checks_excluded, latency_avg_ms = EXCLUDED.latency_avg_ms, latency_max_ms = EXCLUDED.latency_max_ms`)
	if err != nil {
		return domain.RetentionStats{}, fmt.Errorf("roll up hourly checks: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO user_report_aggregates_15m (server_id, bucket_start, reports_total, connection_reports, login_reports, timeout_reports, lag_reports, other_reports) SELECT server_id, date_bin('15 minutes', reported_at, timestamptz '2000-01-01 00:00:00+00'), count(*), count(*) FILTER (WHERE report_type = 'CONNECTION'), count(*) FILTER (WHERE report_type = 'LOGIN'), count(*) FILTER (WHERE report_type = 'TIMEOUT'), count(*) FILTER (WHERE report_type = 'LAG'), count(*) FILTER (WHERE report_type = 'OTHER') FROM user_reports WHERE reported_at < date_trunc('hour', now()) GROUP BY server_id, date_bin('15 minutes', reported_at, timestamptz '2000-01-01 00:00:00+00') ON CONFLICT (server_id, bucket_start) DO UPDATE SET reports_total = EXCLUDED.reports_total, connection_reports = EXCLUDED.connection_reports, login_reports = EXCLUDED.login_reports, timeout_reports = EXCLUDED.timeout_reports, lag_reports = EXCLUDED.lag_reports, other_reports = EXCLUDED.other_reports`)
	if err != nil {
		return domain.RetentionStats{}, fmt.Errorf("roll up user reports: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO daily_uptime (server_id, day, checks_total, checks_successful, checks_excluded, uptime_percent) SELECT server_id, bucket_start::date, sum(checks_total), sum(checks_successful), sum(checks_excluded), round(100.0 * sum(checks_successful) / nullif(sum(checks_total) - sum(checks_excluded), 0), 2) FROM check_aggregates_hourly WHERE bucket_start < date_trunc('day', now()) GROUP BY server_id, bucket_start::date ON CONFLICT (server_id, day) DO UPDATE SET checks_total = EXCLUDED.checks_total, checks_successful = EXCLUDED.checks_successful, checks_excluded = EXCLUDED.checks_excluded, uptime_percent = EXCLUDED.uptime_percent`)
	if err != nil {
		return domain.RetentionStats{}, fmt.Errorf("roll up daily uptime: %w", err)
	}
	stats := domain.RetentionStats{}
	command, err := tx.Exec(ctx, `DELETE FROM checks WHERE checked_at < now() - interval '30 days'`)
	if err != nil {
		return stats, err
	}
	stats.RawDeleted = command.RowsAffected()
	command, err = tx.Exec(ctx, `DELETE FROM check_aggregates_15m WHERE bucket_start < now() - interval '90 days'`)
	if err != nil {
		return stats, err
	}
	stats.FifteenMinuteDeleted = command.RowsAffected()
	command, err = tx.Exec(ctx, `DELETE FROM check_aggregates_hourly WHERE bucket_start < now() - interval '1 year'`)
	if err != nil {
		return stats, err
	}
	stats.HourlyDeleted = command.RowsAffected()
	if _, err = tx.Exec(ctx, `DELETE FROM user_reports WHERE reported_at < now() - interval '30 days'`); err != nil {
		return stats, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM user_report_aggregates_15m WHERE bucket_start < now() - interval '90 days'`); err != nil {
		return stats, err
	}
	if err = tx.Commit(ctx); err != nil {
		return stats, err
	}
	return stats, nil
}

// RecordUserReport stores one anonymous report per server per day. Duplicate
// clicks are intentionally harmless and do not amplify the public signal.
func (s *Postgres) RecordUserReport(ctx context.Context, report domain.UserReport) (domain.CrowdSignal, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.CrowdSignal{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO user_reports (server_id, reported_at, reported_day, report_type, reporter_hash, detail) VALUES ($1::uuid, $2, $2::date, $3::user_report_type, $4, $5) ON CONFLICT (server_id, reporter_hash, reported_day) DO NOTHING`, report.ServerID, report.At, string(report.Type), report.ReporterHash, report.Detail)
	if err != nil {
		return domain.CrowdSignal{}, fmt.Errorf("insert user report: %w", err)
	}
	signal, err := crowdSignal(ctx, tx, report.ServerID, report.At)
	if err != nil {
		return domain.CrowdSignal{}, err
	}
	if signal.Anomalous {
		command, updateErr := tx.Exec(ctx, `UPDATE monitored_servers SET status = 'DEGRADED', status_changed_at = $2, updated_at = now() WHERE id = $1::uuid AND status = 'OPERATIONAL'`, report.ServerID, report.At)
		err = updateErr
		if err != nil {
			return domain.CrowdSignal{}, err
		}
		if command.RowsAffected() == 1 {
			if _, err = tx.Exec(ctx, `INSERT INTO pending_state_notifications (server_id, state, outcome, created_at) VALUES ($1::uuid, 'DEGRADED', 'SUCCESS', $2)`, report.ServerID, report.At); err != nil {
				return domain.CrowdSignal{}, fmt.Errorf("queue crowd notification: %w", err)
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.CrowdSignal{}, err
	}
	return signal, nil
}

func (s *Postgres) PendingStateNotifications(ctx context.Context, limit int) ([]domain.PendingStateNotification, error) {
	rows, err := s.pool.Query(ctx, `SELECT n.id, s.id::text, s.name, s.hostname, s.port, s.status::text, s.transport::text, s.enabled, n.state::text, n.outcome::text, n.created_at FROM pending_state_notifications n JOIN monitored_servers s ON s.id = n.server_id WHERE n.delivered_at IS NULL ORDER BY n.id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []domain.PendingStateNotification
	for rows.Next() {
		var item domain.PendingStateNotification
		if err := rows.Scan(&item.ID, &item.Server.ID, &item.Server.Name, &item.Server.Hostname, &item.Server.Port, &item.Server.Status, &item.Server.Transport, &item.Server.Enabled, &item.State, &item.Result.Outcome, &item.Result.At); err != nil {
			return nil, err
		}
		pending = append(pending, item)
	}
	return pending, rows.Err()
}

func (s *Postgres) MarkStateNotificationDelivered(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE pending_state_notifications SET delivered_at = now() WHERE id = $1 AND delivered_at IS NULL`, id)
	return err
}

func (s *Postgres) CrowdSignal(ctx context.Context, serverID string) (domain.CrowdSignal, error) {
	return crowdSignal(ctx, s.pool, serverID, time.Now())
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func crowdSignal(ctx context.Context, q queryer, serverID string, at time.Time) (domain.CrowdSignal, error) {
	var reports int
	var baseline float64
	err := q.QueryRow(ctx, `WITH current_bucket AS (SELECT date_bin('15 minutes', $2::timestamptz, timestamptz '2000-01-01 00:00:00+00') AS start), historical AS (SELECT count(*)::float8 AS reports FROM user_reports, current_bucket WHERE server_id = $1::uuid AND reported_at >= $2 - interval '28 days' AND reported_at < current_bucket.start AND extract(dow FROM reported_at) = extract(dow FROM current_bucket.start) AND extract(hour FROM reported_at) = extract(hour FROM current_bucket.start) AND floor(extract(minute FROM reported_at) / 15) = floor(extract(minute FROM current_bucket.start) / 15) GROUP BY date_trunc('day', reported_at)) SELECT (SELECT count(*) FROM user_reports, current_bucket WHERE server_id = $1::uuid AND reported_at >= current_bucket.start AND reported_at < current_bucket.start + interval '15 minutes'), coalesce((SELECT avg(reports) FROM historical), 0)`, serverID, at).Scan(&reports, &baseline)
	if err != nil {
		return domain.CrowdSignal{}, fmt.Errorf("read crowd signal: %w", err)
	}
	value := crowd.Evaluate(reports, baseline)
	return domain.CrowdSignal{Reports: value.Reports, Baseline: value.Baseline, Threshold: value.Threshold, Anomalous: value.Anomalous}, nil
}

// UpsertVoxelLinkServer replaces only imported listing metadata and membership.
// Checks, public state, and incidents remain Monitor-owned data.
func (s *Postgres) UpsertVoxelLinkServer(ctx context.Context, imported domain.ImportedServer) (domain.Server, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Server{}, err
	}
	defer tx.Rollback(ctx)
	var server domain.Server
	err = tx.QueryRow(ctx, `INSERT INTO monitored_servers (id, external_source, external_server_id, name, hostname, port, transport) VALUES (gen_random_uuid(), 'voxellink', $1, $2, $3, $4, $5::transport_kind) ON CONFLICT (external_source, external_server_id) DO UPDATE SET name = EXCLUDED.name, hostname = EXCLUDED.hostname, port = EXCLUDED.port, transport = EXCLUDED.transport, updated_at = now() RETURNING id::text, name, hostname, port, status::text, transport::text, enabled`, imported.ExternalID, imported.Name, imported.Hostname, imported.Port, imported.Transport).Scan(&server.ID, &server.Name, &server.Hostname, &server.Port, &server.Status, &server.Transport, &server.Enabled)
	if err != nil {
		return domain.Server{}, fmt.Errorf("upsert VoxelLink server: %w", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM server_members WHERE server_id = $1::uuid AND source = 'voxellink'`, server.ID); err != nil {
		return domain.Server{}, err
	}
	for _, member := range imported.Members {
		if _, err = tx.Exec(ctx, `INSERT INTO server_members (server_id, discord_user_id, role, source) VALUES ($1::uuid, $2, $3, 'voxellink')`, server.ID, member.DiscordUserID, member.Role); err != nil {
			return domain.Server{}, fmt.Errorf("store VoxelLink member: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Server{}, err
	}
	return server, nil
}

func (s *Postgres) VoxelLinkExternalServerIDs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT external_server_id FROM monitored_servers WHERE external_source = 'voxellink' ORDER BY external_server_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Postgres) EnabledServers(ctx context.Context) ([]domain.Server, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id::text, s.name, s.hostname, s.port, CASE WHEN EXISTS (SELECT 1 FROM maintenance_windows mw WHERE mw.server_id = s.id AND now() >= mw.starts_at AND now() < mw.ends_at) THEN 'MAINTENANCE' ELSE s.status::text END, s.transport::text, s.enabled FROM monitored_servers s WHERE s.enabled ORDER BY s.created_at`)
	if err != nil {
		return nil, fmt.Errorf("list enabled servers: %w", err)
	}
	defer rows.Close()
	var servers []domain.Server
	for rows.Next() {
		var server domain.Server
		if err := rows.Scan(&server.ID, &server.Name, &server.Hostname, &server.Port, &server.Status, &server.Transport, &server.Enabled); err != nil {
			return nil, fmt.Errorf("scan server: %w", err)
		}
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

func (s *Postgres) NotificationChannelIDs(ctx context.Context, serverID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT channel_id FROM discord_notification_channels WHERE server_id = $1::uuid AND enabled`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Postgres) SnapshotByExternalID(ctx context.Context, externalID string) (domain.ServerSnapshot, error) {
	return s.snapshot(ctx, `WHERE s.external_source = 'voxellink' AND s.external_server_id = $1`, externalID)
}

func (s *Postgres) SnapshotByID(ctx context.Context, serverID string) (domain.ServerSnapshot, error) {
	return s.snapshot(ctx, `WHERE s.id = $1::uuid`, serverID)
}

func (s *Postgres) SnapshotByDiscordChannel(ctx context.Context, channelID string) (domain.ServerSnapshot, error) {
	return s.snapshot(ctx, `JOIN discord_notification_channels dnc ON dnc.server_id = s.id WHERE dnc.channel_id = $1 AND dnc.enabled`, channelID)
}

func (s *Postgres) PublicSnapshots(ctx context.Context) ([]domain.ServerSnapshot, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id::text, s.name, s.hostname, s.port, CASE WHEN EXISTS (SELECT 1 FROM maintenance_windows mw WHERE mw.server_id = s.id AND now() >= mw.starts_at AND now() < mw.ends_at) THEN 'MAINTENANCE' ELSE s.status::text END, s.transport::text, s.enabled, c.checked_at, c.outcome::text, c.latency_ms FROM monitored_servers s LEFT JOIN LATERAL (SELECT checked_at, outcome, latency_ms FROM checks WHERE server_id = s.id ORDER BY checked_at DESC, id DESC LIMIT 1) c ON true WHERE s.enabled ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var snapshots []domain.ServerSnapshot
	for rows.Next() {
		snapshot, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, rows.Err()
}

func (s *Postgres) snapshot(ctx context.Context, clause string, argument string) (domain.ServerSnapshot, error) {
	var snapshot domain.ServerSnapshot
	row := s.pool.QueryRow(ctx, `SELECT s.id::text, s.name, s.hostname, s.port, CASE WHEN EXISTS (SELECT 1 FROM maintenance_windows mw WHERE mw.server_id = s.id AND now() >= mw.starts_at AND now() < mw.ends_at) THEN 'MAINTENANCE' ELSE s.status::text END, s.transport::text, s.enabled, c.checked_at, c.outcome::text, c.latency_ms FROM monitored_servers s LEFT JOIN LATERAL (SELECT checked_at, outcome, latency_ms FROM checks WHERE server_id = s.id ORDER BY checked_at DESC, id DESC LIMIT 1) c ON true `+clause, argument)
	return scanSnapshot(row, &snapshot)
}

type snapshotScanner interface{ Scan(...any) error }

func scanSnapshot(scanner snapshotScanner, targets ...*domain.ServerSnapshot) (domain.ServerSnapshot, error) {
	snapshot := domain.ServerSnapshot{}
	if len(targets) > 0 {
		snapshot = *targets[0]
	}
	var checkedAt *time.Time
	var outcome *string
	var latency *int
	err := scanner.Scan(&snapshot.ID, &snapshot.Name, &snapshot.Hostname, &snapshot.Port, &snapshot.Status, &snapshot.Transport, &snapshot.Enabled, &checkedAt, &outcome, &latency)
	if err != nil {
		return domain.ServerSnapshot{}, err
	}
	if checkedAt != nil {
		snapshot.LastCheckedAt = *checkedAt
	}
	if outcome != nil {
		snapshot.LastOutcome = domain.Outcome(*outcome)
	}
	if latency != nil {
		snapshot.Latency = time.Duration(*latency) * time.Millisecond
	}
	return snapshot, nil
}

func (s *Postgres) Uptime24h(ctx context.Context, serverID string) (float64, error) {
	var percentage *float64
	err := s.pool.QueryRow(ctx, `SELECT 100.0 * count(*) FILTER (WHERE c.outcome = 'SUCCESS' AND NOT EXISTS (SELECT 1 FROM maintenance_windows mw WHERE mw.server_id = c.server_id AND c.checked_at >= mw.starts_at AND c.checked_at < mw.ends_at)) / NULLIF(count(*) FILTER (WHERE c.outcome <> 'PROBE_ERROR' AND NOT EXISTS (SELECT 1 FROM maintenance_windows mw WHERE mw.server_id = c.server_id AND c.checked_at >= mw.starts_at AND c.checked_at < mw.ends_at)), 0) FROM checks c WHERE c.server_id = $1::uuid AND c.checked_at >= now() - interval '24 hours'`, serverID).Scan(&percentage)
	if err != nil {
		return 0, err
	}
	if percentage == nil {
		return 0, nil
	}
	return *percentage, nil
}

func (s *Postgres) RecentIncidents(ctx context.Context, serverID string, limit int) ([]domain.Incident, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, server_id::text, state, reason::text, started_at, confirmed_at, resolved_at FROM incidents WHERE server_id = $1::uuid ORDER BY started_at DESC LIMIT $2`, serverID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var incidents []domain.Incident
	for rows.Next() {
		var incident domain.Incident
		if err := rows.Scan(&incident.ID, &incident.ServerID, &incident.State, &incident.Reason, &incident.StartedAt, &incident.ConfirmedAt, &incident.ResolvedAt); err != nil {
			return nil, err
		}
		incidents = append(incidents, incident)
	}
	return incidents, rows.Err()
}

func (s *Postgres) ServersForDiscordMember(ctx context.Context, discordUserID string) ([]domain.Server, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id::text, s.name, s.hostname, s.port, CASE WHEN EXISTS (SELECT 1 FROM maintenance_windows mw WHERE mw.server_id = s.id AND now() >= mw.starts_at AND now() < mw.ends_at) THEN 'MAINTENANCE' ELSE s.status::text END, s.transport::text, s.enabled FROM monitored_servers s JOIN server_members m ON m.server_id = s.id WHERE m.discord_user_id = $1 AND m.role IN ('owner', 'manager') ORDER BY s.name`, discordUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var servers []domain.Server
	for rows.Next() {
		var server domain.Server
		if err := rows.Scan(&server.ID, &server.Name, &server.Hostname, &server.Port, &server.Status, &server.Transport, &server.Enabled); err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

func (s *Postgres) SetEnabledForDiscordMember(ctx context.Context, serverID, discordUserID string, enabled bool) error {
	command, err := s.pool.Exec(ctx, `UPDATE monitored_servers SET enabled = $3, updated_at = now() WHERE id = $1::uuid AND EXISTS (SELECT 1 FROM server_members WHERE server_id = monitored_servers.id AND discord_user_id = $2 AND role IN ('owner', 'manager'))`, serverID, discordUserID, enabled)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Postgres) SetNotificationChannelForDiscordMember(ctx context.Context, serverID, discordUserID, channelID string) error {
	command, err := s.pool.Exec(ctx, `INSERT INTO discord_notification_channels (server_id, channel_id) SELECT s.id, $3 FROM monitored_servers s WHERE s.id = $1::uuid AND EXISTS (SELECT 1 FROM server_members WHERE server_id = s.id AND discord_user_id = $2 AND role IN ('owner', 'manager')) ON CONFLICT (server_id) DO UPDATE SET channel_id = EXCLUDED.channel_id, enabled = true, updated_at = now()`, serverID, discordUserID, channelID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Postgres) ScheduleMaintenanceForDiscordMember(ctx context.Context, serverID, discordUserID string, startsAt, endsAt time.Time) error {
	command, err := s.pool.Exec(ctx, `INSERT INTO maintenance_windows (server_id, starts_at, ends_at, created_by_discord_user_id) SELECT s.id, $3, $4, $2 FROM monitored_servers s WHERE s.id = $1::uuid AND EXISTS (SELECT 1 FROM server_members WHERE server_id = s.id AND discord_user_id = $2 AND role IN ('owner', 'manager'))`, serverID, discordUserID, startsAt, endsAt)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return nil
}

// RecordCheck persists one observation and applies the persisted v1 state policy.
// It returns a state change so the notification layer can post exactly once.
func (s *Postgres) RecordCheck(ctx context.Context, server domain.Server, result domain.CheckResult) (bool, domain.PublicStatus, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, "", err
	}
	defer tx.Rollback(ctx)

	var latency *int
	if result.Outcome == domain.Success {
		n := int(result.Latency.Milliseconds())
		latency = &n
	}
	_, err = tx.Exec(ctx, `INSERT INTO checks (server_id, checked_at, outcome, latency_ms, players_online, players_max, error_detail) VALUES ($1::uuid, $2, $3::check_outcome, $4, $5, $6, NULLIF($7, ''))`, server.ID, result.At, string(result.Outcome), latency, result.PlayersOnline, result.PlayersMax, result.Detail)
	if err != nil {
		return false, "", fmt.Errorf("insert check: %w", err)
	}
	if server.Status == domain.Maintenance {
		return false, server.Status, tx.Commit(ctx)
	}

	newState, changed, err := stateAfter(ctx, tx, server, result)
	if err != nil {
		return false, "", err
	}
	if result.Outcome == domain.Success && newState == domain.Operational {
		signal, signalErr := crowdSignal(ctx, tx, server.ID, result.At)
		if signalErr != nil {
			return false, "", signalErr
		}
		if signal.Anomalous {
			newState, changed = domain.Degraded, server.Status != domain.Degraded
		}
	}
	if changed {
		_, err = tx.Exec(ctx, `UPDATE monitored_servers SET status = $2::public_status, status_changed_at = $3, updated_at = now() WHERE id = $1::uuid`, server.ID, string(newState), result.At)
		if err != nil {
			return false, "", fmt.Errorf("update public state: %w", err)
		}
		if newState == domain.Outage {
			startedAt, err := earliestRecentFailure(ctx, tx, server.ID)
			if err != nil {
				return false, "", err
			}
			_, err = tx.Exec(ctx, `INSERT INTO incidents (id, server_id, state, reason, started_at, confirmed_at) VALUES (gen_random_uuid(), $1::uuid, 'OPEN', $2::check_outcome, $3, $4)`, server.ID, string(result.Outcome), startedAt, result.At)
			if err != nil {
				return false, "", fmt.Errorf("open incident: %w", err)
			}
		}
		if server.Status == domain.Outage && newState == domain.Operational {
			_, err = tx.Exec(ctx, `UPDATE incidents SET state = 'RESOLVED', recovery_started_at = $2, resolved_at = $3 WHERE id = (SELECT id FROM incidents WHERE server_id = $1::uuid AND state = 'OPEN' ORDER BY started_at DESC LIMIT 1)`, server.ID, firstRecentSuccess(result.At, ctx, tx, server.ID), result.At)
			if err != nil {
				return false, "", fmt.Errorf("resolve incident: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, "", err
	}
	return changed, newState, nil
}

func stateAfter(ctx context.Context, tx pgx.Tx, server domain.Server, result domain.CheckResult) (domain.PublicStatus, bool, error) {
	if result.Outcome == domain.ProbeError {
		return domain.Unknown, server.Status != domain.Unknown, nil
	}
	if result.Outcome == domain.Success {
		if server.Status == domain.Outage {
			count, err := recentMatchingCount(ctx, tx, server.ID, 2, domain.Success)
			return domain.Operational, err == nil && count == 2, err
		}
		return domain.Operational, server.Status != domain.Operational, nil
	}
	if server.Status == domain.Outage {
		return domain.Outage, false, nil
	}
	count, err := recentFailures(ctx, tx, server.ID, 3)
	return domain.Outage, err == nil && count == 3, err
}

func recentMatchingCount(ctx context.Context, tx pgx.Tx, id string, limit int, target domain.Outcome) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT outcome::text FROM checks WHERE server_id = $1::uuid ORDER BY checked_at DESC, id DESC LIMIT $2) recent WHERE outcome = $3`, id, limit, string(target)).Scan(&n)
	return n, err
}
func recentFailures(ctx context.Context, tx pgx.Tx, id string, limit int) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT outcome::text FROM checks WHERE server_id = $1::uuid ORDER BY checked_at DESC, id DESC LIMIT $2) recent WHERE outcome NOT IN ('SUCCESS', 'PROBE_ERROR')`, id, limit).Scan(&n)
	return n, err
}
func earliestRecentFailure(ctx context.Context, tx pgx.Tx, id string) (time.Time, error) {
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT min(checked_at) FROM (SELECT checked_at FROM checks WHERE server_id = $1::uuid AND outcome NOT IN ('SUCCESS', 'PROBE_ERROR') ORDER BY checked_at DESC, id DESC LIMIT 3) failures`, id).Scan(&at)
	return at, err
}
func firstRecentSuccess(fallback time.Time, ctx context.Context, tx pgx.Tx, id string) time.Time {
	var at time.Time
	if err := tx.QueryRow(ctx, `SELECT min(checked_at) FROM (SELECT checked_at FROM checks WHERE server_id = $1::uuid AND outcome = 'SUCCESS' ORDER BY checked_at DESC, id DESC LIMIT 2) successes`, id).Scan(&at); err != nil {
		return fallback
	}
	return at
}
