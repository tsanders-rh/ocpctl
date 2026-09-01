package worker

import (
	"errors"
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
