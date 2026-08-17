// Package store owns Monitor's PostgreSQL persistence boundary.
package store

import (
	"context"
	"fmt"
	"time"

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

func (s *Postgres) EnabledServers(ctx context.Context) ([]domain.Server, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, name, hostname, port, status::text, transport::text, enabled FROM monitored_servers WHERE enabled ORDER BY created_at`)
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
