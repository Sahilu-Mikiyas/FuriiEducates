package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/furii")
	t.Setenv("SESSION_HASH_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("SESSION_TTL", "")
	t.Setenv("COOKIE_SECURE", "")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.SessionTTL != 12*time.Hour || c.CookieSecure {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoadRejectsInvalidSettings(t *testing.T) {
	cases := map[string]map[string]string{
		"missing database": {"DATABASE_URL": "", "SESSION_HASH_KEY": "0123456789abcdef0123456789abcdef"},
		"short key":        {"DATABASE_URL": "postgres://x", "SESSION_HASH_KEY": "too-short"},
		"bad ttl":          {"DATABASE_URL": "postgres://x", "SESSION_HASH_KEY": "0123456789abcdef0123456789abcdef", "SESSION_TTL": "-1h"},
		"bad cookie flag":  {"DATABASE_URL": "postgres://x", "SESSION_HASH_KEY": "0123456789abcdef0123456789abcdef", "COOKIE_SECURE": "maybe"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("SESSION_TTL", "")
			t.Setenv("COOKIE_SECURE", "")
			for key, value := range env {
				t.Setenv(key, value)
			}
			if _, err := Load(); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}
