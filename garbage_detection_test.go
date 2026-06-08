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

func TestClassifyGarbage_TrailingPunctuationLeak(t *testing.T) {
	// Real leaked forms: the raw firstName had trailing punctuation that defeated the
	// $-anchored suffix when matched against the raw value, but the MPI strips
	// non-alphanumerics before storing -- so "eros canine." became "eroscanine" in the
	// index. The fix matches the suffix against StripNonAlphanumeric(firstName).
	tests := []struct {
		name      string
		firstName string
		lastName  string
	}{
		{"canine trailing period", "eros canine.", ""},
		{"canine parens", "eros (canine)", ""},
		{"k9 trailing period", "alli k9.", ""},
		{"feline trailing space-dot", "whiskers feline .", ""},
		{"dog trailing punct", "rex dog!", ""},
		{"canine with hyphen", "max-canine", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyGarbage(tt.firstName, tt.lastName, "20200101", "", "", "")
			if got != "pet" {
				t.Errorf("ClassifyGarbage(%q) = %q, want \"pet\" (normalized form is a pet name)", tt.firstName, got)
			}
		})
	}
}

func TestClassifyGarbage_DigitRunNotGarbage(t *testing.T) {
	// A digit run in a name is NOT a garbage signal (the old "synthetic_digits" gate
	// was dropped 2026-06-02). Prod proved these are overwhelmingly REAL long-term-care
	// patients: LTC pharmacies append the insurance member ID into the name field
	// ("PADILLA (16669)" -> "padilla16669"), and LTC residents legitimately have no
	// street/zip/phone. So digit-run names must pass WITH OR WITHOUT contact data.
	noContact := []struct{ fn, ln string }{
		{"rita", "padilla16669"},   // real Grane PBM LTC patient, member ID in surname
		{"trinidad", "martinez16991"},
		{"carolyn1943", "scheer"},  // real DOB-year suffix
		{"kathryn519", "blum"},
		{"mary", "kunze718832"},    // formerly flagged as "synthetic" -- not gated anymore
	}
	for _, c := range noContact {
		if got := ClassifyGarbage(c.fn, c.ln, "19430615", "", "", ""); got != "" {
			t.Errorf("ClassifyGarbage(%q,%q) no contact = %q, want empty (real LTC/dirty-field patient)", c.fn, c.ln, got)
		}
	}
	// And the same names with contact data also pass.
	withContact := []struct{ fn, ln string }{
		{"joan104", "palagonia"}, {"jose134", "cruz"}, {"robert308", "preusser"},
		{"maria", "padilla4096"}, {"young", "han06251924"},
	}
	for _, c := range withContact {
		if got := ClassifyGarbage(c.fn, c.ln, "19400716", "123 Main St", "10001", "2125551234"); got != "" {
			t.Errorf("ClassifyGarbage(%q,%q) w/ contact = %q, want empty", c.fn, c.ln, got)
		}
	}
}

func TestClassifyGarbage_ClaimBlob(t *testing.T) {
	blob := "cbbowtieallergycm375huntingtondrsteccnsanmarinococacp91108cq8586994949hnkvontiehl"
	if got := ClassifyGarbage(blob, "bowtieallergy", "19900101", "", "", ""); got != "claim_blob" {
		t.Errorf("long blob firstName = %q, want claim_blob", got)
	}
	// NCPDP control char in raw value.
	if got := ClassifyGarbage("john\x1cdoe", "smith", "19900101", "", "", ""); got != "claim_blob" {
		t.Errorf("control-char name = %q, want claim_blob", got)
	}
	// A long but legitimate compound name under the ceiling must pass.
	if got := ClassifyGarbage("maria de los angeles", "smith", "19900115", "123 Main St", "10001", "2125551234"); got != "" {
		t.Errorf("compound real name = %q, want empty", got)
	}
}

