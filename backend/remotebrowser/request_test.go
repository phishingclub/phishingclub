package remotebrowser

import (
	"testing"

	"github.com/dop251/goja"
)

// TestRequestToMapNil proves a nil Request still yields a fully shaped object so
// a script reading request().country never hits undefined.
func TestRequestToMapNil(t *testing.T) {
	m := requestToMap(nil)
	for _, k := range []string{"ip", "country", "asns", "ja4", "userAgent", "acceptLanguage", "headers"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("nil request map missing key %q", k)
		}
	}
	if m["country"] != "" {
		t.Fatalf("expected empty country, got %v", m["country"])
	}
	if got := m["asns"].([]interface{}); len(got) != 0 {
		t.Fatalf("expected empty asns, got %v", got)
	}
}

// TestRequestToMapValues proves the fields map to the lowercase JS keys.
func TestRequestToMapValues(t *testing.T) {
	ri := &RequestInfo{
		IP:      "1.2.3.4",
		Country: "DE",
		ASNs:    []RequestASN{{Number: 16509, Name: "AMAZON-02"}},
		JA4:     "t13d1516h2_x",
		Headers: map[string]string{"user-agent": "UA"},
	}
	m := requestToMap(ri)
	if m["ip"] != "1.2.3.4" || m["country"] != "DE" || m["ja4"] != "t13d1516h2_x" {
		t.Fatalf("scalar fields wrong: %v", m)
	}
	asn := m["asns"].([]interface{})[0].(map[string]interface{})
	if asn["number"] != uint32(16509) || asn["name"] != "AMAZON-02" {
		t.Fatalf("asn mapping wrong: %v", asn)
	}
	if m["headers"].(map[string]interface{})["user-agent"] != "UA" {
		t.Fatalf("headers mapping wrong: %v", m["headers"])
	}
}

// TestRequestBindingGoja proves the request() binding round-trips into JS the
// way the runner wires it, so request().country and request().asns[0].name are
// readable from a script.
func TestRequestBindingGoja(t *testing.T) {
	r := &Runner{Request: &RequestInfo{
		IP:      "9.9.9.9",
		Country: "DK",
		ASNs:    []RequestASN{{Number: 15169, Name: "GOOGLE"}},
		Headers: map[string]string{"accept-language": "da-DK"},
	}}
	vm := goja.New()
	vm.Set("request", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(requestToMap(r.Request))
	})
	v, err := vm.RunString(`(function(){
		var r = request();
		return r.country + "|" + r.asns[0].name + "|" + r.headers["accept-language"];
	})()`)
	if err != nil {
		t.Fatalf("script error: %v", err)
	}
	if got := v.String(); got != "DK|GOOGLE|da-DK" {
		t.Fatalf("unexpected: %q", got)
	}
}
