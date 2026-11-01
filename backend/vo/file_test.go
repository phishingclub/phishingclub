package vo

import "testing"

func TestNewRelativeFilePathRejectsParentSegment(t *testing.T) {
	for _, p := range []string{"..", "../x", "a/../../x", "../../etc/passwd"} {
		if _, err := NewRelativeFilePath(p); err == nil {
			t.Errorf("NewRelativeFilePath(%q) accepted a parent segment", p)
		}
	}
	for _, p := range []string{"x", "a/b.png", "a..b.png", "a/../b.png"} {
		if _, err := NewRelativeFilePath(p); err != nil {
			t.Errorf("NewRelativeFilePath(%q) = %v, want ok", p, err)
		}
	}
}
