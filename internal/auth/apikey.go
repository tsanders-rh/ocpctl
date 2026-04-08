package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

const (
	// APIKeyPrefix is the prefix for all API keys
	APIKeyPrefix = "ocpctl_"
	// APIKeyLength is the total length of the random part (32 bytes = 43 chars base64)
	APIKeyLength = 32
)

// GenerateAPIKey generates a new random API key with the ocpctl_ prefix
// Returns the full plaintext key (only shown once) and the prefix for display
func GenerateAPIKey() (plainKey string, keyPrefix string, keyHash string, error error) {
	// Generate random bytes
	randomBytes := make([]byte, APIKeyLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", "", fmt.Errorf("generate random bytes: %w", err)
	}

	// Encode to base64 URL-safe (no padding)
	randomPart := base64.RawURLEncoding.EncodeToString(randomBytes)

	// Create full key with prefix
	plainKey = APIKeyPrefix + randomPart

	// Create key prefix (first 12 chars for display)
	keyPrefix = plainKey[:12] + "..."

	// Hash the key for storage
	hasher := sha256.New()
	hasher.Write([]byte(plainKey))
	keyHash = hex.EncodeToString(hasher.Sum(nil))

	return plainKey, keyPrefix, keyHash, nil
}

// ValidateAPIKey validates an API key and returns the associated user
func ValidateAPIKey(ctx context.Context, st *store.Store, plainKey string) (*types.User, error) {
	// Check if it has the correct prefix
	if !strings.HasPrefix(plainKey, APIKeyPrefix) {
		return nil, fmt.Errorf("invalid API key format")
	}

	// Hash the key
	hasher := sha256.New()
	hasher.Write([]byte(plainKey))
	keyHash := hex.EncodeToString(hasher.Sum(nil))

	// Look up the API key
	apiKey, err := st.APIKeys.GetByKeyHash(ctx, keyHash)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, fmt.Errorf("invalid API key")
		}
		return nil, fmt.Errorf("lookup API key: %w", err)
	}

	// Update last used timestamp (fire and forget)
	go func() {
		_ = st.APIKeys.UpdateLastUsed(context.Background(), apiKey.ID)
	}()

	// Get the user
	user, err := st.Users.GetByID(ctx, apiKey.UserID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	// Check if user is active
	if !user.Active {
		return nil, fmt.Errorf("user account is disabled")
	}

	return user, nil
}

// IsAPIKey checks if a token string is an API key
func IsAPIKey(token string) bool {
	return strings.HasPrefix(token, APIKeyPrefix)
}

// ValidateAPIKeyFromContext validates an API key using the store from context
func ValidateAPIKeyFromContext(ctx context.Context, storeVal interface{}, plainKey string) (*types.User, error) {
	st, ok := storeVal.(*store.Store)
	if !ok {
		return nil, fmt.Errorf("invalid store type in context")
	}

	return ValidateAPIKey(ctx, st, plainKey)
}
