package worker

import (
	"sync"
	"testing"
)

func newSlotWorker(maxConcurrent int) *Worker {
	return &Worker{slots: make(chan struct{}, maxConcurrent)}
}

func TestSlotsCapConcurrentJobs(t *testing.T) {
	w := newSlotWorker(3)

	if got := w.freeSlots(); got != 3 {
		t.Fatalf("freeSlots() = %d on a fresh worker, want 3", got)
	}

	for i := 1; i <= 3; i++ {
		if !w.tryClaimSlot() {
			t.Fatalf("claim %d of 3 failed", i)
		}
	}

	// The regression this guards: a fourth job must not start just because a new
	// poll cycle came around while three were already running.
	if w.tryClaimSlot() {
		t.Fatal("claimed a 4th slot on a worker with MaxConcurrent=3")
	}
	if got := w.freeSlots(); got != 0 {
		t.Fatalf("freeSlots() = %d with all slots claimed, want 0", got)
	}

	w.releaseSlot()

	if got := w.freeSlots(); got != 1 {
		t.Fatalf("freeSlots() = %d after one release, want 1", got)
	}
	if !w.tryClaimSlot() {
		t.Fatal("could not claim the slot freed by releaseSlot()")
	}
}

func TestSlotsReleaseWithoutClaimDoesNotBlock(t *testing.T) {
	w := newSlotWorker(1)

	done := make(chan struct{})
	go func() {
		w.releaseSlot()
		close(done)
	}()

	select {
	case <-done:
	case <-make(chan struct{}):
		t.Fatal("releaseSlot() blocked with no slot held")
	}

	if got := w.freeSlots(); got != 1 {
		t.Fatalf("freeSlots() = %d, want 1 (a spurious release must not add capacity)", got)
	}
}

func TestSlotsAreSafeUnderConcurrency(t *testing.T) {
	const capacity = 4
	w := newSlotWorker(capacity)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed int
	)

	wg.Add(32)
	for i := 0; i < 32; i++ {
		go func() {
			defer wg.Done()
			if w.tryClaimSlot() {
				mu.Lock()
				claimed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if claimed != capacity {
		t.Fatalf("%d goroutines claimed slots, want exactly %d", claimed, capacity)
	}
}

func TestSlotCapacityClampsUnusableValues(t *testing.T) {
	// DefaultConfig sets MaxConcurrent, but a hand-built Config could leave it at
	// zero. A zero-capacity channel would wedge the worker at "no slots" rather
	// than limiting it, so clamp instead.
	tests := []struct {
		maxConcurrent int
		want          int
	}{
		{maxConcurrent: 0, want: 1},
		{maxConcurrent: -1, want: 1},
		{maxConcurrent: 1, want: 1},
		{maxConcurrent: 3, want: 3},
	}

	for _, tt := range tests {
		if got := slotCapacity(tt.maxConcurrent); got != tt.want {
			t.Errorf("slotCapacity(%d) = %d, want %d", tt.maxConcurrent, got, tt.want)
		}
	}
}
