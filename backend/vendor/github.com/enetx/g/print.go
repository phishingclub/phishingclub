package g

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"

	"github.com/enetx/g/f"
)

// Write formats according to a format specifier and writes to w.
// It returns a Result containing the number of bytes written or an error.
//
// Example:
//
//	res := g.Write(os.Stdout, "Hello, {}!\n", "world")
//	if res.IsErr() { log.Fatal(res.Err()) }
func Write[T ~string](w io.Writer, format T, args ...any) Result[int] {
	return ResultOf(io.WriteString(w, Format(format, args...).Std()))
}

// Writeln formats according to a format specifier, appends a newline, and writes to w.
// It returns a Result containing the number of bytes written or an error.
//
// Example:
//
//	res := g.Writeln(os.Stdout, "Hello, {}", "world")
//	if res.IsErr() { log.Fatal(res.Err()) }
func Writeln[T ~string](w io.Writer, format T, args ...any) Result[int] {
	return ResultOf(io.WriteString(w, Format(format, args...).Append("\n").Std()))
}

// Print formats according to a format specifier and writes to os.Stdout.
// It returns a Result containing the number of bytes written or an error.
//
// Example:
//
//	g.Print("Hello, {}!\n", "world")
func Print[T ~string](format T, args ...any) Result[int] {
	return Write(os.Stdout, format, args...)
}

// Println formats according to a format specifier, appends a newline, and writes to os.Stdout.
// It returns a Result containing the number of bytes written or an error.
//
// Example:
//
//	g.Println("Hello, {}", "world")
func Println[T ~string](format T, args ...any) Result[int] {
	return Writeln(os.Stdout, format, args...)
}

// Eprint formats according to a format specifier and writes to os.Stderr.
// It returns a Result containing the number of bytes written or an error.
//
// Example:
//
//	g.Eprint("Error: {}", "file not found")
func Eprint[T ~string](format T, args ...any) Result[int] {
	return Write(os.Stderr, format, args...)
}

// Eprintln formats according to a format specifier, appends a newline, and writes to os.Stderr.
// It returns a Result containing the number of bytes written or an error.
//
// Example:
//
//	g.Eprintln("Error: {}", "permission denied")
func Eprintln[T ~string](format T, args ...any) Result[int] {
	return Writeln(os.Stderr, format, args...)
}

// Errorf formats according to a format specifier and returns it as an error.
//
// Example:
//
//	err := g.Errorf("could not open {}: {}", filename, err)
//	if err != nil { /* ... */ }
func Errorf[T ~string](format T, args ...any) error {
	return errors.New(Format(format, args...).Std())
}

// Format processes a template string and replaces placeholders with corresponding values from the provided arguments.
// It supports numeric, named, and auto-indexed placeholders, as well as dynamic invocation of methods on values.
//
// If a placeholder cannot resolve a value or an invoked method fails, the placeholder remains unchanged in the output.
//
// Parameters:
//   - template (T ~string): A string containing placeholders enclosed in `{}`.
//   - args (...any): A variadic list of arguments, which may include:
//   - Positional arguments (numbers, strings, slices, structs, maps, etc.).
//   - A `Named` map for named placeholders.
//
// Placeholder Forms:
//   - Numeric: `{1}`, `{2}` - References positional arguments by their 1-based index.
//   - Named: `{key}`, `{key.MethodName(param1, param2)}` - References keys from a `Named` map and allows method invocation.
//   - Fallback: `{key?fallback}` - Uses `fallback` if the key is not found in the named map.
//   - Auto-index: `{}` - Automatically uses the next positional argument if the placeholder is empty.
//   - Escaping: `\{` and `\}` - Escapes literal braces in the template string.
//
// Returns:
//   - String: A formatted string with all resolved placeholders replaced by their corresponding values.
//
// Notes:
//   - If a placeholder cannot resolve a value (e.g., missing key or out-of-range index), it remains unchanged in the output.
//   - Method invocation supports any type with accessible methods. If the method or its parameters are invalid, the value remains unmodified.
//
// Usage:
//
//	// Example 1: Numeric placeholders
//	result := g.Format("{1} + {2} = {3}", 1, 2, 3)
//
//	// Example 2: Named placeholders
//	named := g.Named{
//		"name": "Alice",
//		"age":  30,
//	}
//	result := g.Format("My name is {name} and I am {age} years old.", named)
//
//	// Example 3: Method invocation on values
//	result := g.Format("Hex: {1.Hex}, Binary: {1.Binary}", g.Int(255))
//
//	// Example 4: Fallbacks and chaining
//	named := g.Named{
//		"name": g.String("   john  "),
//		"city": g.String("New York"),
//	}
//	result := g.Format("Hello, {name.Trim.Title}. Welcome to {city?Unknown}!", named)
func Format[T ~string](template T, args ...any) String {
	tmpl := String(template)

	var (
		named      Named
		positional Slice[any]
	)

	for _, arg := range args {
		switch x := arg.(type) {
		case Named:
			named = x
		case nil:
			positional = append(positional, "<nil>")
		default:
			positional = append(positional, x)
		}
	}

	return parseTmpl(tmpl, named, positional)
}

