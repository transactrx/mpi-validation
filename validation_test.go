package mpivalidation

import (
	"testing"
	"time"
)

func TestStripNonAlphanumeric(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World!", "HelloWorld"},
		{"john-doe@email", "johndoeemail"},
		{"  spaces  ", "spaces"},
		{"abc123", "abc123"},
		{"", ""},
		{"!@#$%", ""},
	}
	for _, tt := range tests {
		got := StripNonAlphanumeric(tt.input)
		if got != tt.want {
			t.Errorf("StripNonAlphanumeric(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestValidateDOB(t *testing.T) {
	tests := []struct {
		name    string
		dob     string
		wantErr bool
	}{
		{"valid DOB", "19900115", false},
		{"valid recent DOB", "20200601", false},
		{"invalid format", "1990-01-15", true},
		{"too short", "199001", true},
		{"future date", time.Now().AddDate(0, 0, 1).Format("20060102"), true},
		{"too old", "18000101", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDOB(tt.dob)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDOB(%q) error = %v, wantErr %v", tt.dob, err, tt.wantErr)
			}
		})
	}
}

func TestValidateGender(t *testing.T) {
	tests := []struct {
		gender  string
		wantErr bool
	}{
		{"0", false},
		{"1", false},
		{"2", false},
		{"3", false},
		{"4", true},
		{"-1", true},
		{"M", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.gender, func(t *testing.T) {
			err := ValidateGender(tt.gender)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGender(%q) error = %v, wantErr %v", tt.gender, err, tt.wantErr)
			}
		})
	}
}

