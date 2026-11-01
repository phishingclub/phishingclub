package model

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/vo"
)

func TestValidateAssetHeader(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		ok    bool
	}{
		{"plain header", "X-Frame-Options", "DENY", true},
		{"set cookie", "Set-Cookie", "sid=abc; Path=/; HttpOnly", true},
		{"lower case name", "cache-control", "no-store", true},
		{"tab in value", "X-Test", "a\tb", true},
		{"space in name", "X Test", "v", false},
		{"colon in name", "X-Test:", "v", false},
		{"empty name", "", "v", false},
		{"cr in value", "X-Test", "a\rb", false},
		{"lf in value", "X-Test", "a\nSet-Cookie: x=y", false},
		{"nul in value", "X-Test", "a\x00b", false},
		{"other control in value", "X-Test", "a\x01b", false},
		{"content length", "Content-Length", "1", false},
		{"transfer encoding", "transfer-encoding", "chunked", false},
		{"connection", "Connection", "close", false},
		{"keep alive", "Keep-Alive", "timeout=5", false},
		{"upgrade", "Upgrade", "websocket", false},
		{"te", "TE", "trailers", false},
		{"trailer", "Trailer", "X-A", false},
		{"proxy connection", "Proxy-Connection", "close", false},
		{"content type", "content-type", "text/html", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAssetHeader(tt.key, tt.value)
			if (err == nil) != tt.ok {
				t.Fatalf("ValidateAssetHeader(%q, %q) err = %v, want ok %v", tt.key, tt.value, err, tt.ok)
			}
		})
	}
}

func TestValidateAssetContentType(t *testing.T) {
	tests := []struct {
		value string
		ok    bool
	}{
		{"", true},
		{"text/html", true},
		{"text/html; charset=utf-8", true},
		{"application/octet-stream", true},
		{"image/svg+xml", true},
		{"text", false},
		{"text/html; charset", false},
		{"text/html\r\nX-A: b", false},
		{"/html", false},
	}
	for _, tt := range tests {
		err := ValidateAssetContentType(tt.value)
		if (err == nil) != tt.ok {
			t.Fatalf("ValidateAssetContentType(%q) err = %v, want ok %v", tt.value, err, tt.ok)
		}
	}
}

func TestAssetHeaderValidateRequiresFields(t *testing.T) {
	h := &AssetHeader{
		AssetID: nullable.NewNullableWithValue(uuid.New()),
		Key:     nullable.NewNullableWithValue(*vo.NewString127Must("X-Test")),
	}
	if err := h.Validate(); err == nil {
		t.Fatalf("missing value was accepted")
	}
	h.Value = nullable.NewNullableWithValue(*vo.NewString2048Must("ok"))
	if err := h.Validate(); err != nil {
		t.Fatalf("valid header rejected: %v", err)
	}
	h.Key = nullable.NewNullableWithValue(*vo.NewString127Must("Content-Length"))
	if err := h.Validate(); err == nil {
		t.Fatalf("blocked header accepted")
	}
}

func TestAssetValidateContentType(t *testing.T) {
	a := &Asset{
		Name:        nullable.NewNullableWithValue(*vo.NewOptionalString127Must("a")),
		ContentType: nullable.NewNullableWithValue(*vo.NewOptionalString255Must("text/plain")),
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("valid content type rejected: %v", err)
	}
	a.ContentType = nullable.NewNullableWithValue(*vo.NewOptionalString255Must("not a type"))
	if err := a.Validate(); err == nil || !strings.Contains(err.Error(), "content") {
		t.Fatalf("invalid content type accepted: %v", err)
	}
	// stored content type is written on update, empty clears it
	a.ContentType = nullable.NewNullableWithValue(*vo.NewEmptyOptionalString255())
	if got := a.ToDBMap()["content_type"]; got != "" {
		t.Fatalf("content_type = %v, want empty", got)
	}
}
