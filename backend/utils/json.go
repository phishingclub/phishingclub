package utils

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

var (
	unmarshalerType     = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

// Unmarshal decodes JSON with the standard library and, when a custom
// UnmarshalJSON method rejects a value, prefixes the error with the dotted
// JSON field path so the caller can tell which field was invalid.
func Unmarshal(data []byte, v any) error {
	err := json.Unmarshal(data, v)
	if err == nil {
		return nil
	}
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return err
	}
	var typ *json.UnmarshalTypeError
	if errors.As(err, &typ) {
		return sanitizeTypeError(typ, nil)
	}
	t, ok := targetType(v)
	if !ok {
		return err
	}
	path, located := locate(data, t)
	if located == nil {
		return err
	}
	if errors.As(located, &typ) {
		return sanitizeTypeError(typ, path)
	}
	if len(path) == 0 {
		return located
	}
	return fmt.Errorf("%s: %v", strings.Join(path, "."), located)
}

// targetType unwraps pointers and interfaces to the concrete type being decoded into
func targetType(v any) (reflect.Type, bool) {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() {
		return nil, false
	}
	return rv.Type(), true
}

// locate finds the first value in document order that fails to decode into t
// and returns the JSON field path leading to it
func locate(raw []byte, t reflect.Type) ([]string, error) {
	if reflect.PointerTo(t).Implements(unmarshalerType) || t.Implements(unmarshalerType) {
		return nil, json.Unmarshal(raw, reflect.New(t).Interface())
	}
	if t.Kind() == reflect.Pointer {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, nil
		}
		return locate(raw, t.Elem())
	}
	switch t.Kind() {
	case reflect.Struct:
		return locateStruct(raw, t)
	case reflect.Slice, reflect.Array:
		var elems []json.RawMessage
		if err := json.Unmarshal(raw, &elems); err != nil {
			return nil, json.Unmarshal(raw, reflect.New(t).Interface())
		}
		for _, e := range elems {
			if path, err := locate(e, t.Elem()); err != nil {
				return path, err
			}
		}
	case reflect.Map:
		var elems map[string]json.RawMessage
		if err := json.Unmarshal(raw, &elems); err != nil {
			return nil, json.Unmarshal(raw, reflect.New(t).Interface())
		}
		for _, e := range elems {
			if path, err := locate(e, t.Elem()); err != nil {
				return path, err
			}
		}
	}
	return nil, json.Unmarshal(raw, reflect.New(t).Interface())
}

func locateStruct(raw []byte, t reflect.Type) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, json.Unmarshal(raw, reflect.New(t).Interface())
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		// decode this single member into a fresh struct so the standard
		// library performs the field matching
		probe, _ := json.Marshal(map[string]json.RawMessage{key: val})
		if err := json.Unmarshal(probe, reflect.New(t).Interface()); err == nil {
			continue
		}
		name, ft, found := resolveField(t, key)
		if !found {
			return nil, json.Unmarshal(probe, reflect.New(t).Interface())
		}
		path, ferr := locate(val, ft)
		if ferr == nil {
			ferr = json.Unmarshal(probe, reflect.New(t).Interface())
		}
		return append([]string{name}, path...), ferr
	}
	return nil, nil
}

// resolveField finds the struct field the standard library would match for
// key and returns its canonical JSON name and type
func resolveField(t reflect.Type, key string) (string, reflect.Type, bool) {
	type cand struct {
		name string
		typ  reflect.Type
	}
	var cands []cand
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if f.Anonymous && name == "" {
				ft := f.Type
				if ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if ft.Kind() == reflect.Struct {
					walk(ft)
					continue
				}
			}
			if !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			cands = append(cands, cand{name, f.Type})
		}
	}
	walk(t)
	for _, c := range cands {
		if c.name == key {
			return c.name, c.typ, true
		}
	}
	for _, c := range cands {
		if strings.EqualFold(c.name, key) {
			return c.name, c.typ, true
		}
	}
	return "", nil, false
}

// sanitizeTypeError rewrites a type mismatch so the message names the JSON
// field and the expected JSON kind without exposing Go type or struct names
func sanitizeTypeError(e *json.UnmarshalTypeError, path []string) error {
	field := e.Field
	if len(path) > 0 {
		field = strings.Join(path, ".")
	}
	expected := jsonKind(e.Type)
	got := e.Value
	// a number that does not fit the target carries the literal in Value
	if strings.HasPrefix(got, "number ") && expected == "number" {
		got = "invalid " + got
	} else {
		got = "expected " + expected + ", got " + got
	}
	if field == "" {
		return errors.New(got)
	}
	return fmt.Errorf("%s: %s", field, got)
}

func jsonKind(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	// types decoded from a JSON string through UnmarshalText, such as uuid.UUID
	if reflect.PointerTo(t).Implements(textUnmarshalerType) {
		return "string"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Struct, reflect.Map:
		return "object"
	}
	return "value"
}
