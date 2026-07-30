package cashgate

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeBackend struct {
	data           map[string][]ruleRecord
	testBINs       map[string]bool
	forceErr       error
	forceCalls     int
	refreshHandler func(error, int)
}

func (b *fakeBackend) get(key string) []ruleRecord {
	return b.data[key]
}

func (b *fakeBackend) isTestPayor(bin string) bool {
	return b.testBINs[normalizeKeyPart(bin)]
}

func (b *fakeBackend) forceRefresh() error {
	b.forceCalls++
	return b.forceErr
}

func (b *fakeBackend) onRefreshError(handler func(error, int)) {
	b.refreshHandler = handler
}

func int64Pointer(value int64) *int64 {
	return &value
}

func testGate(backend gateBackend, mode Mode) *Gate {
	return &Gate{
		backend: backend,
		mode:    mode,
		enabled: true,
	}
}

func TestGateClassify(t *testing.T) {
	backend := &fakeBackend{
		data: map[string][]ruleRecord{
			"100000..": {
				{BINPayerTypeID: int64Pointer(PayerTypeCash)},
			},
			"200000..": {
				{BINPayerTypeID: int64Pointer(3)},
			},
			"200000.PCNX.": {
				{PCNPayerTypeID: int64Pointer(PayerTypeNonAdjudicatedCash)},
			},
			"300000..": {
				{BINPayerTypeID: int64Pointer(PayerTypeCash)},
			},
			"300000.PCNY.GROUP1": {
				{PCNPayerTypeID: int64Pointer(3)},
			},
			"400000..": {
				{BINPayerTypeID: int64Pointer(PayerTypeNonAdjudicatedCash)},
			},
			"500000..": {
				{BINPayerTypeID: int64Pointer(3)},
			},
			"600000..": {
				{BINPayerTypeID: int64Pointer(PayerTypeCash)},
			},
		},
		testBINs: map[string]bool{
			"500000": true,
			"600000": true,
		},
	}
	gate := testGate(backend, ModeSnapshot)

	tests := []struct {
		name       string
		bin        string
		pcn        string
		group      string
		classified Classification
	}{
		{
			name:       "bin cash",
			bin:        "100000",
			classified: ClassificationCash,
		},
		{
			name:       "pcn non-adjudicated cash overrides commercial bin",
			bin:        "200000",
			pcn:        "pcnx",
			classified: ClassificationCash,
		},
		{
			name:       "specific known-other prevents fallback to broader cash bin",
			bin:        "300000",
			pcn:        "pcny",
			group:      "group1",
			classified: ClassificationKnownOther,
		},
		{
			name:       "non-adjudicated cash bin",
			bin:        " 400000 ",
			classified: ClassificationCash,
		},
		{
			name:       "test bin",
			bin:        "500000",
			classified: ClassificationTest,
		},
		{
			name:       "cash takes precedence over test",
			bin:        "600000",
			classified: ClassificationCash,
		},
		{
			name:       "known ordinary payer",
			bin:        "200000",
			classified: ClassificationKnownOther,
		},
		{
			name:       "unknown bin",
			bin:        "999999",
			classified: ClassificationUnknownBIN,
		},
		{
			name:       "missing bin is not a registry miss",
			classified: ClassificationNoBIN,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := gate.Classify(test.bin, test.pcn, test.group); got != test.classified {
				t.Fatalf("Classify(%q, %q, %q) = %q, want %q",
					test.bin, test.pcn, test.group, got, test.classified)
			}
		})
	}
}

func TestGateBooleanCompatibilityMethods(t *testing.T) {
	backend := &fakeBackend{
		data: map[string][]ruleRecord{
			"100000..": {
				{BINPayerTypeID: int64Pointer(PayerTypeCash)},
			},
			"200000..": {
				{BINPayerTypeID: int64Pointer(3)},
			},
		},
		testBINs: map[string]bool{
			"100000": true,
			"200000": true,
		},
	}
	gate := testGate(backend, ModeLive)

	if !gate.IsCashProgram("100000", "", "") {
		t.Fatal("cash BIN must be reported by IsCashProgram")
	}
	if !gate.IsTestPayor("100000") {
		t.Fatal("IsTestPayor must remain an independent predicate when cash has classification precedence")
	}
	if !gate.IsTestPayor(" 200000 ") {
		t.Fatal("test BIN lookup must normalize input")
	}
	if gate.IsTestPayor("") {
		t.Fatal("empty BIN must not be a test payer")
	}
}

func TestDisabledGate(t *testing.T) {
	gate := Disabled()
	if gate.IsEnabled() {
		t.Fatal("Disabled gate must not be enabled")
	}
	if gate.Mode() != ModeDisabled {
		t.Fatalf("Mode() = %q, want %q", gate.Mode(), ModeDisabled)
	}
	if got := gate.Classify("100000", "", ""); got != ClassificationDisabled {
		t.Fatalf("Classify on Disabled gate = %q, want %q", got, ClassificationDisabled)
	}
	if gate.IsCashProgram("100000", "", "") || gate.IsTestPayor("100000") {
		t.Fatal("Disabled gate boolean checks must return false")
	}
	if err := gate.ForceRefresh(); err != nil {
		t.Fatalf("Disabled ForceRefresh returned %v", err)
	}

	var nilGate *Gate
	if nilGate.IsEnabled() || nilGate.Mode() != ModeDisabled {
		t.Fatal("nil Gate must behave as disabled")
	}
}

