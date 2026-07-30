// Package cashgate classifies pharmacy insurance identifiers using Snowflake
// RULEDATA.
//
// A Gate owns payer-type policy and lookup behavior, but it deliberately does
// not decide what a caller should do with a classification. For example, a
// feeder may skip a cash-program record while the MPI service may remove only
// the insurance and continue matching the patient.
//
// Callers must explicitly choose a mode:
//   - ModeLive uses snowflake-cache and periodically refreshes the rules.
//   - ModeSnapshot loads one immutable ruleset at startup and refreshes only
//     when ForceRefresh is called.
//
// New is fail-closed: a nil database, invalid configuration, or failed initial
// rules load returns an error. Disabled is the only intentional gate-less
// constructor.
package cashgate
