package service

import (
	"bytes"
	"testing"
	"text/template"
)

func TestHostUpper(t *testing.T) {
	cases := []struct {
		name    string
		letters string
		args    []any
		want    string
	}{
		{"all matches", "i", []any{"https://mizzil.test/p/AbCiDi"}, "https://mIzzIl.test/p/AbCiDi"},
		{"first match", "i", []any{1, "https://mizzil.test/p/AbCiDi"}, "https://mIzzil.test/p/AbCiDi"},
		{"second match", "i", []any{2, "https://mizzil.test/p/AbCiDi"}, "https://mizzIl.test/p/AbCiDi"},
		{"occurrence past the end", "i", []any{3, "https://mizzil.test/x"}, "https://mizzil.test/x"},
		{"several letters", "il", []any{"https://mizzil.test/"}, "https://mizzIL.test/"},
		{"letters matched ignoring case", "I", []any{1, "https://mizzil.test"}, "https://mIzzil.test"},
		{"not found is unchanged", "q", []any{"https://mizzil.test/?id=1"}, "https://mizzil.test/?id=1"},
		{"query untouched", "i", []any{"https://mizzil.test/?id=iii&next=https://mizzil.test/"}, "https://mIzzIl.test/?id=iii&next=https://mizzil.test/"},
		{"fragment untouched", "i", []any{"https://mizzil.test#i"}, "https://mIzzIl.test#i"},
		{"port kept", "i", []any{"https://mizzil.test:8443/i"}, "https://mIzzIl.test:8443/i"},
		{"user info untouched", "i", []any{"https://i:i@mizzil.test/"}, "https://i:i@mIzzIl.test/"},
		{"subdomain counts as host", "i", []any{2, "https://login.mizzil.test/"}, "https://login.mIzzil.test/"},
		{"existing upper kept", "i", []any{2, "https://mIzzil.test/"}, "https://mIzzIl.test/"},
		{"relative path unchanged", "i", []any{"/missing/i"}, "/missing/i"},
		{"query url in relative path unchanged", "i", []any{"/p?r=https://mizzil.test"}, "/p?r=https://mizzil.test"},
		{"ipv4 unchanged", "1", []any{"https://10.0.0.1/"}, "https://10.0.0.1/"},
		{"ipv6 unchanged", "a", []any{"https://[fe80::a]/"}, "https://[fe80::a]/"},
		{"empty url unchanged", "i", []any{""}, ""},
		{"backslash ends the host", "b", []any{`https://a.test\@b.test/b`}, `https://a.test\@b.test/b`},
		{"non letter bytes kept", "-", []any{"https://mi-zz.test/"}, "https://mi-zz.test/"},
	}
	for _, tc := range cases {
		got, err := HostUpper(tc.letters, tc.args...)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHostUpperErrors(t *testing.T) {
	cases := []struct {
		name    string
		letters string
		args    []any
	}{
		{"empty letters", "", []any{"https://a.test"}},
		{"no url", "i", nil},
		{"too many args", "i", []any{1, 2, "https://a.test"}},
		{"zero occurrence", "i", []any{0, "https://a.test"}},
		{"negative occurrence", "i", []any{-1, "https://a.test"}},
		{"occurrence not int", "i", []any{"1", "https://a.test"}},
		{"url not string", "i", []any{1}},
	}
	for _, tc := range cases {
		if _, err := HostUpper(tc.letters, tc.args...); err == nil {
			t.Errorf("%s: expected an error", tc.name)
		}
	}
}

func TestHostUpperInTemplate(t *testing.T) {
	data := map[string]any{"URL": "https://mizzil.test/path/AbCi"}
	cases := map[string]string{
		`{{.URL | hostUpper "i"}}`:   "https://mIzzIl.test/path/AbCi",
		`{{.URL | hostUpper "i" 1}}`: "https://mIzzil.test/path/AbCi",
		`{{.URL | hostUpper "i" 2}}`: "https://mizzIl.test/path/AbCi",
		`{{hostUpper "i" 2 .URL}}`:   "https://mizzIl.test/path/AbCi",
	}
	for src, want := range cases {
		tmpl, err := template.New("t").Funcs(TemplateFuncs()).Parse(src)
		if err != nil {
			t.Fatalf("%s: parse: %v", src, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			t.Fatalf("%s: execute: %v", src, err)
		}
		if buf.String() != want {
			t.Errorf("%s: got %q, want %q", src, buf.String(), want)
		}
	}
}