func TestForceRefresh(t *testing.T) {
	expected := errors.New("refresh failed")
	backend := &fakeBackend{forceErr: expected}
	gate := testGate(backend, ModeSnapshot)

	err := gate.ForceRefresh()
	if !errors.Is(err, expected) {
		t.Fatalf("ForceRefresh error = %v, want wrapped %v", err, expected)
	}
	if backend.forceCalls != 1 {
		t.Fatalf("ForceRefresh calls = %d, want 1", backend.forceCalls)
	}
}

func TestOnPersistentRefreshFailure(t *testing.T) {
	backend := &fakeBackend{}
	gate := testGate(backend, ModeLive)

	var calls int
	var received error
	gate.OnPersistentRefreshFailure(func(err error) {
		calls++
		received = err
	})
	if backend.refreshHandler == nil {
		t.Fatal("Live gate did not register refresh handler")
	}

	refreshErr := errors.New("Snowflake unavailable")
	backend.refreshHandler(refreshErr, 1)
	backend.refreshHandler(refreshErr, 2)
	if calls != 0 {
		t.Fatalf("halt called %d times before threshold", calls)
	}
	backend.refreshHandler(refreshErr, PersistentRefreshFailureThreshold)
	backend.refreshHandler(refreshErr, PersistentRefreshFailureThreshold+1)
	if calls != 1 {
		t.Fatalf("halt called %d times, want exactly once", calls)
	}
	if !errors.Is(received, refreshErr) {
		t.Fatalf("halt error = %v, want %v", received, refreshErr)
	}

	snapshotBackend := &fakeBackend{}
	snapshot := testGate(snapshotBackend, ModeSnapshot)
	snapshot.OnPersistentRefreshFailure(func(error) {
		t.Fatal("Snapshot gate must not call a persistent-refresh handler")
	})
	if snapshotBackend.refreshHandler != nil {
		t.Fatal("Snapshot gate must not register a refresh handler")
	}
}

func TestNormalizeConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		wantMode    Mode
		wantRefresh time.Duration
		wantError   string
	}{
		{
			name: "live defaults refresh",
			config: Config{
				Mode:     ModeLive,
				Database: " cpe_prod ",
				Schema:   " data_dictionary ",
			},
			wantMode:    ModeLive,
			wantRefresh: DefaultRefreshInterval,
		},
		{
			name: "snapshot",
			config: Config{
				Mode:     ModeSnapshot,
				Database: "CPE_PROD",
				Schema:   "DATA_DICTIONARY",
			},
			wantMode: ModeSnapshot,
		},
		{
			name: "missing mode",
			config: Config{
				Database: "CPE_PROD",
				Schema:   "DATA_DICTIONARY",
			},
			wantError: "mode must be",
		},
		{
			name: "disabled is constructor error",
			config: Config{
				Mode:     ModeDisabled,
				Database: "CPE_PROD",
				Schema:   "DATA_DICTIONARY",
			},
			wantError: "use Disabled",
		},
		{
			name: "snapshot rejects interval",
			config: Config{
				Mode:            ModeSnapshot,
				Database:        "CPE_PROD",
				Schema:          "DATA_DICTIONARY",
				RefreshInterval: time.Hour,
			},
			wantError: "snapshot mode",
		},
		{
			name: "live rejects negative interval",
			config: Config{
				Mode:            ModeLive,
				Database:        "CPE_PROD",
				Schema:          "DATA_DICTIONARY",
				RefreshInterval: -time.Second,
			},
			wantError: "must not be negative",
		},
		{
			name: "database required",
			config: Config{
				Mode:   ModeSnapshot,
				Schema: "DATA_DICTIONARY",
			},
			wantError: "database is required",
		},
		{
			name: "schema required",
			config: Config{
				Mode:     ModeSnapshot,
				Database: "CPE_PROD",
			},
			wantError: "schema is required",
		},
		{
			name: "identifier rejected",
			config: Config{
				Mode:     ModeSnapshot,
				Database: "CPE-PROD",
				Schema:   "DATA_DICTIONARY",
			},
			wantError: "not a valid",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeConfig(test.config)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("normalizeConfig error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeConfig returned %v", err)
			}
			if got.Mode != test.wantMode {
				t.Fatalf("mode = %q, want %q", got.Mode, test.wantMode)
			}
			if got.RefreshInterval != test.wantRefresh {
				t.Fatalf("refresh interval = %v, want %v", got.RefreshInterval, test.wantRefresh)
			}
			if got.Database != "CPE_PROD" || got.Schema != "DATA_DICTIONARY" {
				t.Fatalf("location = %s.%s, want CPE_PROD.DATA_DICTIONARY", got.Database, got.Schema)
			}
		})
	}
}

func TestNewRequiresDatabaseConnection(t *testing.T) {
	_, err := New(nil, Config{
		Mode:     ModeSnapshot,
		Database: "CPE_PROD",
		Schema:   "DATA_DICTIONARY",
	})
	if err == nil || !strings.Contains(err.Error(), "use Disabled") {
		t.Fatalf("New(nil) error = %v, want explicit Disabled guidance", err)
	}
}

func TestPayerTypeSQLUsesQualifiedNormalizedRules(t *testing.T) {
	query := payerTypeSQL("CPE_PROD", "DATA_DICTIONARY")
	for _, required := range []string{
		"CPE_PROD.DATA_DICTIONARY.RULEDATA_PLAN",
		"CPE_PROD.DATA_DICTIONARY.RULEDATA_PLAN_PCN",
		"UPPER(TRIM(rdp.BIN))",
		"IFNULL(rdpp.NUMBER, '')",
		"IFNULL(rdpp.GROUP_ID, '')",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("payerTypeSQL missing %q", required)
		}
	}
}
