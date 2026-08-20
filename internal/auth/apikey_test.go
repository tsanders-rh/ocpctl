package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateAPIKey(t *testing.T) {
	plain, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}

	if !strings.HasPrefix(plain, APIKeyPrefix) {
		t.Errorf("plain key %q missing prefix %q", plain, APIKeyPrefix)
	}
	if prefix != plain[:12]+"..." {
		t.Errorf("display prefix = %q, want %q", prefix, plain[:12]+"...")
	}

	// Hash must be the hex sha256 of the plaintext key (so lookups match).
	want := sha256.Sum256([]byte(plain))
	if hash != hex.EncodeToString(want[:]) {
		t.Errorf("hash = %q, want sha256(plain)", hash)
	}
	// sha256 hex is 64 chars.
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash))
	}
}

func TestGenerateAPIKey_Unique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		plain, _, hash, err := GenerateAPIKey()
		if err != nil {
			t.Fatalf("GenerateAPIKey: %v", err)
		}
		if seen[plain] {
			t.Fatalf("duplicate plaintext key: %q", plain)
		}
		if seen[hash] {
			t.Fatalf("duplicate hash: %q", hash)
		}
		seen[plain] = true
		seen[hash] = true
	}
}

func TestIsAPIKey(t *testing.T) {
	cases := map[string]bool{
		"ocpctl_abc123":           true,
		"ocpctl_":                 true,
		"eyJhbGciOiJIUzI1NiIs...": false, // JWT
		"":                        false,
		"OCPCTL_upper":            false, // case-sensitive prefix
		"prefix_ocpctl_x":         false,
	}
	for token, want := range cases {
		if got := IsAPIKey(token); got != want {
			t.Errorf("IsAPIKey(%q) = %v, want %v", token, got, want)
		}
	}
}