func TestClassifyGarbage_JunkPlaceholder(t *testing.T) {
	for _, fn := range []string{"test", "patient", "unknown", "noname", "sample", "none", "xxx", "donotuse", "zztest", "newborn"} {
		if got := ClassifyGarbage(fn, "smith", "20240101", "", "", ""); got != "junk_placeholder" {
			t.Errorf("ClassifyGarbage(%q)=%q, want junk_placeholder", fn, got)
		}
	}
	// "baby" is a real given name -- must NOT be flagged.
	if got := ClassifyGarbage("baby", "kuriakose", "19620706", "", "", ""); got != "" {
		t.Errorf("ClassifyGarbage(baby)=%q, want empty (real Kerala/Filipino name)", got)
	}
	// "na" is the N/A placeholder but also a real Korean/Vietnamese given name.
	// Reject only when no contact corroborates; with contact it's a real person.
	if got := ClassifyGarbage("na", "smith", "20240101", "", "", ""); got != "junk_placeholder" {
		t.Errorf("ClassifyGarbage(na) no contact = %q, want junk_placeholder", got)
	}
	if got := ClassifyGarbage("na", "yi", "19380501", "123 Main St", "10001", "2125551234"); got != "" {
		t.Errorf("ClassifyGarbage(na) w/ contact = %q, want empty (real name)", got)
	}
}

func TestClassifyGarbage_SurnamesMustNotMatch(t *testing.T) {
	// Real human surnames that END in a pet suffix. We deliberately do NOT match the
	// species regex against lastName, so these legitimate families must pass. Validated
	// against 250k real human-signal patients pulled from production OpenSearch.
	surnames := []string{
		"apfel", "stifel", "stiefel", "teufel", "scheffel", "christoffel", "zweifel",
		"werfel", "duffel", "warfel", "stoffel", "zipfel", "woelfel", "manteuffel",
		"bacani", "pellicani", "ascani", "carcani", "policani",
		"whitehorse", "roanhorse", "alequin",
	}
	for _, ln := range surnames {
		t.Run(ln, func(t *testing.T) {
			// Even with no contact data + Jan-1 DOB (worst case for ambiguous path),
			// a real firstName + these surnames must not be flagged.
			got := ClassifyGarbage("mary", ln, "19400101", "", "", "")
			if got != "" {
				t.Errorf("ClassifyGarbage(%q, %q) = %q, want empty (real surname)", "mary", ln, got)
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

func TestClassifyGarbage_PetLastName(t *testing.T) {
	// Species token as the WHOLE lastName -> pet (exact match, not suffix).
	for _, ln := range []string{"dog", "Cat", "K9", "canine", "FELINE", "kitten", "puppy", "bunny", "ferret", "hamster", "parrot", "gecko", "iguana"} {
		if got := ClassifyGarbage("rex", ln, "20210101", "", "", ""); got != "pet" {
			t.Errorf("ClassifyGarbage(rex, %q) = %q, want pet", ln, got)
		}
	}
	// Real surnames that merely CONTAIN or end in a species token must NOT be flagged --
	// exact match protects them where suffix matching would not.
	for _, ln := range []string{"hodge", "catalano", "dogget", "doggett", "catt", "k99", "whitehorse", "roanhorse", "apfel"} {
		if got := ClassifyGarbage("mary", ln, "19400101", "", "", ""); got != "" {
			t.Errorf("ClassifyGarbage(mary, %q) = %q, want empty (real surname, not exact species)", ln, got)
		}
	}
}

func TestClassifyGarbage_InstitutionalFacility(t *testing.T) {
	// Unambiguous facility word as the whole lastName -> institutional_facility.
	for _, ln := range []string{"hospice", "Pharmacy", "CLINIC", "facility", "snf", "ltc", "rx", "healthcare", "infusion"} {
		if got := ClassifyGarbage("symbii", ln, "19510101", "", "", ""); got != "institutional_facility" {
			t.Errorf("ClassifyGarbage(symbii, %q) = %q, want institutional_facility", ln, got)
		}
	}
	// Surname-risky words are deliberately NOT gated (real surnames Home/Center/Stock/Card).
	for _, ln := range []string{"home", "center", "health", "stock", "card"} {
		if got := ClassifyGarbage("john", ln, "19500101", "123 Main St", "10001", "2125551234"); got != "" {
			t.Errorf("ClassifyGarbage(john, %q) = %q, want empty (ambiguous; needs first-name co-signal)", ln, got)
		}
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
