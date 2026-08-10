package lure

import (
	"fmt"

	"github.com/phishingclub/phishingclub/random"
)

// Algorithm selects how a generated code is produced. The resolver keys only on
// the stored code and never reads this, so a new algorithm can be added without
// touching the request path.
type Algorithm string

const (
	// AlgorithmCrockford32 draws from the Crockford base32 alphabet.
	AlgorithmCrockford32 Algorithm = "crockford32"
	// AlgorithmBase58 draws from the Bitcoin base58 alphabet.
	AlgorithmBase58 Algorithm = "base58"
)

// DefaultAlgorithm is used when a campaign template has no explicit choice.
const DefaultAlgorithm = AlgorithmCrockford32

// IsValidAlgorithm reports whether a is a known generator.
func IsValidAlgorithm(a Algorithm) bool {
	switch a {
	case AlgorithmCrockford32, AlgorithmBase58:
		return true
	}
	return false
}

// AlphabetFor returns the symbol set an algorithm draws from.
func AlphabetFor(a Algorithm) string {
	switch a {
	case AlgorithmBase58:
		return Base58Alphabet
	default:
		return Alphabet
	}
}

// Code is an allocated identifier, stored and matched exactly as it appears
// here.
type Code struct {
	Display string
}

// NewCustomCode builds a Code from an operator supplied string, kept verbatim.
func NewCustomCode(s string) Code {
	return Code{Display: s}
}

// Generate returns a new random code of the given length.
func Generate(algorithm Algorithm, length int) (Code, error) {
	codes, err := GenerateBatch(algorithm, length, 1)
	if err != nil {
		return Code{}, err
	}
	return codes[0], nil
}

// maxRandomBlock caps a single entropy read so a large batch does not ask for
// one allocation the size of the whole batch.
const maxRandomBlock = 64 * 1024

// GenerateBatch returns n new random codes of the given length.
//
// Entropy is drawn in blocks covering many codes, because scheduling a large
// campaign asks for tens of thousands at once. Symbols are drawn by rejection
// sampling: a byte taken modulo an alphabet size that does not divide 256 would
// favour the symbols at the start and shrink the real key space.
func GenerateBatch(algorithm Algorithm, length int, n int) ([]Code, error) {
	if !IsValidAlgorithm(algorithm) {
		return nil, fmt.Errorf("unknown lure code algorithm: %s", algorithm)
	}
	if length < MinLength || length > MaxLength {
		return nil, fmt.Errorf(
			"lure code length must be between %d and %d, got %d",
			MinLength,
			MaxLength,
			length,
		)
	}
	if n <= 0 {
		return []Code{}, nil
	}
	alphabet := AlphabetFor(algorithm)
	size := len(alphabet)
	// largest multiple of the alphabet size within a byte range, above which a
	// draw is discarded. an alphabet dividing 256 gives 256, which is why this is
	// an int and the comparison below widens the byte rather than narrowing it.
	limit := 256 / size * size

	// symbols wanted plus headroom for discarded draws. running short only costs
	// another read, so this is a guess rather than a bound.
	block := n*length + (n*length)/4 + 16
	if block > maxRandomBlock {
		block = maxRandomBlock
	}

	codes := make([]Code, 0, n)
	out := make([]byte, 0, length)
	buf := []byte{}
	for len(codes) < n {
		if len(buf) == 0 {
			drawn, err := random.GenerateRandomBytes(block)
			if err != nil {
				return nil, fmt.Errorf("failed to generate lure code: %w", err)
			}
			buf = drawn
		}
		b := buf[0]
		buf = buf[1:]
		if int(b) >= limit {
			continue
		}
		out = append(out, alphabet[int(b)%size])
		if len(out) == length {
			// string copies, so out can be reused
			codes = append(codes, Code{Display: string(out)})
			out = out[:0]
		}
	}
	return codes, nil
}
