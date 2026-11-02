package migrations

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestExtractUp(t *testing.T) {
	up, err := extractUp("-- +goose Up\nCREATE TABLE a();\n-- +goose Down\nDROP TABLE a;\n")
	if err != nil || up != "CREATE TABLE a();" {
		t.Fatalf("extractUp = %q, %v", up, err)
	}
	if _, err := extractUp("CREATE TABLE a();"); err == nil {
		t.Fatal("a file without an Up marker must be rejected")
	}
	if _, err := extractUp("-- +goose Up\n-- +goose Down\n"); err == nil {
		t.Fatal("an empty Up section must be rejected")
	}
}

func TestEmbeddedMigrationsAreOrderedAndUnique(t *testing.T) {
	list, err := readMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range list {
		if m.version != int64(i+1) {
			t.Fatalf("migration %s has version %d, expected %d (no gaps or duplicates)", m.name, m.version, i+1)
		}
	}
}

// freshDatabase creates an empty database on TEST_DATABASE_URL's server with
// the Supabase roles the migrations expect, and drops it after the test.
func freshDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("set TEST_DATABASE_URL to run migration tests")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	// Roles belong to the whole server and go test runs packages at once, so
	// another package may create one between the check and the CREATE.
	for _, role := range []string{"postgres", "anon", "authenticated"} {
		if _, err := admin.Exec(ctx, fmt.Sprintf(`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN CREATE ROLE %s NOLOGIN; END IF; EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL; END $$`, role, pgx.Identifier{role}.Sanitize())); err != nil {
			t.Fatal(err)
		}
	}
	name := fmt.Sprintf("furii_migration_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name+" ENCODING 'UTF8' LC_COLLATE 'C' LC_CTYPE 'C' TEMPLATE template0"); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return pool
}

// applyThrough runs the Up sections of migrations 1..last without the runner.
func applyThrough(t *testing.T, pool *pgxpool.Pool, first, last int64) {
	t.Helper()
	list, err := readMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list {
		if m.version < first || m.version > last {
			continue
		}
		if _, err := pool.Exec(context.Background(), m.upSQL); err != nil {
			t.Fatalf("apply %s: %v", m.name, err)
		}
	}
}

func seedOwner(t *testing.T, pool *pgxpool.Pool, school string) (string, string) {
	t.Helper()
	var schoolID, userID string
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `INSERT INTO schools(name) VALUES($1) RETURNING id::text`, school).Scan(&schoolID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(school_id,email,password_hash,display_name) VALUES($1,$2,'x','Admin') RETURNING id::text`, schoolID, strings.ToLower(school)+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	return schoolID, userID
}

func seedCurriculum(t *testing.T, pool *pgxpool.Pool, name string, auditSchool, auditUser string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO curricula(name,provider,version_label) VALUES($1,'Ministry','2026') RETURNING id::text`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if auditSchool != "" {
		if _, err := pool.Exec(ctx, `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id) VALUES($1,$2,'curriculum.created','curriculum',$3)`, auditSchool, auditUser, id); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func ownerOf(t *testing.T, pool *pgxpool.Pool, table, id string) string {
	t.Helper()
	var owner *string
	if err := pool.QueryRow(context.Background(), "SELECT school_id::text FROM "+table+" WHERE id=$1", id).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner == nil {
		return ""
	}
	return *owner
}

func TestCurriculumOwnershipBackfillWithSeveralSchools(t *testing.T) {
	pool := freshDatabase(t)
	applyThrough(t, pool, 1, 13)
	ctx := context.Background()
	schoolA, adminA := seedOwner(t, pool, "Alpha")
	schoolB, adminB := seedOwner(t, pool, "Beta")
	curriculumA := seedCurriculum(t, pool, "Alpha Curriculum", schoolA, adminA)
	curriculumB := seedCurriculum(t, pool, "Beta Curriculum", schoolB, adminB)
	orphan := seedCurriculum(t, pool, "Unattributed", "", "")

	var subjectID, nodeOne, nodeTwo, mappingID string
	if err := pool.QueryRow(ctx, `INSERT INTO subjects(curriculum_id,name) VALUES($1,'Math') RETURNING id::text`, curriculumB).Scan(&subjectID); err != nil {
		t.Fatal(err)
	}
	for _, target := range []*string{&nodeOne, &nodeTwo} {
		if err := pool.QueryRow(ctx, `INSERT INTO curriculum_nodes(subject_id,node_type,title) VALUES($1,'skill','Skill') RETURNING id::text`, subjectID).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO curriculum_mappings(source_node_id,target_node_id,mapping_type,reviewed_by) VALUES($1,$2,'related',$3) RETURNING id::text`, nodeOne, nodeTwo, adminB).Scan(&mappingID); err != nil {
		t.Fatal(err)
	}

	applyThrough(t, pool, 14, 14)

	if got := ownerOf(t, pool, "curricula", curriculumA); got != schoolA {
		t.Errorf("Alpha's curriculum owner = %q, want %q", got, schoolA)
	}
	if got := ownerOf(t, pool, "curricula", curriculumB); got != schoolB {
		t.Errorf("Beta's curriculum owner = %q, want %q", got, schoolB)
	}
	if got := ownerOf(t, pool, "curricula", orphan); got != "" {
		t.Errorf("an unattributable curriculum should become platform-owned, got owner %q", got)
	}
	if got := ownerOf(t, pool, "curriculum_mappings", mappingID); got != schoolB {
		t.Errorf("mapping owner = %q, want reviewer's school %q", got, schoolB)
	}
	// Both schools may now hold the same curriculum version.
	if _, err := pool.Exec(ctx, `INSERT INTO curricula(school_id,name,provider,version_label) VALUES($1,'Beta Curriculum','Ministry','2026')`, schoolA); err != nil {
		t.Errorf("a second school could not create the same curriculum version: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO curricula(school_id,name,provider,version_label) VALUES($1,'Beta Curriculum','Ministry','2026')`, schoolB); err == nil {
		t.Error("a school must not hold two copies of the same curriculum version")
	}
}

func TestCurriculumOwnershipBackfillWithOneSchool(t *testing.T) {
	pool := freshDatabase(t)
	applyThrough(t, pool, 1, 13)
	schoolID, _ := seedOwner(t, pool, "Solo")
	orphan := seedCurriculum(t, pool, "Unattributed", "", "")
	applyThrough(t, pool, 14, 14)
	if got := ownerOf(t, pool, "curricula", orphan); got != schoolID {
		t.Fatalf("on a single-school installation the school should own every curriculum, got %q", got)
	}
}

func TestRunnerAppliesAllMigrations(t *testing.T) {
	pool := freshDatabase(t)
	ctx := context.Background()
	if err := Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	// Running again is a no-op.
	if err := Up(ctx, pool); err != nil {
		t.Fatal(err)
	}
	list, _ := readMigrations()
	var version int64
	if err := pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != list[len(list)-1].version {
		t.Fatalf("ledger version %d, want %d", version, list[len(list)-1].version)
	}
}
