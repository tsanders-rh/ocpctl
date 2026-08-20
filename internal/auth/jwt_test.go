package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func testUser() *types.User {
	return &types.User{
		ID:           "user-123",
		Email:        "alice@example.com",
		Role:         types.RoleAdmin,
		Teams:        []string{"platform"},
		ManagedTeams: []string{"platform"},
	}
}

func TestGenerateAndValidateAccessToken(t *testing.T) {
	a := NewAuth("test-secret", time.Hour, 24*time.Hour)
	u := testUser()

	tok, err := a.GenerateAccessToken(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	claims, err := a.ValidateAccessToken(tok)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if claims.UserID != u.ID {
		t.Errorf("UserID = %q, want %q", claims.UserID, u.ID)
	}
	if claims.Email != u.Email {
		t.Errorf("Email = %q, want %q", claims.Email, u.Email)
	}
	if claims.Role != string(u.Role) {
		t.Errorf("Role = %q, want %q", claims.Role, u.Role)
	}
	if claims.Subject != u.ID {
		t.Errorf("Subject = %q, want %q", claims.Subject, u.ID)
	}
	if claims.Issuer != "ocpctl" {
		t.Errorf("Issuer = %q, want ocpctl", claims.Issuer)
	}
}

func TestValidateAccessToken_WrongSecret(t *testing.T) {
	issuer := NewAuth("secret-A", time.Hour, time.Hour)
	verifier := NewAuth("secret-B", time.Hour, time.Hour)

	tok, err := issuer.GenerateAccessToken(testUser())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if _, err := verifier.ValidateAccessToken(tok); err == nil {
		t.Fatal("expected validation to fail with a different secret")
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	// Negative TTL => token is already expired at issue time.
	a := NewAuth("test-secret", -time.Minute, time.Hour)
	tok, err := a.GenerateAccessToken(testUser())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	_, err = a.ValidateAccessToken(tok)
	if err == nil {
		t.Fatal("expected expired token to fail validation")
	}
	if !strings.Contains(err.Error(), "parse token") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestValidateAccessToken_Tampered(t *testing.T) {
	a := NewAuth("test-secret", time.Hour, time.Hour)
	tok, err := a.GenerateAccessToken(testUser())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// Flip the last character of the signature segment.
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT parts, got %d", len(parts))
	}
	sig := []byte(parts[2])
	if sig[len(sig)-1] == 'A' {
		sig[len(sig)-1] = 'B'
	} else {
		sig[len(sig)-1] = 'A'
	}
	tampered := parts[0] + "." + parts[1] + "." + string(sig)

	if _, err := a.ValidateAccessToken(tampered); err == nil {
		t.Fatal("expected tampered token to fail validation")
	}
}

func TestValidateAccessToken_RejectsNoneAlg(t *testing.T) {
	a := NewAuth("test-secret", time.Hour, time.Hour)

	// Forge a token signed with the "none" algorithm.
	claims := &Claims{UserID: "attacker", Role: string(types.RoleAdmin)}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}

	if _, err := a.ValidateAccessToken(signed); err == nil {
		t.Fatal("expected 'none'-signed token to be rejected")
	}
}

func TestValidateAccessToken_Garbage(t *testing.T) {
	a := NewAuth("test-secret", time.Hour, time.Hour)
	for _, s := range []string{"", "not.a.token", "a.b", "....."} {
		if _, err := a.ValidateAccessToken(s); err == nil {
			t.Errorf("expected error for garbage token %q", s)
		}
	}
}

func TestGenerateRefreshToken_UniqueAndDecodable(t *testing.T) {
	a := NewAuth("test-secret", time.Hour, time.Hour)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok, err := a.GenerateRefreshToken()
		if err != nil {
			t.Fatalf("generate refresh: %v", err)
		}
		if tok == "" {
			t.Fatal("empty refresh token")
		}
		if seen[tok] {
			t.Fatalf("duplicate refresh token generated: %q", tok)
		}
		seen[tok] = true
	}
}

func TestTTLGetters(t *testing.T) {
	a := NewAuth("s", 15*time.Minute, 7*24*time.Hour)
	if a.GetAccessTTL() != 15*time.Minute {
		t.Errorf("access TTL = %v", a.GetAccessTTL())
	}
	if a.GetRefreshTTL() != 7*24*time.Hour {
		t.Errorf("refresh TTL = %v", a.GetRefreshTTL())
	}
}
