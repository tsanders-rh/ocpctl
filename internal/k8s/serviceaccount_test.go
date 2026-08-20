package k8s

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	authv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// tokenReactor makes the fake CreateToken subresource return a fixed token.
func tokenReactor(token string) k8stesting.ReactionFunc {
	return func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "token" {
			return false, nil, nil // not the token subresource; let the tracker handle it
		}
		return true, &authv1.TokenRequest{Status: authv1.TokenRequestStatus{Token: token}}, nil
	}
}

func saExists(t *testing.T, cs *fake.Clientset, name string) bool {
	t.Helper()
	_, err := cs.CoreV1().ServiceAccounts("default").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected get error: %v", err)
	}
	return err == nil
}

func crbExists(t *testing.T, cs *fake.Clientset, name string) bool {
	t.Helper()
	_, err := cs.RbacV1().ClusterRoleBindings().Get(context.Background(), name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("unexpected get error: %v", err)
	}
	return err == nil
}

func TestCreatePoolLeaseServiceAccount_Success(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "serviceaccounts", tokenReactor("tok-abc"))
	m := &ServiceAccountManager{clientset: cs}

	creds, err := m.CreatePoolLeaseServiceAccount(context.Background(), "web", 8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.SAName != "ocpctl-lease-web" || creds.SANamespace != "default" {
		t.Errorf("unexpected SA identity: %+v", creds)
	}
	if creds.Token != "tok-abc" {
		t.Errorf("token = %q, want tok-abc", creds.Token)
	}
	if creds.TokenExpiresAt.IsZero() {
		t.Error("expected non-zero token expiry")
	}
	if !saExists(t, cs, "ocpctl-lease-web") {
		t.Error("expected ServiceAccount to exist")
	}
	if !crbExists(t, cs, "ocpctl-lease-web-admin") {
		t.Error("expected ClusterRoleBinding to exist")
	}
}

func TestCreatePoolLeaseServiceAccount_SACreateFails(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "serviceaccounts", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() == "" { // the SA itself, not the token subresource
			return true, nil, fmt.Errorf("sa boom")
		}
		return false, nil, nil
	})
	m := &ServiceAccountManager{clientset: cs}

	if _, err := m.CreatePoolLeaseServiceAccount(context.Background(), "web", 8); err == nil {
		t.Fatal("expected error when SA creation fails")
	}
}

func TestCreatePoolLeaseServiceAccount_CRBFailsCleansUpSA(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "clusterrolebindings", func(a k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("crb boom")
	})
	m := &ServiceAccountManager{clientset: cs}

	if _, err := m.CreatePoolLeaseServiceAccount(context.Background(), "web", 8); err == nil {
		t.Fatal("expected error when CRB creation fails")
	}
	if saExists(t, cs, "ocpctl-lease-web") {
		t.Error("expected ServiceAccount to be cleaned up after CRB failure")
	}
}

func TestCreatePoolLeaseServiceAccount_TokenFailsCleansUpAll(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "serviceaccounts", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() == "token" {
			return true, nil, fmt.Errorf("token boom")
		}
		return false, nil, nil
	})
	m := &ServiceAccountManager{clientset: cs}

	if _, err := m.CreatePoolLeaseServiceAccount(context.Background(), "web", 8); err == nil {
		t.Fatal("expected error when token creation fails")
	}
	if saExists(t, cs, "ocpctl-lease-web") {
		t.Error("expected ServiceAccount cleaned up after token failure")
	}
	if crbExists(t, cs, "ocpctl-lease-web-admin") {
		t.Error("expected ClusterRoleBinding cleaned up after token failure")
	}
}

func TestDeletePoolLeaseServiceAccount(t *testing.T) {
	cs := fake.NewSimpleClientset()
	cs.PrependReactor("create", "serviceaccounts", tokenReactor("tok-abc"))
	m := &ServiceAccountManager{clientset: cs}

	if _, err := m.CreatePoolLeaseServiceAccount(context.Background(), "web", 8); err != nil {
		t.Fatalf("setup create failed: %v", err)
	}

	if err := m.DeletePoolLeaseServiceAccount(context.Background(), "web"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if saExists(t, cs, "ocpctl-lease-web") {
		t.Error("expected ServiceAccount removed")
	}
	if crbExists(t, cs, "ocpctl-lease-web-admin") {
		t.Error("expected ClusterRoleBinding removed")
	}
}

func TestNewServiceAccountManager_BadKubeconfig(t *testing.T) {
	if _, err := NewServiceAccountManager(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("expected error for missing kubeconfig")
	}
}

func TestDeletePoolLeaseServiceAccount_NotFoundTolerated(t *testing.T) {
	cs := fake.NewSimpleClientset()
	m := &ServiceAccountManager{clientset: cs}
	// Nothing exists; NotFound must be swallowed and return nil.
	if err := m.DeletePoolLeaseServiceAccount(context.Background(), "ghost"); err != nil {
		t.Errorf("expected nil error for missing resources, got %v", err)
	}
}
