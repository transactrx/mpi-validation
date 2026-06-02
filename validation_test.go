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

func TestIsValidNPI(t *testing.T) {
	tests := []struct {
		npi  string
		want bool
	}{
		{"1234567893", true},  // valid 10-digit NPI
		{"1234567890", false}, // invalid Luhn
		{"123456789", false},  // too short
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
			name:    "has zip only",
			patient: InboundPatientIdRequest{Zip: "10001"},
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
