// validate package is a collection of validation functions
// the validation errors are carried to the frontened
// so be mindful of the error messages by making them user friendly
package validate

import (
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/utils"
)

// ErrorIfStringEqual checks if two strings are equal
func ErrorIfStringEqual(a string, b string) error {
	if a == b {
		return errs.NewValidationError(
			fmt.Errorf(
				"must not be equal to %s",
				b,
			),
		)
	}
	return nil
}

// StringGreaterThan checks if a string is not empty
func StringGreaterThan(s string, length int) bool {
	return len(s) > length
}

// ErrorIfStringGreaterThan checks if a string is not empty and
// returns an error if it is
func ErrorIfStringGreaterThan(s string, length int) error {
	if StringGreaterThan(s, length) {
		return errs.NewValidationError(
			fmt.Errorf(
				"is greater than %d",
				length,
			),
		)
	}
	return nil
}

// StringLessThan checks if a string is not empty
func StringLessThan(s string, length int) bool {
	return len(s) < length
}

// ErrorIfStringLessThan checks if a string is not empty
// and returns an error if it is
func ErrorIfStringLessThan(s string, length int) error {
	if StringLessThan(s, length) {
		return errs.NewValidationError(
			fmt.Errorf(
				"is less than %d characters",
				length,
			),
		)
	}
	return nil
}

// StringBetween checks if a string is between min and max
func StringBetween(s string, min int, max int) bool {
	return len(s) > min && len(s) < max
}

// StringBetweenOrEqualTo checks if a string is between or equal to min and max
func StringBetweenOrEqualTo(s string, min int, max int) bool {
	return len(s) >= min && len(s) <= max
}

// ErrorIfStringBetween checks if a string is between min and max
// and returns an error if it is not
func ErrorIfStringNotBetween(s string, min int, max int) error {
	if StringBetween(s, min, max) {
		return nil
	}
	return errs.NewValidationError(
		fmt.Errorf(
			"must be between %d and %d characters",
			min,
			max,
		),
	)
}

// ErrorIfStringNotbetweenOrEqualTo checks if a string is between or equal to min and max
// and returns an error if it is not
func ErrorIfStringNotbetweenOrEqualTo(s string, min int, max int) error {
	if StringBetweenOrEqualTo(s, min, max) {
		return nil
	}
	return errs.NewValidationError(
		fmt.Errorf(
			"must be between %d and %d characters",
			min,
			max,
		),
	)
}

// ErrorIfIntEqual checks if two ints are equal
func ErrorIfIntEqual(a int, b int) error {
	if a == b {
		return errs.NewValidationError(
			fmt.Errorf("must not be equal to %d", b),
		)
	}
	return nil
}

// ErrorIfLessThan checks if an int is less than
func ErrorIfLessThan(a int, b int) error {
	if a < b {
		return errs.NewValidationError(
			fmt.Errorf("must be greater than or equal to %d", b),
		)
	}
	return nil
}

// ErrorIfIntLargerThan checks if an int is larger than
func ErrorIfIntLargerThan(a int, b int) error {
	if a > b {
		return errs.NewValidationError(
			fmt.Errorf("must be less than or equal to %d", b),
		)
	}
	return nil
}

// ErrorIfIntEqualOrLargerThan checks if an int is larger than
func ErrorIfIntEqualOrLargerThan(a int, b int) error {
	if a >= b {
		return errs.NewValidationError(
			fmt.Errorf("must be less than %d", b),
		)
	}
	return nil
}

// ErrorIfIntEqualOrLessThan checks if an int is less than
func ErrorIfIntEqualOrLessThan(a int, b int) error {
	if a <= b {
		return errs.NewValidationError(
			fmt.Errorf("must be greater than %d but is %d", b, a),
		)
	}
	return nil
}

// ErrorIfNotbetweenOrEqualTo checks if a string is between or equal to min and max
// and returns an error if it is not
func ErrorIfNotbetweenOrEqualTo(s, min, max int) error {
	if (s >= min) && (s <= max) {
		return nil
	}
	return errs.NewValidationError(
		fmt.Errorf(
			"must be between %d and %d",
			min,
			max,
		),
	)
}

// ErrorIfNil
func ErrorIfNil(i any) error {
	if i == nil {
		return errs.NewValidationError(
			fmt.Errorf("must not be nil"),
		)
	}
	return nil
}

// ErrorIfFailsToParseUUID
func ErrorIfFailsToParseUUID(s string) (*uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, errs.NewValidationError(
			fmt.Errorf("must be a valid uuid"),
		)
	}
	if uuid.Nil == id {
		return nil, errs.NewValidationError(
			fmt.Errorf("must not be nil UUID"),
		)
	}
	return &id, nil
}

// UuidIsNil checks if a uuid is not nil
func UuidIsNil(id uuid.UUID) bool {
	return id == uuid.Nil
}

// ErrorIfUuidIsNil checks if a uuid is not nil
func ErrorIfUuidIsNil(id uuid.UUID) error {
	if UuidIsNil(id) {
		return errs.NewValidationError(
			fmt.Errorf("uuid must not be nil"),
		)
	}
	return nil
}

// UuidRefIsNilOrZero checks if a uuid is not nil and not zero value
func UuidRefIsNilOrZero(id *uuid.UUID) bool {
	return id == nil || *id == uuid.Nil
}

