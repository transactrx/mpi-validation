package mpivalidation

import (
	"regexp"
	"strings"
)

// petSuffixRegex matches pet/animal species suffixes on firstName (case-insensitive).
// These are appended by veterinary systems to identify animal patients.
// Examples: "chicocanine", "simbafeline", "benjidog", "buddyk9", "birdyequine"
//
// Includes truncated suffixes from field-length limits:
//   - cani (canine->cani), fel (feline->fel), equin (equine->equin)
//
// Also matches exotic/small animal species suffixes that are unambiguously non-human.
//
// IMPORTANT: this is matched against StripNonAlphanumeric(firstName), NOT the raw value.
// The MPI runs CleanData (strips all non-alphanumerics) before storing, so "eros canine."
// and "eros (canine)" both become "eroscanine". Matching the raw value let trailing
// punctuation bypass the $-anchor and slip past the gate while the MPI happily stored
// the clean pet name -- the leak that put pets into the index.
//
// Excluded from this regex (handled as ambiguous with extra guards):
//   - "fe$" -- matches "Josephe" etc.
//   - "cat$" -- matches "Catherine" truncated
//   - "can$" -- matches "Duncan"
var petSuffixRegex = regexp.MustCompile(
	`(?i)(canine|canin|cani|feline|feli|fel|dog|k9|equine|equin|pup|pet|kitten|` +
		`bunny|rabbit|ferret|hamster|guinea|turtle|parrot|gecko|iguana|horse|goat|pig)$`)

// petPrefixRegex matches a leading species token followed by a space (e.g. "k9 buddy",
// "dog rex"). Matched against the raw (TrimSpace'd) firstName because StripNonAlphanumeric
// removes the space this pattern depends on.
var petPrefixRegex = regexp.MustCompile(`(?i)^(k9|dog)\s`)

// ambiguousPetRegex matches suffixes that are only safe when combined with
// additional signals (DOB = Jan 1st AND no address AND no phone).
//   - "fe$" -- truncated feline, but matches "Josephe"
//   - "cat$" -- feline suffix, but matches "Catherine" truncated
//   - "can$" -- truncated canine, but matches "Duncan"
var ambiguousPetRegex = regexp.MustCompile(`(?i)(fe|cat|can)$`)

// multiDigitRegex matches a run of 3+ consecutive digits. Real names never contain
// one; this run is the signature of synthetic/test data (Faker surname + random
// digits like "kunze718832"). Validated against 250k real human-signal patients:
// only 38/250k hit, and those were themselves garbage. NOTE: a single trailing digit
// is NOT flagged -- real dedup-suffixed names exist (dorothy1, maria1, frances27).
var multiDigitRegex = regexp.MustCompile(`[0-9]{3,}`)

// maxNameLen is the cleaned-name length above which a value is treated as a claim
// blob (a whole NCPDP segment dumped into the name field). Real firstName cleaned
// length is p99.9=12 in a 250k sample; only 4/250k exceeded 25. 40 is a safe ceiling.
const maxNameLen = 40

// junkFirstNames are exact placeholder/test tokens that are never a real first name.
// Deliberately EXCLUDES "baby" -- it is a real given name (Kerala-Christian/Filipino);
// firstName=baby records are adults with real surnames and contact data.
var junkFirstNames = map[string]bool{
	"test": true, "patient": true, "unknown": true,
	"noname": true, "sample": true, "none": true, "xxx": true,
	"donotuse": true, "zztest": true, "newborn": true,
}

// ambiguousJunkFirstNames are tokens that LOOK like placeholders but can also be
// real given names, so they need a corroborating signal before rejection.
//   - "na" -- the "N/A" placeholder, but also a real Korean/Vietnamese given name.
// These reject only when no contact data (phone/address) corroborates the suspicion.
var ambiguousJunkFirstNames = map[string]bool{
	"na": true,
}

// hasNCPDPControlChars reports whether s contains an NCPDP separator control
// character (FS/GS/RS/US, 0x1C-0x1F). Their presence means a raw NCPDP segment
// leaked into a demographic field -- a parse failure, never a real name.
func hasNCPDPControlChars(s string) bool {
	for _, r := range s {
		if r >= 0x1C && r <= 0x1F {
			return true
		}
	}
	return false
}

