package vo

import (
	"strings"
	"testing"
)

func TestNewCampaignTimeZone(t *testing.T) {
	valid := []string{
		"UTC", "Etc/UTC", "Europe/Copenhagen", "America/Argentina/Buenos_Aires", "Etc/GMT+5",
		"Pacific/Chatham", "Antarctica/Troll",
		"America/Port-au-Prince", "Asia/Kolkata",
	}
	for _, s := range valid {
		tz, err := NewCampaignTimeZone(s)
		if err != nil {
			t.Fatalf("%q: unexpected error %v", s, err)
		}
		if tz.String() != s || tz.Location() == nil {
			t.Fatalf("%q: bad value", s)
		}
	}
	invalid := []string{
		"", "Local", "localtime", "posixrules", "Factory", "right/Europe/Copenhagen",
		"posix/Europe/Copenhagen", "GMT", "Nowhere/Land", "../etc/passwd", "Europe/../../etc/passwd",
		"/etc/passwd", "Europe//Copenhagen", "Europe/Copenhagen/", "Europe/Copen hagen",
		"Europe\\Copenhagen", strings.Repeat("A", 65), "Europe/Copenhagen\x00",
	}
	for _, s := range invalid {
		if _, err := NewCampaignTimeZone(s); err == nil {
			t.Fatalf("%q: expected an error", s)
		}
	}
}

func TestCampaignTimeZoneJSON(t *testing.T) {
	var tz CampaignTimeZone
	if err := tz.UnmarshalJSON([]byte(`"Europe/Copenhagen"`)); err != nil {
		t.Fatal(err)
	}
	b, err := tz.MarshalJSON()
	if err != nil || string(b) != `"Europe/Copenhagen"` {
		t.Fatalf("got %s %v", b, err)
	}
	if err := tz.UnmarshalJSON([]byte(`"../etc/passwd"`)); err == nil {
		t.Fatal("expected an error")
	}
}
