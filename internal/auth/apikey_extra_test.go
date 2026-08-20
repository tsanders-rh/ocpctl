package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestWithAuth(header string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if header != "" {
		r.Header.Set("Authorization", header)
	}
	return r
}

func TestValidateAPIKeyRejectsBadFormat(t *testing.T) {
	// A key without the ocpctl_ prefix is rejected before any store lookup, so a
	// nil store is safe here.
	if _, err := ValidateAPIKey(context.Background(), nil, "not-an-ocpctl-key"); err == nil {
		t.Error("expected error for key without prefix")
	}
}

func TestValidateAPIKeyFromContextWrongStoreType(t *testing.T) {
	if _, err := ValidateAPIKeyFromContext(context.Background(), "not-a-store", APIKeyPrefix+"abc"); err == nil {
		t.Error("expected error for non-*store.Store value")
	}
}

func TestIsIAMRequest(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{"sigv4", "AWS4-HMAC-SHA256 Credential=AKIA...", true},
		{"bearer", "Bearer abc", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := requestWithAuth(tt.header)
			if got := IsIAMRequest(r); got != tt.want {
				t.Errorf("IsIAMRequest(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}

func TestNewIAMAuthenticatorDisabled(t *testing.T) {
	// Disabled path must not touch AWS config; it returns a usable, disabled auth.
	iamAuth, err := NewIAMAuthenticator(nil, nil, false, "my-group")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iamAuth.enabledIAMAuth {
		t.Error("expected IAM auth to be disabled")
	}
	if _, err := iamAuth.ValidateIAMRequest(context.Background(), requestWithAuth("AWS4-HMAC-SHA256 x")); err == nil {
		t.Error("expected disabled authenticator to reject validation")
	}
}
