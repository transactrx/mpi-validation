package mpivalidation

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const dobLayout = "20060102"
const maxAgeDOB = 130

// StripNonAlphanumeric matches the MPI's CleanupField behavior: removes all
// non-alphanumeric characters so our validation mirrors what the MPI does
// after running recordLinkage.CleanData on the inbound request.
var nonAlphanumericRe = regexp.MustCompile("[^a-zA-Z0-9]+")

func StripNonAlphanumeric(s string) string {
	return nonAlphanumericRe.ReplaceAllString(s, "")
}

// ValidateMPIRequest checks the fields that the MPI service requires for getUniquePatientId.
// This prevents sending requests over NATS that will be rejected with 400.
func ValidateMPIRequest(patient *InboundPatientIdRequest) error {
	if StripNonAlphanumeric(patient.FirstName) == "" ||
		StripNonAlphanumeric(patient.LastName) == "" ||
		StripNonAlphanumeric(patient.DOB) == "" ||
		StripNonAlphanumeric(patient.Gender) == "" {
		return fmt.Errorf("missing required demographics: firstName, lastName, dob, and gender are all required")
	}

	if err := ValidateDOB(patient.DOB); err != nil {
		return fmt.Errorf("invalid dob %q: %w", patient.DOB, err)
	}

	if err := ValidateGender(patient.Gender); err != nil {
		return err
	}

	if !HasSufficientDataForCreation(patient) {
		return fmt.Errorf("insufficient data: need at least one of phone, address (street/zip), insurance (bin+cardHolderId), or pharmacy (pharmacyNpi+rxPatientId)")
	}

	return nil
}

// ValidateInvalidateRequest checks the fields that the MPI service requires for invalidateInsurance.
func ValidateInvalidateRequest(request *InvalidateInsuranceRequest) error {
	if StripNonAlphanumeric(request.FirstName) == "" ||
		StripNonAlphanumeric(request.LastName) == "" ||
		StripNonAlphanumeric(request.DOB) == "" ||
		StripNonAlphanumeric(request.Gender) == "" {
		return fmt.Errorf("missing required demographics: firstName, lastName, dob, and gender are all required")
	}

	if err := ValidateDOB(request.DOB); err != nil {
		return fmt.Errorf("invalid dob %q: %w", request.DOB, err)
	}

	if err := ValidateGender(request.Gender); err != nil {
		return err
	}

	if StripNonAlphanumeric(request.Bin) == "" {
		return fmt.Errorf("bin is required")
	}

	if StripNonAlphanumeric(request.PCN) == "" {
		return fmt.Errorf("pcn is required")
	}

	if StripNonAlphanumeric(request.CardHolderId) == "" {
		return fmt.Errorf("cardHolderId is required")
	}

	return nil
}

// HasSufficientDataForCreation mirrors the MPI's HasSufficientDataForPatientCreation.
// The MPI runs CleanData (strip non-alphanumeric) then validateInboundPatient (which
// clears invalid phone/NPI/rxPatientId) before checking for sufficient data.
// If no existing patient is found, the MPI requires at least one identifying data group
// beyond demographics to create a new patient record.
func HasSufficientDataForCreation(patient *InboundPatientIdRequest) bool {
	phone := StripNonAlphanumeric(patient.Phone)
	if !IsValidUSPhoneNumber(phone) {
		phone = ""
	}

	street := StripNonAlphanumeric(patient.Street)
	zip := StripNonAlphanumeric(patient.Zip)

	bin := StripNonAlphanumeric(patient.Bin)
	cardHolderId := StripNonAlphanumeric(patient.CardHolderId)
	pcn := StripNonAlphanumeric(patient.PCN)

	pharmacyNpi := StripNonAlphanumeric(patient.PharmacyNpi)
	if !IsValidNPI(pharmacyNpi) {
		pharmacyNpi = ""
	}

	rxPatientId := StripNonAlphanumeric(patient.RxPatientId)
	if !IsValidRxPatientId(rxPatientId) {
		rxPatientId = ""
	}

	hasPhone := phone != ""
	hasAddress := street != "" || zip != ""
	// PCN is required so that insurance can be persisted: the storage key
	// (GetInPatientInsuranceKey in masterPatientIndex) requires bin+chid+pcn.
	// Accepting bin+chid alone here would let validation pass and then store
	// no insurance — the thin-patient bug.
	hasInsurance := bin != "" && cardHolderId != "" && pcn != ""
	hasPharmacy := pharmacyNpi != "" && rxPatientId != ""

	return hasPhone || hasAddress || hasInsurance || hasPharmacy
}

