// Package migrate applies VoxelLink Monitor's embedded PostgreSQL migrations.
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/alexandergg-0520/voxellink-monitor/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		var existing bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.monitored_servers') IS NOT NULL`).Scan(&existing); err != nil {
			return err
		}
		if existing {
			if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES (1)`); err != nil {
				return fmt.Errorf("baseline existing schema: %w", err)
			}
			var hasRollups bool
			if err := pool.QueryRow(ctx, `SELECT to_regclass('public.check_aggregates_15m') IS NOT NULL`).Scan(&hasRollups); err != nil {
				return err
			}
			if hasRollups {
				if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES (2)`); err != nil {
					return fmt.Errorf("baseline retention schema: %w", err)
				}
			}
		}
	}
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := versionOf(entry.Name())
		if err != nil {
			return err
		}
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		source, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(source)); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, version)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", entry.Name(), err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
func versionOf(name string) (int, error) {
	value, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("invalid migration name %q", name)
	}
	version, err := strconv.Atoi(value)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("invalid migration version %q", name)
	}
	return version, nil
}
