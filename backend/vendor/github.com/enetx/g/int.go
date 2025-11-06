package g

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"strconv"

	"github.com/enetx/g/cmp"
	"github.com/enetx/g/constraints"
	"github.com/enetx/g/rand"
)

// NewInt creates a new Int with the provided int value.
func NewInt[T constraints.Integer | rune | byte](i T) Int { return Int(i) }

// Transform applies a transformation function to the Int and returns the result.
func (i Int) Transform(fn func(Int) Int) Int { return fn(i) }

// Min returns the minimum of Ints.
func (i Int) Min(b ...Int) Int { return cmp.Min(append(b, i)...) }

// Max returns the maximum of Ints.
func (i Int) Max(b ...Int) Int { return cmp.Max(append(b, i)...) }

// RandomRange returns a random Int in the inclusive range [i, to].
// The order of bounds does not matter (it normalizes to [min, max]).
// Works for negative bounds and the full int64 range without overflow or bias.
func (i Int) RandomRange(to Int) Int {
	lo, hi := i, to

	if lo > hi {
		lo, hi = hi, lo
	}

	if lo == hi {
		return lo
	}

	const bias = uint64(1) << 63 // 2^63 = 9223372036854775808

	ulo := uint64(lo) + bias
	uhi := uint64(hi) + bias

	w := uhi - ulo + 1

	if w == 0 {
		return Int(int64(rand.U64()))
	}

	randv := rand.N(w)
	result := int64((ulo + randv) - bias)

	return Int(result)
}

// Abs returns the absolute value of the Int.
func (i Int) Abs() Int {
	if i < 0 {
		return -i
	}

	return i
}

// Add adds two Ints and returns the result.
func (i Int) Add(b Int) Int { return i + b }

// BigInt returns the Int as a *big.Int.
func (i Int) BigInt() *big.Int { return big.NewInt(i.Int64()) }

// Div divides two Ints and returns the result.
func (i Int) Div(b Int) Int { return i / b }

// Eq checks if two Ints are equal.
func (i Int) Eq(b Int) bool { return i == b }

// Gt checks if the Int is greater than the specified Int.
func (i Int) Gt(b Int) bool { return i > b }

// Gte checks if the Int is greater than or equal to the specified Int.
func (i Int) Gte(b Int) bool { return i >= b }

// Float returns the Int as an Float.
func (i Int) Float() Float { return Float(i) }

// String returns the Int as an String.
func (i Int) String() String { return String(strconv.FormatInt(int64(i), 10)) }

// Std returns the Int as an int.
func (i Int) Std() int { return int(i) }

// Cmp compares two Ints and returns an cmp.Ordering.
func (i Int) Cmp(b Int) cmp.Ordering { return cmp.Cmp(i, b) }

// Int16 returns the Int as an int16.
func (i Int) Int16() int16 { return int16(i) }

// Int32 returns the Int as an int32.
func (i Int) Int32() int32 { return int32(i) }

// Int64 returns the Int as an int64.
func (i Int) Int64() int64 { return int64(i) }

// Int8 returns the Int as an int8.
func (i Int) Int8() int8 { return int8(i) }

// IsZero checks if the Int is 0.
func (i Int) IsZero() bool { return i == 0 }

// IsNegative checks if the Int is negative.
func (i Int) IsNegative() bool { return i < 0 }

// IsPositive checks if the Int is positive.
func (i Int) IsPositive() bool { return i >= 0 }

// Lt checks if the Int is less than the specified Int.
func (i Int) Lt(b Int) bool { return i < b }

// Lte checks if the Int is less than or equal to the specified Int.
func (i Int) Lte(b Int) bool { return i <= b }

// Mul multiplies two Ints and returns the result.
func (i Int) Mul(b Int) Int { return i * b }

// Ne checks if two Ints are not equal.
func (i Int) Ne(b Int) bool { return i != b }

// Random returns a random Int in the range [0, hi].
func (i Int) Random() Int {
	if i <= 0 {
		return 0
	}

	return Int(rand.N(uint64(i)))
}

// Rem returns the remainder of the division between the receiver and the input value.
func (i Int) Rem(b Int) Int { return i % b }

// Sub subtracts two Ints and returns the result.
func (i Int) Sub(b Int) Int { return i - b }

// Binary returns the Int as a binary string.
func (i Int) Binary() String { return String(fmt.Sprintf("%08b", i)) }

// Hex returns the Int as a hexadecimal string.
func (i Int) Hex() String { return String(fmt.Sprintf("%x", i)) }

// Octal returns the Int as an octal string.
func (i Int) Octal() String { return String(fmt.Sprintf("%o", i)) }

// UInt returns the Int as a uint.
func (i Int) UInt() uint { return uint(i) }

// UInt16 returns the Int as a uint16.
func (i Int) UInt16() uint16 { return uint16(i) }

// UInt32 returns the Int as a uint32.
func (i Int) UInt32() uint32 { return uint32(i) }

// UInt64 returns the Int as a uint64.
func (i Int) UInt64() uint64 { return uint64(i) }

// UInt8 returns the Int as a uint8.
func (i Int) UInt8() uint8 { return uint8(i) }

// bytesFromInt converts Int to Bytes using the given byte order.
// For BE: removes leading zeros while preserving the sign bit.
// For LE: removes trailing zeros while preserving the sign bit.
func bytesFromInt(i Int, order binary.ByteOrder) Bytes {
	var buf [8]byte
	order.PutUint64(buf[:], uint64(i))

	switch order {
	case binary.BigEndian:
		start := 0
		for start < 7 && buf[start] == 0 {
			start++
		}

		if i >= 0 && buf[start]&0x80 != 0 {
			start--
		}

		if i < 0 && start > 0 && buf[start]&0x80 == 0 {
			start--
		}

		return Bytes(buf[start:])
	case binary.LittleEndian:
		end := 8
		for end > 1 && buf[end-1] == 0 {
			end--
		}

		if i >= 0 && buf[end-1]&0x80 != 0 {
			end++
		}

		if i < 0 && end < 8 && buf[end-1]&0x80 == 0 {
			end++
		}

		return Bytes(buf[:end])
	}

	return Bytes(buf[:])
}

// BytesBE converts the Int to Bytes in BigEndian order.
// Leading zero bytes are removed while preserving the sign bit for negative numbers.
func (i Int) BytesBE() Bytes {
	return bytesFromInt(i, binary.BigEndian)
}

// BytesLE converts the Int to Bytes in LittleEndian order.
// Trailing zero bytes are removed while preserving the sign bit for negative numbers
func (i Int) BytesLE() Bytes {
	return bytesFromInt(i, binary.LittleEndian)
}

// Print writes the value of the Int to the standard output (console)
// and returns the Int unchanged.
func (i Int) Print() Int { fmt.Print(i); return i }

// Println writes the value of the Int to the standard output (console) with a newline
// and returns the Int unchanged.
func (i Int) Println() Int { fmt.Println(i); return i }
