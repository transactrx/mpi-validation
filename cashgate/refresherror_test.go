package cashgate

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestOnRefreshErrorReportsEveryFailure(t *testing.T) {
	backend := &fakeBackend{}
	gate := testGate(backend, ModeLive)

	var gotErrs []error
	var gotCounts []int
	gate.OnRefreshError(func(err error, consecutiveFailures int) {
		gotErrs = append(gotErrs, err)
		gotCounts = append(gotCounts, consecutiveFailures)
	})
	if backend.refreshHandler == nil {
		t.Fatal("OnRefreshError did not register with the backend")
	}

	first := errors.New("snowflake unreachable")
	second := errors.New("still unreachable")
	backend.refreshHandler(first, 1)
	backend.refreshHandler(second, 2)

	if len(gotErrs) != 2 {
		t.Fatalf("handler invocations = %d, want 2", len(gotErrs))
	}
	if !errors.Is(gotErrs[0], first) || !errors.Is(gotErrs[1], second) {
		t.Fatalf("handler errors = %v, want [%v %v]", gotErrs, first, second)
	}
	if gotCounts[0] != 1 || gotCounts[1] != 2 {
		t.Fatalf("consecutive failure counts = %v, want [1 2]", gotCounts)
	}
}

// The rule cache holds a single refresh-error handler, so the two registration
// methods are mutually exclusive. The conflicting registration panics rather than
// silently orphaning a handler the caller believes is armed — a startup panic is
// cheaper to diagnose than a callback that never fires in production.
func TestConflictingRegistrationPanics(t *testing.T) {
	register := map[string][2]func(*Gate){
		"halt after error": {
			func(g *Gate) { g.OnRefreshError(func(error, int) {}) },
			func(g *Gate) { g.OnPersistentRefreshFailure(func(error) {}) },
		},
		"error after halt": {
			func(g *Gate) { g.OnPersistentRefreshFailure(func(error) {}) },
			func(g *Gate) { g.OnRefreshError(func(error, int) {}) },
		},
	}

	for name, calls := range register {
		t.Run(name, func(t *testing.T) {
			gate := testGate(&fakeBackend{}, ModeLive)
			calls[0](gate)

			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatal("conflicting registration did not panic")
				}
				msg, ok := recovered.(string)
				if !ok {
					t.Fatalf("panic value = %#v, want a string", recovered)
				}
				// The message has to tell the caller it is an either/or choice.
				for _, want := range []string{
					"OnRefreshError",
					"OnPersistentRefreshFailure",
					"mutually exclusive",
					"either one or the other, not both",
				} {
					if !strings.Contains(msg, want) {
						t.Errorf("panic message is missing %q; got: %s", want, msg)
					}
				}
			}()

			calls[1](gate)
		})
	}
}

// Replacing your own handler is not a conflict — only the other kind is.
func TestReRegisteringSameKindDoesNotPanic(t *testing.T) {
	backend := &fakeBackend{}
	gate := testGate(backend, ModeLive)

	second := 0
	gate.OnRefreshError(func(error, int) { t.Error("replaced handler fired") })
	gate.OnRefreshError(func(error, int) { second++ })
	backend.refreshHandler(errors.New("boom"), 1)
	if second != 1 {
		t.Fatalf("replacement handler calls = %d, want 1", second)
	}

	haltGate := testGate(&fakeBackend{}, ModeLive)
	haltGate.OnPersistentRefreshFailure(func(error) {})
	haltGate.OnPersistentRefreshFailure(func(error) {})
}

// Clearing releases the slot, so a caller can switch which callback is armed.
func TestClearingReleasesTheHandlerSlot(t *testing.T) {
	backend := &fakeBackend{}
	gate := testGate(backend, ModeLive)

	gate.OnRefreshError(func(error, int) {})
	gate.OnRefreshError(nil)

	haltCalls := 0
	gate.OnPersistentRefreshFailure(func(error) { haltCalls++ })
	for attempt := 1; attempt <= 3; attempt++ {
		backend.refreshHandler(errors.New("boom"), attempt)
	}
	if haltCalls != 1 {
		t.Fatalf("halt calls after switching registrations = %d, want 1", haltCalls)
	}

	// And back the other way.
	gate.OnPersistentRefreshFailure(nil)
	errorCalls := 0
	gate.OnRefreshError(func(error, int) { errorCalls++ })
	backend.refreshHandler(errors.New("boom"), 1)
	if errorCalls != 1 {
		t.Fatalf("error handler calls after switching back = %d, want 1", errorCalls)
	}
}

// The documented way to get both behaviors: consecutiveFailures carries
// everything a caller needs to implement the halt action themselves.
func TestOnRefreshErrorCanImplementHaltItself(t *testing.T) {
	backend := &fakeBackend{}
	gate := testGate(backend, ModeLive)

	errorCalls := 0
	haltCalls := 0
	var haltOnce sync.Once
	gate.OnRefreshError(func(err error, consecutiveFailures int) {
		errorCalls++
		if consecutiveFailures >= PersistentRefreshFailureThreshold {
			haltOnce.Do(func() { haltCalls++ })
		}
	})

	failure := errors.New("refresh failed")
	for attempt := 1; attempt <= 4; attempt++ {
		backend.refreshHandler(failure, attempt)
	}

	if errorCalls != 4 {
		t.Fatalf("OnRefreshError calls = %d, want 4", errorCalls)
	}
	if haltCalls != 1 {
		t.Fatalf("caller-implemented halt calls = %d, want 1", haltCalls)
	}
}

func TestOnRefreshErrorBelowThresholdDoesNotHalt(t *testing.T) {
	backend := &fakeBackend{}
	gate := testGate(backend, ModeLive)

	haltCalls := 0
	gate.OnPersistentRefreshFailure(func(error) { haltCalls++ })

	backend.refreshHandler(errors.New("transient"), 1)
	backend.refreshHandler(errors.New("transient"), 2)

	if haltCalls != 0 {
		t.Fatalf("halt calls = %d, want 0 below the threshold", haltCalls)
	}
}

func TestOnRefreshErrorNilClearsRegistration(t *testing.T) {
	backend := &fakeBackend{}
	gate := testGate(backend, ModeLive)

	gate.OnRefreshError(func(error, int) {})
	if backend.refreshHandler == nil {
		t.Fatal("OnRefreshError did not register with the backend")
	}

	gate.OnRefreshError(nil)
	if backend.refreshHandler != nil {
		t.Fatal("a nil handler did not clear the backend registration")
	}
}

func TestOnRefreshErrorIgnoredWithoutAutomaticRefresh(t *testing.T) {
	snapshot := &fakeBackend{}
	testGate(snapshot, ModeSnapshot).OnRefreshError(func(error, int) {})
	if snapshot.refreshHandler != nil {
		t.Fatal("snapshot gate registered a refresh-error handler")
	}

	// Must not panic.
	Disabled().OnRefreshError(func(error, int) {})
	var nilGate *Gate
	nilGate.OnRefreshError(func(error, int) {})
}
