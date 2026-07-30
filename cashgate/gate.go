package cashgate

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Mode controls how a Gate keeps its in-memory RULEDATA rules current.
type Mode string

const (
	// ModeLive loads rules synchronously at startup and uses snowflake-cache to
	// refresh them in the background.
	ModeLive Mode = "live"

	// ModeSnapshot loads one ruleset synchronously at startup. It performs no
	// background work; ForceRefresh is the only way to replace the snapshot.
	ModeSnapshot Mode = "snapshot"

	// ModeDisabled identifies a Gate returned by Disabled. New rejects this
	// mode so disabling can never happen accidentally through configuration.
	ModeDisabled Mode = "disabled"
)

// Config locates RULEDATA and selects the gate's lifecycle.
//
// Database and Schema are required and must be unquoted Snowflake identifiers.
// RefreshInterval applies only to ModeLive; zero selects
// DefaultRefreshInterval. ModeSnapshot rejects a non-zero interval so a caller
// cannot mistakenly believe that a batch snapshot refreshes automatically.
type Config struct {
	Mode            Mode
	Database        string
	Schema          string
	RefreshInterval time.Duration
}

// Classification is the library's answer for one BIN/PCN/group lookup.
type Classification string

const (
	// ClassificationCash means the resolved payer type is Cash or
	// NonAdjudicatedCash.
	ClassificationCash Classification = "cash"

	// ClassificationTest means the BIN's RULEDATA plan name identifies a test
	// or QA payer.
	ClassificationTest Classification = "test"

	// ClassificationKnownOther means RULEDATA knows the resolved payer and it
	// is neither cash-like nor a test payer.
	ClassificationKnownOther Classification = "known-other"

	// ClassificationUnknownBIN means a non-empty BIN has no BIN-level RULEDATA
	// entry.
	ClassificationUnknownBIN Classification = "unknown-bin"

	// ClassificationNoBIN distinguishes missing input from a registry miss.
	ClassificationNoBIN Classification = "no-bin"

	// ClassificationDisabled is returned only by a Gate created with Disabled.
	ClassificationDisabled Classification = "disabled"
)

// String returns the stable metric/log value for a classification.
func (c Classification) String() string {
	return string(c)
}

const (
	// DefaultRefreshInterval is used by ModeLive when Config.RefreshInterval is
	// zero.
	DefaultRefreshInterval = 2 * time.Hour

	// PersistentRefreshFailureThreshold is the number of consecutive Live-mode
	// refresh failures tolerated while serving the last good ruleset.
	PersistentRefreshFailureThreshold = 3
)

const (
	// PayerTypeCash is RULEDATA's base Cash payer type.
	PayerTypeCash int64 = 1

	// PayerTypeNonAdjudicatedCash is RULEDATA's cash-like,
	// non-adjudicated payer type.
	PayerTypeNonAdjudicatedCash int64 = 1465449159205023744
)

// Gate classifies BIN/PCN/group values against an in-memory ruleset.
type Gate struct {
	backend  gateBackend
	mode     Mode
	enabled  bool
	haltOnce sync.Once
}

// New constructs a fail-closed Gate and synchronously loads its initial
// RULEDATA ruleset.
func New(db *sql.DB, cfg Config) (*Gate, error) {
	if db == nil {
		return nil, fmt.Errorf("cashgate: database connection is required; use Disabled for an intentional opt-out")
	}

	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	var backend gateBackend
	switch normalized.Mode {
	case ModeLive:
		backend, err = newLiveBackend(db, normalized)
	case ModeSnapshot:
		backend, err = newSnapshotBackend(db, normalized)
	default:
		// normalizeConfig catches this. Keep the branch defensive in case a
		// future mode is added without a constructor implementation.
		err = fmt.Errorf("cashgate: unsupported mode %q", normalized.Mode)
	}
	if err != nil {
		return nil, err
	}

	return &Gate{
		backend: backend,
		mode:    normalized.Mode,
		enabled: true,
	}, nil
}

// Disabled returns the only intentionally gate-less Gate. Its boolean checks
// return false, Classify returns ClassificationDisabled, and ForceRefresh is a
// no-op.
func Disabled() *Gate {
	return &Gate{mode: ModeDisabled}
}

// Mode reports the gate's configured lifecycle mode.
func (g *Gate) Mode() Mode {
	if g == nil {
		return ModeDisabled
	}
	return g.mode
}

// IsEnabled reports whether the gate has a loaded RULEDATA backend.
func (g *Gate) IsEnabled() bool {
	return g != nil && g.enabled
}

