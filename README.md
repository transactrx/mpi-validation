# mpi-validation

Shared Go library for validating patient records **before** they reach the Master Patient Index (MPI).

Every service that feeds the MPI (`getUniquePatientId`, `invalidateInsurance`) applies the same
rules from here, so a record rejected by one feeder is rejected by all of them. The goal is to keep
junk out of the production index: 400-rejects that never should have been sent, veterinary
patients, NCPDP claim blobs dumped into a name field, facility accounts, and cash/test-payer
transactions.

```
go get github.com/transactrx/mpi-validation
```

Go 1.25.5+.

## Packages

| Package | Import | Purpose |
| --- | --- | --- |
| `mpivalidation` | `github.com/transactrx/mpi-validation` | Field validation, sufficiency rules, garbage classification |
| `cashgate` | `github.com/transactrx/mpi-validation/cashgate` | Snowflake RULEDATA-backed cash/test payer classification |

---

## `mpivalidation`

### Request validation

```go
import mpivalidation "github.com/transactrx/mpi-validation"

if err := mpivalidation.ValidateMPIRequest(&req); err != nil {
    // Don't send it — the MPI would reject it with a 400.
    return err
}
```

`ValidateMPIRequest` mirrors what the MPI itself does — `CleanData` (strip all non-alphanumerics)
then `validateInboundPatient` — so the local answer matches the remote one:

1. `firstName`, `lastName`, `dob`, `gender` must all be non-empty after cleaning.
2. `dob` must be `YYYYMMDD`, not in the future, not more than 130 years ago.
3. `gender` must be `0` (Unknown), `1` (Male), `2` (Female), or `3` (Other).
4. The record must carry **at least one identifying data group** beyond demographics
   (`HasSufficientDataForCreation`).

`ValidateInvalidateRequest` applies the same demographics rules and additionally requires
`bin`, `pcn`, and `cardHolderId`.

### Sufficiency for patient creation

`HasSufficientDataForCreation` requires **one** of:

- **Phone** — a valid US phone number
- **Address** — a usable street *or* a usable ZIP
- **Insurance** — `bin` + `cardHolderId` + **`pcn`**
- **Pharmacy** — a Luhn-valid `pharmacyNpi` + a non-placeholder `rxPatientId`

`pcn` is required for the insurance group on purpose. The MPI's insurance storage key needs
bin+chid+pcn; accepting bin+chid alone let validation pass and then store no insurance at all —
the "thin patient" bug.

Placeholders are normalized away before the check, so they do not count as identifying data:

- **Street** — `NA`, `NONE`, `NULL`, `UNKNOWN`, `UNAVAILABLE`, `NOT AVAILABLE`, `NOT APPLICABLE`,
  `NO ADDRESS`, all-zero strings, and any of those decorated *only* with unit metadata
  (`N/A APT 3`, `NONE #2`, `UNKNOWN STE B`). Punctuation is normalized to separators first so
  `N.A.` and `N/A` behave identically; `#` survives as a unit designator.
- **ZIP** — must be 5 or 9 digits, and a five-digit prefix of repeated digits (`00000`, `99999`)
  is treated as a placeholder.
- **Phone** — must match US phone format with valid area/exchange leading digits.

Exported helpers: `StripNonAlphanumeric`, `ValidateDOB`, `ValidateGender`, `IsValidUSPhoneNumber`,
`IsValidUSZipCode`, `IsValidNPI` (Luhn over the `80840` prefix), `IsValidRxPatientId`,
`IsTestPayorName`.

### Garbage classification

```go
class := mpivalidation.ClassifyGarbage(firstName, lastName, dob, street, zip, phone)
if class != "" {
    // Drop the record; log `class` as the reason.
}
```

Pass the **raw** field values — the prefix rules need the whitespace that cleaning would remove.
Returns `""` for a legitimate patient, otherwise one of:

| Classification | Trigger |
| --- | --- |
| `pet` | Unambiguous species suffix/prefix on firstName (`…canine`, `…k9`, `k9 buddy`), or a species token as the entire lastName |
| `claim_blob` | Cleaned name over 40 chars, or NCPDP control characters (0x1C–0x1F) in a name — a parse failure |
| `junk_placeholder` | Exact placeholder firstName (`test`, `unknown`, `newborn`, …); ambiguous ones (`na`, `office`, `mrs`) only when no contact data corroborates |
| `junk_pair` | Exact synthetic first+last pair (`discount/card`, `mickey/mouse`, `first/last`, `dummy/dummy`) |
| `junk_numeric_donor` | All-numeric firstName with lastName exactly `donor` |
| `system_statsafe` | `statsafe*` firstName |
| `system_ekit` | eKit system token in either name field |
| `institutional_housestock` | firstName `house` + lastName `stock` |
| `institutional_stock_account` | `stockaccoun*` in the lastName |
| `institutional_facility` | Unambiguous facility word as the whole lastName (`clinic`, `pharmacy`, `hospice`, `snf`, `ltc`, …) |
| `ambiguous_pet` | Ambiguous species suffix (`…fel`, `…pet`, `…pup`, `…pig`, `…goat`, `…horse`, `…guinea`) **and** no contact data **and** not a known real human name |

