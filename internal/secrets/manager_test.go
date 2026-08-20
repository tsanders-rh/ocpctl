package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// mockSecrets is a mock secretsAPI. It counts calls so tests can assert on
// caching (i.e. that the AWS API is NOT hit on a cache hit).
type mockSecrets struct {
	value *string // SecretString to return (nil => binary secret)
	err   error
	calls int
}

func (m *mockSecrets) GetSecretValue(_ context.Context, _ *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: m.value}, nil
}

func newTestManager(client secretsAPI) *Manager {
	return &Manager{
		client: client,
		cache:  make(map[string]*cachedSecret),
		ttl:    5 * time.Minute,
	}
}

func TestGetSecret_Success(t *testing.T) {
	m := newTestManager(&mockSecrets{value: aws.String("s3cr3t")})
	got, err := m.GetSecret(context.Background(), "db/password")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("got %q, want s3cr3t", got)
	}
}

func TestGetSecret_CachesValue(t *testing.T) {
	mock := &mockSecrets{value: aws.String("cached-value")}
	m := newTestManager(mock)

	for i := 0; i < 3; i++ {
		v, err := m.GetSecret(context.Background(), "key")
		if err != nil || v != "cached-value" {
			t.Fatalf("call %d: v=%q err=%v", i, v, err)
		}
	}
	if mock.calls != 1 {
		t.Fatalf("expected exactly 1 AWS call (rest served from cache), got %d", mock.calls)
	}
}

func TestGetSecret_ExpiredCacheRefetches(t *testing.T) {
	mock := &mockSecrets{value: aws.String("v1")}
	m := newTestManager(mock)

	// Seed an already-expired cache entry.
	m.cache["key"] = &cachedSecret{value: "stale", expiresAt: time.Now().Add(-time.Minute)}

	v, err := m.GetSecret(context.Background(), "key")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if v != "v1" {
		t.Fatalf("expected refetch to return v1, got %q", v)
	}
	if mock.calls != 1 {
		t.Fatalf("expected 1 AWS call on expired cache, got %d", mock.calls)
	}
}

func TestGetSecret_BinarySecretError(t *testing.T) {
	m := newTestManager(&mockSecrets{value: nil}) // nil SecretString => binary
	_, err := m.GetSecret(context.Background(), "binkey")
	if err == nil {
		t.Fatal("expected error for binary secret")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("expected binary error, got %v", err)
	}
}

func TestGetSecret_APIError_NoSecretLeak(t *testing.T) {
	m := newTestManager(&mockSecrets{err: errors.New("AccessDeniedException")})
	_, err := m.GetSecret(context.Background(), "db/password")
	if err == nil {
		t.Fatal("expected error")
	}
	// Error should name the secret ID but never contain a fetched value
	// (there is none here) — sanity check the wrapping mentions the key.
	if !strings.Contains(err.Error(), "db/password") {
		t.Errorf("expected error to reference secret name, got %v", err)
	}
}

func TestGetJSONSecret(t *testing.T) {
	m := newTestManager(&mockSecrets{value: aws.String(`{"user":"admin","port":5432}`)})
	var target struct {
		User string `json:"user"`
		Port int    `json:"port"`
	}
	if err := m.GetJSONSecret(context.Background(), "db/conn", &target); err != nil {
		t.Fatalf("GetJSONSecret: %v", err)
	}
	if target.User != "admin" || target.Port != 5432 {
		t.Fatalf("unexpected parse result: %+v", target)
	}
}

func TestGetJSONSecret_InvalidJSON(t *testing.T) {
	m := newTestManager(&mockSecrets{value: aws.String("not json")})
	var target map[string]any
	if err := m.GetJSONSecret(context.Background(), "db/conn", &target); err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestInvalidateCache(t *testing.T) {
	mock := &mockSecrets{value: aws.String("v")}
	m := newTestManager(mock)

	if _, err := m.GetSecret(context.Background(), "key"); err != nil {
		t.Fatalf("prime cache: %v", err)
	}
	m.InvalidateCache("key")
	if _, err := m.GetSecret(context.Background(), "key"); err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if mock.calls != 2 {
		t.Fatalf("expected 2 AWS calls after invalidation, got %d", mock.calls)
	}
}

func TestInvalidateAllCache(t *testing.T) {
	mock := &mockSecrets{value: aws.String("v")}
	m := newTestManager(mock)
	if _, err := m.GetSecret(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetSecret(context.Background(), "b"); err != nil {
		t.Fatal(err)
	}
	m.InvalidateAllCache()
	if len(m.cache) != 0 {
		t.Fatalf("expected empty cache, got %d entries", len(m.cache))
	}
}

func TestGetSecretWithFallback_EnvVar(t *testing.T) {
	// secretName empty => skip Secrets Manager, read from env (dev path).
	m := newTestManager(&mockSecrets{err: errors.New("should not be called")})
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("MY_TOKEN", "from-env")

	v, err := m.GetSecretWithFallback(context.Background(), "", "MY_TOKEN", true)
	if err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if v != "from-env" {
		t.Fatalf("got %q, want from-env", v)
	}
}

func TestGetSecretWithFallback_RequiredMissing(t *testing.T) {
	m := newTestManager(&mockSecrets{})
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("MISSING_TOKEN", "")

	_, err := m.GetSecretWithFallback(context.Background(), "", "MISSING_TOKEN", true)
	if err == nil {
		t.Fatal("expected error when required env var is missing")
	}
}

func TestGetSecretWithFallback_ProductionRequiresName(t *testing.T) {
	m := newTestManager(&mockSecrets{})
	t.Setenv("ENVIRONMENT", "production")

	_, err := m.GetSecretWithFallback(context.Background(), "", "SOME_TOKEN", true)
	if err == nil {
		t.Fatal("expected error: production requires a secret name")
	}
}
