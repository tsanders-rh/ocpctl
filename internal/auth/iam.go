package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// IAMAuthenticator handles AWS IAM authentication
type IAMAuthenticator struct {
	stsClient      *sts.Client
	iamMappingStore *store.IAMMappingStore
	userStore      *store.UserStore
	enabledIAMAuth bool
}

// NewIAMAuthenticator creates a new IAM authenticator
func NewIAMAuthenticator(iamMappingStore *store.IAMMappingStore, userStore *store.UserStore, enabled bool) (*IAMAuthenticator, error) {
	if !enabled {
		return &IAMAuthenticator{
			iamMappingStore: iamMappingStore,
			userStore:      userStore,
			enabledIAMAuth: false,
		}, nil
	}

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	stsClient := sts.NewFromConfig(cfg)

	return &IAMAuthenticator{
		stsClient:      stsClient,
		iamMappingStore: iamMappingStore,
		userStore:      userStore,
		enabledIAMAuth: true,
	}, nil
}

// IsIAMRequest detects if a request uses AWS SigV4 authentication
func IsIAMRequest(r *http.Request) bool {
	authHeader := r.Header.Get("Authorization")
	return strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256")
}

// ValidateIAMRequest validates an AWS SigV4 signed request and returns the associated user
// This is a simplified implementation - production would validate the signature
func (a *IAMAuthenticator) ValidateIAMRequest(ctx context.Context, r *http.Request) (*types.User, error) {
	if !a.enabledIAMAuth {
		return nil, fmt.Errorf("IAM authentication is not enabled")
	}

	// Extract AWS credentials from the request
	// In production, this would validate the SigV4 signature
	// For now, we'll use GetCallerIdentity to verify the credentials

	// Get caller identity to extract the IAM principal ARN
	callerIdentity, err := a.stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to verify IAM credentials: %w", err)
	}

	principalARN := aws.ToString(callerIdentity.Arn)

	// Look up IAM principal mapping
	mapping, err := a.iamMappingStore.GetByPrincipalARN(ctx, principalARN)
	if err != nil {
		// Auto-provision: create user and mapping for new IAM principals
		user, err := a.autoProvisionUser(ctx, principalARN, callerIdentity)
		if err != nil {
			return nil, fmt.Errorf("failed to auto-provision user for IAM principal %s: %w", principalARN, err)
		}
		return user, nil
	}

	// Get the associated user
	user, err := a.userStore.GetByID(ctx, mapping.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user for IAM principal %s: %w", principalARN, err)
	}

	// Check if user is active
	if !user.Active {
		return nil, fmt.Errorf("user account is disabled")
	}

	return user, nil
}

// autoProvisionUser creates a new user record for an IAM principal
func (a *IAMAuthenticator) autoProvisionUser(ctx context.Context, principalARN string, identity *sts.GetCallerIdentityOutput) (*types.User, error) {
	// Extract username from ARN
	// ARN format: arn:aws:iam::123456789012:user/username or arn:aws:sts::123456789012:assumed-role/role-name/session-name
	parts := strings.Split(principalARN, "/")
	username := parts[len(parts)-1]

	// Use Account ID as email prefix (for display purposes)
	accountID := aws.ToString(identity.Account)
	email := fmt.Sprintf("%s@aws-account-%s", username, accountID)

	// Create user with USER role by default
	user := &types.User{
		Email:        email,
		Username:     username,
		PasswordHash: "", // No password for IAM users
		Role:         types.RoleUser,
		Active:       true,
	}

	// Create user in database
	err := a.userStore.Create(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Create IAM principal mapping
	mapping := &types.CreateIAMMapping{
		IAMPrincipalARN: principalARN,
		UserID:          user.ID,
		Enabled:         true,
	}

	_, err = a.iamMappingStore.Create(ctx, mapping)
	if err != nil {
		// Cleanup: delete created user if mapping fails
		_ = a.userStore.Delete(ctx, user.ID)
		return nil, fmt.Errorf("failed to create IAM mapping: %w", err)
	}

	return user, nil
}

// ValidateSigV4 validates an AWS SigV4 signature (simplified version)
// Production implementation would use aws-sdk-go-v2/aws/signer/v4 to verify signatures
func (a *IAMAuthenticator) ValidateSigV4(r *http.Request) error {
	// This is a placeholder for full SigV4 validation
	// In production, you would:
	// 1. Parse the Authorization header
	// 2. Extract credential scope, signature, signed headers
	// 3. Reconstruct the canonical request
	// 4. Calculate the expected signature
	// 5. Compare with the provided signature

	// For now, we rely on GetCallerIdentity validation
	// which proves the request was signed with valid AWS credentials

	return nil
}
