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
		{"dog suffix", "RexDog", "", "pet"},
		{"k9 suffix", "BuddyK9", "", "pet"},
		{"equine suffix", "BirdyEquine", "", "pet"},
		{"equin suffix", "DaisyEquin", "", "pet"},
		{"puppy suffix", "FidoPuppy", "", "pet"},
		{"kitten suffix", "FuzzyKitten", "", "pet"},
		{"bunny suffix", "FluffyBunny", "", "pet"},
		{"rabbit suffix", "CottonRabbit", "", "pet"},
		{"ferret suffix", "SlinkyFerret", "", "pet"},
		{"hamster suffix", "PipsqueakHamster", "", "pet"},
		{"turtle suffix", "SlowpokesTurtle", "", "pet"},
		{"parrot suffix", "CrackerParrot", "", "pet"},
		{"gecko suffix", "SpottyGecko", "", "pet"},
		{"iguana suffix", "SpikeyIguana", "", "pet"},

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
	// "pet$" is now an AMBIGUOUS suffix (real Armenian names end in -pet), so "Carpet"
	// only rejects when no contact data corroborates. With contact data it passes —
	// the FP-averse tradeoff of demoting the suffix.
	got := ClassifyGarbage("Carpet", "", "19900115", "", "", "")
	if got != "ambiguous_pet" {
		t.Errorf("ClassifyGarbage(\"Carpet\") no contact = %q, want \"ambiguous_pet\"", got)
	}
	got = ClassifyGarbage("Carpet", "", "19900115", "123 Main St", "10001", "2125551234")
	if got != "" {
		t.Errorf("ClassifyGarbage(\"Carpet\") w/ contact = %q, want empty (contact data corroborates)", got)
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

func TestClassifyGarbage_AmbiguousPetSuffixes(t *testing.T) {
	// Demoted suffixes (fel, pet, pup, pig, goat, horse, guinea): reject only when no
	// contact data corroborates AND the name is not a known real human name.
	tests := []struct {
		name      string
		firstName string
		street    string
		zip       string
		phone     string
		want      string
	}{
		// No contact data, not allowlisted -> ambiguous_pet
		{"fel suffix no contact", "MittsFel", "", "", "", "ambiguous_pet"},
		{"pet suffix no contact", "BellasPet", "", "", "", "ambiguous_pet"},
		{"pup suffix no contact", "MaxPup", "", "", "", "ambiguous_pet"},
		{"pig suffix no contact", "WilburPig", "", "", "", "ambiguous_pet"},
		{"goat suffix no contact", "BillyGoat", "", "", "", "ambiguous_pet"},
		{"horse suffix no contact", "ThunderHorse", "", "", "", "ambiguous_pet"},
		{"guinea suffix no contact", "PopcornGuinea", "", "", "", "ambiguous_pet"},

		// Any contact data -> pass
		{"fel suffix with address", "MittsFel", "123 Main St", "", "", ""},
		{"pet suffix with zip", "BellasPet", "", "10001", "", ""},
		{"pup suffix with phone", "MaxPup", "", "", "2125551234", ""},

		// Former (fe|cat|can) tier is gone: its only corroborator (Jan-1 DOB + no
		// contact) is the signature of REAL defaulted-DOB LTC patients.
		{"cat suffix no contact", "SimbaCat", "", "", "", ""},
		{"fe suffix no contact", "MittsFe", "", "", "", ""},
		{"can suffix no contact", "BuddyCan", "", "", "", ""},
		{"Duncan no contact", "Duncan", "", "", "", ""},
		{"Aoife no contact", "Aoife", "", "", "", ""},
		{"Josephe no contact", "Josephe", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Jan-1 DOB on purpose: it must NOT influence the outcome.
			got := ClassifyGarbage(tt.firstName, "", "20200101", tt.street, tt.zip, tt.phone)
			if got != tt.want {
				t.Errorf("ClassifyGarbage(%q, street=%q, zip=%q, phone=%q) = %q, want %q",
					tt.firstName, tt.street, tt.zip, tt.phone, got, tt.want)
			}
		})
	}
}

func TestClassifyGarbage_RealNamesWithPetLikeSuffixes(t *testing.T) {
	// Real human first names measured in production PAID claims (2026-06). These end in
	// demoted species suffixes and were being dropped (~850-1500 claims per 6 weeks).
	// They must pass even with NO contact data (LTC residents) and Jan-1 DOB.
	names := []string{
		"KARAPET", "HAYRAPET", "YEGISAPET", // Armenian
		"RAFEL", "MARIFEL", // Filipino
		"SURAFEL", // Ethiopian
		"CHRISTOFFEL", "STOFFEL", // Dutch/Afrikaans
	}
	for _, fn := range names {
		t.Run(fn, func(t *testing.T) {
			if got := ClassifyGarbage(fn, "GRIGORYAN", "19400101", "", "", ""); got != "" {
				t.Errorf("ClassifyGarbage(%q) no contact + Jan-1 DOB = %q, want empty (allowlisted real name)", fn, got)
			}
			if got := ClassifyGarbage(fn, "GRIGORYAN", "19471112", "123 Main St", "10001", "2125551234"); got != "" {
				t.Errorf("ClassifyGarbage(%q) w/ contact = %q, want empty (real name)", fn, got)
			}
		})
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
