package cashgate

import (
	"database/sql"
	"fmt"
	"regexp"
	"sync"

	mpivalidation "github.com/transactrx/mpi-validation"
	snowflakecache "github.com/transactrx/snowflake-cache/pkg/snowflake-cache"
)

const (
	tableRuleDataPlan    = "RULEDATA_PLAN"
	tableRuleDataPlanPCN = "RULEDATA_PLAN_PCN"
)

var snowflakeIdentifierRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_$]*$`)

type ruleRecord struct {
	Key            string  `db:"key"`
	BIN            string  `db:"bin"`
	PCN            string  `db:"pcn"`
	GroupID        string  `db:"group_id"`
	Name           *string `db:"name"`
	BINPayerTypeID *int64  `db:"bin_payer_type_id"`
	PCNPayerTypeID *int64  `db:"pcn_payer_type_id"`
}

type pcnRuleKey struct {
	pcn   string
	group string
}

// binRules is one immutable, pre-indexed view of every RULEDATA row for a BIN
// plus the test-payer signal derived from those same rows.
type binRules struct {
	known    bool
	binFound bool
	binCash  bool
	pcnCash  map[pcnRuleKey]bool
	isTest   bool
}

func compileBINRules(records []ruleRecord) binRules {
	if len(records) == 0 {
		return binRules{}
	}

	rules := binRules{
		known:   true,
		pcnCash: make(map[pcnRuleKey]bool),
	}
	for _, record := range records {
		if record.PCN == "" && record.GroupID == "" {
			rules.binFound = true
			if record.Name != nil && mpivalidation.IsTestPayorName(*record.Name) {
				rules.isTest = true
			}
			if isCashPayerType(record.BINPayerTypeID) {
				rules.binCash = true
			}
			continue
		}

		key := pcnRuleKey{pcn: record.PCN, group: record.GroupID}
		if isCashPayerType(record.PCNPayerTypeID) {
			rules.pcnCash[key] = true
		} else if _, found := rules.pcnCash[key]; !found {
			// Presence with a false value is an authoritative known-other
			// override. Do not lose it merely because it is not cash.
			rules.pcnCash[key] = false
		}
	}
	return rules
}

type gateBackend interface {
	rulesForBIN(bin string) binRules
	forceRefresh() error
	onRefreshError(handler func(error, int))
}

type liveBackend struct {
	cache    snowflakecache.DbCache[ruleRecord]
	compiled sync.Map
}

type cachedBINRules struct {
	firstRecord *ruleRecord
	recordCount int
	rules       binRules
}

func newLiveBackend(db *sql.DB, cfg Config) (*liveBackend, error) {
	cache, err := snowflakecache.CreateCache[ruleRecord](
		nil,
		payerTypeSQL(cfg.Database, cfg.Schema),
		monitoredTables(cfg.Database, cfg.Schema),
		"Key",
		cfg.RefreshInterval,
		db,
		cfg.Database+"."+cfg.Schema,
	)
	if err != nil {
		return nil, fmt.Errorf("cashgate: initialize live RULEDATA cache: %w", err)
	}
	return &liveBackend{cache: cache}, nil
}

func (b *liveBackend) rulesForBIN(bin string) binRules {
	bin = normalizeKeyPart(bin)
	records := b.cache.Get(bin)
	if len(records) == 0 {
		return binRules{}
	}

	firstRecord := &records[0]
	if cachedValue, found := b.compiled.Load(bin); found {
		cached := cachedValue.(cachedBINRules)
		if cached.firstRecord == firstRecord && cached.recordCount == len(records) {
			return cached.rules
		}
	}

	rules := compileBINRules(records)
	b.compiled.Store(bin, cachedBINRules{
		firstRecord: firstRecord,
		recordCount: len(records),
		rules:       rules,
	})
	return rules
}

func (b *liveBackend) forceRefresh() error {
	if err := b.cache.ForceRefresh(); err != nil {
		return err
	}
	b.compiled.Clear()
	return nil
}

func (b *liveBackend) onRefreshError(handler func(error, int)) {
	b.cache.OnRefreshError(handler)
}

type snapshotBackend struct {
	db    *sql.DB
	query string
	mu    sync.RWMutex
	byBIN map[string]binRules
}

func newSnapshotBackend(db *sql.DB, cfg Config) (*snapshotBackend, error) {
	backend := &snapshotBackend{
		db:    db,
		query: payerTypeSQL(cfg.Database, cfg.Schema),
	}
	if err := backend.reload(); err != nil {
		return nil, fmt.Errorf("cashgate: load RULEDATA snapshot: %w", err)
	}
	return backend, nil
}

func (b *snapshotBackend) rulesForBIN(bin string) binRules {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.byBIN[normalizeKeyPart(bin)]
}

func (b *snapshotBackend) forceRefresh() error {
	return b.reload()
}

func (b *snapshotBackend) onRefreshError(func(error, int)) {
	// Snapshot mode has no background refresh.
}

func (b *snapshotBackend) reload() error {
	rows, err := b.db.Query(b.query)
	if err != nil {
		return err
	}
	defer func() {
		_ = rows.Close()
	}()

	rawByBIN := make(map[string][]ruleRecord)
	for rows.Next() {
		var (
			record         ruleRecord
			name           sql.NullString
			binPayerTypeID sql.NullInt64
			pcnPayerTypeID sql.NullInt64
		)
		if err := rows.Scan(
			&record.Key,
			&record.BIN,
			&record.PCN,
			&record.GroupID,
			&name,
			&binPayerTypeID,
			&pcnPayerTypeID,
		); err != nil {
			return fmt.Errorf("scan RULEDATA row: %w", err)
		}

		record.BIN = normalizeKeyPart(record.BIN)
		record.PCN = normalizeKeyPart(record.PCN)
		record.GroupID = normalizeKeyPart(record.GroupID)
		record.Key = record.BIN
		if name.Valid {
			value := name.String
			record.Name = &value
		}
		if binPayerTypeID.Valid {
			value := binPayerTypeID.Int64
			record.BINPayerTypeID = &value
		}
		if pcnPayerTypeID.Valid {
			value := pcnPayerTypeID.Int64
			record.PCNPayerTypeID = &value
		}
		rawByBIN[record.BIN] = append(rawByBIN[record.BIN], record)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate RULEDATA rows: %w", err)
	}

	byBIN := make(map[string]binRules, len(rawByBIN))
	for bin, records := range rawByBIN {
		byBIN[bin] = compileBINRules(records)
	}
	b.replace(byBIN)
	return nil
}

func (b *snapshotBackend) replace(byBIN map[string]binRules) {
	b.mu.Lock()
	b.byBIN = byBIN
	b.mu.Unlock()
}

func payerTypeSQL(database, schema string) string {
	plan := qualifyTable(database, schema, tableRuleDataPlan)
	planPCN := qualifyTable(database, schema, tableRuleDataPlanPCN)

	return `
  SELECT DISTINCT
    UPPER(TRIM(rdp.BIN)) AS "key",
    UPPER(TRIM(rdp.BIN)) AS "bin",
    '' AS "pcn",
    '' AS "group_id",
    rdp.NAME AS "name",
    rdp.PAYER_TYPE_ID AS "bin_payer_type_id",
    NULL AS "pcn_payer_type_id"
  FROM ` + plan + ` rdp
  WHERE rdp.BIN IS NOT NULL
