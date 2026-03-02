package mpivalidation

import "testing"

func TestClassifyGarbage_PetNames(t *testing.T) {
	tests := []struct {
		name      string
		firstName string
		lastName  string
		want      string
	}{
		// Unambiguous pet suffixes — must match
		{"canine suffix", "Chico Canine", "", "pet"},
		{"canine suffix no space", "ChicoCanine", "", "pet"},
		{"canin suffix", "BuddyCanin", "", "pet"},
		{"cani suffix", "MaxCani", "", "pet"},
		{"feline suffix", "SimbaFeline", "", "pet"},
		{"feli suffix", "WhiskersFeli", "", "pet"},
		{"fel suffix", "MittsFel", "", "pet"},
		{"dog suffix", "RexDog", "", "pet"},
		{"k9 suffix", "BuddyK9", "", "pet"},
		{"equine suffix", "BirdyEquine", "", "pet"},
		{"equin suffix", "DaisyEquin", "", "pet"},
		{"pup suffix", "MaxPup", "", "pet"},
		{"pet suffix", "BellasPet", "", "pet"},
		{"kitten suffix", "FuzzyKitten", "", "pet"},
		{"bunny suffix", "FluffyBunny", "", "pet"},
		{"rabbit suffix", "CottonRabbit", "", "pet"},
		{"ferret suffix", "SlinkyFerret", "", "pet"},
		{"hamster suffix", "PipsqueakHamster", "", "pet"},
		{"guinea suffix", "PopcornGuinea", "", "pet"},
		{"turtle suffix", "SlowpokesTurtle", "", "pet"},
		{"parrot suffix", "CrackerParrot", "", "pet"},
		{"gecko suffix", "SpottyGecko", "", "pet"},
		{"iguana suffix", "SpikeyIguana", "", "pet"},
		{"horse suffix", "ThunderHorse", "", "pet"},
		{"goat suffix", "BillyGoat", "", "pet"},
		{"pig suffix", "WilburPig", "", "pet"},

		// Prefix patterns
		{"k9 prefix", "K9 Buddy", "", "pet"},
		{"dog prefix", "Dog Rex", "", "pet"},

		// Case insensitive
		{"case insensitive canine", "chicCANINE", "", "pet"},
		{"case insensitive feline", "simbaFELINE", "", "pet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyGarbage(tt.firstName, tt.lastName, "19900101", "", "", "")
			if got != tt.want {
				t.Errorf("ClassifyGarbage(%q, %q) = %q, want %q", tt.firstName, tt.lastName, got, tt.want)
			}
		})
	}
}

func TestClassifyGarbage_MustNotMatch(t *testing.T) {
	tests := []struct {
		name      string
		firstName string
		lastName  string
	}{
		// Real names that must NOT be flagged
		{"Catherine", "Catherine", "Smith"},
		{"Duncan", "Duncan", "Jones"},
		{"Felix", "Felix", "Garcia"},
		{"Peter", "Peter", "Johnson"},
		{"Josephe", "Josephe", "Martin"},
		{"Abel", "Abel", "Rodriguez"},
		{"Rachel", "Rachel", "Williams"},
		{"Michael", "Michael", "Brown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Give them full address data so ambiguous check won't fire
			got := ClassifyGarbage(tt.firstName, tt.lastName, "19900115", "123 Main St", "10001", "2125551234")
			if got != "" {
				t.Errorf("ClassifyGarbage(%q, %q) = %q, want empty (legitimate name)", tt.firstName, tt.lastName, got)
			}
		})
	}
}

func TestClassifyGarbage_CarpetEndingInPet(t *testing.T) {
	// "Carpet" ends in "pet" so it WILL match the primary regex.
	// This is acceptable — "Carpet" is not a real first name. The regex was validated
	// against 47M real patient records during the Phase 5c cleanup.
	got := ClassifyGarbage("Carpet", "", "19900115", "123 Main St", "10001", "2125551234")
	if got != "pet" {
		t.Errorf("ClassifyGarbage(\"Carpet\") = %q, want \"pet\" (ends in pet suffix)", got)
	}
}

