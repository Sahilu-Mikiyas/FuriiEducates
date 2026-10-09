package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL   string
	HTTPAddr      string
	SessionHashKey []byte
	SessionTTL    time.Duration
	CookieSecure  bool
}

func Load() (Config, error) {
	var c Config
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.HTTPAddr = os.Getenv("HTTP_ADDR")
	if c.HTTPAddr == "" { c.HTTPAddr = ":8080" }
	if c.DatabaseURL == "" { return c, fmt.Errorf("DATABASE_URL is required") }
	key := os.Getenv("SESSION_HASH_KEY")
	if len(key) < 32 { return c, fmt.Errorf("SESSION_HASH_KEY must contain at least 32 bytes") }
	c.SessionHashKey = []byte(key)
	c.SessionTTL = 12 * time.Hour
	if raw := os.Getenv("SESSION_TTL"); raw != "" {
		d, err := time.ParseDuration(raw); if err != nil || d <= 0 { return c, fmt.Errorf("SESSION_TTL must be a positive duration") }; c.SessionTTL = d
	}
	secure := os.Getenv("COOKIE_SECURE")
	if secure != "" { v, err := strconv.ParseBool(secure); if err != nil { return c, fmt.Errorf("COOKIE_SECURE must be true or false") }; c.CookieSecure = v }
	return c, nil
}
