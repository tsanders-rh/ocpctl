package k8s

import (
	"context"
	"fmt"
	"os"
	"time"

	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Portable kubeconfig defaults.
//
// EKS/GKE kubeconfigs produced by the cloud CLIs use exec-based auth
// (`aws eks get-token`, `gke-gcloud-auth-plugin`), which requires the consumer
// to have the cloud CLI + IAM permissions on every kubectl call. That makes the
// downloaded kubeconfig unusable in CI, for external users, or for automation.
// GeneratePortableKubeconfig replaces that exec-based auth with a static,
// embedded ServiceAccount token so a downloaded kubeconfig works standalone.
const (
	// PortableSAName is the ServiceAccount created on the target cluster to back
	// the portable kubeconfig token.
	PortableSAName = "ocpctl-admin"
	// PortableSANamespace is the namespace for the portable ServiceAccount.
	PortableSANamespace = "kube-system"
	// PortableCRBName is the ClusterRoleBinding granting the SA cluster-admin.
	PortableCRBName = "ocpctl-admin"
	// DefaultPortableTokenTTLHours is the token lifetime used for clusters that
	// have no TTL of their own (30 days).
	DefaultPortableTokenTTLHours = 30 * 24
)

// PortableCredentials describes the ServiceAccount token embedded into a
// portable kubeconfig.
type PortableCredentials struct {
	SAName      string
	SANamespace string
	Token       string
	ExpiresAt   time.Time
}

// ResolvePortableTokenTTL returns the token lifetime (in hours) for a cluster:
// the cluster's own TTL when set, otherwise the 30-day default.
func ResolvePortableTokenTTL(clusterTTLHours int) int {
	if clusterTTLHours > 0 {
		return clusterTTLHours
	}
	return DefaultPortableTokenTTLHours
}

// GeneratePortableKubeconfig reads an exec-based kubeconfig (execKubeconfigPath),
// mints a cluster-admin ServiceAccount token on the target cluster, and writes a
// standalone token-based kubeconfig to outputPath (which may equal
// execKubeconfigPath to overwrite in place).
//
// It must run on a host that can satisfy the exec-based auth (i.e. the worker,
// which has the cloud CLI + credentials). The returned credentials should be
// persisted to ClusterOutputs so the API can serve/inspect the token later.
func GeneratePortableKubeconfig(ctx context.Context, execKubeconfigPath, outputPath string, ttlHours int) (*PortableCredentials, error) {
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	loader.ExplicitPath = execKubeconfigPath
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{})

	// Extract the server URL + CA from the source kubeconfig before we swap auth.
	raw, err := cc.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig %q: %w", execKubeconfigPath, err)
	}
	server, caData, err := extractServerCA(&raw)
	if err != nil {
		return nil, err
	}

	// Build a client using the exec-based auth (invokes the cloud CLI plugin).
	restCfg, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build client config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	creds, err := CreatePortableToken(ctx, clientset, ttlHours)
	if err != nil {
		return nil, err
	}

	kubeconfig, err := buildKubeconfig(server, caData, creds.SAName, creds.Token)
	if err != nil {
		return nil, fmt.Errorf("render portable kubeconfig: %w", err)
	}
	if err := os.WriteFile(outputPath, kubeconfig, 0600); err != nil {
		return nil, fmt.Errorf("write portable kubeconfig %q: %w", outputPath, err)
	}

	return creds, nil
}

// CreatePortableToken ensures the ocpctl-admin ServiceAccount + cluster-admin
// ClusterRoleBinding exist on the target cluster and returns a time-bound token.
//
// Creation is idempotent (AlreadyExists is tolerated) so cluster-create retries
// don't fail on a partially-provisioned SA. Unlike the pool-lease flow this does
// not tear down the SA on error: the caller falls back to the exec-based
// kubeconfig, and any orphaned SA is harmless and reused on retry.
func CreatePortableToken(ctx context.Context, clientset kubernetes.Interface, ttlHours int) (*PortableCredentials, error) {
	labels := map[string]string{
		"ocpctl.io/managed": "true",
		"ocpctl.io/purpose": "portable-kubeconfig",
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PortableSAName,
			Namespace: PortableSANamespace,
			Labels:    labels,
		},
	}
	if _, err := clientset.CoreV1().ServiceAccounts(PortableSANamespace).Create(ctx, sa, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create ServiceAccount: %w", err)
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   PortableCRBName,
			Labels: labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      PortableSAName,
				Namespace: PortableSANamespace,
			},
		},
	}
	if _, err := clientset.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create ClusterRoleBinding: %w", err)
	}

	expirationSeconds := int64(ttlHours * 3600)
	tokenRequest := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			ExpirationSeconds: &expirationSeconds,
		},
	}
	resp, err := clientset.CoreV1().ServiceAccounts(PortableSANamespace).CreateToken(ctx, PortableSAName, tokenRequest, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}

	// The API server may cap the requested lifetime; prefer the actual expiry it
	// returns, falling back to the requested duration if unset (e.g. fakes).
	expiresAt := resp.Status.ExpirationTimestamp.Time
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(time.Duration(ttlHours) * time.Hour)
	}

	return &PortableCredentials{
		SAName:      PortableSAName,
		SANamespace: PortableSANamespace,
		Token:       resp.Status.Token,
		ExpiresAt:   expiresAt,
	}, nil
}

// extractServerCA pulls the API server URL and CA bundle from the current
// context of a kubeconfig. CA data may be inline (CertificateAuthorityData) or a
// file reference (CertificateAuthority); an empty CA signals the caller to fall
// back to insecure-skip-tls-verify.
func extractServerCA(raw *clientcmdapi.Config) (server string, caData []byte, err error) {
	ctxName := raw.CurrentContext
	if ctxName == "" {
		return "", nil, fmt.Errorf("kubeconfig has no current-context")
	}
	kctx, ok := raw.Contexts[ctxName]
	if !ok {
		return "", nil, fmt.Errorf("kubeconfig context %q not found", ctxName)
	}
	cluster, ok := raw.Clusters[kctx.Cluster]
	if !ok {
		return "", nil, fmt.Errorf("kubeconfig cluster %q not found", kctx.Cluster)
	}
	if cluster.Server == "" {
		return "", nil, fmt.Errorf("kubeconfig cluster %q has no server URL", kctx.Cluster)
	}

	caData = cluster.CertificateAuthorityData
	if len(caData) == 0 && cluster.CertificateAuthority != "" {
		// Best effort: embed the referenced CA file so the output stays portable.
		if b, readErr := os.ReadFile(cluster.CertificateAuthority); readErr == nil {
			caData = b
		}
	}
	return cluster.Server, caData, nil
}

// buildKubeconfig renders a standalone token-based kubeconfig. If caData is
// empty it falls back to insecure-skip-tls-verify so the config still works.
func buildKubeconfig(server string, caData []byte, saName, token string) ([]byte, error) {
	cfg := clientcmdapi.NewConfig()

	cluster := clientcmdapi.NewCluster()
	cluster.Server = server
	if len(caData) > 0 {
		cluster.CertificateAuthorityData = caData
	} else {
		cluster.InsecureSkipTLSVerify = true
	}
	cfg.Clusters["cluster"] = cluster

	authInfo := clientcmdapi.NewAuthInfo()
	authInfo.Token = token
	cfg.AuthInfos[saName] = authInfo

	kctx := clientcmdapi.NewContext()
	kctx.Cluster = "cluster"
	kctx.AuthInfo = saName
	cfg.Contexts["ocpctl"] = kctx

	cfg.CurrentContext = "ocpctl"

	return clientcmd.Write(*cfg)
}
