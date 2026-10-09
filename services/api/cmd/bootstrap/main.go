// Command bootstrap creates the initial school administrator in a controlled environment.
// Run it only for first-time setup; credentials are read from environment, never command-line arguments.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	schoolName := strings.TrimSpace(os.Getenv("BOOTSTRAP_SCHOOL_NAME"))
	email := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL"))
	displayName := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_NAME"))
	password := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if databaseURL == "" || schoolName == "" || email == "" || displayName == "" || password == "" {
		log.Fatal("DATABASE_URL, BOOTSTRAP_SCHOOL_NAME, BOOTSTRAP_ADMIN_EMAIL, BOOTSTRAP_ADMIN_NAME, and BOOTSTRAP_ADMIN_PASSWORD are required")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Fatalf("invalid password: %v", err)
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("database connection: %v", err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		log.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(61420261009)`); err != nil {
		log.Fatalf("lock initial setup: %v", err)
	}
	var existingSchools int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM schools`).Scan(&existingSchools); err != nil {
		log.Fatalf("check existing school: %v", err)
	}
	if existingSchools != 0 {
		log.Fatal("a school already exists; refusing to run the initial bootstrap again")
	}
	var schoolID, userID string
	if err := tx.QueryRow(ctx, `INSERT INTO schools(name) VALUES ($1) RETURNING id::text`, schoolName).Scan(&schoolID); err != nil {
		log.Fatalf("create school: %v", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO users(school_id,email,password_hash,display_name) VALUES ($1,$2,$3,$4) RETURNING id::text`, schoolID, email, hash, displayName).Scan(&userID); err != nil {
		log.Fatalf("create administrator: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES ($1,'school_admin')`, userID); err != nil {
		log.Fatalf("assign administrator role: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("save administrator: %v", err)
	}
	fmt.Printf("Created school %s and initial administrator %s\n", schoolID, userID)
}
