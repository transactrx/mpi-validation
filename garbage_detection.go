package mpivalidation

import (
	"regexp"
	"strings"
)

// petNameRegex matches pet/animal species suffixes on firstName (case-insensitive).
// These are appended by veterinary systems to identify animal patients.
// Examples: "chicocanine", "simbafeline", "benjidog", "buddyk9", "birdyequine"
//
// Includes truncated suffixes from field-length limits:
//   - cani (canine->cani), fel (feline->fel), equin (equine->equin)
//
// Also matches exotic/small animal species suffixes that are unambiguously non-human.
//
// Excluded from primary regex (handled as ambiguous with extra guards):
//   - "fe$" -- matches "Josephe" etc.
//   - "cat$" -- matches "Catherine" truncated
//   - "can$" -- matches "Duncan"
var petNameRegex = regexp.MustCompile(
	`(?i)(canine|canin|cani|feline|feli|fel|dog|k9|equine|equin|pup|pet|kitten|` +
		`bunny|rabbit|ferret|hamster|guinea|turtle|parrot|gecko|iguana|horse|goat|pig)$` +
		`|(?i)^(k9|dog)\s`)

// ambiguousPetRegex matches suffixes that are only safe when combined with
// additional signals (DOB = Jan 1st AND no address AND no phone).
//   - "fe$" -- truncated feline, but matches "Josephe"
//   - "cat$" -- feline suffix, but matches "Catherine" truncated
//   - "can$" -- truncated canine, but matches "Duncan"
var ambiguousPetRegex = regexp.MustCompile(`(?i)(fe|cat|can)$`)

// ClassifyGarbage checks if a patient record is identifiable garbage (pet, system junk,
// or institutional account). Returns the classification or empty string if legitimate.
//
// Parameters are the raw field values (before any cleaning). This is important because
// StripNonAlphanumeric removes spaces, and the prefix pattern "^(k9|dog)\s" needs the space.
//
// Return values:
//   - "pet" -- matched primary pet name regex (unambiguous animal species)
//   - "system_statsafe" -- pharmaceutical data artifact
//   - "institutional_housestock" -- house stock institutional account
//   - "ambiguous_pet" -- matched ambiguous suffix with corroborating signals
//   - "" -- legitimate patient, no match
func ClassifyGarbage(firstName, lastName, dob, street, zip, phone string) string {
	// 1. Primary regex -- unambiguous animal species suffixes/prefixes
	if petNameRegex.MatchString(firstName) {
		return "pet"
	}

	lower := strings.ToLower(strings.TrimSpace(firstName))
	lowerLast := strings.ToLower(strings.TrimSpace(lastName))

	// 2. System artifacts
	if lower == "statsafe" {
		return "system_statsafe"
	}

	// 3. Institutional accounts
	if lower == "house" && lowerLast == "stock" {
		return "institutional_housestock"
	}

	// 4. Ambiguous suffixes -- only with strong signals (Jan 1st DOB, no address, no phone)
	if ambiguousPetRegex.MatchString(firstName) {
		if isJanFirst(dob) && street == "" && zip == "" && phone == "" {
			return "ambiguous_pet"
		}
	}

	return ""
}

// isJanFirst returns true if the DOB string is in format YYYYMMDD and the month/day is 0101.
func isJanFirst(dob string) bool {
	if len(dob) < 8 {
		return false
	}
	return dob[4:8] == "0101"
}
