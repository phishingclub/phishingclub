package lure

import (
	"strings"
	"testing"
)

func TestIsCandidate(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"4H7K9QM2XR3T", true},
		{"zQ8fRt2mKp9x", true},
		// an operator written code may be any text, so no alphabet is applied
		{"special-42", true},
		{"Special_42", true},
		{"hello", true},
		// a single dot is allowed so a lure can end in invoice.pdf
		{"invoice.pdf", true},
		{"main.4f3a2b1c.js", true},
		// values that could not sit in a single path segment unescaped
		{"hello world", false},
		{"a/b", false},
		{"a%2Fb", false},
		{"a?b", false},
		{"a#b", false},
		{"", false},
		// a browser resolves a lone dot away before sending, and the resolver
		// refuses a whole path carrying a doubled dot rather than cleaning it
		{".", false},
		{"..", false},
		{"...", false},
		{"invoice..pdf", false},
		{"v1..2", false},
		// a single dot elsewhere in the segment is still fine
		{".hidden", true},
		{strings.Repeat("a", MaxCustomLength), true},
		{strings.Repeat("a", MaxCustomLength+1), false},
	}
	for _, c := range cases {
		if got := IsCandidate(c.in); got != c.want {
			t.Errorf("IsCandidate(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsCandidateRejectsUnescapableValues(t *testing.T) {
	for _, in := range []string{"a/b", "%2Fetc", "a\\b", "a b"} {
		if IsCandidate(in) {
			t.Errorf("IsCandidate(%q) should be false", in)
		}
	}
}

func TestNewCustomCodeIsKeptVerbatim(t *testing.T) {
	// an operator written code reaches the URL as chosen, so Special-42
	// and special-42 stay distinct links
	for _, in := range []string{"special-42", "Special_42", "HR-Survey-2026", "invoice.pdf"} {
		if got := NewCustomCode(in).Display; got != in {
			t.Errorf("display form was altered: %q -> %q", in, got)
		}
	}
}

func TestGenerateLength(t *testing.T) {
	for _, algorithm := range []Algorithm{AlgorithmCrockford32, AlgorithmBase58} {
		for length := MinLength; length <= MaxLength; length++ {
			code, err := Generate(algorithm, length)
			if err != nil {
				t.Fatalf("Generate(%s, %d) failed: %v", algorithm, length, err)
			}
			if len(code.Display) != length {
				t.Errorf("Generate(%s, %d) gave length %d", algorithm, length, len(code.Display))
			}
		}
	}
}

func TestGenerateStaysInsideItsAlphabet(t *testing.T) {
	for _, algorithm := range []Algorithm{AlgorithmCrockford32, AlgorithmBase58} {
		alphabet := AlphabetFor(algorithm)
		for i := 0; i < 500; i++ {
			code, err := Generate(algorithm, MaxLength)
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}
			for _, r := range code.Display {
				if !strings.ContainsRune(alphabet, r) {
					t.Fatalf("%s produced %q outside its alphabet: %q", algorithm, string(r), code.Display)
				}
			}
		}
	}
}

func TestGenerateCoversEachAlphabet(t *testing.T) {
	// rejection sampling must reach every symbol and accept enough draws to
	// terminate. an alphabet dividing 256 gives a limit of 256, which held in a
	// byte would be zero and reject everything.
	for _, algorithm := range []Algorithm{AlgorithmCrockford32, AlgorithmBase58} {
		alphabet := AlphabetFor(algorithm)
		seen := map[rune]bool{}
		for i := 0; i < 8000; i++ {
			code, err := Generate(algorithm, MaxLength)
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}
			for _, r := range code.Display {
				seen[r] = true
			}
		}
		for _, r := range alphabet {
			if !seen[r] {
				t.Errorf("%s never generated symbol %q", algorithm, string(r))
			}
		}
	}
}

