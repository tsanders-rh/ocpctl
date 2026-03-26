package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// Manager handles retrieval of secrets from AWS Secrets Manager with caching
type Manager struct {
	client *secretsmanager.Client
	cache  map[string]*cachedSecret
	mu     sync.RWMutex
	ttl    time.Duration
}

// cachedSecret stores a secret value with its expiration time
type cachedSecret struct {
	value     string
	expiresAt time.Time
}

// NewManager creates a new secrets manager instance
func NewManager(ctx context.Context) (*Manager, error) {
	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	client := secretsmanager.NewFromConfig(cfg)

	return &Manager{
		client: client,
		cache:  make(map[string]*cachedSecret),
		ttl:    5 * time.Minute, // Cache secrets for 5 minutes
	}, nil
}

// GetSecret retrieves a secret from AWS Secrets Manager with caching
func (m *Manager) GetSecret(ctx context.Context, secretName string) (string, error) {
	// Check cache first
	m.mu.RLock()
	cached, exists := m.cache[secretName]
	m.mu.RUnlock()

	if exists && time.Now().Before(cached.expiresAt) {
		return cached.value, nil
	}

	// Retrieve from AWS Secrets Manager
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}

	result, err := m.client.GetSecretValue(ctx, input)
	if err != nil {
		return "", fmt.Errorf("get secret %s: %w", secretName, err)
	}

	var secretValue string
	if result.SecretString != nil {
		secretValue = *result.SecretString
	} else {
		return "", fmt.Errorf("secret %s is binary, expected string", secretName)
	}

	// Cache the secret
	m.mu.Lock()
	m.cache[secretName] = &cachedSecret{
		value:     secretValue,
		expiresAt: time.Now().Add(m.ttl),
	}
	m.mu.Unlock()

	return secretValue, nil
}

// GetSecretWithFallback retrieves a secret from AWS Secrets Manager, falling back to environment variable
func (m *Manager) GetSecretWithFallback(ctx context.Context, secretName, envVar string, required bool) (string, error) {
	environment := os.Getenv("ENVIRONMENT")

	// In production, always use Secrets Manager
	if environment == "production" {
		if secretName == "" {
			return "", fmt.Errorf("secret name must be specified in production (set %s_SECRET_NAME)", envVar)
		}

		value, err := m.GetSecret(ctx, secretName)
		if err != nil {
			if required {
				return "", fmt.Errorf("failed to retrieve required secret %s: %w", secretName, err)
			}
			log.Printf("WARNING: Failed to retrieve secret %s: %v", secretName, err)
			return "", nil
		}
		return value, nil
	}

	// In development, try Secrets Manager first if configured, then fall back to env var
	if secretName != "" {
		value, err := m.GetSecret(ctx, secretName)
		if err == nil {
			log.Printf("Retrieved secret %s from AWS Secrets Manager", secretName)
			return value, nil
		}
		log.Printf("Failed to retrieve secret %s from AWS Secrets Manager (will try env var): %v", secretName, err)
	}

	// Fall back to environment variable
	value := os.Getenv(envVar)
	if value == "" && required {
		return "", fmt.Errorf("%s not set (use AWS Secrets Manager or set environment variable for development)", envVar)
	}

	if value != "" {
		log.Printf("Using %s from environment variable (development mode)", envVar)
	}

	return value, nil
}

// GetJSONSecret retrieves a secret and parses it as JSON
func (m *Manager) GetJSONSecret(ctx context.Context, secretName string, target interface{}) error {
	secretValue, err := m.GetSecret(ctx, secretName)
	if err != nil {
		return err
	}

	if err := json.Unmarshal([]byte(secretValue), target); err != nil {
		return fmt.Errorf("parse secret %s as JSON: %w", secretName, err)
	}

	return nil
}

// InvalidateCache clears the cache for a specific secret (useful for rotation)
func (m *Manager) InvalidateCache(secretName string) {
	m.mu.Lock()
	delete(m.cache, secretName)
	m.mu.Unlock()
	log.Printf("Invalidated cache for secret: %s", secretName)
}

// InvalidateAllCache clears the entire secret cache
func (m *Manager) InvalidateAllCache() {
	m.mu.Lock()
	m.cache = make(map[string]*cachedSecret)
	m.mu.Unlock()
	log.Println("Invalidated all secret caches")
}