// ErrorIfUuidRefIsNilOrZero checks if a uuid is not nil and not zero value
func ErrorIfUuidRefIsNilOrZero(id *uuid.UUID) error {
	if UuidRefIsNilOrZero(id) {
		return errs.NewValidationError(
			fmt.Errorf("uuid must not be nil or zero valued"),
		)
	}
	return nil
}

// TimeRefIsNilOrZero checks if a time is nil or zero value
func TimeRefIsNilOrZero(t *time.Time) bool {
	return t == nil || t.IsZero()
}

// ErrorIfTimeRefIsNilOrZero checks if a time is nil or zero value
func ErrorIfTimeRefIsNilOrZero(t *time.Time) error {
	if TimeRefIsNilOrZero(t) {
		return errs.NewValidationError(
			fmt.Errorf("must not be nil or zero valued"),
		)
	}
	return nil
}

// TimeIsNil checks if a time is not nil
func TimeIsNil(t time.Time) bool {
	return t.IsZero()
}

// ErrorIfTimeIsNil checks if a time is not nil
func ErrorIfTimeIsNil(t time.Time) error {
	if TimeIsNil(t) {
		return errs.NewValidationError(
			fmt.Errorf("must not be nil"),
		)
	}
	return nil
}

// IsAlphaNumeric checks if a string is alphanumeric
func IsAlphaNumeric(s string) bool {
	return regexp.MustCompile(`^[a-zA-Z0-9]+$`).MatchString(s)
}

// ErrorIfNotAlphaNumeric checks if a string is alphanumeric
// and returns an error if it is not
func ErrorIfNotAlphaNumeric(s string) error {
	if IsAlphaNumeric(s) {
		return nil
	}
	return errs.NewValidationError(
		fmt.Errorf("must be alphanumeric"),
	)
}

// IsValidEmail checks if a string is a valid email
func ErrorIfMailInvalid(s string) error {
	const min = 5
	const max = 254
	l := len(s)
	if l < min || l > max {
		return errs.NewValidationError(
			fmt.Errorf(
				"must be between %d and %d characters",
				min,
				max,
			),
		)
	}
	// Check is mail RFC 5322 (and extension by RFC 6532) valid
	_, err := mail.ParseAddress(s)
	if err != nil {
		// Remove the "mail:" prefix from the error message
		err = errors.New(strings.TrimPrefix(err.Error(), "mail:"))
		return errs.NewValidationError(err)
	}

	r := `^.+@.+\..+`
	pattern := regexp.MustCompile(r)
	// check if the email address matches the pattern.
	if !pattern.MatchString(s) {
		return errs.NewValidationError(
			fmt.Errorf(
				"simple pattern '%s' failed",
				r,
			),
		)
	}
	return nil
}

// ErrorIfStringNotMatch checks if a string matches a pattern
func ErrorIfStringNotMatch(s string, r string) error {
	pattern := regexp.MustCompile(r)
	if !pattern.MatchString(s) {
		return errs.NewValidationError(
			fmt.Errorf(
				"pattern '%s' failed",
				r,
			),
		)
	}
	return nil
}

// ErrorIfStringEmpty checks if a string is empty
func ErrorIfStringEmpty(s string) error {
	if s == "" {
		return errs.NewValidationError(
			errors.New(
				"must not be empty",
			),
		)
	}
	return nil
}

// ErrorIfNotContains checks if a slice of strings contains a string
func ErrorIfNotContains(s []string, v string) error {
	if !slices.Contains(s, v) {
		return errs.NewValidationError(
			fmt.Errorf(
				"must contain %s",
				v,
			),
		)

	}
	return nil
}

// WrapErrorWithField wraps an error with a field name
func WrapErrorWithField(err error, field string) error {
	return errs.NewValidationError(
		fmt.Errorf(
			"%s: %w",
			field,
			err,
		),
	)
}

// D validates the id is not nil or zero and returns an error if it is
// with a field error indicator
func ID(id *uuid.UUID) error {
	if err := ErrorIfUuidRefIsNilOrZero(id); err != nil {
		return WrapErrorWithField(err, "id")
	}
	return nil
}

// NotNil validates the id is not nil or zero and returns an error if it is
// with a field error indicator
func NotNilField(value any, key string) error {
	if err := ErrorIfNil(value); err != nil {
		return WrapErrorWithField(err, key)
	}
	return nil
}

// NullableFieldRequired validates the field is not nil or zero and returns an error if it is
func NullableFieldRequired[T any](fieldName string, value nullable.Nullable[T]) error {
	if !value.IsSpecified() || value.IsNull() {
		return WrapErrorWithField(
			errs.NewValidationError(
				errors.New("required"),
			),
			fieldName,
		)
	}
	return nil
}

// OneOfNullableFieldsRequired validates that one of the fields is supplied and not null
// input is a map of map[string]nullable.Nullable[T] where the string is the fieldname of T
func OneOfNullableFieldsRequired(fields map[string]any) error {
	for _, v := range fields {
		v, ok := v.(nullable.Nullable[any])
		// if any field is not castable to nullable.Nullable[any] then
		// break and return the error
		if !ok {
			continue
		}
		if v.IsSpecified() && !v.IsNull() {
			return nil
		}
	}
	keys := utils.MapKeys(fields)
	return fmt.Errorf("one of the fields (%s) must be supplied", strings.Join(keys, ", "))
}
