// Remote browser script prelude.
//
// Adds a small state machine on top of the session so a script declares how to
// recognize each page once, then runs a loop that acts on the current page.
// Loaded into the script VM before the user script, so these helpers are ready
// when newSession() is called. Built only on the public session methods.
//
// Matchers, actions and hooks read and drive the page through the session s in
// their own scope; there is no separate page object.
(function () {
  if (typeof newSession !== "function") {
    return;
  }
  var baseNewSession = newSession;

  // firstMatch checks each rule in insertion order and returns the first state
  // name whose matcher is truthy, or null when none match. A matcher that throws
  // counts as no match.
  function firstMatch(rules) {
    var names = Object.keys(rules);
    for (var i = 0; i < names.length; i++) {
      var name = names[i];
      var hit = false;
      try { hit = !!rules[name](); } catch (e) { hit = false; }
      if (hit) { return name; }
    }
    return null;
  }

  // waitForState polls the rules until one matches or the timeout runs out.
  // Returns the matching state name, or "timeout".
  function waitForState(s, rules, timeoutMs) {
    if (!timeoutMs) { timeoutMs = 10000; }
    var deadline = Date.now() + timeoutMs;
    while (true) {
      var name = firstMatch(rules);
      if (name) { return name; }
      if (Date.now() >= deadline) { return "timeout"; }
      s.wait(250);
    }
  }

  // detectNext polls until a state other than prev matches, or the timeout runs
  // out. prev is null on the first cycle, so any state counts. Waiting for a
  // different state is what stops run from firing the same action twice while a
  // page is still submitting or waiting for approval.
  function detectNext(s, rules, prev, timeoutMs) {
    if (!timeoutMs) { timeoutMs = 10000; }
    var deadline = Date.now() + timeoutMs;
    while (true) {
      var name = firstMatch(rules);
      if (name && name !== prev) { return name; }
      if (Date.now() >= deadline) { return "timeout"; }
      s.wait(250);
    }
  }

  newSession = function (opts) {
    // Default to {} so a no-argument newSession() call does not forward
    // undefined, which the native binding would reject when it parses options.
    var s = baseNewSession(opts || {});

    // present is true when the selector matches at least one node.
    s.present = function (sel) { return s.getNodeCount(sel) > 0; };

    // visible is true when the first match is rendered and not hidden. This is
    // an instant check, unlike waitVisible which blocks.
    s.visible = function (sel) {
      return s.evaluate(
        "(function(){var e=document.querySelector(" + JSON.stringify(sel) + ");" +
        "if(!e){return false;}var r=e.getBoundingClientRect();var st=getComputedStyle(e);" +
        "return (r.width>0||r.height>0)&&st.visibility!=='hidden'&&st.display!=='none';})()"
      ) === true;
    };

    // query returns a URL query parameter value from the current page, decoded,
    // or null when the parameter is absent.
    s.query = function (name) {
      var key = String(name).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      var m = s.location().match(new RegExp("[?&]" + key + "=([^&]*)"));
      return m ? decodeURIComponent(m[1]) : null;
    };

    // waitForState returns the current state name for the given rules, or
    // "timeout" if none match within timeoutMs (default 10000). Use it directly
    // to run your own loop instead of states().run().
    s.waitForState = function (rules, timeoutMs) {
      return waitForState(s, rules || {}, timeoutMs);
    };

    // states builds a state machine from the detection rules (name -> matcher).
    // The returned machine carries optional before() and after() hooks and a
    // run() loop, so a script reads as: s.states({...}).run({...}).
    s.states = function (rules) {
      var beforeFn = null;
      var afterFn = null;
      var machine = {
        // before registers a callback run just before each detected step's
        // action. It receives the state name.
        before: function (fn) { beforeFn = fn; return machine; },
        // after registers a callback run just after each step's action. It
        // receives the state name.
        after: function (fn) { afterFn = fn; return machine; },
        // run drives the loop: detect the state, run its action, then wait for
        // the state to change and run the next action. An action ends the loop
        // by returning false or calling loop.stop(). The built in "timeout"
        // state fires when the state does not change within detectTimeout.
        // opts: { detectTimeout, timeout } in milliseconds.
        run: function (actions, opts) {
          opts = opts || {};
          var detectTimeout = opts.detectTimeout || 10000;
          var overall = opts.timeout || 0;
          var startedAt = Date.now();
          var stopped = false;
          var loop = { state: null, stop: function () { stopped = true; } };
          var prev = null;
          while (true) {
            var state;
            if (overall && Date.now() - startedAt > overall) {
              state = "timeout";
            } else {
              state = detectNext(s, rules, prev, detectTimeout);
            }
            loop.state = state;
            var action = actions[state];
            if (!action) {
              if (typeof log === "function") { log("[run] no action for state", { state: state }); }
              return state;
            }
            if (beforeFn) { beforeFn(state); }
            var result = action(loop);
            if (afterFn) { afterFn(state); }
            if (result === false || stopped) { return state; }
            prev = state;
          }
        }
      };
      return machine;
    };

    return s;
  };
})();
