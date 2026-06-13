package k8s

import (
	"context"
	"fmt"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ServiceAccountManager manages Kubernetes ServiceAccounts for cluster pool leasing
type ServiceAccountManager struct {
	clientset *kubernetes.Clientset
}

// PoolLeaseCredentials contains ServiceAccount credentials for pool cluster leasing
type PoolLeaseCredentials struct {
	SAName          string
	SANamespace     string
	Token           string
	TokenExpiresAt  time.Time
}

// NewServiceAccountManager creates a new ServiceAccount manager from a kubeconfig file
func NewServiceAccountManager(kubeconfigPath string) (*ServiceAccountManager, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	return &ServiceAccountManager{
		clientset: clientset,
	}, nil
}

// CreatePoolLeaseServiceAccount creates a ServiceAccount with cluster-admin and generates time-bound token
//
// This creates:
// 1. ServiceAccount in default namespace
// 2. ClusterRoleBinding granting cluster-admin access
// 3. Time-bound token with expiration matching lease duration
//
// The token automatically expires when the lease ends (enforced by Kubernetes).
func (m *ServiceAccountManager) CreatePoolLeaseServiceAccount(ctx context.Context, clusterName string, leaseDurationHours int) (*PoolLeaseCredentials, error) {
	saName := fmt.Sprintf("ocpctl-lease-%s", clusterName)
	saNamespace := "default"

	// 1. Create ServiceAccount in default namespace
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: saNamespace,
			Labels: map[string]string{
				"ocpctl.io/managed":      "true",
				"ocpctl.io/cluster-name": clusterName,
				"ocpctl.io/purpose":      "pool-lease",
			},
		},
	}

	_, err := m.clientset.CoreV1().ServiceAccounts(saNamespace).Create(ctx, sa, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create ServiceAccount: %w", err)
	}

	// 2. Create ClusterRoleBinding to cluster-admin
	crbName := fmt.Sprintf("ocpctl-lease-%s-admin", clusterName)
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: crbName,
			Labels: map[string]string{
				"ocpctl.io/managed":      "true",
				"ocpctl.io/cluster-name": clusterName,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: saNamespace,
			},
		},
	}

	_, err = m.clientset.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{})
	if err != nil {
		// Clean up ServiceAccount if ClusterRoleBinding creation fails
		_ = m.clientset.CoreV1().ServiceAccounts(saNamespace).Delete(ctx, saName, metav1.DeleteOptions{})
		return nil, fmt.Errorf("create ClusterRoleBinding: %w", err)
	}

	// 3. Generate time-bound token
	expirationSeconds := int64(leaseDurationHours * 3600)
	tokenRequest := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			ExpirationSeconds: &expirationSeconds,
		},
	}

	tokenResponse, err := m.clientset.CoreV1().ServiceAccounts(saNamespace).CreateToken(ctx, saName, tokenRequest, metav1.CreateOptions{})
	if err != nil {
		// Clean up on failure
		_ = m.clientset.RbacV1().ClusterRoleBindings().Delete(ctx, crbName, metav1.DeleteOptions{})
		_ = m.clientset.CoreV1().ServiceAccounts(saNamespace).Delete(ctx, saName, metav1.DeleteOptions{})
		return nil, fmt.Errorf("create token: %w", err)
	}

	return &PoolLeaseCredentials{
		SAName:         saName,
		SANamespace:    saNamespace,
		Token:          tokenResponse.Status.Token,
		TokenExpiresAt: time.Now().Add(time.Duration(leaseDurationHours) * time.Hour),
	}, nil
}

// DeletePoolLeaseServiceAccount removes ServiceAccount and ClusterRoleBinding
//
// This is called during POOL_CLEAN to revoke credentials before cluster refresh.
// Errors are non-fatal (NotFound errors are ignored).
func (m *ServiceAccountManager) DeletePoolLeaseServiceAccount(ctx context.Context, clusterName string) error {
	saName := fmt.Sprintf("ocpctl-lease-%s", clusterName)
	crbName := fmt.Sprintf("ocpctl-lease-%s-admin", clusterName)
	saNamespace := "default"

	// Delete ClusterRoleBinding (ignore NotFound errors)
	err := m.clientset.RbacV1().ClusterRoleBindings().Delete(ctx, crbName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete ClusterRoleBinding: %w", err)
	}

	// Delete ServiceAccount (ignore NotFound errors)
	err = m.clientset.CoreV1().ServiceAccounts(saNamespace).Delete(ctx, saName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete ServiceAccount: %w", err)
	}

	return nil
}
