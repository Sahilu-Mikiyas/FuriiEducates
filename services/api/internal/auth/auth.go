package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type Identity struct {
	UserID      string   `json:"user_id"`
	SchoolID    string   `json:"school_id"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
}
type Service struct { db *pgxpool.Pool; key []byte; ttl time.Duration }

func NewService(db *pgxpool.Pool, key []byte, ttl time.Duration) *Service { return &Service{db: db, key: key, ttl: ttl} }

func (s *Service) Login(ctx context.Context, email, password string) (string, Identity, error) {
	var userID, schoolID, displayName, status, encoded string
	err := s.db.QueryRow(ctx, `SELECT id::text, school_id::text, display_name, status, password_hash FROM users WHERE lower(email)=lower($1)`, strings.TrimSpace(email)).Scan(&userID, &schoolID, &displayName, &status, &encoded)
	if errors.Is(err, pgx.ErrNoRows) { return "", Identity{}, ErrUnauthenticated }
	if err != nil { return "", Identity{}, err }
	if status != "active" || !verifyPassword(encoded, password) { return "", Identity{}, ErrUnauthenticated }
	roles, err := s.roles(ctx, userID)
	if err != nil { return "", Identity{}, err }
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil { return "", Identity{}, fmt.Errorf("create session token: %w", err) }
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(s.ttl)
	if _, err := s.db.Exec(ctx, `INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`, userID, s.digest(token), expires); err != nil { return "", Identity{}, err }
	return token, Identity{userID, schoolID, displayName, roles}, nil
}

func (s *Service) Identity(ctx context.Context, token string) (Identity, error) {
	var id Identity
	err := s.db.QueryRow(ctx, `SELECT u.id::text, u.school_id::text, u.display_name FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND s.revoked_at IS NULL AND u.status='active'`, s.digest(token)).Scan(&id.UserID, &id.SchoolID, &id.DisplayName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) { return Identity{}, ErrUnauthenticated }
		return Identity{}, err
	}
	id.Roles, err = s.roles(ctx, id.UserID)
	return id, err
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, s.digest(token))
	return err
}

func (s *Service) roles(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT role FROM user_roles WHERE user_id=$1 ORDER BY role`, userID)
	if err != nil { return nil, err }; defer rows.Close()
	roles := []string{}
	for rows.Next() { var role string; if err := rows.Scan(&role); err != nil { return nil, err }; roles = append(roles, role) }
	return roles, rows.Err()
}

func (s *Service) digest(token string) []byte { mac := hmac.New(sha256.New, s.key); _, _ = mac.Write([]byte(token)); return mac.Sum(nil) }

// HashPassword produces an encoded Argon2id hash suitable for provisioning accounts.
func HashPassword(password string) (string, error) {
	if len(password) < 12 { return "", fmt.Errorf("password must be at least 12 bytes") }
	salt := make([]byte, 16); if _, err := rand.Read(salt); err != nil { return "", err }
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func verifyPassword(encoded, password string) bool {
	var saltRaw, hashRaw string; var memory, iterations uint32; var parallelism uint8; var version int
	if _, err := fmt.Sscanf(encoded, "$argon2id$v=%d$m=%d,t=%d,p=%d$%s", &version, &memory, &iterations, &parallelism, &saltRaw); err != nil || version != 19 { return false }
	parts := strings.Split(saltRaw, "$"); if len(parts) != 2 { return false }; saltRaw, hashRaw = parts[0], parts[1]
	salt, e1 := base64.RawStdEncoding.DecodeString(saltRaw); expected, e2 := base64.RawStdEncoding.DecodeString(hashRaw)
	if e1 != nil || e2 != nil || memory < 32768 || memory > 262144 || iterations < 2 || iterations > 10 || parallelism == 0 || parallelism > 8 { return false }
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	return hmac.Equal(actual, expected)
}
