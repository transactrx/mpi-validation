package cashgate

import (
	"testing"

	snowflakecache "github.com/transactrx/snowflake-cache/pkg/snowflake-cache"
)

type fakeRuleCache struct {
	data           map[string][]ruleRecord
	refreshData    map[string][]ruleRecord
	refreshHandler func(error, int)
}

var _ snowflakecache.DbCache[ruleRecord] = (*fakeRuleCache)(nil)

func (c *fakeRuleCache) Get(key string) []ruleRecord {
	return c.data[key]
}

func (c *fakeRuleCache) GetAll() []ruleRecord {
	var records []ruleRecord
	for _, binRecords := range c.data {
		records = append(records, binRecords...)
	}
	return records
}

func (c *fakeRuleCache) ForceRefresh() error {
	if c.refreshData != nil {
		c.data = c.refreshData
	}
	return nil
}

func (c *fakeRuleCache) OnRefreshError(handler func(error, int)) {
	c.refreshHandler = handler
}

func TestLiveBackendMemoizesAndRecompilesBINRules(t *testing.T) {
	cache := &fakeRuleCache{
		data: map[string][]ruleRecord{
			"100000": {
				{BINPayerTypeID: int64Pointer(PayerTypeCash)},
			},
		},
	}
	backend := &liveBackend{cache: cache}

	first := backend.rulesForBIN("100000")
	if !first.binCash || first.isTest {
		t.Fatalf("initial compiled rules = %+v, want cash/non-test", first)
	}
	cachedValue, found := backend.compiled.Load("100000")
	if !found {
		t.Fatal("live backend did not memoize compiled BIN rules")
	}
	cached := cachedValue.(cachedBINRules)

	second := backend.rulesForBIN("100000")
	secondCachedValue, found := backend.compiled.Load("100000")
	if !found {
		t.Fatal("memoized BIN rules disappeared")
	}
	secondCached := secondCachedValue.(cachedBINRules)
	if cached.firstRecord != secondCached.firstRecord {
		t.Fatal("unchanged raw slice was unnecessarily recompiled")
	}
	if !second.binCash {
		t.Fatal("memoized cash result changed")
	}

	cache.data = map[string][]ruleRecord{
		"100000": {
			{
				BINPayerTypeID: int64Pointer(3),
				Name:           stringPointer("PowerLine Test Claims"),
			},
		},
	}
	refreshed := backend.rulesForBIN("100000")
	if refreshed.binCash || !refreshed.isTest {
		t.Fatalf("refreshed compiled rules = %+v, want ordinary/test", refreshed)
	}
	refreshedCachedValue, found := backend.compiled.Load("100000")
	if !found {
		t.Fatal("refreshed BIN rules were not memoized")
	}
	refreshedCached := refreshedCachedValue.(cachedBINRules)
	if refreshedCached.firstRecord == cached.firstRecord {
		t.Fatal("new raw slice reused stale compiled BIN rules")
	}
}

func TestLiveBackendForceRefreshClearsCompiledRules(t *testing.T) {
	cache := &fakeRuleCache{
		data: map[string][]ruleRecord{
			"100000": {
				{BINPayerTypeID: int64Pointer(PayerTypeCash)},
			},
		},
		refreshData: map[string][]ruleRecord{
			"100000": {
				{BINPayerTypeID: int64Pointer(3)},
			},
		},
	}
	backend := &liveBackend{cache: cache}
	_ = backend.rulesForBIN("100000")

	if err := backend.forceRefresh(); err != nil {
		t.Fatalf("forceRefresh: %v", err)
	}
	if _, found := backend.compiled.Load("100000"); found {
		t.Fatal("forceRefresh retained compiled rules from the old cache generation")
	}
	if got := backend.rulesForBIN("100000"); got.binCash {
		t.Fatal("post-refresh compile retained stale cash classification")
	}
}
