package controller

import "testing"

func TestRunnerHostMatches(t *testing.T) {
	cases := []struct {
		host, domain string
		want         bool
	}{
		{"runner.example.test", "runner.example.test", true},
		{"Runner.Example.Test.", "runner.example.test", true},
		{"runner.example.test:443", "runner.example.test", true},
		{"other.example.test", "runner.example.test", false},
		{"runner.example.test.evil.test", "runner.example.test", false},
		{"runner.example.test", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := runnerHostMatches(c.host, c.domain); got != c.want {
			t.Errorf("runnerHostMatches(%q, %q) = %v", c.host, c.domain, got)
		}
	}
}
