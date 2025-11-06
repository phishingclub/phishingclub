package g

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Ok returns a new Result[T] containing the given value.
func Ok[T any](value T) Result[T] { return Result[T]{v: value} }

// Err returns a new Result[T] containing the given error.
func Err[T any](err error) Result[T] {
	if err == nil {
		err = errors.New("g.Err called with a nil error")
	}

	return Result[T]{err: err}
}

// ResultOf returns a new Result[T] based on the provided value and error.
// If err is not nil, it returns an Err Result.
// Otherwise, it returns an Ok Result.
func ResultOf[T any](value T, err error) Result[T] {
	if err != nil {
		return Err[T](err)
	}

	return Ok(value)
}

// TransformResult applies a function to the contained Ok value, returning a new Result.
// If the input Result is Err, the error is propagated.
// This is also known as 'and_then' or 'flat_map'.
func TransformResult[T, U any](r Result[T], fn func(T) Result[U]) Result[U] {
	if r.IsOk() {
		return fn(r.v)
	}

	return Err[U](r.err)
}

// TransformResultOf applies a function that returns a (value, error) tuple to the contained Ok value.
// If the input Result is Err, the error is propagated.
func TransformResultOf[T, U any](r Result[T], fn func(T) (U, error)) Result[U] {
	if r.IsOk() {
		return ResultOf(fn(r.v))
	}

	return Err[U](r.err)
}

// Ok returns the value held in the Result.
//
// WARNING: If the Result contains an error, this method will return the zero value
// for type T. Always check IsOk() before calling this method, or use safer alternatives
// like Result(), Unwrap(), or UnwrapOr().
func (r Result[T]) Ok() T { return r.v }

// Err returns the error held in the Result. If the result is Ok, it returns nil.
func (r Result[T]) Err() error { return r.err }

// IsOk returns true if the Result contains a value (no error).
func (r Result[T]) IsOk() bool { return r.err == nil }

// IsErr returns true if the Result contains an error.
func (r Result[T]) IsErr() bool { return r.err != nil }

// Result returns the value and error, conforming to the standard Go multi-value return pattern.
func (r Result[T]) Result() (T, error) {
	if r.IsOk() {
		return r.v, nil
	}

	var zero T
	return zero, r.err
}

// Unwrap returns the value held in the Result. If the Result is Err, it panics.
func (r Result[T]) Unwrap() T {
	if r.IsOk() {
		return r.v
	}

	if pc, file, line, ok := runtime.Caller(1); ok {
		out := fmt.Sprintf(
			"[%s:%d] [%s] unwrapped an Err value: %v", filepath.Base(file), line, runtime.FuncForPC(pc).Name(), r.err)
		fmt.Fprintln(os.Stderr, out)
	}

	panic(r.err)
}

// UnwrapOr returns the value held in the Result. If the Result is Err, it returns the provided default value.
func (r Result[T]) UnwrapOr(value T) T {
	if r.IsOk() {
		return r.v
	}

	return value
}

// UnwrapOrDefault returns the contained value if Ok, otherwise returns the zero value for T.
func (r Result[T]) UnwrapOrDefault() T {
	if r.IsOk() {
		return r.v
	}

	var zero T
	return zero
}

// Expect returns the value held in the Result. If the Result is Err, it panics with the provided message.
func (r Result[T]) Expect(msg string) T {
	if r.IsOk() {
		return r.v
	}

	out := fmt.Sprintf("Expect() failed: %s: %v", msg, r.err)
	fmt.Fprintln(os.Stderr, out)
	panic(out)
}

// Then applies a function to the contained value (if Ok) and returns the result.
// If the Result is Err, it returns the same Err without applying the function.
func (r Result[T]) Then(fn func(T) Result[T]) Result[T] {
	if r.IsOk() {
		return fn(r.v)
	}

	return r
}

// ThenOf applies a function to the contained value (if Ok) and returns a new Result
// based on the returned (T, error) tuple.
func (r Result[T]) ThenOf(fn func(T) (T, error)) Result[T] {
	if r.IsOk() {
		return ResultOf(fn(r.v))
	}

	return r
}

// MapErr transforms the error in an Err Result by applying a function to it.
// It is useful for custom error handling, like replacing one error with another.
// If the Result is Ok, it does nothing.
func (r Result[T]) MapErr(fn func(error) error) Result[T] {
	if r.IsErr() {
		return Err[T](fn(r.err))
	}

	return r
}

// Option converts a Result into an Option.
// If the Result is Ok, it returns Some(value).
// If the Result is Err, it returns None.
func (r Result[T]) Option() Option[T] {
	if r.IsOk() {
		return Some(r.v)
	}

	return None[T]()
}

// String returns a string representation of the Result.
func (r Result[T]) String() string {
	if r.IsOk() {
		return fmt.Sprintf("Ok(%v)", r.v)
	}

	return fmt.Sprintf("Err(%v)", r.err)
}
