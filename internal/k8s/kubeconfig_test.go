package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// writeKubeconfig renders a kubeconfig with a single token-based user pointing at
// server, and returns its path. If currentContext is false, current-context is
// left unset.
func writeKubeconfig(t *testing.T, server string, currentContext bool) string {
	t.Helper()
	cfg := clientcmdapi.NewConfig()
	cl := clientcmdapi.NewCluster()
	cl.Server = server
	cl.InsecureSkipTLSVerify = true
	cfg.Clusters["c1"] = cl
	ai := clientcmdapi.NewAuthInfo()
	ai.Token = "bootstrap-token"
	cfg.AuthInfos["u1"] = ai
	kctx := clientcmdapi.NewContext()
	kctx.Cluster = "c1"
	kctx.AuthInfo = "u1"
	cfg.Contexts["ctx1"] = kctx
	if currentContext {
		cfg.CurrentContext = "ctx1"
	}

	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

func TestResolvePortableTokenTTL(t *testing.T) {
	cases := []struct {
		name     string
		ttlHours int
		want     int
	}{
		{"cluster ttl set", 72, 72},
		{"no ttl -> 30d default", 0, DefaultPortableTokenTTLHours},
		{"negative -> 30d default", -5, DefaultPortableTokenTTLHours},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolvePortableTokenTTL(tc.ttlHours); got != tc.want {
				t.Errorf("ResolvePortableTokenTTL(%d) = %d, want %d", tc.ttlHours, got, tc.want)
			}
		})
	}
}

func TestCreatePortableToken_Success(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "serviceaccounts", tokenReactor("portable-tok"))
	creds, err := CreatePortableToken(context.Background(), cs, 72)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.SAName != PortableSAName || creds.SANamespace != PortableSANamespace {
		t.Errorf("unexpected SA identity: %+v", creds)
	}
	if creds.Token != "portable-tok" {
		t.Errorf("token = %q, want portable-tok", creds.Token)
	}
	if creds.ExpiresAt.IsZero() {
		t.Error("expected non-zero token expiry")
	}

	// SA lives in kube-system, not the default namespace helpers assume.
	if _, err := cs.CoreV1().ServiceAccounts(PortableSANamespace).Get(context.Background(), PortableSAName, metav1.GetOptions{}); err != nil {
		t.Errorf("expected ServiceAccount in %s: %v", PortableSANamespace, err)
	}
	if _, err := cs.RbacV1().ClusterRoleBindings().Get(context.Background(), PortableCRBName, metav1.GetOptions{}); err != nil {
		t.Errorf("expected ClusterRoleBinding: %v", err)
	}
}

func TestCreatePortableToken_IdempotentOnAlreadyExists(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "serviceaccounts", tokenReactor("portable-tok"))
	// First call provisions SA + CRB.
	if _, err := CreatePortableToken(context.Background(), cs, 72); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	// Second call must tolerate AlreadyExists and still mint a token.
	creds, err := CreatePortableToken(context.Background(), cs, 72)
	if err != nil {
		t.Fatalf("second call failed (should be idempotent): %v", err)
	}
	if creds.Token != "portable-tok" {
		t.Errorf("token = %q, want portable-tok", creds.Token)
	}
}

func TestCreatePortableToken_TokenFails(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "serviceaccounts", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() == "token" {
			return true, nil, fmt.Errorf("token boom")
		}
		return false, nil, nil
	})
	if _, err := CreatePortableToken(context.Background(), cs, 72); err == nil {
		t.Fatal("expected error when token creation fails")
	}
}

func TestBuildKubeconfig_EmbedsCAAndToken(t *testing.T) {
	out, err := buildKubeconfig("https://api.example.com", []byte("CA-BYTES"), "ocpctl-admin", "tok-123")
	if err != nil {
		t.Fatalf("buildKubeconfig: %v", err)
	}

	cfg, err := clientcmd.Load(out)
	if err != nil {
		t.Fatalf("rendered kubeconfig does not parse: %v", err)
	}
	cluster, ok := cfg.Clusters["cluster"]
	if !ok {
		t.Fatal("missing cluster entry")
	}
	if cluster.Server != "https://api.example.com" {
		t.Errorf("server = %q", cluster.Server)
	}
	if string(cluster.CertificateAuthorityData) != "CA-BYTES" {
		t.Errorf("CA data = %q, want CA-BYTES", cluster.CertificateAuthorityData)
	}
	if cluster.InsecureSkipTLSVerify {
		t.Error("insecure-skip-tls-verify should be false when CA is embedded")
	}
	if got := cfg.AuthInfos["ocpctl-admin"]; got == nil || got.Token != "tok-123" {
		t.Errorf("token not embedded: %+v", got)
	}
	// No exec-based auth must remain in the portable kubeconfig.
	if cfg.AuthInfos["ocpctl-admin"].Exec != nil {
		t.Error("portable kubeconfig must not contain exec auth")
	}
	if cfg.CurrentContext != "ocpctl" {
		t.Errorf("current-context = %q, want ocpctl", cfg.CurrentContext)
	}
}

