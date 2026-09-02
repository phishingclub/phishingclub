package utils

import (
	"encoding/json"
	"errors"
	"testing"
)

// bounded mirrors the value object pattern of validating inside UnmarshalJSON
type bounded struct{ v string }

func (b *bounded) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if len(s) > 4 {
		return errors.New("must be at most 4 characters")
	}
	b.v = s
	return nil
}

type embedded struct {
	Label bounded `json:"label"`
}

type request struct {
	embedded
	Name  bounded  `json:"name"`
	Opt   *bounded `json:"opt"`
	Age   int      `json:"age"`
	Inner struct {
		Name bounded `json:"name"`
	} `json:"inner"`
	Items []struct {
		Name bounded `json:"name"`
	} `json:"items"`
	Tags map[string]bounded `json:"tags"`
}

func TestUnmarshalFieldPaths(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"valid", `{"name":"ok","age":1}`, ""},
		{"top level field", `{"name":"toolong"}`, "name: must be at most 4 characters"},
		{"case insensitive key", `{"NAME":"toolong"}`, "name: must be at most 4 characters"},
		{"embedded promoted", `{"label":"toolong"}`, "label: must be at most 4 characters"},
		{"pointer field", `{"opt":"toolong"}`, "opt: must be at most 4 characters"},
		{"pointer null", `{"opt":null}`, ""},
		{"first invalid wins", `{"age":1,"name":"toolong","label":"toolong"}`, "name: must be at most 4 characters"},
		{"nested", `{"inner":{"name":"toolong"}}`, "inner.name: must be at most 4 characters"},
		{"slice element", `{"items":[{"name":"ok"},{"name":"toolong"}]}`, "items.name: must be at most 4 characters"},
		{"map value", `{"tags":{"k":"toolong"}}`, "tags: must be at most 4 characters"},
		{"unknown field ignored", `{"nope":1}`, ""},
		{"syntax", `{"name":}`, "invalid character '}' looking for beginning of value"},
		{"empty", ``, "unexpected end of JSON input"},
		{"type mismatch plain", `{"age":"x"}`, "age: expected number, got string"},
		{"type mismatch nested", `{"inner":{"age":1,"name":5}}`, "inner.name: expected string, got number"},
		{"type mismatch inside unmarshaler", `{"name":5}`, "name: expected string, got number"},
		{"number does not fit", `{"age":1.5}`, "age: invalid number 1.5"},
		{"top level wrong kind", `[1]`, "expected object, got array"},
	}
	for _, c := range cases {
		var req any = &request{}
		err := Unmarshal([]byte(c.body), &req)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != c.want {
			t.Errorf("%s:\n got: %q\nwant: %q", c.name, got, c.want)
		}
	}
}
