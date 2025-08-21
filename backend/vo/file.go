package vo

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/phishingclub/phishingclub/validate"
)

type FileName struct {
	inner string
}

// NewFileName creates a new file name
func NewFileName(p string) (*FileName, error) {
	p = strings.TrimSpace(p)
	p = path.Clean(p)
	// string can not start with /, ./ or ..
	err := validate.ErrorIfStringNotbetweenOrEqualTo(p, 1, 255)
	if err != nil {
		return nil, fmt.Errorf("invalid file name: %w", err)
	}

	// allow only a-zA-Z0-9_-. and space
	err = validate.ErrorIfStringNotMatch(p, `^[a-zA-Z0-9_\-.\s]+$`)
	if err != nil {
		// Find and return the illegal characters
		illegalChars := ""
		for _, char := range p {
			if !strings.ContainsRune(`abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-. `, char) {
				illegalChars += string(char)
			}
		}
		return nil, fmt.Errorf("invalid file name - Allows is characters, numbers, _, -, . and space. Illegal characters: %s", illegalChars)
	}
	return &FileName{
		inner: p,
	}, nil
}

// NewFileNameMust creates a new file name and panics if it fails
func NewFileNameMust(p string) *FileName {
	f, err := NewFileName(p)
	if err != nil {
		panic(err)
	}
	return f
}

// MarshalJSON implements the json.Marshaler interface
func (s FileName) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *FileName) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	v, err := NewFileName(str)
	if err != nil {
		return err
	}
	s.inner = v.inner
	return nil
}

// String returns the string representation of the file name
func (f FileName) String() string {
	return f.inner
}

type RelativeFilePath struct {
	inner string
}

// NewRelativeFilePath creates a new file path
func NewRelativeFilePath(p string) (*RelativeFilePath, error) {
	p = strings.TrimSpace(p)
	p = path.Clean(p)
	// string can not start with /, ./ or ..
	err := validate.ErrorIfStringNotbetweenOrEqualTo(p, 1, 512)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// allow only a-zA-Z0-9_-. and / and space
	err = validate.ErrorIfStringNotMatch(p, `^[a-zA-Z0-9_\-./\s]+$`)
	if err != nil {
		// Find and return the illegal characters
		illegalChars := ""
		for _, char := range p {
			if !strings.ContainsRune(`abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-. /`, char) {
				illegalChars += string(char)
			}
		}
		return nil, fmt.Errorf("Invalid path. Invalid characters: %s", illegalChars)
	}
	return &RelativeFilePath{
		inner: p,
	}, nil
}

// NewRelativeFilePathMust creates a new file path and panics if it fails
func NewRelativeFilePathMust(p string) *RelativeFilePath {
	f, err := NewRelativeFilePath(p)
	if err != nil {
		panic(err)
	}
	return f
}

// MarshalJSON implements the json.Marshaler interface
func (s RelativeFilePath) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *RelativeFilePath) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	v, err := NewRelativeFilePath(str)
	if err != nil {
		return err
	}
	s.inner = v.inner
	return nil
}

// String returns the string representation of the file path
func (f RelativeFilePath) String() string {
	return f.inner
}

type OptionalRelativePath struct {
	inner string
}

// NewOptionalRelativePath creates a new optional file path that is realive
// from another folder. We make this shorter than the full path to avoid that
// the path in full path is longer than its limits, as this path is used in
// the full path along with its parent folders.
func NewOptionalRelativePath(p string) (*OptionalRelativePath, error) {
	p = strings.TrimSpace(p)
	p = path.Clean(p)
	if len(p) > 0 {
		err := validate.ErrorIfStringNotbetweenOrEqualTo(p, 1, 255)
		if err != nil {
			return nil, fmt.Errorf("invalid file path: %w", err)
		}
		// allow only a-zA-Z0-9_-. and /
		err = validate.ErrorIfStringNotMatch(p, `^[a-zA-Z0-9_\-./]+$`)
		if err != nil {
			_ = err
			return nil, fmt.Errorf("invalid path - Allows is characters, numbers and _, -, . and /")
		}
	}
	return &OptionalRelativePath{
		inner: p,
	}, nil
}

// MarshalJSON implements the json.Marshaler interface
func (s OptionalRelativePath) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.inner)
}

// UnmarshalJSON unmarshals the json into a string
func (s *OptionalRelativePath) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	v, err := NewOptionalRelativePath(str)
	if err != nil {
		return err
	}
	s.inner = v.inner
	return nil
}

// String returns the string representation of the optional file path
func (o OptionalRelativePath) String() string {
	return o.inner
}
