// Remote browser script prelude.
//
// Adds a small state machine on top of the session so a script declares how to
// recognize each page once, then runs a loop that acts on the current page.
// Loaded into the script VM before the user script, so these helpers are ready
// when newSession() is called. Built only on the public session methods.
(function () {
  if (typeof newSession !== "function") {
    return;
  }
  var baseNewSession = newSession;

  // pageInspector reads the current page. Passed to matchers and actions as p.
  //   p.url            current URL as a plain string
  //   p.present(sel)   the selector matches at least one node
  //   p.count(sel)     how many nodes match
  //   p.text(sel)      text of the first match
  //   p.visible(sel)   the first match is rendered and not hidden
  //   p.query(name)    value of a URL query parameter, decoded, or null
  function pageInspector(s) {
    var url = s.location();
    return {
      url: url,
      present: function (sel) { return s.getNodeCount(sel) > 0; },
      count: function (sel) { return s.getNodeCount(sel); },
      text: function (sel) { return s.getText(sel); },
      visible: function (sel) {
        return s.evaluate(
          "(function(){var e=document.querySelector(" + JSON.stringify(sel) + ");" +
          "if(!e){return false;}var r=e.getBoundingClientRect();var st=getComputedStyle(e);" +
          "return (r.width>0||r.height>0)&&st.visibility!=='hidden'&&st.display!=='none';})()"
        ) === true;
      },
      query: function (name) {
        var key = String(name).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
        var m = url.match(new RegExp("[?&]" + key + "=([^&]*)"));
        return m ? decodeURIComponent(m[1]) : null;
      }
    };
  }

  // firstMatch checks each rule in insertion order and returns the first state
  // name whose matcher is truthy, or null when none match.
  function firstMatch(s, rules) {
    var p = pageInspector(s);
    var names = Object.keys(rules);
    for (var i = 0; i < names.length; i++) {
      var name = names[i];
      var hit = false;
      try { hit = !!rules[name](p); } catch (e) { hit = false; }
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
      var name = firstMatch(s, rules);
      if (name) { return name; }
      if (Date.now() >= deadline) { return "timeout"; }
      s.wait(250);
    }
  }

  newSession = function (opts) {
    var s = baseNewSession(opts);
    var declared = null;

    // states declares the detection rules once: name -> matcher(p).
    s.states = function (rules) {
      declared = rules;
      return s;
    };

    // waitForState returns the current state name using the given rules, or the
    // rules declared with states(). Returns "timeout" if none match in time.
    s.waitForState = function (rules, timeoutMs) {
      return waitForState(s, rules || declared || {}, timeoutMs);
    };

    // run drives the loop: detect the current state, run its action, repeat. An
    // action ends the loop by returning false or calling loop.stop(); any other
    // return re-detects. The built in "timeout" state fires when nothing matched
    // within detectTimeout. opts: { detectTimeout, timeout } in milliseconds.
    s.run = function (actions, opts) {
      if (!declared) { throw new Error("run: call states({...}) before run({...})"); }
      opts = opts || {};
      var detectTimeout = opts.detectTimeout || 10000;
      var overall = opts.timeout || 0;
      var startedAt = Date.now();
      var stopped = false;
      var loop = { state: null, stop: function () { stopped = true; } };

      while (true) {
        var state;
        if (overall && Date.now() - startedAt > overall) {
          state = "timeout";
        } else {
          state = waitForState(s, declared, detectTimeout);
        }
        loop.state = state;
        var action = actions[state];
        if (!action) {
          if (typeof log === "function") { log("[run] no action for state", { state: state }); }
          return state;
        }
        var result = action(pageInspector(s), loop);
        if (result === false || stopped) { return state; }
      }
    };

    return s;
  };
})();
