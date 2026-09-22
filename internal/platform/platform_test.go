package platform

import "testing"

func TestNormalization(t *testing.T) {
	if got := NormalizeEmail(" User@Example.COM "); got != "user@example.com" {
		t.Fatalf("email = %q", got)
	}
	if got := NormalizePlate("  dhaka-12-3456  "); got != "DHAKA-12-3456" {
		t.Fatalf("plate = %q", got)
	}
	if !ValidEmail("user@example.com") || ValidEmail("not-an-email") {
		t.Fatal("email validation mismatch")
	}
}

func TestIDsAndTokens(t *testing.T) {
	a, b := NewID(), NewID()
	if len(a) != 32 || a == b {
		t.Fatalf("invalid ids %q %q", a, b)
	}
	token := NewToken()
	if len(token) != 64 || HashToken(token) == token {
		t.Fatal("invalid token generation or hashing")
	}
}
