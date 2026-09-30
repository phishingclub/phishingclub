package service

import "testing"

// TestValidateSessionScript checks that a config naming a session script is
// only accepted when the Scripts feature is enabled on the instance.
func TestValidateSessionScript(t *testing.T) {
	cases := []struct {
		name    string
		script  string
		enabled bool
		wantErr bool
	}{
		{"empty script, feature off", "", false, false},
		{"empty script, feature on", "", true, false},
		{"whitespace script, feature off", "   ", false, false},
		{"script set, feature on", "pick-proxy", true, false},
		{"script set, feature off", "pick-proxy", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Proxy{ScriptEnabled: tc.enabled}
			err := m.validateSessionScript(&ProxyServiceConfigYAML{Script: tc.script})
			if tc.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
