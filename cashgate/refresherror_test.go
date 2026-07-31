package cashgate

import (
	"errors"
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

// The backend holds a single handler, so the Gate must fan out rather than let
// one registration silently replace the other.
func TestOnRefreshErrorAndPersistentFailureCoexist(t *testing.T) {
	for _, order := range []string{"error-first", "halt-first"} {
		t.Run(order, func(t *testing.T) {
			backend := &fakeBackend{}
			gate := testGate(backend, ModeLive)

			errorCalls := 0
			haltCalls := 0
			register := map[string]func(){
				"error-first": func() {
					gate.OnRefreshError(func(error, int) { errorCalls++ })
					gate.OnPersistentRefreshFailure(func(error) { haltCalls++ })
				},
				"halt-first": func() {
					gate.OnPersistentRefreshFailure(func(error) { haltCalls++ })
					gate.OnRefreshError(func(error, int) { errorCalls++ })
				},
			}
			register[order]()

			failure := errors.New("refresh failed")
			for attempt := 1; attempt <= 4; attempt++ {
				backend.refreshHandler(failure, attempt)
			}

			if errorCalls != 4 {
				t.Fatalf("OnRefreshError calls = %d, want 4", errorCalls)
			}
			// Threshold is 3, and the halt action fires at most once.
			if haltCalls != 1 {
				t.Fatalf("halt calls = %d, want 1", haltCalls)
			}
		})
	}
}

func TestOnRefreshErrorBelowThresholdDoesNotHalt(t *testing.T) {
	backend := &fakeBackend{}
	gate := testGate(backend, ModeLive)

	errorCalls := 0
	haltCalls := 0
	gate.OnRefreshError(func(error, int) { errorCalls++ })
	gate.OnPersistentRefreshFailure(func(error) { haltCalls++ })

	backend.refreshHandler(errors.New("transient"), 1)
	backend.refreshHandler(errors.New("transient"), 2)

	if errorCalls != 2 {
		t.Fatalf("OnRefreshError calls = %d, want 2", errorCalls)
	}
	if haltCalls != 0 {
		t.Fatalf("halt calls = %d, want 0 below the threshold", haltCalls)
	}
}

func TestOnRefreshErrorNilClearsOnlyWhenNoHandlersRemain(t *testing.T) {
	backend := &fakeBackend{}
	gate := testGate(backend, ModeLive)

	gate.OnRefreshError(func(error, int) {})
	gate.OnPersistentRefreshFailure(func(error) {})

	// Clearing one handler must leave the other wired up.
	gate.OnRefreshError(nil)
	if backend.refreshHandler == nil {
		t.Fatal("clearing OnRefreshError unregistered the surviving halt handler")
	}

	gate.OnPersistentRefreshFailure(nil)
	if backend.refreshHandler != nil {
		t.Fatal("backend handler was not cleared once no Gate handlers remained")
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