// ClassifyGarbage checks if a patient record is identifiable garbage (pet, system junk,
// or institutional account). Returns the classification or empty string if legitimate.
//
// Parameters are the raw field values (before any cleaning). This is important because
// StripNonAlphanumeric removes spaces, and the prefix pattern "^(k9|dog)\s" needs the space.
//
// Return values:
//   - "pet" -- matched primary pet name regex (unambiguous animal species)
//   - "claim_blob" -- a raw NCPDP segment leaked into a name field (length / control chars)
//   - "synthetic_digits" -- name has a 3+ digit run AND no contact data (synthetic/test)
//   - "junk_placeholder" -- exact placeholder token (test, unknown, newborn, ...)
//   - "system_statsafe" -- pharmaceutical data artifact
//   - "institutional_housestock" -- house stock institutional account
//   - "ambiguous_pet" -- matched ambiguous suffix with corroborating signals
//   - "" -- legitimate patient, no match
func ClassifyGarbage(firstName, lastName, dob, street, zip, phone string) string {
	// 1. Primary regex -- unambiguous animal species suffixes/prefixes.
	// Match the suffix against the MPI-normalized form (non-alphanumerics stripped),
	// since that is exactly what the MPI stores. The prefix needs the raw value
	// because StripNonAlphanumeric removes the space it depends on.
	firstNameClean := StripNonAlphanumeric(firstName)
	lastNameClean := StripNonAlphanumeric(lastName)
	if petSuffixRegex.MatchString(firstNameClean) || petPrefixRegex.MatchString(firstName) {
		return "pet"
	}
	// NOTE: deliberately NOT matching the species suffix against lastName. Validated
	// against 250k real human-signal patients: applying these suffixes (especially the
	// truncated -fel/-cani/-equin and -horse/-pig) to surnames falsely flags real
	// families -- Apfel, Stifel, Bacani, Pellicani, Whitehorse, Roanhorse, Alequin, etc.
	// Species-in-lastName pets are rare and are handled by the cleanup classifier, which
	// can use corroborating signals (placeholder DOB, missing contact data) instead of a
	// blunt gate that blocks real patients.

	// 2. Claim blob -- a whole NCPDP segment dumped into a name field (parse failure).
	if len(firstNameClean) > maxNameLen || len(lastNameClean) > maxNameLen ||
		hasNCPDPControlChars(firstName) || hasNCPDPControlChars(lastName) {
		return "claim_blob"
	}

	// 3. Synthetic/test data -- a run of 3+ digits in the name, with corroboration.
	// A 3+ digit run is a SUSPICIOUS name signal, not proof of garbage on its own:
	// REAL patients arrive with numeric suffixes polluting either field -- real given
	// names (joan104, jose134, robert308, james1212, michael00000) and real surnames
	// (padilla4096, han06251924) alike. Those records all carry real contact data.
	// The synthetic/test/claim-blob records we actually want to block come in
	// CONTACTLESS. So follow the holistic rule: a digit-run name is garbage only when
	// no contact signal (phone/address) corroborates it. With contact, treat it as a
	// real patient with a dirty field and let it through.
	hasContact := strings.TrimSpace(street) != "" || strings.TrimSpace(zip) != "" ||
		strings.TrimSpace(phone) != ""
	if !hasContact &&
		(multiDigitRegex.MatchString(firstNameClean) || multiDigitRegex.MatchString(lastNameClean)) {
		return "synthetic_digits"
	}

	lower := strings.ToLower(strings.TrimSpace(firstName))
	lowerLast := strings.ToLower(strings.TrimSpace(lastName))

	// 4. Junk placeholder firstNames (exact match on the cleaned, lowercased value).
	cleanedLowerFirst := strings.ToLower(firstNameClean)
	if junkFirstNames[cleanedLowerFirst] {
		return "junk_placeholder"
	}
	// Ambiguous placeholders that can also be real names -- reject only when no contact.
	if ambiguousJunkFirstNames[cleanedLowerFirst] && !hasContact {
		return "junk_placeholder"
	}

	// 5. System artifacts
	if lower == "statsafe" {
		return "system_statsafe"
	}

	// 6. Institutional accounts
	if lower == "house" && lowerLast == "stock" {
		return "institutional_housestock"
	}

	// 4. Ambiguous suffixes -- only with strong signals (Jan 1st DOB, no address, no phone).
	// Matched against the normalized form for the same reason as the primary suffix.
	if ambiguousPetRegex.MatchString(firstNameClean) {
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
