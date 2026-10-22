package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

var migrationName = regexp.MustCompile(`^([0-9]+)_[a-zA-Z0-9_-]+\.sql$`)

const advisoryLockKey int64 = 731904221067

// Up serializes migration runs across API instances, reconciles the verified
// Supabase-applied legacy migrations, and then applies each remaining Goose
// Up section atomically.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockKey)

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS public.goose_db_version (
			id BIGSERIAL PRIMARY KEY,
			version_id BIGINT NOT NULL,
			is_applied BOOLEAN NOT NULL,
			tstamp TIMESTAMP NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("ensure migration ledger: %w", err)
	}
	var rows int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM public.goose_db_version").Scan(&rows); err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	if rows == 0 {
		if _, err := conn.Exec(ctx, "INSERT INTO public.goose_db_version (version_id, is_applied) VALUES (0, true)"); err != nil {
			return fmt.Errorf("initialize migration ledger: %w", err)
		}
	}

	version, applied, err := currentVersion(ctx, conn)
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("migration ledger ends with a rolled-back version %d; inspect it before starting the API", version)
	}
	if version == 3 {
		if err := baselineVerifiedSupabaseMigrations(ctx, conn); err != nil {
			return err
		}
		version = 10
	}

	migrations, err := readMigrations()
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("no SQL migrations were embedded")
	}
	if version > migrations[len(migrations)-1].version {
		return fmt.Errorf("database migration version %d is newer than this application (%d)", version, migrations[len(migrations)-1].version)
	}

	for _, migration := range migrations {
		if migration.version <= version {
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", migration.name, err)
		}
		if _, err := tx.Conn().PgConn().Exec(ctx, migration.upSQL).ReadAll(); err != nil {
			_ = tx.Rollback(context.Background())
			return fmt.Errorf("apply migration %s: %w", migration.name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO public.goose_db_version (version_id, is_applied) VALUES ($1, true)", migration.version); err != nil {
			_ = tx.Rollback(context.Background())
			return fmt.Errorf("record migration %s: %w", migration.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", migration.name, err)
		}
	}
	return nil
}

type migration struct {
	name    string
	version int64
	upSQL   string
}

func currentVersion(ctx context.Context, conn *pgxpool.Conn) (int64, bool, error) {
	var version int64
	var applied bool
	err := conn.QueryRow(ctx, "SELECT version_id, is_applied FROM public.goose_db_version ORDER BY id DESC LIMIT 1").Scan(&version, &applied)
	if err != nil {
		return 0, false, fmt.Errorf("read current migration version: %w", err)
	}
	return version, applied, nil
}

func readMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	var result []migration
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid migration version in %s: %w", entry.Name(), err)
		}
		data, err := files.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		upSQL, err := extractUp(string(data))
		if err != nil {
			return nil, fmt.Errorf("parse migration %s: %w", entry.Name(), err)
		}
		result = append(result, migration{name: entry.Name(), version: version, upSQL: upSQL})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	return result, nil
}

func extractUp(source string) (string, error) {
	lines := strings.Split(source, "\n")
	start, end := -1, len(lines)
	for i, line := range lines {
		switch strings.TrimSpace(line) {
		case "-- +goose Up":
			if start >= 0 {
				return "", fmt.Errorf("duplicate Goose Up marker")
			}
			start = i + 1
		case "-- +goose Down":
			if start >= 0 {
				end = i
			}
		}
	}
	if start < 0 {
		return "", fmt.Errorf("missing Goose Up marker")
	}
	sql := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	if sql == "" {
		return "", fmt.Errorf("empty Goose Up section")
	}
	return sql, nil
}

// The app's original migrations 00004-00009 and 00010 were applied through
// Supabase's migration service. Only baseline them when its exact two ledger
// entries and the expected secured schema are present.
func baselineVerifiedSupabaseMigrations(ctx context.Context, conn *pgxpool.Conn) error {
	var hasLedger bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('supabase_migrations.schema_migrations') IS NOT NULL").Scan(&hasLedger); err != nil {
		return fmt.Errorf("check Supabase migration ledger: %w", err)
	}
	if !hasLedger {
		return fmt.Errorf("Goose is at version 3 but Supabase migration history is absent; refusing to guess whether migrations 00004-00010 already ran")
	}

	rows, err := conn.Query(ctx, `
		SELECT name
		FROM supabase_migrations.schema_migrations
		WHERE name IN (
			'furii_assessments_learning_recovery_00004_00009',
			'secure_public_schema_access'
		)`)
	if err != nil {
		return fmt.Errorf("read Supabase migration history: %w", err)
	}
	found := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("read Supabase migration history: %w", err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read Supabase migration history: %w", err)
	}
	rows.Close()
	if len(found) != 2 || !found["furii_assessments_learning_recovery_00004_00009"] || !found["secure_public_schema_access"] {
		return fmt.Errorf("Goose is at version 3, but the exact Supabase records for migrations 00004-00009 and 00010 are missing; refusing to replay them")
	}

	var missingTables int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM (VALUES
			('staff_invitations'),
			('assessments'),
			('assessment_attempt_events'),
			('assessment_attempt_items'),
			('intervention_outcomes'),
			('recommendations')
		) AS expected(table_name)
		WHERE to_regclass('public.' || expected.table_name) IS NULL`).Scan(&missingTables); err != nil {
		return fmt.Errorf("verify migrated schema: %w", err)
	}
	if missingTables != 0 {
		return fmt.Errorf("Supabase migration records exist but %d expected schema tables are missing; refusing to baseline", missingTables)
	}

	var unsecuredTables, grantedTables int
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind IN ('r', 'p')
		  AND NOT c.relrowsecurity`).Scan(&unsecuredTables); err != nil {
		return fmt.Errorf("verify row-level security: %w", err)
	}
	if err := conn.QueryRow(ctx, `
		SELECT count(*)
		FROM information_schema.role_table_grants
		WHERE table_schema = 'public'
		  AND grantee IN ('anon', 'authenticated')`).Scan(&grantedTables); err != nil {
		return fmt.Errorf("verify API role grants: %w", err)
	}
	if unsecuredTables != 0 || grantedTables != 0 {
		return fmt.Errorf("Supabase migration records exist but security checks failed (tables without RLS: %d, API role grants: %d); refusing to baseline", unsecuredTables, grantedTables)
	}

	for version := int64(4); version <= 10; version++ {
		if _, err := conn.Exec(ctx, "INSERT INTO public.goose_db_version (version_id, is_applied) VALUES ($1, true)", version); err != nil {
			return fmt.Errorf("baseline verified migration version %d: %w", version, err)
		}
	}
	return nil
}