**The precision rules are load-bearing, not stylistic.** They come from production measurement and
the comments in `garbage_detection.go` explain each one. In particular:

- Species suffixes are matched against firstName only, **never** lastName — surname matching
  falsely flags real families (Apfel, Bacani, Whitehorse, Roanhorse). Species-in-lastName is
  handled by exact equality on full animal nouns instead.
- Ambiguous suffixes are demoted to the corroborated tier because they end real names —
  Karapet, Rafel, Surafel, Christoffel — and unconditionally rejecting them dropped
  ~850–1500 real paid claims per 6 weeks in prod.
- **Jan-1 DOB is never used as corroboration.** It is the defaulted-DOB signature of real
  long-term-care patients, who also legitimately have no street/zip/phone.
- A digit run in a name is **not** gated. LTC pharmacies append member IDs into the name field
  (`PADILLA (16669)`), so the old rule rejected real Medicare-D patients.

Before changing a rule here, read the comment above it and measure against production paid claims.

---

## `cashgate`

Classifies a pharmacy insurance identifier (BIN / PCN / group) against Snowflake `RULEDATA`, so
callers can keep cash-program and test-payer transactions out of the MPI.

The gate owns *policy*, not *action* — it tells you what an identifier is, and each caller decides
what to do. A feeder may skip the record entirely; the MPI service may drop only the insurance and
still match the patient.

```go
import "github.com/transactrx/mpi-validation/cashgate"

db, err := cashgate.NewSnowflakeConnection(cashgate.SnowflakeConfig{
    Account:    account,
    User:       user,
    PrivateKey: base64PEM, // base64-encoded PKCS8 or PKCS1 PEM
    Database:   "MY_DB",
    Schema:     "MY_SCHEMA",
    Warehouse:  warehouse, // optional if the user/role has a default
})
if err != nil {
    return err
}

gate, err := cashgate.New(db, cashgate.Config{
    Mode:            cashgate.ModeLive,
    Database:        "MY_DB",
    Schema:          "MY_SCHEMA",
    RefreshInterval: 2 * time.Hour, // zero selects DefaultRefreshInterval
})
if err != nil {
    return err
}

switch gate.Classify(bin, pcn, group) {
case cashgate.ClassificationCash:
    // cash program
case cashgate.ClassificationTest:
    // test/QA payer
}
```

### Modes

| Mode | Behavior |
| --- | --- |
| `ModeLive` | Loads rules synchronously at startup, then refreshes in the background via `snowflake-cache`. For long-running services. |
| `ModeSnapshot` | Loads one immutable ruleset at startup. No background work; `ForceRefresh` is the only way to replace it. For batch jobs. `RefreshInterval` must be zero. |
| `ModeDisabled` | Only reachable via `cashgate.Disabled()`. `New` rejects it, so a config typo can never silently disable the gate. |

`New` is **fail-closed**: a nil DB, an invalid identifier, or a failed initial load returns an
error rather than a permissive gate. `Disabled()` is the one intentional opt-out — its predicates
return false, `Classify` returns `ClassificationDisabled`, and `ForceRefresh` is a no-op.

### Classifications

`cash`, `test`, `known-other`, `unknown-bin`, `no-bin`, `disabled`.

Lookup cascade: `BIN.PCN.GROUP` → `BIN.PCN` → `BIN`. A present PCN/group rule is **authoritative**,
including when it says "not cash" — the lookup does not fall through to a broader cash BIN. Cash
takes precedence over test when both signals are present, because existing callers check cash
first. `IsTestPayor(bin)` is an independent predicate that stays true regardless.

All rules and the test-payer signal for a BIN come from one immutable cache view, so a refresh
cannot mix payer data from one ruleset with test status from another.

### Resilience

In Live mode, register a halt action for persistent refresh failure:

```go
gate.OnPersistentRefreshFailure(func(err error) {
    logger.Errorw("cashgate rules are stale; halting", "error", err)
    shutdown()
})
```

It fires **once**, when consecutive refresh failures reach `PersistentRefreshFailureThreshold` (3).
Until then the gate keeps serving the last good ruleset.

### Data source

Reads `RULEDATA_PLAN` and `RULEDATA_PLAN_PCN` from the configured database/schema. Cash payer types
are `PayerTypeCash` (1) and `PayerTypeNonAdjudicatedCash` (1465449159205023744). Test payers are
identified by plan **name** (`IsTestPayorName`: a `test` token at a word boundary), not a frozen BIN
list, so newly added test payers are caught automatically without a code change.

Database and schema must be valid unquoted Snowflake identifiers (`^[A-Z_][A-Z0-9_$]*$`); they are
validated before being interpolated into SQL.

---

## Development

```sh
go test ./...
go test -bench=. ./cashgate    # guards the live hot-path lookup against regressions
```

The test suites are the specification for the precision rules — `garbage_detection_test.go` and
`validation_test.go` encode the production-measured cases that each rule must and must not match.
Add a test case for any rule you touch.