func TestClassifyGarbage_SystemArtifacts(t *testing.T) {
	tests := []struct {
		name      string
		firstName string
		lastName  string
		want      string
	}{
		{"statsafe lowercase", "statsafe", "", "system_statsafe"},
		{"statsafe mixed case", "StAtSaFe", "", "system_statsafe"},
		{"statsafe uppercase", "STATSAFE", "", "system_statsafe"},
		{"housestock", "House", "Stock", "institutional_housestock"},
		{"housestock mixed case", "HOUSE", "STOCK", "institutional_housestock"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyGarbage(tt.firstName, tt.lastName, "19900101", "", "", "")
			if got != tt.want {
				t.Errorf("ClassifyGarbage(%q, %q) = %q, want %q", tt.firstName, tt.lastName, got, tt.want)
			}
		})
	}
}

func TestClassifyGarbage_AmbiguousPet(t *testing.T) {
	// Ambiguous suffixes (fe, cat, can) only match with Jan 1st DOB + no address + no phone
	tests := []struct {
		name      string
		firstName string
		lastName  string
		dob       string
		street    string
		zip       string
		phone     string
		want      string
	}{
		{
			name:      "ambiguous cat suffix with signals",
			firstName: "SimbaCat",
			dob:       "20200101",
			want:      "ambiguous_pet",
		},
		{
			name:      "ambiguous fe suffix with signals",
			firstName: "MittsFe",
			dob:       "20150101",
			want:      "ambiguous_pet",
		},
		{
			name:      "ambiguous can suffix with signals",
			firstName: "BuddyCan",
			dob:       "20180101",
			want:      "ambiguous_pet",
		},
		{
			name:      "ambiguous but has address",
			firstName: "SimbaCat",
			dob:       "20200101",
			street:    "123 Main St",
			want:      "",
		},
		{
			name:      "ambiguous but has zip",
			firstName: "SimbaCat",
			dob:       "20200101",
			zip:       "10001",
			want:      "",
		},
		{
			name:      "ambiguous but has phone",
			firstName: "SimbaCat",
			dob:       "20200101",
			phone:     "2125551234",
			want:      "",
		},
		{
			name:      "ambiguous but non-Jan1 DOB",
			firstName: "SimbaCat",
			dob:       "20200215",
			want:      "",
		},
		{
			name:      "Duncan with address (not ambiguous)",
			firstName: "Duncan",
			dob:       "19850101",
			street:    "456 Oak Ave",
			zip:       "90210",
			want:      "",
		},
		{
			name:      "Josephe with phone (not ambiguous)",
			firstName: "Josephe",
			dob:       "19950101",
			phone:     "3105551234",
			want:      "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyGarbage(tt.firstName, tt.lastName, tt.dob, tt.street, tt.zip, tt.phone)
			if got != tt.want {
				t.Errorf("ClassifyGarbage(%q, %q, dob=%q, street=%q, zip=%q, phone=%q) = %q, want %q",
					tt.firstName, tt.lastName, tt.dob, tt.street, tt.zip, tt.phone, got, tt.want)
			}
		})
	}
}

func TestClassifyGarbage_AmbiguousRealNames_WithSignals(t *testing.T) {
	// Real names ending in ambiguous suffixes WITH all signals present
	// These WILL match as ambiguous_pet — this is by design.
	// In production, the combination of Jan 1st DOB + no address + no phone
	// on a name ending in "cat"/"can"/"fe" is overwhelmingly pet data.
	tests := []struct {
		name      string
		firstName string
	}{
		{"Duncan no data Jan1", "Duncan"},
		{"Aoife no data Jan1", "Aoife"}, // Irish name ending in "fe"
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyGarbage(tt.firstName, "", "20000101", "", "", "")
			if got != "ambiguous_pet" {
				t.Errorf("ClassifyGarbage(%q) with Jan1+no data = %q, want \"ambiguous_pet\"", tt.firstName, got)
			}
		})
	}
}

func TestIsJanFirst(t *testing.T) {
	tests := []struct {
		dob  string
		want bool
	}{
		{"20200101", true},
		{"19900101", true},
		{"20200215", false},
		{"20201231", false},
		{"short", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isJanFirst(tt.dob)
		if got != tt.want {
			t.Errorf("isJanFirst(%q) = %v, want %v", tt.dob, got, tt.want)
		}
	}
}

func TestClassifyGarbage_EmptyInputs(t *testing.T) {
	got := ClassifyGarbage("", "", "", "", "", "")
	if got != "" {
		t.Errorf("ClassifyGarbage with all empty = %q, want empty", got)
	}
}

func TestClassifyGarbage_LegitimatePatient(t *testing.T) {
	got := ClassifyGarbage("John", "Doe", "19900115", "123 Main St", "10001", "2125551234")
	if got != "" {
		t.Errorf("ClassifyGarbage(John Doe) = %q, want empty", got)
	}
}
