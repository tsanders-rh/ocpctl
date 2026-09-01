package worker

import (
	"errors"
	"strings"
	"testing"
)

func TestDetectTransientError(t *testing.T) {
	t.Run("nil error is not transient", func(t *testing.T) {
		if got := DetectTransientError(nil); got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("known transient error is detected", func(t *testing.T) {
		err := errors.New("waiting for cluster: cluster is not reachable yet")
		te := DetectTransientError(err)
		if te == nil {
			t.Fatal("expected transient error, got nil")
		}
		if te.BackoffMins != 3 {
			t.Fatalf("expected backoff 3, got %d", te.BackoffMins)
		}
	})

	t.Run("Azure SkuNotAvailable is permanent even with transient symptom", func(t *testing.T) {
		// Reproduces #148: an Azure SkuNotAvailable on one master stalls
		// provisioning, which bubbles up wrapped in a transient-looking
		// "cluster is not reachable" message. It must NOT be retried.
		err := errors.New(`RESPONSE 409: SkuNotAvailable: The requested VM size ` +
			`'Standard_D8s_v5' is currently not available in location 'eastus2'. ` +
			`failed for Capacity Restrictions. cluster is not reachable`)
		if te := DetectTransientError(err); te != nil {
			t.Fatalf("expected permanent (nil), got transient: %+v", te)
		}
	})

	t.Run("Azure NotAvailableForSubscription is permanent", func(t *testing.T) {
		err := errors.New("zone restriction: NotAvailableForSubscription")
		if te := DetectTransientError(err); te != nil {
			t.Fatalf("expected permanent (nil), got transient: %+v", te)
		}
	})

	t.Run("unrelated error is not transient", func(t *testing.T) {
		if te := DetectTransientError(errors.New("some other failure")); te != nil {
			t.Fatalf("expected nil, got %+v", te)
		}
	})
}

func TestDetectPermanentError(t *testing.T) {
	t.Run("nil error is not permanent", func(t *testing.T) {
		if cause, _ := DetectPermanentError(nil); cause != "" {
			t.Fatalf("expected no cause, got %q", cause)
		}
	})

	t.Run("ResourceGroupNotFound with transient symptom is permanent", func(t *testing.T) {
		// The originally-reported #148 case: a permanent DNS/resource-group
		// misconfiguration co-occurs with a transient "cluster is not reachable".
		// It must be classified permanent so it is NOT retried.
		err := errors.New("failed to fetch dependency: cluster is not reachable: " +
			"ResourceGroupNotFound: Resource group 'os4-common' could not be found")
		cause, detail := DetectPermanentError(err)
		if cause == "" {
			t.Fatal("expected permanent classification, got none")
		}
		if detail == "" {
			t.Fatal("expected a detail line, got empty")
		}
		// And it must not be routed to the transient retry path.
		if te := DetectTransientError(err); te != nil {
			t.Fatalf("expected non-transient, got %+v", te)
		}
	})

	t.Run("AuthorizationFailed is permanent", func(t *testing.T) {
		err := errors.New("AuthorizationFailed: The client does not have authorization to perform action")
		if cause, _ := DetectPermanentError(err); cause == "" {
			t.Fatal("expected permanent classification, got none")
		}
	})

	t.Run("QuotaExceeded is permanent", func(t *testing.T) {
		err := errors.New("compute.VirtualMachinesClient: QuotaExceeded: Operation could not be completed")
		if cause, _ := DetectPermanentError(err); cause == "" {
			t.Fatal("expected permanent classification, got none")
		}
	})

	t.Run("prefers final level=error fatal line as detail", func(t *testing.T) {
		err := errors.New(
			"time=\"...\" level=info msg=checking ResourceGroupNotFound (informational)\n" +
				"time=\"...\" level=debug msg=some other line\n" +
				"time=\"...\" level=error msg=ResourceGroupNotFound: Resource group 'os4-common' could not be found")
		_, detail := DetectPermanentError(err)
		if !strings.Contains(detail, "level=error") {
			t.Fatalf("expected the level=error line as detail, got %q", detail)
		}
	})

	t.Run("plain transient error is not permanent", func(t *testing.T) {
		if cause, _ := DetectPermanentError(errors.New("cluster is not reachable")); cause != "" {
			t.Fatalf("expected no permanent cause, got %q", cause)
		}
	})
}