// IsValidUSPhoneNumber validates a US phone number format.
var usPhoneRe = regexp.MustCompile(`^(?:\([2-9]\d{2}\) ?|[2-9]\d{2}(?:-?|\.?| ?))[2-9]\d{2}[-. ]?\d{4}$`)

func IsValidUSPhoneNumber(phone string) bool {
	return phone != "" && usPhoneRe.MatchString(phone)
}

// IsValidNPI validates a National Provider Identifier using the Luhn algorithm.
func IsValidNPI(npi string) bool {
	if len(npi) != 10 && len(npi) != 15 {
		return false
	}
	if len(npi) == 10 {
		npi = "80840" + npi
	}
	return luhn(npi)
}

func luhn(s string) bool {
	length := len(s)
	oddOrEven := length & 1
	sum := 0
	for i := 0; i < length; i++ {
		digit, err := strconv.Atoi(string(s[i]))
		if err != nil {
			return false
		}
		if ((i & 1) ^ oddOrEven) == 0 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum != 0 && sum%10 == 0
}

// IsValidRxPatientId checks if the RxPatientId is a legitimate identifier (not a bogus placeholder).
var bogusRxPatientIds = map[string]bool{
	"0": true, "00": true, "000": true, "0000": true,
	"00000": true, "000000": true, "0000000": true,
	"00000000": true, "000000000": true, "0000000000": true,
	"1": true, "123": true, "1234": true, "12345": true,
	"123456": true, "1234567": true, "12345678": true, "123456789": true, "1234567890": true,
	"999999999": true, "9999999999": true,
	"UNKNOWN": true, "NONE": true, "NA": true, "N/A": true,
	"TEST": true, "TEMP": true, "NEW": true,
}

func IsValidRxPatientId(rxPatientId string) bool {
	if len(rxPatientId) == 0 {
		return false
	}
	upper := strings.ToUpper(strings.TrimSpace(rxPatientId))
	if len(upper) == 0 {
		return false
	}
	if bogusRxPatientIds[upper] {
		return false
	}
	if len(upper) >= 3 {
		allSame := true
		for i := 1; i < len(upper); i++ {
			if upper[i] != upper[0] {
				allSame = false
				break
			}
		}
		if allSame {
			return false
		}
	}
	return true
}

// ValidateDOB validates a date of birth string in YYYYMMDD format.
func ValidateDOB(dobStr string) error {
	dob, err := time.Parse(dobLayout, dobStr)
	if err != nil {
		return fmt.Errorf("must be YYYYMMDD format: %w", err)
	}

	if dob.After(time.Now()) {
		return fmt.Errorf("cannot be in the future")
	}

	cutoff := time.Now().AddDate(-maxAgeDOB, 0, 0)
	if dob.Before(cutoff) {
		return fmt.Errorf("over %d years of age are not allowed", maxAgeDOB)
	}

	return nil
}

// ValidateGender validates the gender code (0=Unknown, 1=Male, 2=Female, 3=Other).
func ValidateGender(gender string) error {
	g, err := strconv.Atoi(gender)
	if err != nil || g < 0 || g > 3 {
		return fmt.Errorf("invalid gender %q: must be 0 (Unknown), 1 (Male), 2 (Female), or 3 (Other)", gender)
	}
	return nil
}

// testPayorNameRe matches a "test" token at a word boundary, case-insensitive.
// The \b anchor means it fires on "Test", "TEST", "Testing" but NOT on a "test"
// substring inside a real word ("greatest", "latest", "contest") — so it won't
// false-positive on legitimate plan names. RULEDATA plan names are a controlled
// vocabulary and the test/QA payors follow this convention consistently
// (e.g. "QS/1 Test Claims", "PowerLine Test", "Express Scripts Test", "TEST PLAN").
var testPayorNameRe = regexp.MustCompile(`(?i)\btest`)

// IsTestPayorName reports whether a payor/plan name belongs to a test or QA payor
// whose transactions must never create MPI patients. Test payors (QS/1, PowerLine,
// Express Scripts Test, state-Medicaid test BINs, etc.) carry hardcoded sentinel DOBs
// and synthetic demographics; left ungated they mint junk patients in the production
// index. This is a rule, not a frozen BIN list, so newly-added test payors are caught
// automatically. The caller resolves a BIN/PCN/group to its plan name before calling.
func IsTestPayorName(planName string) bool {
	return testPayorNameRe.MatchString(planName)
}
