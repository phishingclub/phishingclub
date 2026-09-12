package remotebrowser

import (
	"testing"

	"github.com/dop251/goja"
	"github.com/phishingclub/phishingclub/embedded"
)

// TestPreludeStateMachine runs the real prelude JS in a goja VM against a
// stubbed session and asserts states(), run(), and the loop control work. It
// checks the two mechanics the prelude relies on: reassigning the newSession
// global and adding methods to the session object from JS.
func TestPreludeStateMachine(t *testing.T) {
	vm := goja.New()

	// stage advances as actions run, simulating page progression:
	// 0 password, 1 totp, 2 done.
	stage := 0

	newSession := func(goja.FunctionCall) goja.Value {
		s := vm.NewObject()
		_ = s.Set("location", func(goja.FunctionCall) goja.Value {
			if stage >= 2 {
				return vm.ToValue("https://myaccount.example/home")
			}
			return vm.ToValue("https://login.microsoftonline.com/step")
		})
		_ = s.Set("getNodeCount", func(call goja.FunctionCall) goja.Value {
			sel := call.Argument(0).String()
			if stage == 0 && sel == "input[type=password]" {
				return vm.ToValue(1)
			}
			if stage == 1 && sel == "input[name=otc]" {
				return vm.ToValue(1)
			}
			return vm.ToValue(0)
		})
		_ = s.Set("getText", func(goja.FunctionCall) goja.Value { return vm.ToValue("") })
		_ = s.Set("evaluate", func(goja.FunctionCall) goja.Value { return vm.ToValue(false) })
		_ = s.Set("wait", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
		return s
	}
	if err := vm.Set("newSession", newSession); err != nil {
		t.Fatalf("set newSession: %v", err)
	}

	var logs []string
	_ = vm.Set("log", func(call goja.FunctionCall) goja.Value {
		logs = append(logs, call.Argument(0).String())
		return goja.Undefined()
	})

	if _, err := vm.RunString(embedded.RemoteBrowserPreludeJS); err != nil {
		t.Fatalf("prelude failed to load: %v", err)
	}

	script := `
	  var visited = [];
	  var s = newSession({});
	  if (typeof s.states !== "function") { throw new Error("s.states missing"); }
	  if (typeof s.run !== "function") { throw new Error("s.run missing"); }
	  if (typeof s.waitForState !== "function") { throw new Error("s.waitForState missing"); }

	  s.states({
	    password: function (p) { return p.present("input[type=password]"); },
	    totp:     function (p) { return p.present("input[name=otc]"); },
	    done:     function (p) { return !p.url.includes("microsoftonline.com"); },
	  });

	  s.run({
	    password: function (p, loop) { visited.push("password"); advance(); },
	    totp:     function (p, loop) { visited.push("totp"); advance(); },
	    done:     function (p, loop) { visited.push("done"); loop.stop(); },
	  }, { detectTimeout: 1000 });

	  visited.join(",");
	`

	// advance() bumps the Go stage counter so the stub page moves forward.
	_ = vm.Set("advance", func(goja.FunctionCall) goja.Value {
		stage++
		return goja.Undefined()
	})

	v, err := vm.RunString(script)
	if err != nil {
		t.Fatalf("script failed: %v", err)
	}
	got := v.String()
	want := "password,totp,done"
	if got != want {
		t.Fatalf("state walk = %q, want %q (logs: %v)", got, want, logs)
	}
}