UNION ALL
  SELECT DISTINCT
    UPPER(TRIM(rdp.BIN)) AS "key",
    UPPER(TRIM(rdp.BIN)) AS "bin",
    UPPER(TRIM(IFNULL(rdpp.NUMBER, ''))) AS "pcn",
    UPPER(TRIM(IFNULL(rdpp.GROUP_ID, ''))) AS "group_id",
    rdp.NAME AS "name",
    rdp.PAYER_TYPE_ID AS "bin_payer_type_id",
    rdpp.PAYER_TYPE_ID AS "pcn_payer_type_id"
  FROM ` + plan + ` rdp
  INNER JOIN ` + planPCN + ` rdpp ON rdp.ID = rdpp.PLAN_ID
  WHERE rdp.BIN IS NOT NULL
`
}

func monitoredTables(database, schema string) []string {
	return []string{
		qualifyTable(database, schema, tableRuleDataPlan),
		qualifyTable(database, schema, tableRuleDataPlanPCN),
	}
}

func qualifyTable(database, schema, table string) string {
	return database + "." + schema + "." + table
}

func validateSnowflakeIdentifier(label, value string) error {
	if value == "" {
		return fmt.Errorf("cashgate: %s is required", label)
	}
	if !snowflakeIdentifierRE.MatchString(value) {
		return fmt.Errorf("cashgate: %s %q is not a valid unquoted Snowflake identifier", label, value)
	}
	return nil
}
