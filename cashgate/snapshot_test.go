package cashgate

import (
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

var snapshotColumns = []string{
	"key",
	"bin",
	"pcn",
	"group_id",
	"name",
	"bin_payer_type_id",
	"pcn_payer_type_id",
}

func TestSnapshotGateLoadsAndRefreshesAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	mock.ExpectQuery("SELECT DISTINCT").
		WillReturnRows(sqlmock.NewRows(snapshotColumns).
			AddRow("ignored", " 100000 ", "", "", "Cash Plan", PayerTypeCash, nil).
			AddRow("ignored", "200000", "", "", "Commercial Plan", int64(3), nil).
			AddRow("ignored", "200000", " pcnx ", "", "Commercial Plan", int64(3), PayerTypeNonAdjudicatedCash).
			AddRow("ignored", "300000", "", "", "PowerLine Test Claims", int64(3), nil))

	gate, err := New(db, Config{
		Mode:     ModeSnapshot,
		Database: "CPE_PROD",
		Schema:   "DATA_DICTIONARY",
	})
	if err != nil {
		t.Fatalf("New snapshot Gate: %v", err)
	}

	if gate.Mode() != ModeSnapshot || !gate.IsEnabled() {
		t.Fatalf("snapshot gate mode/enabled = %q/%v", gate.Mode(), gate.IsEnabled())
	}
	if !gate.IsCashProgram("100000", "", "") {
		t.Fatal("snapshot did not load BIN-level cash")
	}
	if !gate.IsCashProgram("200000", "PCNX", "") {
		t.Fatal("snapshot did not normalize/load PCN-level NonAdjudicatedCash")
	}
	if got := gate.Classify("300000", "", ""); got != ClassificationTest {
		t.Fatalf("test BIN classification = %q, want %q", got, ClassificationTest)
	}
	if got := gate.Classify("999999", "", ""); got != ClassificationUnknownBIN {
		t.Fatalf("registry miss = %q, want %q", got, ClassificationUnknownBIN)
	}

	mock.ExpectQuery("SELECT DISTINCT").
		WillReturnRows(sqlmock.NewRows(snapshotColumns).
			AddRow("ignored", "100000", "", "", "Now Commercial", int64(3), nil).
			AddRow("ignored", "400000", "", "", "New Test Plan", int64(3), nil))

	if err := gate.ForceRefresh(); err != nil {
		t.Fatalf("ForceRefresh snapshot: %v", err)
	}
	if got := gate.Classify("100000", "", ""); got != ClassificationKnownOther {
		t.Fatalf("refreshed classification = %q, want %q", got, ClassificationKnownOther)
	}
	if got := gate.Classify("200000", "", ""); got != ClassificationUnknownBIN {
		t.Fatalf("removed BIN classification = %q, want %q", got, ClassificationUnknownBIN)
	}
	if !gate.IsTestPayor("400000") {
		t.Fatal("ForceRefresh did not replace precomputed test-BIN set")
	}
	if gate.IsTestPayor("300000") {
		t.Fatal("ForceRefresh retained a stale test BIN")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet SQL expectations: %v", err)
	}
}

func TestSnapshotInitialLoadFailureIsConstructorError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	loadErr := errors.New("Snowflake unavailable")
	mock.ExpectQuery("SELECT DISTINCT").WillReturnError(loadErr)

	_, err = New(db, Config{
		Mode:     ModeSnapshot,
		Database: "CPE_PROD",
		Schema:   "DATA_DICTIONARY",
	})
	if !errors.Is(err, loadErr) {
		t.Fatalf("New snapshot error = %v, want wrapped %v", err, loadErr)
	}
}

func TestSnapshotScanFailureDoesNotReplaceLastGoodRules(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

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

	mock.ExpectQuery("SELECT DISTINCT").
		WillReturnRows(sqlmock.NewRows(snapshotColumns).
			AddRow("ignored", "200000", "", "", "Broken", "not-an-integer", nil))

	if err := gate.ForceRefresh(); err == nil {
		t.Fatal("ForceRefresh must return a scan error")
	}
	if !gate.IsCashProgram("100000", "", "") {
		t.Fatal("failed refresh replaced the last good snapshot")
	}
	if got := gate.Classify("200000", "", ""); got != ClassificationUnknownBIN {
		t.Fatalf("partially scanned BIN leaked into cache: got %q", got)
	}
}

func TestSnapshotRowsIterationFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	rowErr := errors.New("stream interrupted")
	rows := sqlmock.NewRows(snapshotColumns).
		AddRow("ignored", "100000", "", "", "Cash", PayerTypeCash, nil).
		RowError(0, rowErr)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT")).WillReturnRows(rows)

	_, err = New(db, Config{
		Mode:     ModeSnapshot,
		Database: "CPE_PROD",
		Schema:   "DATA_DICTIONARY",
	})
	if err == nil || !strings.Contains(err.Error(), rowErr.Error()) {
		t.Fatalf("row iteration error = %v, want containing %q", err, rowErr)
	}
}

func TestSnapshotConcurrentReadsAndReplacement(t *testing.T) {
	cash := int64Pointer(PayerTypeCash)
	ordinary := int64Pointer(3)
	backend := &snapshotBackend{}
	backend.replace(
		map[string]binRules{
			"100000": compileBINRules([]ruleRecord{{BINPayerTypeID: cash}}),
		},
	)
	gate := testGate(backend, ModeSnapshot)

	// The only valid correlated states are:
	//   snapshot A: cash payer, not test -> cash
	//   snapshot B: ordinary payer, test -> test
	//
	// A known-other result would prove that Classify combined snapshot B's
	// payer data with snapshot A's test status.
	var waitGroup sync.WaitGroup
	for reader := 0; reader < 8; reader++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for iteration := 0; iteration < 1_000; iteration++ {
				classification := gate.Classify("100000", "", "")
				if classification != ClassificationCash &&
					classification != ClassificationTest {
					t.Errorf("concurrent classification = %q", classification)
					return
				}
			}
		}()
	}

	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		for iteration := 0; iteration < 1_000; iteration++ {
			if iteration%2 == 0 {
				backend.replace(
					map[string]binRules{
						"100000": compileBINRules([]ruleRecord{{BINPayerTypeID: cash}}),
					},
				)
				continue
			}
			backend.replace(
				map[string]binRules{
					"100000": compileBINRules([]ruleRecord{{
						BINPayerTypeID: ordinary,
						Name:           stringPointer("Test Commercial Plan"),
					}}),
				},
			)
		}
	}()

	waitGroup.Wait()
}