func parseTmpl(tmpl String, named Named, positional Slice[any]) String {
	var builder Builder
	length := tmpl.Len()
	builder.Grow(length)

	var autoidx, idx Int

	for idx < length {
		char := tmpl[idx]
		if char == '\\' && idx+1 < length {
			next := tmpl[idx+1]
			if next == '{' || next == '}' {
				builder.WriteByte(next)
				idx += 2

				continue
			}
		}

		if char == '{' {
			cidx := tmpl[idx+1:].Index("}")
			if cidx.IsNegative() {
				builder.WriteByte(char)
				idx++

				continue
			}

			eidx := idx + 1 + cidx
			placeholder := tmpl[idx+1 : eidx]

			trimmed := placeholder.Trim()
			if trimmed.Empty() || trimmed[0] == '.' {
				autoidx++
				if autoidx <= positional.Len() {
					placeholder = autoidx.String() + trimmed
				}
			}

			replaced := processPlaceholder(placeholder, named, positional)
			builder.WriteString(replaced)

			idx = eidx + 1
		} else {
			builder.WriteByte(tmpl[idx])
			idx++
		}
	}

	return builder.String()
}

func processPlaceholder(placeholder String, named Named, positional Slice[any]) String {
	var (
		keyfall String
		mods    String
		key     String
		fall    String
	)

	if idx := placeholder.Index("."); idx.IsPositive() {
		keyfall = placeholder[:idx]
		mods = placeholder[idx+1:]
	} else {
		keyfall = placeholder
	}

	if idx := keyfall.Index("?"); idx.IsPositive() {
		key = keyfall[:idx]
		fall = keyfall[idx+1:]
	} else {
		key = keyfall
	}

	value := resolveValue(key, fall, named, positional)
	if value == nil {
		return "{" + placeholder + "}"
	}

	if mods.NotEmpty() {
		mods.
			Split(".").
			Exclude(f.IsZero).
			ForEach(func(segment String) {
				name, params := parseMod(segment)
				value = applyMod(value, name, params)
			})
	}

	return String(fmt.Sprint(value))
}

func resolveValue(key, fall String, named Named, positional Slice[any]) any {
	if num := key.ToInt(); num.IsOk() {
		idx := num.v - 1
		if idx.IsNegative() || idx.Gte(positional.Len()) {
			return nil
		}

		return positional[idx]
	}

	value := Map[String, any](named).Get(key)
	if value.IsNone() && fall.NotEmpty() {
		value = Map[String, any](named).Get(fall)
	}

	return value.UnwrapOrDefault()
}

func parseMod(segment String) (String, Slice[String]) {
	oidx := segment.Index("(")
	if oidx.IsNegative() {
		return segment, nil
	}

	cidx := segment.LastIndex(")")
	if cidx.Lt(oidx) {
		return segment, nil
	}

	params := segment[oidx+1 : cidx].Split(",").Collect()
	name := segment[:oidx]

	return name, params
}

func toType(param String, targetType reflect.Type) Result[reflect.Value] {
	switch targetType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i64, err := strconv.ParseInt(param.Std(), 10, targetType.Bits())
		if err != nil {
			return Err[reflect.Value](err)
		}
		return Ok(reflect.ValueOf(i64).Convert(targetType))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u64, err := strconv.ParseUint(param.Std(), 10, targetType.Bits())
		if err != nil {
			return Err[reflect.Value](err)
		}
		return Ok(reflect.ValueOf(u64).Convert(targetType))
	case reflect.Bool:
		b, err := strconv.ParseBool(param.Std())
		if err != nil {
			return Err[reflect.Value](err)
		}
		return Ok(reflect.ValueOf(b))
	case reflect.Float32, reflect.Float64:
		param = param.ReplaceAll("_", ".")
		fl, err := strconv.ParseFloat(param.Std(), targetType.Bits())
		if err != nil {
			return Err[reflect.Value](err)
		}
		return Ok(reflect.ValueOf(fl).Convert(targetType))
	default:
		switch targetType {
		case reflect.TypeOf(""):
			return Ok(reflect.ValueOf(param.Std()))
		case reflect.TypeOf(String("")):
			return Ok(reflect.ValueOf(param))
		default:
			return Err[reflect.Value](fmt.Errorf("unsupported type: %s", targetType))
		}
	}
}