func TestIsTestPayorName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Real test/QA payor names from RULEDATA_PLAN — must be flagged.
		{"QS/1 Test Claims", true},
		{"PowerLine Test", true},
		{"PowerLine AWS Test Payor", true},
		{"Express Scripts Test", true},
		{"Prime Therapeutics Test", true},
		{"MassHealth - DR Testing", true},
		{"MedImpact Testing BIN", true},
		{"TEST PLAN", true},
		{"West Virginia Test BIN", true},
		{"RedSail Commercial E1 (Test)", true},
		{"Testing", true},
		{"powerline test claims", true},
		// Real production payor names that contain "test" as a substring — must NOT be flagged.
		{"Greatest Health Plan", false},
		{"Latest Choice Rx", false},
		{"Contest Pharmacy Benefits", false},
		{"Caremark", false},
		{"OptumRx", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTestPayorName(tt.name); got != tt.want {
				t.Errorf("IsTestPayorName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsValidUSPhoneNumber(t *testing.T) {
	tests := []struct {
		phone string
		want  bool
	}{
		{"2125551234", true},
		{"(212) 5551234", true},
		{"212-555-1234", true},
		{"212.555.1234", true},
		{"212 555 1234", true},
		{"1234567890", false}, // starts with 1
		{"0005551234", false}, // area code starts with 0
		{"", false},
		{"abc", false},
		{"123", false},
	}
	for _, tt := range tests {
		got := IsValidUSPhoneNumber(tt.phone)
		if got != tt.want {
			t.Errorf("IsValidUSPhoneNumber(%q) = %v, want %v", tt.phone, got, tt.want)
		}
	}
}

func TestIsValidUSZipCode(t *testing.T) {
	tests := []struct {
		zip  string
		want bool
	}{
		{"10001", true},
		{"33601", true},
		{"123456789", true},
		{"00000", false},
		{"99999", false},
		{"11111", false},
		{"55555", false},
		{"000001234", false},
		{"999991234", false},
		{"1234", false},
		{"123456", false},
		{"12345-6789", false},
		{"ABCDE", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsValidUSZipCode(tt.zip); got != tt.want {
			t.Errorf("IsValidUSZipCode(%q) = %v, want %v", tt.zip, got, tt.want)
		}
	}
}

func TestIsValidNPI(t *testing.T) {
	tests := []struct {
		npi  string
		want bool
	}{
		{"1234567893", true},   // valid 10-digit NPI
		{"1234567890", false},  // invalid Luhn
		{"123456789", false},   // too short
		{"12345678901", false}, // wrong length
		{"", false},
		{"abcdefghij", false},
	}
	for _, tt := range tests {
		got := IsValidNPI(tt.npi)
		if got != tt.want {
			t.Errorf("IsValidNPI(%q) = %v, want %v", tt.npi, got, tt.want)
		}
	}
}

func TestIsValidRxPatientId(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"valid", "ABC123", true},
		{"empty", "", false},
		{"bogus zero", "0", false},
		{"bogus zeros", "00000", false},
		{"bogus sequential", "123456789", false},
		{"bogus UNKNOWN", "UNKNOWN", false},
		{"bogus unknown lowercase", "unknown", false},
		{"bogus NONE", "NONE", false},
		{"bogus NA", "NA", false},
		{"bogus N/A", "N/A", false},
		{"bogus TEST", "TEST", false},
		{"bogus TEMP", "TEMP", false},
		{"bogus NEW", "NEW", false},
		{"all same char", "AAAA", false},
		{"all same digit", "999", false},
		{"two same ok", "AA", true}, // len < 3, all-same check skipped
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidRxPatientId(tt.id)
			if got != tt.want {
				t.Errorf("IsValidRxPatientId(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestHasSufficientDataForCreation(t *testing.T) {
	tests := []struct {
		name    string
		patient InboundPatientIdRequest
		want    bool
	}{
		{
			name:    "has valid phone",
			patient: InboundPatientIdRequest{Phone: "2125551234"},
			want:    true,
		},
		{
			name:    "has street",
			patient: InboundPatientIdRequest{Street: "123 Main St"},
			want:    true,
		},
		{
			name:    "N/A street placeholder only",
			patient: InboundPatientIdRequest{Street: "N/A"},
			want:    false,
		},
		{
			name:    "unknown street placeholder only",
			patient: InboundPatientIdRequest{Street: "unknown"},
			want:    false,
		},
		{
			name:    "not available street placeholder only",
			patient: InboundPatientIdRequest{Street: "Not Available"},
			want:    false,
		},
		{
			name:    "N/A street placeholder with number metadata",
			patient: InboundPatientIdRequest{Street: "N/A #1"},
			want:    false,
		},
		{
			name:    "N_A street placeholder with number metadata",
			patient: InboundPatientIdRequest{Street: "N_A #1"},
			want:    false,
		},
		{
			name:    "N@A street placeholder with apartment metadata",
			patient: InboundPatientIdRequest{Street: "N@A apt 1"},
			want:    false,
		},
		{
			name:    "not.available street placeholder with apartment metadata",
			patient: InboundPatientIdRequest{Street: "not.available apt 1"},
			want:    false,
		},
		{
			name:    "no-address street placeholder with unit metadata",
			patient: InboundPatientIdRequest{Street: "no-address unit 3"},
			want:    false,
		},
		{
			name:    "unknown street placeholder with apartment metadata",
			patient: InboundPatientIdRequest{Street: "unknown apt 2"},
			want:    false,
		},
		{
			name:    "no address street placeholder with unit metadata",
			patient: InboundPatientIdRequest{Street: "no address unit 3"},
			want:    false,
		},
		{
			name:    "unknown street placeholder with compact apartment metadata",
			patient: InboundPatientIdRequest{Street: "unknown apt2"},
			want:    false,
		},
		{
			name:    "none street placeholder with compact suite metadata",
			patient: InboundPatientIdRequest{Street: "none ste100"},
			want:    false,
		},
		{
			name:    "all-zero street placeholder only",
			patient: InboundPatientIdRequest{Street: "0000"},
			want:    false,
		},
		{
			name:    "placeholder token inside legitimate street",
			patient: InboundPatientIdRequest{Street: "1 Unknown Rd"},
			want:    true,
		},
		{
			name:    "placeholder-looking street name without unit-only suffix",
			patient: InboundPatientIdRequest{Street: "Unknown Rd"},
			want:    true,
		},
		{
			name:    "apartment prefix in legitimate street token",
			patient: InboundPatientIdRequest{Street: "Unknown Aptos"},
			want:    true,
		},
		{
			name:    "unit prefix in legitimate street token",
			patient: InboundPatientIdRequest{Street: "Unknown Unity"},
			want:    true,
		},
		{
			name:    "suite prefix in legitimate street token",
			patient: InboundPatientIdRequest{Street: "None Steuben"},
			want:    true,
		},
		{
			name:    "unit metadata followed by street words",
			patient: InboundPatientIdRequest{Street: "Unknown Unit Circle Rd"},
			want:    true,
		},
		{
			name:    "has zip only",
			patient: InboundPatientIdRequest{Zip: "10001"},
			want:    true,
		},
		{
			name:    "has formatted ZIP+4 only",
			patient: InboundPatientIdRequest{Zip: "12345-6789"},
			want:    true,
		},
		{
			name:    "has normalized ZIP+4 only",
			patient: InboundPatientIdRequest{Zip: "123456789"},
			want:    true,
		},
		{
			name:    "placeholder zero zip only",
			patient: InboundPatientIdRequest{Zip: "00000"},
			want:    false,
		},
		{
			name:    "placeholder repeated-digit zip only",
			patient: InboundPatientIdRequest{Zip: "55555"},
			want:    false,
		},
		{
			name:    "malformed zip only",
			patient: InboundPatientIdRequest{Zip: "1234"},
			want:    false,
		},
		{
			name:    "street remains sufficient with invalid zip",
			patient: InboundPatientIdRequest{Street: "123 Main St", Zip: "00000"},
			want:    true,
		},
		{
			name:    "has insurance (bin + cardHolderId + pcn)",
			patient: InboundPatientIdRequest{Bin: "004336", CardHolderId: "123456789", PCN: "RXPCN01"},
			want:    true,
		},
		{
			name:    "has pharmacy (valid NPI + valid rxPatientId)",
			patient: InboundPatientIdRequest{PharmacyNpi: "1234567893", RxPatientId: "PAT001"},
			want:    true,
		},
		{
			name:    "insurance missing cardHolderId",
			patient: InboundPatientIdRequest{Bin: "004336", PCN: "RXPCN01"},
			want:    false,
		},
		{
			name:    "insurance missing PCN (bin + cardHolderId only)",
			patient: InboundPatientIdRequest{Bin: "004336", CardHolderId: "123456789"},
			want:    false,
		},
		{
			name:    "insurance missing bin",
			patient: InboundPatientIdRequest{CardHolderId: "123456789", PCN: "RXPCN01"},
			want:    false,
		},
		{
			name:    "pharmacy missing rxPatientId",
			patient: InboundPatientIdRequest{PharmacyNpi: "1234567893"},
			want:    false,
		},
		{
			name:    "pharmacy with bogus rxPatientId",
			patient: InboundPatientIdRequest{PharmacyNpi: "1234567893", RxPatientId: "0"},
			want:    false,
		},
		{
			name:    "invalid phone only",
			patient: InboundPatientIdRequest{Phone: "1234567890"},
			want:    false,
		},
		{
			name:    "no data",
			patient: InboundPatientIdRequest{},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasSufficientDataForCreation(&tt.patient)
			if got != tt.want {
				t.Errorf("HasSufficientDataForCreation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlaceholderStreetPunctuationNormalization(t *testing.T) {
	for separator := byte(0); separator < 128; separator++ {
		if separator >= 'A' && separator <= 'Z' ||
			separator >= 'a' && separator <= 'z' ||
			separator >= '0' && separator <= '9' {
			continue
		}

		punctuation := string(separator)
		for _, street := range []string{
			"N" + punctuation + "A apt 1",
			"not" + punctuation + "available unit 2",
			"no" + punctuation + "address #3",
			"N/A A" + punctuation + "P" + punctuation + "T 1",
			"unknown U" + punctuation + "N" + punctuation + "I" + punctuation + "T 2",
		} {
			if got := normalizedValidStreet(street); got != "" {
				t.Errorf("normalizedValidStreet(%q) = %q, want empty", street, got)
			}
		}

		for base := range placeholderStreetBases {
			for split := 1; split < len(base); split++ {
				street := base[:split] + punctuation + base[split:] + " apt 1"
				if got := normalizedValidStreet(street); got != "" {
					t.Errorf("normalizedValidStreet(%q) = %q, want empty", street, got)
				}
			}
		}

		for designator := range streetUnitDesignators {
			for split := 1; split < len(designator); split++ {
				street := "unknown " +
					designator[:split] + punctuation + designator[split:] + " 1"
				if got := normalizedValidStreet(street); got != "" {
					t.Errorf("normalizedValidStreet(%q) = %q, want empty", street, got)
				}
			}
		}
	}

	for _, street := range []string{
		"N•A apt 1",
		"not—available unit 2",
		"no…address #3",
		"N/A A•P•T 1",
	} {
		if got := normalizedValidStreet(street); got != "" {
			t.Errorf("normalizedValidStreet(%q) = %q, want empty", street, got)
		}
	}
}

func TestPlaceholderStreetUnitDesignatorBoundaries(t *testing.T) {
	for _, street := range []string{
		"Unknown Aptos",
		"Unknown Aptos2",
		"Unknown Unity",
		"Unknown Unity7",
		"None Steuben",
		"None Steuben2",
		"Unknown Suiteable",
		"Unknown Unit Circle Rd",
		"Unknown A.P.T.O.S",
		"Unknown U.N.I.T.Y",
		"None S.T.E.U.B.E.N",
	} {
		t.Run(street, func(t *testing.T) {
			if got := normalizedValidStreet(street); got == "" {
				t.Errorf("normalizedValidStreet(%q) = empty, want legitimate street", street)
			}
		})
	}

	for _, street := range []string{
		"N/A apt B",
		"N/A apt #1",
		"N/A apt",
		"N/A apt unit 2",
		"N/A # #1",
		"N/A a.p.t 1",
		"N/A u.n.i.t #B",
		"unknown apt2",
		"unknown apartment3A",
		"unknown unit4",
		"none suite500",
		"none ste100",
	} {
		t.Run(street, func(t *testing.T) {
			if got := normalizedValidStreet(street); got != "" {
				t.Errorf("normalizedValidStreet(%q) = %q, want empty", street, got)
			}
		})
	}

	for designator := range streetUnitDesignators {
		for suffix := byte('A'); suffix <= 'Z'; suffix++ {
			street := "Unknown " + designator + string(suffix)
			if got := normalizedValidStreet(street); got == "" {
				t.Errorf("normalizedValidStreet(%q) = empty, want legitimate street", street)
			}
		}

		for suffix := byte('0'); suffix <= '9'; suffix++ {
			street := "Unknown " + designator + string(suffix)
			if got := normalizedValidStreet(street); got != "" {
				t.Errorf("normalizedValidStreet(%q) = %q, want empty", street, got)
			}
		}
	}
}

func TestValidateMPIRequest(t *testing.T) {
	validPatient := InboundPatientIdRequest{
		FirstName: "John",
		LastName:  "Doe",
		DOB:       "19900115",
		Gender:    "1",
		Phone:     "2125551234",
	}

	t.Run("valid request", func(t *testing.T) {
		p := validPatient
		if err := ValidateMPIRequest(&p); err != nil {
			t.Errorf("ValidateMPIRequest() unexpected error: %v", err)
		}
	})

	t.Run("missing firstName", func(t *testing.T) {
		p := validPatient
		p.FirstName = ""
		if err := ValidateMPIRequest(&p); err == nil {
			t.Error("ValidateMPIRequest() expected error for missing firstName")
		}
	})

	t.Run("missing lastName", func(t *testing.T) {
		p := validPatient
		p.LastName = ""
		if err := ValidateMPIRequest(&p); err == nil {
			t.Error("ValidateMPIRequest() expected error for missing lastName")
		}
	})

	t.Run("invalid DOB", func(t *testing.T) {
		p := validPatient
		p.DOB = "invalid"
		if err := ValidateMPIRequest(&p); err == nil {
			t.Error("ValidateMPIRequest() expected error for invalid DOB")
		}
	})

	t.Run("invalid gender", func(t *testing.T) {
		p := validPatient
		p.Gender = "5"
		if err := ValidateMPIRequest(&p); err == nil {
			t.Error("ValidateMPIRequest() expected error for invalid gender")
		}
	})

	t.Run("insufficient data", func(t *testing.T) {
		p := InboundPatientIdRequest{
			FirstName: "John",
			LastName:  "Doe",
			DOB:       "19900115",
			Gender:    "1",
		}
		if err := ValidateMPIRequest(&p); err == nil {
			t.Error("ValidateMPIRequest() expected error for insufficient data")
		}
	})

	t.Run("placeholder zip is insufficient", func(t *testing.T) {
		p := InboundPatientIdRequest{
			FirstName: "John",
			LastName:  "Doe",
			DOB:       "19900115",
			Gender:    "1",
			Zip:       "00000",
		}
		if err := ValidateMPIRequest(&p); err == nil {
			t.Error("ValidateMPIRequest() expected insufficient-data error for placeholder ZIP")
		}
	})
}

func TestValidateInvalidateRequest(t *testing.T) {
	valid := InvalidateInsuranceRequest{
		FirstName:    "John",
		LastName:     "Doe",
		DOB:          "19900115",
		Gender:       "1",
		Bin:          "004336",
		PCN:          "ADV",
		CardHolderId: "123456789",
	}

	t.Run("valid request", func(t *testing.T) {
		r := valid
		if err := ValidateInvalidateRequest(&r); err != nil {
			t.Errorf("ValidateInvalidateRequest() unexpected error: %v", err)
		}
	})

	t.Run("missing bin", func(t *testing.T) {
		r := valid
		r.Bin = ""
		if err := ValidateInvalidateRequest(&r); err == nil {
			t.Error("expected error for missing bin")
		}
	})

	t.Run("missing pcn", func(t *testing.T) {
		r := valid
		r.PCN = ""
		if err := ValidateInvalidateRequest(&r); err == nil {
			t.Error("expected error for missing pcn")
		}
	})

	t.Run("missing cardHolderId", func(t *testing.T) {
		r := valid
		r.CardHolderId = ""
		if err := ValidateInvalidateRequest(&r); err == nil {
			t.Error("expected error for missing cardHolderId")
		}
	})
}
