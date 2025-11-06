package g

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/enetx/g/cmp"
	"github.com/enetx/g/constraints"
)

// NewFloat creates a new Float with the provided value.
func NewFloat[T constraints.Float | constraints.Integer](float T) Float { return Float(float) }

// Transform applies a transformation function to the Float and returns the result.
func (f Float) Transform(fn func(Float) Float) Float { return fn(f) }

// BytesBE returns the IEEE-754 representation of the Float as Bytes in BigEndian order.
// The Float is converted to its 64-bit IEEE-754 binary representation.
func (f Float) BytesBE() Bytes {
	var buf [8]byte
	bits := math.Float64bits(float64(f))
	binary.BigEndian.PutUint64(buf[:], bits)

	return Bytes(buf[:])
}

// BytesLE returns the IEEE-754 representation of the Float as Bytes in LittleEndian order.
// The Float is converted to its 64-bit IEEE-754 binary representation.
func (f Float) BytesLE() Bytes {
	var buf [8]byte
	bits := math.Float64bits(float64(f))
	binary.LittleEndian.PutUint64(buf[:], bits)

	return Bytes(buf[:])
}

// Min returns the minimum of two Floats.
func (f Float) Min(b ...Float) Float { return cmp.Min(append(b, f)...) }

// Max returns the maximum of two Floats.
func (f Float) Max(b ...Float) Float { return cmp.Max(append(b, f)...) }

// Sqrt returns the square root of the Float.
//
// Returns:
//   - Float: The square root of the Float. If the Float is negative, the result is NaN.
//
// Example usage:
//
//	f := g.NewFloat(9)
//	result := f.Sqrt() // result = 3
func (f Float) Sqrt() Float { return Float(math.Sqrt(f.Std())) }

// Pow raises the Float to the power of the given exponent.
//
// Parameters:
//   - exp (Float): The exponent to raise the Float to.
//
// Returns:
//   - Float: The result of raising the Float to the specified power.
//
// Example usage:
//
//	f := g.NewFloat(2)
//	result := f.Pow(3) // result = 8
func (f Float) Pow(exp Float) Float { return Float(math.Pow(f.Std(), exp.Std())) }

// Mod returns the floating-point remainder of f / b as defined by IEEE 754.
//
// Parameters:
//   - b (Float): The divisor.
//
// Returns:
//   - Float: The remainder of f / b.
//
// Example usage:
//
//	f := g.NewFloat(5.5)
//	result := f.Mod(2) // result = 1.5
func (f Float) Mod(b Float) Float { return Float(math.Mod(f.Std(), b.Std())) }

// Abs returns the absolute value of the Float.
func (f Float) Abs() Float { return Float(math.Abs(f.Std())) }

// Add adds two Floats and returns the result.
func (f Float) Add(b Float) Float { return f + b }

// BigFloat returns the Float as a *big.Float.
func (f Float) BigFloat() *big.Float { return big.NewFloat(f.Std()) }

// Cmp compares two Floats and returns an cmp.Ordering.
func (f Float) Cmp(b Float) cmp.Ordering { return cmp.Cmp(f, b) }

// Div divides two Floats and returns the result.
func (f Float) Div(b Float) Float { return f / b }

// IsZero checks if the Float is 0.
func (f Float) IsZero() bool { return f.Eq(0) }

// Eq checks if two Floats are equal.
func (f Float) Eq(b Float) bool { return f.Cmp(b).IsEq() }

// Std returns the Float as a float64.
func (f Float) Std() float64 { return float64(f) }

// Gt checks if the Float is greater than the specified Float.
func (f Float) Gt(b Float) bool { return f.Cmp(b).IsGt() }

// Int returns the Float as an Int.
func (f Float) Int() Int { return Int(f) }

// String returns the Float as an String.
func (f Float) String() String { return String(strconv.FormatFloat(f.Std(), 'g', -1, 64)) }

// Lt checks if the Float is less than the specified Float.
func (f Float) Lt(b Float) bool { return f.Cmp(b).IsLt() }

// Mul multiplies two Floats and returns the result.
func (f Float) Mul(b Float) Float { return f * b }

// Ne checks if two Floats are not equal.
func (f Float) Ne(b Float) bool { return !f.Eq(b) }

// Round rounds the Float to the nearest integer and returns the result as an Int.
func (f Float) Round() Int { return Int(math.Round(f.Std())) }

// RoundDecimal rounds the Float value to the specified number of decimal places.
//
// Parameters:
//   - precision (Int): The number of decimal places to round to. If negative, the Float is returned unchanged.
//     Values greater than 308 are capped at 308 to prevent overflow.
//
// Returns:
//   - Float: A new Float value rounded to the specified number of decimal places.
func (f Float) RoundDecimal(precision Int) Float {
	if precision < 0 {
		return f
	}

	if precision > 308 { // 10^309 == +Inf
		precision = 308
	}

	pow := math.Pow(10, float64(precision))

	return Float(math.Round(f.Std()*pow) / pow)
}

// TruncDecimal truncates the Float value to the specified number of decimal places.
//
// Parameters:
//   - precision (Int): The number of decimal places to truncate to. If negative, the Float is returned unchanged.
//     Values greater than 308 are capped at 308.
//
// Returns:
//   - Float: A new Float value truncated to the specified number of decimal places.
func (f Float) TruncDecimal(precision Int) Float {
	if precision < 0 {
		return f
	}

	if precision > 308 {
		precision = 308
	}

	pow := math.Pow(10, float64(precision))

	return Float(math.Trunc(f.Std()*pow) / pow)
}

// CeilDecimal rounds the Float value up (towards +Inf) to the specified number of decimal places.
//
// Parameters:
//   - precision (Int): The number of decimal places to round up to. If negative, the Float is returned unchanged.
//     Values greater than 308 are capped at 308.
//
// Returns:
//   - Float: A new Float value rounded up to the specified number of decimal places.
func (f Float) CeilDecimal(precision Int) Float {
	if precision < 0 {
		return f
	}

	if precision > 308 {
		precision = 308
	}

	pow := math.Pow(10, float64(precision))

	return Float(math.Ceil(f.Std()*pow) / pow)
}

// FloorDecimal rounds the Float value down (towards -Inf) to the specified number of decimal places.
//
// Parameters:
//   - precision (Int): The number of decimal places to round down to. If negative, the Float is returned unchanged.
//     Values greater than 308 are capped at 308.
//
// Returns:
//   - Float: A new Float value rounded down to the specified number of decimal places.
func (f Float) FloorDecimal(precision Int) Float {
	if precision < 0 {
		return f
	}

	if precision > 308 {
		precision = 308
	}

	pow := math.Pow(10, float64(precision))

	return Float(math.Floor(f.Std()*pow) / pow)
}

// Sub subtracts two Floats and returns the result.
func (f Float) Sub(b Float) Float { return f - b }

// Bits returns IEEE-754 representation of f.
func (f Float) Bits() uint64 { return math.Float64bits(f.Std()) }

// Float32 returns the Float as a float32.
func (f Float) Float32() float32 { return float32(f) }

// Print writes the value of the Float to the standard output (console)
// and returns the Float unchanged.
func (f Float) Print() Float { fmt.Print(f); return f }

// Println writes the value of the Float to the standard output (console) with a newline
// and returns the Float unchanged.
func (f Float) Println() Float { fmt.Println(f); return f }
