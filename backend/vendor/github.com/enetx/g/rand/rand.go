package rand

import (
	"crypto/rand"
	"encoding/binary"
	"io"

	"github.com/enetx/g/constraints"
)

// U64 returns a cryptographically secure random uint64 value.
// It reads 8 random bytes from crypto/rand.Reader and interprets
// them as a little-endian unsigned integer.
// Panics if the system random number generator is unavailable.
func U64() uint64 {
	var b [8]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		panic(err)
	}

	return binary.LittleEndian.Uint64(b[:])
}

// N generates a random non-negative integer within the range [0, max).
// The generated integer will be less than the provided maximum value.
// If max is less than or equal to 0, the function will treat it as if max is 1.
//
// Usage:
//
//	n := 10
//	randomInt := rand.N(n)
//	fmt.Printf("Random integer between 0 and %d: %d\n", max, randomInt)
//
// Parameters:
//   - n (int): The maximum bound for the random integer to be generated.
//
// Returns:
//   - int: A random non-negative integer within the specified range.
func N[T constraints.Integer](n T) T {
	if n <= 0 {
		return 0
	}

	w := uint64(n)
	if w == 1 {
		return 0
	}

	if w&(w-1) == 0 {
		return T(U64() & (w - 1))
	}

	const maxU64 = ^uint64(0)
	limit := maxU64 - (maxU64 % w)

	for {
		randv := U64()
		if randv < limit {
			return T(randv % w)
		}
	}
}
