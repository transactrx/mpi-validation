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
// Excluded from this regex (handled by the corroborated ambiguous tier below):
//   - "fel$" -- matches real names RAFEL, MARIFEL (Filipino), SURAFEL (Ethiopian),
//     CHRISTOFFEL, STOFFEL (Dutch/Afrikaans)
//   - "pet$" -- matches real Armenian names KARAPET, HAYRAPET, YEGISAPET
//   - "pup$", "pig$", "goat$", "horse$", "guinea$" -- demoted with the above after prod
//     measurement (2026-06, paid claims) showed these suffixes flag ~850-1500 real
//     claims per 6 weeks
var petSuffixRegex = regexp.MustCompile(
	`(?i)(canine|canin|cani|feline|feli|dog|k9|equine|equin|puppy|kitten|` +
		`bunny|rabbit|ferret|hamster|turtle|parrot|gecko|iguana)$`)

// petPrefixRegex matches a leading species token followed by a space (e.g. "k9 buddy",
// "dog rex"). Matched against the raw (TrimSpace'd) firstName because StripNonAlphanumeric
// removes the space this pattern depends on.
var petPrefixRegex = regexp.MustCompile(`(?i)^(k9|dog)\s`)

// ambiguousPetSuffixRegex matches species suffixes that are also productive endings of
// real human names (see petSuffixRegex comment for the measured prod examples). These
// reject only when BOTH guards hold: no contact data (street/zip/phone) AND the name is
// not in humanPetLikeNames. Demoted from the unconditional tier 2026-06 after prod
// measurement showed they were dropping real paid claims.
//
// Known residual FP risk: a real patient with one of these name endings who is NOT in
// the allowlist and has no contact data (e.g. an LTC resident) is still rejected.
// Extend humanPetLikeNames as prod measurement surfaces more real names.
var ambiguousPetSuffixRegex = regexp.MustCompile(`(?i)(fel|pet|pup|pig|goat|horse|guinea)$`)

// humanPetLikeNames are real human first names (cleaned, lowercased) that end in an
// ambiguous species suffix. Every entry was measured in production PAID claims
// (2026-06, 6-week window) -- these are real patients, never garbage.
//   - Armenian: Karapet, Hayrapet, Yegisapet
//   - Filipino: Rafel, Marifel
//   - Ethiopian: Surafel
//   - Dutch/Afrikaans: Christoffel, Stoffel
var humanPetLikeNames = map[string]bool{
	"karapet": true, "hayrapet": true, "yegisapet": true,
	"rafel": true, "marifel": true, "surafel": true,
	"christoffel": true, "stoffel": true,
}

// NOTE: the former (fe|cat|can)$ tier is GONE. Its corroborator was "DOB = Jan 1 AND no
// contact data", but Jan-1 is the defaulted-DOB signature of REAL long-term-care
// patients (who also legitimately lack street/zip/phone -- they live in a facility), so
// the corroborator selected exactly the population it was meant to protect. Without it,
// fe/cat/can are hopeless false-positive generators (Josephe, Aoife, Duncan, and "Fe"
// itself is a real Filipino name). Pets named *fe/*cat/*can are left to the cleanup
// classifier, which can use richer corroboration than an ingest gate.

// petLastNames are species tokens that are garbage only when they are the ENTIRE
// (cleaned) lastName. Veterinary systems sometimes drop the species into the surname
// field ("Rex"/"Dog", "Mittens"/"Cat"). Matched by EXACT equality, never as a suffix:
// no human surname is exactly "dog"/"cat"/"k9", whereas the species *suffix* regex would
// falsely flag real surnames (Apfel, Whitehorse, Roanhorse) -- which is why the firstName
// pet regex is deliberately NOT applied to lastName. Only full, unambiguous animal nouns
// are listed (no truncations like "fel"/"cani", which could be real short surnames).
var petLastNames = map[string]bool{
	"dog": true, "cat": true, "k9": true, "canine": true, "feline": true,
	"puppy": true, "kitten": true, "bunny": true, "ferret": true, "hamster": true,
	"parrot": true, "gecko": true, "iguana": true,
}

// facilityLastNames are institutional/non-person words that identify a facility account
// rather than a patient when they are the ENTIRE (cleaned) lastName ("PCH"/"Pharmacy",
// "Symbii Utah"/"Hospice"). Matched by EXACT equality. Deliberately EXCLUDES words that
// are also real surnames -- home (real surname Home), center, health, stock (Stock), and
// card (Card) -- those need a first-name co-signal and are left to the cleanup classifier.
var facilityLastNames = map[string]bool{
	"clinic": true, "pharmacy": true, "hospice": true, "facility": true,
	"snf": true, "ltc": true, "rx": true, "healthcare": true, "infusion": true,
}

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
//   - "pet" -- unambiguous animal species (firstName suffix/prefix, or whole lastName)
//   - "claim_blob" -- a raw NCPDP segment leaked into a name field (length / control chars)
//   - "junk_placeholder" -- exact placeholder token (test, unknown, newborn, ...)
//   - "system_statsafe" -- pharmaceutical data artifact
//   - "institutional_housestock" -- house stock institutional account
//   - "institutional_facility" -- facility account (clinic/pharmacy/hospice/... as whole lastName)
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
	// Species token as the WHOLE lastName (exact match only -- see petLastNames). This is
	// the safe complement to the rule below: exact equality cannot hit a real surname,
	// whereas suffix matching against lastName would.
	if petLastNames[strings.ToLower(lastNameClean)] {
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

	// NOTE: digit-run in a name is NOT a garbage signal and is deliberately NOT gated.
	// Investigation (2026-06-02, prod) proved the "synthetic_digits" class was
	// overwhelmingly REAL long-term-care patients: LTC pharmacies append the insurance
	// member ID into the name field (e.g. "PADILLA (16669)" -> "padilla16669"), and LTC
	// residents legitimately have no street/zip/phone (they live in a facility). The old
	// "digit-run AND no contact" rule therefore rejected real Medicare-D LTC patients.
	// There is no reliable standalone signal for the genuinely-synthetic remainder, so
	// the gate is dropped. hasContact is still computed below for the ambiguous-token rule.
	hasContact := strings.TrimSpace(street) != "" || strings.TrimSpace(zip) != "" ||
		strings.TrimSpace(phone) != ""

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
	// Facility account -- an unambiguous non-person word as the whole lastName.
	if facilityLastNames[strings.ToLower(lastNameClean)] {
		return "institutional_facility"
	}

	// 7. Ambiguous species suffixes -- corroborated tier. Reject only when no contact
	// data corroborates AND the name is not a known real human name. Matched against
	// the normalized form for the same reason as the primary suffix. Deliberately does
	// NOT use DOB as a signal: Jan-1 DOBs are the defaulted-DOB population of real LTC
	// patients (see the NOTE on the regex above).
	if ambiguousPetSuffixRegex.MatchString(firstNameClean) &&
		!humanPetLikeNames[cleanedLowerFirst] && !hasContact {
		return "ambiguous_pet"
	}

	return ""
}
