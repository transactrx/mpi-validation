package cashgate

import (
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestGateRuleCountDelegatesToBackend(t *testing.T) {
	gate := testGate(&fakeBackend{rules: 42}, ModeLive)
	if got := gate.RuleCount(); got != 42 {
		t.Fatalf("RuleCount() = %d, want 42", got)
	}
}

// RuleCount is deliberately independent of the enabled flag so that flag can
// change or go away without silently zeroing a caller's cache health signal.
func TestGateRuleCountIgnoresEnabledFlag(t *testing.T) {
	gate := testGate(&fakeBackend{rules: 7}, ModeLive)
	gate.enabled = false
	if got := gate.RuleCount(); got != 7 {
		t.Fatalf("RuleCount() with enabled=false = %d, want 7", got)
	}
}

func TestDisabledGateRuleCountIsZero(t *testing.T) {
	if got := Disabled().RuleCount(); got != 0 {
		t.Fatalf("Disabled().RuleCount() = %d, want 0", got)
	}
	var nilGate *Gate
	if got := nilGate.RuleCount(); got != 0 {
		t.Fatalf("nil gate RuleCount() = %d, want 0", got)
	}
}

func TestLiveBackendRuleCountFollowsRefresh(t *testing.T) {
	cache := &fakeRuleCache{
		data: map[string][]ruleRecord{
			"100000": {{BIN: "100000"}, {BIN: "100000", PCN: "PCNX"}},
			"200000": {{BIN: "200000"}},
		},
		refreshData: map[string][]ruleRecord{
			"100000": {{BIN: "100000"}},
		},
	}
	backend := &liveBackend{cache: cache}

	if got := backend.ruleCount(); got != 3 {
		t.Fatalf("ruleCount() before refresh = %d, want 3", got)
	}
	if err := backend.forceRefresh(); err != nil {
		t.Fatalf("forceRefresh: %v", err)
	}
	if got := backend.ruleCount(); got != 1 {
		t.Fatalf("ruleCount() after refresh = %d, want 1", got)
	}
}

func TestSnapshotGateRuleCountFollowsReload(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	mock.ExpectQuery("SELECT DISTINCT").
		WillReturnRows(sqlmock.NewRows(snapshotColumns).
			AddRow("ignored", "100000", "", "", "Cash Plan", PayerTypeCash, nil).
			AddRow("ignored", "100000", "PCNX", "", "Cash Plan", PayerTypeCash, int64(3)).
			AddRow("ignored", "200000", "", "", "Commercial Plan", int64(3), nil))
	mock.ExpectQuery("SELECT DISTINCT").
		WillReturnRows(sqlmock.NewRows(snapshotColumns).
			AddRow("ignored", "100000", "", "", "Cash Plan", PayerTypeCash, nil))

	gate, err := New(db, Config{
		Mode:     ModeSnapshot,
		Database: "CPE_PROD",
		Schema:   "DATA_DICTIONARY",
	})
	if err != nil {
		t.Fatalf("New snapshot Gate: %v", err)
	}

	if got := gate.RuleCount(); got != 3 {
		t.Fatalf("RuleCount() after load = %d, want 3", got)
	}
	if err := gate.ForceRefresh(); err != nil {
		t.Fatalf("ForceRefresh: %v", err)
	}
	if got := gate.RuleCount(); got != 1 {
		t.Fatalf("RuleCount() after refresh = %d, want 1", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}