func TestBuildKubeconfig_NoCAFallsBackToInsecure(t *testing.T) {
	out, err := buildKubeconfig("https://api.example.com", nil, "ocpctl-admin", "tok-123")
	if err != nil {
		t.Fatalf("buildKubeconfig: %v", err)
	}
	cfg, err := clientcmd.Load(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !cfg.Clusters["cluster"].InsecureSkipTLSVerify {
		t.Error("expected insecure-skip-tls-verify when no CA data present")
	}
}

func TestExtractServerCA(t *testing.T) {
	raw := clientcmdapi.NewConfig()
	c := clientcmdapi.NewCluster()
	c.Server = "https://api.example.com"
	c.CertificateAuthorityData = []byte("CA")
	raw.Clusters["c1"] = c
	kctx := clientcmdapi.NewContext()
	kctx.Cluster = "c1"
	raw.Contexts["ctx1"] = kctx
	raw.CurrentContext = "ctx1"

	server, ca, err := extractServerCA(raw)
	if err != nil {
		t.Fatalf("extractServerCA: %v", err)
	}
	if server != "https://api.example.com" || string(ca) != "CA" {
		t.Errorf("server=%q ca=%q", server, ca)
	}
}

func TestExtractServerCA_NoCurrentContext(t *testing.T) {
	raw := clientcmdapi.NewConfig()
	if _, _, err := extractServerCA(raw); err == nil {
		t.Error("expected error when current-context is empty")
	}
}

func TestExtractServerCA_ContextNotFound(t *testing.T) {
	raw := clientcmdapi.NewConfig()
	raw.CurrentContext = "missing"
	if _, _, err := extractServerCA(raw); err == nil {
		t.Error("expected error when current-context references a missing context")
	}
}

func TestExtractServerCA_ClusterNotFound(t *testing.T) {
	raw := clientcmdapi.NewConfig()
	kctx := clientcmdapi.NewContext()
	kctx.Cluster = "ghost"
	raw.Contexts["ctx1"] = kctx
	raw.CurrentContext = "ctx1"
	if _, _, err := extractServerCA(raw); err == nil {
		t.Error("expected error when context references a missing cluster")
	}
}

func TestExtractServerCA_NoServer(t *testing.T) {
	raw := clientcmdapi.NewConfig()
	raw.Clusters["c1"] = clientcmdapi.NewCluster() // empty Server
	kctx := clientcmdapi.NewContext()
	kctx.Cluster = "c1"
	raw.Contexts["ctx1"] = kctx
	raw.CurrentContext = "ctx1"
	if _, _, err := extractServerCA(raw); err == nil {
		t.Error("expected error when cluster has no server URL")
	}
}

func TestExtractServerCA_CAFileReference(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caPath, []byte("FILE-CA"), 0600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	raw := clientcmdapi.NewConfig()
	cl := clientcmdapi.NewCluster()
	cl.Server = "https://api.example.com"
	cl.CertificateAuthority = caPath
	raw.Clusters["c1"] = cl
	kctx := clientcmdapi.NewContext()
	kctx.Cluster = "c1"
	raw.Contexts["ctx1"] = kctx
	raw.CurrentContext = "ctx1"

	_, ca, err := extractServerCA(raw)
	if err != nil {
		t.Fatalf("extractServerCA: %v", err)
	}
	if string(ca) != "FILE-CA" {
		t.Errorf("CA = %q, want FILE-CA (read from referenced file)", ca)
	}
}

func TestGeneratePortableKubeconfig_MissingFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	if _, err := GeneratePortableKubeconfig(context.Background(), filepath.Join(t.TempDir(), "nope"), out, 1); err == nil {
		t.Error("expected error for a missing source kubeconfig")
	}
}

func TestGeneratePortableKubeconfig_NoCurrentContext(t *testing.T) {
	src := writeKubeconfig(t, "https://api.example.com", false)
	out := filepath.Join(t.TempDir(), "out")
	if _, err := GeneratePortableKubeconfig(context.Background(), src, out, 1); err == nil {
		t.Error("expected error when source kubeconfig has no current-context")
	}
}

func TestGeneratePortableKubeconfig_TokenCreateFails(t *testing.T) {
	// Point at a closed port so the ServiceAccount/token API calls fail fast
	// (connection refused) instead of dialing a real cluster.
	src := writeKubeconfig(t, "https://127.0.0.1:1", true)
	out := filepath.Join(t.TempDir(), "out")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := GeneratePortableKubeconfig(ctx, src, out, 1); err == nil {
		t.Error("expected error when the cluster is unreachable for token creation")
	}
	// The exec/bootstrap kubeconfig must be left untouched on failure so callers
	// can fall back to it.
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("expected no output file on failure, stat err = %v", err)
	}
}
