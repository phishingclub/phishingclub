package vo

import "github.com/go-errors/errors"

func unwrapError(err error) error {
	unwrapped := errors.Unwrap(err)
	if unwrapped == nil {
		return err
	}
	return unwrapped
}