func extractFromMapOrd(param String, slice reflect.Value) Option[any] {
	for slice.Kind() == reflect.Interface || slice.Kind() == reflect.Pointer {
		if slice.IsNil() {
			return None[any]()
		}
		slice = slice.Elem()
	}

	if !slice.IsValid() || slice.Kind() != reflect.Slice {
		return None[any]()
	}

	elemT := slice.Type().Elem()
	if elemT.Kind() != reflect.Struct {
		return None[any]()
	}

	if _, ok := elemT.FieldByName("Key"); !ok {
		return None[any]()
	}

	if _, ok := elemT.FieldByName("Value"); !ok {
		return None[any]()
	}

	ps := param.Std()

	var (
		pi    int
		pierr error
		pf    float64
		pferr error
	)

	for i := 0; i < slice.Len(); i++ {
		el := slice.Index(i)
		k := el.FieldByName("Key").Interface()
		v := el.FieldByName("Value").Interface()

		switch kk := k.(type) {
		case string:
			if kk == ps {
				return Some(v)
			}
		case String:
			if string(kk) == ps {
				return Some(v)
			}
		case int:
			if pi == 0 && pierr == nil {
				pi, pierr = strconv.Atoi(ps)
			}
			if pierr == nil && kk == pi {
				return Some(v)
			}
		case Int:
			if pi == 0 && pierr == nil {
				pi, pierr = strconv.Atoi(ps)
			}
			if pierr == nil && int(kk) == pi {
				return Some(v)
			}
		case float64:
			if pf == 0 && pferr == nil {
				pf, pferr = strconv.ParseFloat(param.ReplaceAll("_", ".").Std(), 64)
			}
			if pferr == nil && kk == pf {
				return Some(v)
			}
		case Float:
			if pf == 0 && pferr == nil {
				pf, pferr = strconv.ParseFloat(param.ReplaceAll("_", ".").Std(), 64)
			}
			if pferr == nil && float64(kk) == pf {
				return Some(v)
			}
		default:
			if s, ok := k.(fmt.Stringer); ok && s.String() == ps {
				return Some(v)
			}
		}
	}

	return None[any]()
}

func resolveIndirect(targetType reflect.Value) reflect.Value {
	for targetType.Kind() == reflect.Interface || targetType.Kind() == reflect.Pointer {
		if targetType.IsNil() {
			return reflect.Value{}
		}

		targetType = targetType.Elem()
	}

	return targetType
}

func callMethod(method reflect.Value, params Slice[String]) Option[any] {
	methodType := method.Type()
	numIn := methodType.NumIn()
	isVariadic := methodType.IsVariadic()

	if isVariadic {
		numIn--
	}

	var args []reflect.Value

	for i := range numIn {
		arg := toType(params[i], methodType.In(i))
		if arg.IsErr() {
			return None[any]()
		}

		args = append(args, arg.v)
	}

	if isVariadic {
		elemType := methodType.In(numIn).Elem()
		for _, param := range params[numIn:] {
			arg := toType(param, elemType)
			if arg.IsErr() {
				return None[any]()
			}

			args = append(args, arg.v)
		}
	}

	results := method.Call(args)

	if len(results) > 0 {
		return Some(results[0].Interface())
	}

	return None[any]()
}

func applyMod(value any, name String, params Slice[String]) any {
	switch name {
	case "type":
		return fmt.Sprintf("%T", value)
	case "debug":
		return fmt.Sprintf("%#v", value)
	}

	current := reflect.ValueOf(value)

	if method := current.MethodByName(name.Std()); method.IsValid() && method.Kind() == reflect.Func {
		if result := callMethod(method, params); result.IsSome() {
			return result.v
		}
		return value
	}

	for current.Kind() == reflect.Pointer || current.Kind() == reflect.Interface {
		if current.IsNil() {
			return value
		}
		current = current.Elem()
	}

	if method := current.MethodByName(name.Std()); method.IsValid() && method.Kind() == reflect.Func {
		if result := callMethod(method, params); result.IsSome() {
			return result.v
		}
		return value
	}

	switch current.Kind() {
	case reflect.Map:
		key := toType(name, current.Type().Key())
		if key.IsErr() {
			return value
		}

		current = resolveIndirect(current.MapIndex(key.v))
	case reflect.Slice, reflect.Array:
		if pair := extractFromMapOrd(name, current); pair.IsSome() {
			return pair.v
		}

		idx := name.ToInt()
		if idx.IsErr() || idx.v.Gte(Int(current.Len())) {
			return value
		}

		current = current.Index(idx.v.Std())
	case reflect.Struct:
		current = current.FieldByName(name.Std())
	}

	if current.IsValid() && current.CanInterface() {
		return current.Interface()
	}

	return value
}
