package cashgate

import (
	"fmt"
	"testing"
)

// BenchmarkLiveGateClassifyHighCardinalityBIN guards the live hot path against
// regressing to a scan of every PCN/group row for a shared BIN.
func BenchmarkLiveGateClassifyHighCardinalityBIN(b *testing.B) {
	const pcnRows = 10_000

	records := make([]ruleRecord, 0, pcnRows+1)
	records = append(records, ruleRecord{BINPayerTypeID: int64Pointer(3)})
	for index := 0; index < pcnRows; index++ {
		payerType := int64(3)
		if index == pcnRows-1 {
			payerType = PayerTypeNonAdjudicatedCash
		}
		records = append(records, ruleRecord{
			PCN:            fmt.Sprintf("PCN%05d", index),
			GroupID:        fmt.Sprintf("GROUP%05d", index),
			PCNPayerTypeID: int64Pointer(payerType),
		})
	}

	backend := &liveBackend{
		cache: &fakeRuleCache{
			data: map[string][]ruleRecord{
				"100000": records,
			},
		},
	}
	gate := testGate(backend, ModeLive)

	// Warm the per-generation compiled index before timing steady-state claim
	// classification.
	if got := gate.Classify("100000", "PCN09999", "GROUP09999"); got != ClassificationCash {
		b.Fatalf("warmup Classify = %q, want %q", got, ClassificationCash)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if got := gate.Classify("100000", "PCN09999", "GROUP09999"); got != ClassificationCash {
			b.Fatalf("Classify = %q, want %q", got, ClassificationCash)
		}
	}
}