// Classify resolves an insurance identifier using the same cascade as the
// existing services:
//
//	BIN.PCN.GROUP -> BIN.PCN -> BIN
//
// The backend returns every rule and the test-payer signal for the BIN from one
// immutable cache view. A refresh therefore cannot mix payer data from one
// ruleset with test status from another.
//
// A present PCN/group rule is authoritative, including when it says "not
// cash"; the lookup does not fall through to a broader cash BIN in that case.
// Cash takes precedence over test when both signals are present because
// existing callers check cash before test.
func (g *Gate) Classify(bin, pcn, group string) Classification {
	if !g.IsEnabled() {
		return ClassificationDisabled
	}

	bin = normalizeKeyPart(bin)
	pcn = normalizeKeyPart(pcn)
	group = normalizeKeyPart(group)
	if bin == "" {
		return ClassificationNoBIN
	}

	rules := g.backend.rulesForBIN(bin)
	if !rules.known {
		return ClassificationUnknownBIN
	}

	if pcn != "" && group != "" {
		if cash, found := rules.pcnCash[pcnRuleKey{pcn: pcn, group: group}]; found {
			return classifyKnownRule(cash, rules.isTest)
		}
	}

	if pcn != "" {
		if cash, found := rules.pcnCash[pcnRuleKey{pcn: pcn}]; found {
			return classifyKnownRule(cash, rules.isTest)
		}
	}

	return classifyKnownRule(rules.binFound && rules.binCash, rules.isTest)
}

func classifyKnownRule(cash, test bool) Classification {
	if cash {
		return ClassificationCash
	}
	if test {
		return ClassificationTest
	}
	return ClassificationKnownOther
}

// IsCashProgram reports whether Classify resolves to ClassificationCash.
func (g *Gate) IsCashProgram(bin, pcn, group string) bool {
	return g.Classify(bin, pcn, group) == ClassificationCash
}

// IsTestPayor reports whether the BIN's plan name is a test/QA name. This
// independent predicate remains true even if Classify gives a cash signal
// precedence for the same BIN.
func (g *Gate) IsTestPayor(bin string) bool {
	if !g.IsEnabled() {
		return false
	}
	bin = normalizeKeyPart(bin)
	return bin != "" && g.backend.rulesForBIN(bin).isTest
}

// ForceRefresh synchronously replaces the current ruleset. In ModeSnapshot it
// is the only refresh mechanism. In ModeLive it delegates to snowflake-cache.
// It is a no-op for Disabled.
func (g *Gate) ForceRefresh() error {
	if !g.IsEnabled() {
		return nil
	}
	if err := g.backend.forceRefresh(); err != nil {
		return fmt.Errorf("cashgate: force refresh: %w", err)
	}
	return nil
}

// OnPersistentRefreshFailure registers the Live-mode halt action. The Gate
// invokes it once when the consecutive failure count reaches
// PersistentRefreshFailureThreshold. Snapshot and Disabled gates have no
// background refresh and therefore ignore this registration.
func (g *Gate) OnPersistentRefreshFailure(onHalt func(error)) {
	if !g.IsEnabled() || g.mode != ModeLive {
		return
	}
	if onHalt == nil {
		g.backend.onRefreshError(nil)
		return
	}
	g.backend.onRefreshError(func(err error, consecutiveFailures int) {
		if consecutiveFailures >= PersistentRefreshFailureThreshold {
			g.haltOnce.Do(func() {
				onHalt(err)
			})
		}
	})
}

func normalizeConfig(cfg Config) (Config, error) {
	cfg.Database = strings.ToUpper(strings.TrimSpace(cfg.Database))
	cfg.Schema = strings.ToUpper(strings.TrimSpace(cfg.Schema))

	switch cfg.Mode {
	case ModeLive:
		if cfg.RefreshInterval < 0 {
			return Config{}, fmt.Errorf("cashgate: refresh interval must not be negative")
		}
		if cfg.RefreshInterval == 0 {
			cfg.RefreshInterval = DefaultRefreshInterval
		}
	case ModeSnapshot:
		if cfg.RefreshInterval != 0 {
			return Config{}, fmt.Errorf("cashgate: snapshot mode does not refresh automatically; refresh interval must be zero")
		}
	case ModeDisabled:
		return Config{}, fmt.Errorf("cashgate: disabled mode is not valid for New; use Disabled")
	default:
		return Config{}, fmt.Errorf("cashgate: mode must be %q or %q, got %q", ModeLive, ModeSnapshot, cfg.Mode)
	}

	if err := validateSnowflakeIdentifier("database", cfg.Database); err != nil {
		return Config{}, err
	}
	if err := validateSnowflakeIdentifier("schema", cfg.Schema); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func normalizeKeyPart(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func isCashPayerType(id *int64) bool {
	if id == nil {
		return false
	}
	return *id == PayerTypeCash || *id == PayerTypeNonAdjudicatedCash
}
