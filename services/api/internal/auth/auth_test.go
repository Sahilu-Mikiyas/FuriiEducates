package auth

import (
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	encoded, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected encoding: %s", encoded)
	}
	if !verifyPassword(encoded, "correct-horse-battery-staple") {
		t.Fatal("correct password was rejected")
	}
	if verifyPassword(encoded, "correct-horse-battery-stapler") {
		t.Fatal("wrong password was accepted")
	}
	other, _ := HashPassword("correct-horse-battery-staple")
	if other == encoded {
		t.Fatal("two hashes of the same password share a salt")
	}
}

func TestHashPasswordRejectsShortPasswords(t *testing.T) {
	if _, err := HashPassword("short-pass"); err == nil {
		t.Fatal("passwords under 12 bytes must be rejected")
	}
}

func TestVerifyPasswordRejectsMalformedOrWeakEncodings(t *testing.T) {
	encoded, _ := HashPassword("correct-horse-battery-staple")
	parts := strings.Split(encoded, "$")
	weak := strings.Join([]string{"", parts[1], parts[2], "m=1024,t=1,p=1", parts[4], parts[5]}, "$")
	for name, value := range map[string]string{
		"empty":           "",
		"wrong algorithm": strings.Replace(encoded, "argon2id", "argon2i", 1),
		"too few parts":   "$argon2id$v=19$m=65536,t=3,p=2$salt",
		"weak params":     weak,
		"bad salt":        strings.Join([]string{"", parts[1], parts[2], parts[3], "!!!", parts[5]}, "$"),
	} {
		if verifyPassword(value, "correct-horse-battery-staple") {
			t.Errorf("%s encoding was accepted", name)
		}
	}
}
