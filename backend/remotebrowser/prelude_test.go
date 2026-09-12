package remotebrowser

import (
	"testing"

	"github.com/dop251/goja"
	"github.com/phishingclub/phishingclub/embedded"
)

// TestPreludeStateMachine runs the real prelude JS in a goja VM against a
// stubbed session and asserts states() returns a machine whose before/after
// hooks and run loop work, and that present/visible/query were added to the
// session. It checks the mechanic the prelude relies on: reassigning the
// newSession global and augmenting the returned session from JS.
func TestPreludeStateMachine(t *testing.T) {
	vm := goja.New()

	// stage advances as actions run, simulating page progression:
	// 0 password, 1 totp, 2 done.
	stage := 0

	// badArg records the regression where the prelude wrapper forwards undefined
	// to the native newSession for a no-argument call.
	badArg := false
	newSession := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			a := call.Argument(0)
			if goja.IsUndefined(a) || goja.IsNull(a) {
				badArg = true
			}
		}
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
	  var beforeSeen = [];
	  var afterSeen = [];
	  var s = newSession();
	  if (typeof s.states !== "function") { throw new Error("s.states missing"); }
	  if (typeof s.waitForState !== "function") { throw new Error("s.waitForState missing"); }
	  if (typeof s.present !== "function") { throw new Error("s.present missing"); }
	  if (typeof s.visible !== "function") { throw new Error("s.visible missing"); }
	  if (typeof s.query !== "function") { throw new Error("s.query missing"); }

	  var machine = s.states({
	    password: function () { return s.present("input[type=password]"); },
	    totp:     function () { return s.present("input[name=otc]"); },
	    done:     function () { return !s.location().includes("microsoftonline.com"); },
	  });
	  if (typeof machine.run !== "function") { throw new Error("machine.run missing"); }
	  if (typeof machine.before !== "function") { throw new Error("machine.before missing"); }
	  if (typeof machine.after !== "function") { throw new Error("machine.after missing"); }

	  machine
	    .before(function (state) { beforeSeen.push(state); })
	    .after(function (state) { afterSeen.push(state); })
	    .run({
	      password: function (loop) { visited.push("password"); advance(); },
	      totp:     function (loop) { visited.push("totp"); advance(); },
	      done:     function (loop) { visited.push("done"); loop.stop(); },
	    }, { detectTimeout: 1000 });

	  visited.join(",") + "|" + beforeSeen.join(",") + "|" + afterSeen.join(",");
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
	if badArg {
		t.Fatalf("newSession wrapper forwarded undefined/null opts for a no-argument call")
	}
	got := v.String()
	want := "password,totp,done|password,totp,done|password,totp,done"
	if got != want {
		t.Fatalf("state walk = %q, want %q (logs: %v)", got, want, logs)
	}
}
