package remotebrowser

import "testing"

// TestLogProvenance locks in how a log line is classified. Runner diagnostics
// go through log() and are marked Internal so the controller keeps them off the
// recipient timeline. The script author's log() binding goes through scriptLog
// and is not Internal, so it is recorded. Classification comes from the source,
// never from the message text.
func TestLogProvenance(t *testing.T) {
	ch := make(chan RunEvent, 8)
	e := newChannelEmitter(ch)

	// runner diagnostic
	e.log("[session] connecting to remote browser")
	// script author line, with a second data argument
	e.scriptLog("captured value", map[string]string{"field": "otp"})
	// a script line whose text looks like a runner prefix must still be treated
	// as a script line, because only the source decides.
	e.scriptLog("[session] not really internal")
	// a runner diagnostic with a plain message must still be internal.
	e.log("plain diagnostic")

	got := make([]RunEvent, 0, 4)
	for i := 0; i < 4; i++ {
		got = append(got, <-ch)
	}

	if !got[0].Internal {
		t.Errorf("runner log must be Internal, got %+v", got[0])
	}
	if got[1].Internal {
		t.Errorf("script log must not be Internal, got %+v", got[1])
	}
	if got[1].Message != "captured value" || got[1].Data == nil {
		t.Errorf("script log must keep message and data, got %+v", got[1])
	}
	if got[2].Internal {
		t.Errorf("script log with a prefix like text must not be Internal, got %+v", got[2])
	}
	if !got[3].Internal {
		t.Errorf("runner log with a plain message must be Internal, got %+v", got[3])
	}
	for i, ev := range got {
		if ev.Type != "log" {
			t.Errorf("event %d type = %q, want log", i, ev.Type)
		}
	}
}
