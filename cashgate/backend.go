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

type gateBackend interface {
	get(key string) []ruleRecord
	isTestPayor(bin string) bool
	forceRefresh() error
	onRefreshError(handler func(error, int))
}

type liveBackend struct {
	cache snowflakecache.DbCache[ruleRecord]
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

func (b *liveBackend) get(key string) []ruleRecord {
	return b.cache.Get(key)
}

func (b *liveBackend) isTestPayor(bin string) bool {
	for _, record := range b.cache.Get(buildCacheKey(bin, "", "")) {
		if record.Name != nil && mpivalidation.IsTestPayorName(*record.Name) {
			return true
		}
	}
	return false
}

func (b *liveBackend) forceRefresh() error {
	return b.cache.ForceRefresh()
}

func (b *liveBackend) onRefreshError(handler func(error, int)) {
	b.cache.OnRefreshError(handler)
}

type snapshotBackend struct {
	db       *sql.DB
	query    string
	mu       sync.RWMutex
	byKey    map[string][]ruleRecord
	testBINs map[string]struct{}
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

func (b *snapshotBackend) get(key string) []ruleRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.byKey[key]
}

func (b *snapshotBackend) isTestPayor(bin string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.testBINs[normalizeKeyPart(bin)]
	return ok
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

	byKey := make(map[string][]ruleRecord)
	testBINs := make(map[string]struct{})
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
		record.Key = buildCacheKey(record.BIN, record.PCN, record.GroupID)
		if name.Valid {
			value := name.String
			record.Name = &value
			if mpivalidation.IsTestPayorName(value) {
				testBINs[record.BIN] = struct{}{}
			}
		}
		if binPayerTypeID.Valid {
			value := binPayerTypeID.Int64
			record.BINPayerTypeID = &value
		}
		if pcnPayerTypeID.Valid {
			value := pcnPayerTypeID.Int64
			record.PCNPayerTypeID = &value
		}
		byKey[record.Key] = append(byKey[record.Key], record)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate RULEDATA rows: %w", err)
	}

	b.replace(byKey, testBINs)
	return nil
}

func (b *snapshotBackend) replace(byKey map[string][]ruleRecord, testBINs map[string]struct{}) {
	b.mu.Lock()
	b.byKey = byKey
	b.testBINs = testBINs
	b.mu.Unlock()
}

func payerTypeSQL(database, schema string) string {
	plan := qualifyTable(database, schema, tableRuleDataPlan)
	planPCN := qualifyTable(database, schema, tableRuleDataPlanPCN)

	return `
  SELECT DISTINCT
    UPPER(TRIM(rdp.BIN)) || '..' AS "key",
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
    UPPER(TRIM(rdp.BIN)) || '.' ||
      UPPER(TRIM(IFNULL(rdpp.NUMBER, ''))) || '.' ||
      UPPER(TRIM(IFNULL(rdpp.GROUP_ID, ''))) AS "key",
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