func TestBase58UsesBothCases(t *testing.T) {
	// base58 treats the two cases as distinct symbols, which is why matching is
	// never folded
	lower := false
	upper := false
	for i := 0; i < 500 && (!lower || !upper); i++ {
		code, err := Generate(AlgorithmBase58, MaxLength)
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}
		if strings.ToLower(code.Display) != code.Display {
			upper = true
		}
		if strings.ToUpper(code.Display) != code.Display {
			lower = true
		}
	}
	if !lower || !upper {
		t.Error("base58 should produce both cases")
	}
}

func TestGenerateExcludesConfusableGlyphs(t *testing.T) {
	for i := 0; i < 500; i++ {
		crockford, err := Generate(AlgorithmCrockford32, MaxLength)
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}
		if strings.ContainsAny(crockford.Display, "ILOU") {
			t.Fatalf("crockford code contains an excluded glyph: %q", crockford.Display)
		}
		base58, err := Generate(AlgorithmBase58, MaxLength)
		if err != nil {
			t.Fatalf("Generate failed: %v", err)
		}
		if strings.ContainsAny(base58.Display, "0OIl") {
			t.Fatalf("base58 code contains an excluded glyph: %q", base58.Display)
		}
	}
}

func TestGenerateRejectsBadInput(t *testing.T) {
	if _, err := Generate("nope", DefaultLength); err == nil {
		t.Error("expected an error for an unknown algorithm")
	}
	if _, err := Generate(AlgorithmCrockford32, MinLength-1); err == nil {
		t.Error("expected an error for a length below the minimum")
	}
	if _, err := Generate(AlgorithmCrockford32, MaxLength+1); err == nil {
		t.Error("expected an error for a length above the maximum")
	}
}

func TestGenerateBatch(t *testing.T) {
	// a schedule run draws every code at once, so the batch must return exactly
	// what was asked for and stay in its alphabet across entropy block refills
	for _, algorithm := range []Algorithm{AlgorithmCrockford32, AlgorithmBase58} {
		alphabet := AlphabetFor(algorithm)
		codes, err := GenerateBatch(algorithm, DefaultLength, 5000)
		if err != nil {
			t.Fatalf("GenerateBatch(%s) failed: %v", algorithm, err)
		}
		if len(codes) != 5000 {
			t.Fatalf("GenerateBatch(%s) gave %d codes, want 5000", algorithm, len(codes))
		}
		seen := map[string]bool{}
		for _, code := range codes {
			if len(code.Display) != DefaultLength {
				t.Fatalf("code %q is not %d characters", code.Display, DefaultLength)
			}
			for _, r := range code.Display {
				if !strings.ContainsRune(alphabet, r) {
					t.Fatalf("%s produced %q outside its alphabet: %q", algorithm, string(r), code.Display)
				}
			}
			seen[code.Display] = true
		}
		// the allocator dedupes, so a batch need not be distinct, but a broken draw
		// repeating one code shows up here
		if len(seen) < 4990 {
			t.Errorf("%s produced only %d distinct codes out of 5000", algorithm, len(seen))
		}
	}
}

func TestGenerateBatchRejectsBadInput(t *testing.T) {
	if _, err := GenerateBatch("nope", DefaultLength, 1); err == nil {
		t.Error("expected an error for an unknown algorithm")
	}
	if _, err := GenerateBatch(AlgorithmCrockford32, MinLength-1, 1); err == nil {
		t.Error("expected an error for a length below the minimum")
	}
	codes, err := GenerateBatch(AlgorithmCrockford32, DefaultLength, 0)
	if err != nil || len(codes) != 0 {
		t.Errorf("GenerateBatch with n 0 = %v,%v want empty,nil", codes, err)
	}
}

func TestIsValidAlgorithm(t *testing.T) {
	if !IsValidAlgorithm(AlgorithmCrockford32) || !IsValidAlgorithm(AlgorithmBase58) {
		t.Error("both shipped algorithms should be valid")
	}
	if IsValidAlgorithm("base64") {
		t.Error("unknown algorithm should not validate")
	}
}
