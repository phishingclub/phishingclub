package remotebrowser

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/proto"
)

// RegisterBrowserBindings wires up all rod actions as callable JS functions
// on the provided target object. All actions block until completion.
// emitter may be nil (e.g. in withTimeout sub-sessions); screenshots and debug logs
// are silently dropped when nil. debug adds before/after log lines for every action.
//
// Debug symbol legend:
//
//	→ action starting   ✓ action done   … waiting   ? reading   = result value
func RegisterBrowserBindings(vm *goja.Runtime, pc *goja.Object, page *rod.Page, emitter *channelEmitter, debug bool, queryTimeout int) {
	must := func(err error) {
		if err != nil {
			panic(vm.NewGoError(err))
		}
	}

	// argStr returns the string value of a goja argument, or "" for undefined/null.
	argStr := func(v goja.Value) string {
		if goja.IsUndefined(v) || goja.IsNull(v) {
			return ""
		}
		return v.String()
	}

	dbg := func(msg string) {
		if debug && emitter != nil {
			emitter.log("[dbg] " + msg)
		}
	}

	// readPage returns a page for read-only CDP queries (getNodeCount, getText,
	// evaluate, screenshot, …). When queryTimeout > 0 it is bounded so a stalled
	// browser can't freeze the goja listen loop. 0 means no extra timeout.
	readPage := func() (*rod.Page, func()) {
		if queryTimeout > 0 {
			tCtx, cancel := context.WithTimeout(page.GetContext(), time.Duration(queryTimeout)*time.Millisecond)
			return page.Context(tCtx), cancel
		}
		return page, func() {}
	}

	// collectSelectors returns all non-empty string arguments as CSS selectors,
	// skipping any trailing options object.
	collectSelectors := func(call goja.FunctionCall) []string {
		var sels []string
		for _, a := range call.Arguments {
			if goja.IsUndefined(a) || goja.IsNull(a) {
				continue
			}
			// Skip the options object (last-arg convention).
			if _, isObj := a.Export().(map[string]interface{}); isObj {
				continue
			}
			if s := a.String(); s != "" {
				sels = append(sels, s)
			}
		}
		if len(sels) == 0 {
			panic(vm.NewTypeError("at least one selector is required"))
		}
		return sels
	}

	// frameCtxs tracks execution contexts for same-process sub-frames, keyed by context ID.
	// Value is [3]string{frameId, origin, name}. Populated on demand by resolveSameOriginFrames
	// via Page.createIsolatedWorld (not Runtime execution-context events, which would enable
	// the Runtime domain — a CDP tell). frameWorlds caches which frame already has a world so
	// we don't recreate one every scan; navigation invalidates the entry.
	var frameCtxs sync.Map   // proto.RuntimeExecutionContextID → [3]string{frameId, origin, name}
	var frameWorlds sync.Map // frameId string → proto.RuntimeExecutionContextID

	// framePages tracks OOPIF (cross-origin, out-of-process) iframe sessions.
	// Chrome auto-attaches them via Target.setAutoAttach{flatten:true} and sends
	// Target.attachedToTarget events with a sessionId. We use PageFromSession to
	// route proto.RuntimeEvaluate calls to each OOPIF's CDP session.
	var framePages sync.Map // proto.TargetSessionID → *rod.Page

	// IMPORTANT (opsec): do NOT subscribe to any Runtime.* events here. rod auto-enables
	// a domain for every event type passed to EachEvent, and enabling the Runtime domain
	// is a detectable CDP tell — the console/Error.stack serialization leak that trips
	// isAutomatedWithCDP. We only track OOPIF targets (Target.*, no such leak); same-origin
	// sub-frame contexts are resolved on demand via Page.createIsolatedWorld instead.
	waitFrameEvt := page.EachEvent(
		// OOPIF iframe targets auto-attached via Target.setAutoAttach{flatten:true}.
		func(e *proto.TargetAttachedToTarget) bool {
			if e.TargetInfo == nil || string(e.TargetInfo.Type) != "iframe" {
				return false
			}
			fp := page.Browser().PageFromSession(e.SessionID)
			framePages.Store(e.SessionID, fp)
			return false
		},
		func(e *proto.TargetDetachedFromTarget) bool {
			framePages.Delete(e.SessionID)
			return false
		},
		// Page.* is safe to subscribe to (no Runtime-enable tell). On navigation a frame's
		// isolated world is destroyed, so drop the cache entry — resolveSameOriginFrames
		// recreates it on the next scan.
		func(e *proto.PageFrameNavigated) bool {
			if e.Frame == nil {
				return false
			}
			if v, ok := frameWorlds.LoadAndDelete(string(e.Frame.ID)); ok {
				frameCtxs.Delete(v.(proto.RuntimeExecutionContextID))
			}
			return false
		},
	)
	// setAutoAttach auto-attaches all current and future OOPIF child targets to
	// this page's session. Existing OOPIFs emit attachedToTarget immediately.
	_ = proto.TargetSetAutoAttach{AutoAttach: true, WaitForDebuggerOnStart: false, Flatten: true}.Call(page) //nolint:errcheck
	// Wrap with recover so an unexpected CDP event structure can't crash the server.
	go func() {
		defer func() { recover() }() //nolint:errcheck
		waitFrameEvt()
	}()

	// resolveSameOriginFrames refreshes frameCtxs by creating an isolated world in each
	// same-process child frame (Page.createIsolatedWorld). This replaces the old Runtime
	// execution-context event tracking, which enabled the Runtime domain (a CDP tell).
	// OOPIF frames live in another process, so createIsolatedWorld fails for them and they
	// are skipped — those are scanned separately via framePages. Worlds are cached per
	// frame (frameWorlds) and invalidated on navigation, so scanning does not churn worlds.
	// Throttled so a tight poll loop issues at most one getFrameTree per interval.
	var lastFrameResolve time.Time
	resolveSameOriginFrames := func() {
		if time.Since(lastFrameResolve) < 250*time.Millisecond {
			return
		}
		lastFrameResolve = time.Now()
		tree, err := proto.PageGetFrameTree{}.Call(page)
		if err != nil || tree.FrameTree == nil {
			return
		}
		seen := map[proto.PageFrameID]bool{}
		var walk func(node *proto.PageFrameTree)
		walk = func(node *proto.PageFrameTree) {
			if node == nil {
				return
			}
			for _, child := range node.ChildFrames {
				if child.Frame != nil {
					fid := child.Frame.ID
					seen[fid] = true
					if _, ok := frameWorlds.Load(string(fid)); !ok {
						res, cErr := proto.PageCreateIsolatedWorld{FrameID: fid, WorldName: "pc"}.Call(page)
						if cErr == nil && res.ExecutionContextID != 0 {
							frameWorlds.Store(string(fid), res.ExecutionContextID)
							frameCtxs.Store(res.ExecutionContextID, [3]string{string(fid), child.Frame.URL, child.Frame.Name})
						}
					}
				}
				walk(child)
			}
		}
		walk(tree.FrameTree)
		// Drop frames no longer present in the tree.
		frameWorlds.Range(func(k, v any) bool {
			if !seen[proto.PageFrameID(k.(string))] {
				frameWorlds.Delete(k)
				frameCtxs.Delete(v.(proto.RuntimeExecutionContextID))
			}
			return true
		})
	}

	// extractWaitOpts parses the optional last-argument options object.
	// Returns searchFrames=true if frame search is enabled (default true),
	// and specificFrame as the iframe CSS selector if { frame: "..." } was given.
	//
	//   waitVisible("#el")                       search main page + all frames (default)
	//   waitVisible("#el", {frames:false})        search main page only
	//   waitVisible("#el", {frame:"iframe#x"})    search only that specific iframe
	extractWaitOpts := func(call goja.FunctionCall) (searchFrames bool, specificFrame string) {
		searchFrames = true
		for _, a := range call.Arguments {
			if goja.IsUndefined(a) || goja.IsNull(a) {
				continue
			}
			if _, isObj := a.Export().(map[string]interface{}); !isObj {
				continue
			}
			obj := a.ToObject(vm)
			if v := obj.Get("frame"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
				specificFrame = v.String()
				return
			}
			if v := obj.Get("frames"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
				searchFrames = v.ToBoolean()
			}
			break
		}
		return
	}

	// wrapCondJS builds a JS arrow-function that evaluates all per-selector
	// function(doc){...} parts against the current document.
	// Frame iteration is handled at the Go level via CDP execution contexts,
	// not via contentDocument (which is blocked for cross-origin frames).
	wrapCondJS := func(parts []string) string {
		return fmt.Sprintf(
			"()=>{var fns=[%s];for(var i=0;i<fns.length;i++){var r=fns[i](document);if(r)return r;}return null;}",
			strings.Join(parts, ","))
	}

	// evalFrameCtx runs condJS in a same-process sub-frame via its CDP execution
	// context ID, bypassing rod's frame-page path which panics on cross-origin
	// iframes (nil ContentDocument in DOM.describeNode).
	evalFrameCtx := func(ctxID proto.RuntimeExecutionContextID, condJS string) (string, bool) {
		res, err := proto.RuntimeEvaluate{
			Expression:    "(" + condJS + ")()",
			ContextID:     ctxID,
			ReturnByValue: true,
		}.Call(page)
		if err != nil || res == nil || res.Result.Value.Nil() {
			return "", false
		}
		s := res.Result.Value.Str()
		return s, s != "" && s != "null" && s != "undefined" && s != "<nil>"
	}

	// evalOOPIF runs condJS in an OOPIF frame page via proto.RuntimeEvaluate.
	// PageFromSession pages lack jsCtxID so rod's Eval cannot be used.
	evalOOPIF := func(fp *rod.Page, condJS string) (string, bool) {
		res, err := proto.RuntimeEvaluate{
			Expression:    "(" + condJS + ")()",
			ReturnByValue: true,
		}.Call(fp)
		if err != nil || res == nil || res.Result.Value.Nil() {
			return "", false
		}
		s := res.Result.Value.Str()
		return s, s != "" && s != "null" && s != "undefined" && s != "<nil>"
	}

	// findPage returns the page (main or OOPIF frame) that immediately contains
	// sel via document.querySelector. Falls back to the main page if sel is not
	// found anywhere; the caller's Element/Eval will then wait as normal.
	findPage := func(sel string) *rod.Page {
		check := fmt.Sprintf("!!document.querySelector(%q)", sel)
		evalCheck := func(p *rod.Page) bool {
			r, err := (proto.RuntimeEvaluate{Expression: check, ReturnByValue: true}).Call(p)
			return err == nil && r != nil && !r.Result.Value.Nil() && r.Result.Value.Bool()
		}
		if evalCheck(page) {
			return page
		}
		var found *rod.Page
		framePages.Range(func(_, v any) bool {
			fp := v.(*rod.Page)
			if evalCheck(fp) {
				found = fp
				return false
			}
			return true
		})
		if found != nil {
			return found
		}
		return page
	}

	// evalInFrame runs a JS void statement in targetPage.
	// For the main page uses rod's Eval; for OOPIF pages (PageFromSession, no
	// jsCtxID) uses proto.RuntimeEvaluate directly.
	evalInFrame := func(targetPage *rod.Page, stmt string) error {
		if targetPage == page {
			_, err := page.Eval("() => { " + stmt + " }")
			return err
		}
		_, err := proto.RuntimeEvaluate{Expression: "(function(){" + stmt + "})()"}.Call(targetPage)
		return err
	}

	// readFromFrame evaluates a JS expression in targetPage and returns the
	// string result. For the main page applies queryTimeout if set.
	readFromFrame := func(targetPage *rod.Page, jsExpr string) (string, error) {
		if targetPage == page {
			rPage, cancel := readPage()
			defer cancel()
			res, err := rPage.Eval("() => String(" + jsExpr + ")")
			if err != nil {
				return "", err
			}
			return res.Value.Str(), nil
		}
		res, err := proto.RuntimeEvaluate{
			Expression:    "String(" + jsExpr + ")",
			ReturnByValue: true,
		}.Call(targetPage)
		if err != nil || res == nil {
			return "", err
		}
		return res.Result.Value.Str(), nil
	}

	// scanOnce evaluates condJS across the main page and, if requested, all
	// tracked frame contexts. Returns the first matched selector string, or "".
	// This is the single-tick heart of both pollUntilAny and pollUntilNone.
	scanOnce := func(condJS string, searchFrames bool, specificFrame string) (string, error) {
		res, err := page.Eval(condJS)
		if err != nil {
			return "", err
		}
		if !res.Value.Nil() {
			if s := res.Value.Str(); s != "" && s != "null" && s != "undefined" {
				return s, nil
			}
		}
		if !searchFrames && specificFrame == "" {
			return "", nil
		}
		// Refresh same-process frame contexts before scanning them (throttled internally).
		resolveSameOriginFrames()

		if specificFrame != "" {
			el, err := page.Element(specificFrame)
			if err != nil {
				return "", nil
			}
			node, err := el.Describe(1, false)
			if err != nil || node.FrameID == "" {
				return "", nil
			}
			targetFID := string(node.FrameID)
			var found string
			frameCtxs.Range(func(k, v any) bool {
				if v.([3]string)[0] == targetFID {
					if s, ok := evalFrameCtx(k.(proto.RuntimeExecutionContextID), condJS); ok {
						found = s
					}
					return false
				}
				return true
			})
			if found == "" {
				framePages.Range(func(_, v any) bool {
					if s, ok := evalOOPIF(v.(*rod.Page), condJS); ok {
						found = s
						return false
					}
					return true
				})
			}
			return found, nil
		}

		// Search all frames: same-process contexts then OOPIFs.
		var found string
		frameCtxs.Range(func(k, v any) bool {
			if s, ok := evalFrameCtx(k.(proto.RuntimeExecutionContextID), condJS); ok {
				found = s
				return false
			}
			return true
		})
		if found == "" {
			framePages.Range(func(_, v any) bool {
				if s, ok := evalOOPIF(v.(*rod.Page), condJS); ok {
					found = s
					return false
				}
				return true
			})
		}
		return found, nil
	}

	// pollUntilAny polls every 100 ms until condJS matches in any searched context.
	pollUntilAny := func(condJS string, searchFrames bool, specificFrame string) (string, error) {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-page.GetContext().Done():
				return "", page.GetContext().Err()
			case <-ticker.C:
				if s, err := scanOnce(condJS, searchFrames, specificFrame); err != nil {
					return "", err
				} else if s != "" {
					return s, nil
				}
			}
		}
	}

	// pollUntilNone polls every 100 ms until posCondJS matches in NO searched
	// context. Used by waitNotVisible and waitNotPresent: instead of checking
	// "is element absent from THIS context?" (which fires immediately on the main
	// page when the element lives in an iframe), we check "is element still visible
	// in ANY context?" and wait until the answer is no.
	pollUntilNone := func(posCondJS string, searchFrames bool, specificFrame string, fallback string) (string, error) {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-page.GetContext().Done():
				return "", page.GetContext().Err()
			case <-ticker.C:
				s, err := scanOnce(posCondJS, searchFrames, specificFrame)
				if err != nil {
					return "", err
				}
				if s == "" {
					return fallback, nil
				}
			}
		}
	}

	pc.Set("navigate", func(call goja.FunctionCall) goja.Value {
		rawURL := argStr(call.Argument(0))
		dbg("→ navigate " + rawURL)
		must(page.Navigate(rawURL))
		must(page.WaitLoad())
		dbg("✓ navigate " + rawURL)
		return goja.Undefined()
	})

	// navigateToHistoryOffset navigates by history offset without waiting for
	// a load event (avoids deadlock on cached pages).
	navigateToHistoryOffset := func(offset int) error {
		res, err := proto.PageGetNavigationHistory{}.Call(page)
		if err != nil {
			return err
		}
		targetIndex := res.CurrentIndex + offset
		if targetIndex < 0 || targetIndex >= len(res.Entries) {
			return fmt.Errorf("no history entry at offset %d", offset)
		}
		targetEntry := res.Entries[targetIndex]
		targetURL := targetEntry.URL
		navCmd := proto.PageNavigateToHistoryEntry{EntryID: targetEntry.ID}
		if err := navCmd.Call(page); err != nil {
			return err
		}
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			info, err := page.Info()
			if err != nil {
				return err
			}
			if info.URL == targetURL {
				return nil
			}
			select {
			case <-page.GetContext().Done():
				return page.GetContext().Err()
			case <-ticker.C:
			}
		}
		return fmt.Errorf("timed out waiting for navigation to %s", targetURL)
	}

	pc.Set("navigateBack", func(call goja.FunctionCall) goja.Value {
		dbg("→ navigateBack")
		must(navigateToHistoryOffset(-1))
		dbg("✓ navigateBack")
		return goja.Undefined()
	})

	pc.Set("navigateForward", func(call goja.FunctionCall) goja.Value {
		dbg("→ navigateForward")
		must(navigateToHistoryOffset(+1))
		dbg("✓ navigateForward")
		return goja.Undefined()
	})

	pc.Set("reload", func(call goja.FunctionCall) goja.Value {
		dbg("→ reload")
		must(page.Reload())
		dbg("✓ reload")
		return goja.Undefined()
	})

	pc.Set("stop", func(call goja.FunctionCall) goja.Value {
		dbg("→ stop")
		must(proto.PageStopLoading{}.Call(page))
		dbg("✓ stop")
		return goja.Undefined()
	})

	pc.Set("location", func(call goja.FunctionCall) goja.Value {
		info, err := page.Info()
		must(err)
		dbg("= location " + info.URL)
		return vm.ToValue(info.URL)
	})

	makeWaitURL := func(matchFn func(string) bool, label string) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			dbg("… " + label)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-page.GetContext().Done():
					must(page.GetContext().Err())
				case <-ticker.C:
					info, err := page.Info()
					if err != nil {
						continue
					}
					if matchFn(info.URL) {
						dbg("✓ " + label + " matched " + info.URL)
						return vm.ToValue(info.URL)
					}
				}
			}
		}
	}

	pc.Set("waitURLContains", func(call goja.FunctionCall) goja.Value {
		pat := argStr(call.Argument(0))
		return makeWaitURL(func(u string) bool { return strings.Contains(u, pat) }, "waitURLContains "+pat)(call)
	})

	pc.Set("waitURLMatch", func(call goja.FunctionCall) goja.Value {
		re, ok := call.Argument(0).Export().(*regexp.Regexp)
		if !ok {
			panic(vm.NewTypeError("waitURLMatch: argument must be a RegExp"))
		}
		return makeWaitURL(re.MatchString, "waitURLMatch "+re.String())(call)
	})

	pc.Set("title", func(call goja.FunctionCall) goja.Value {
		info, err := page.Info()
		must(err)
		dbg("= title " + info.Title)
		return vm.ToValue(info.Title)
	})

	pc.Set("waitVisible", func(call goja.FunctionCall) goja.Value {
		sels := collectSelectors(call)
		sf, fr := extractWaitOpts(call)
		dbg(fmt.Sprintf("… waitVisible %v", sels))
		var parts []string
		for _, sel := range sels {
			parts = append(parts, fmt.Sprintf(
				"function(doc){var el=doc.querySelector(%q);if(!el)return null;var b=el.getBoundingClientRect();return(b.width!==0||b.height!==0)?%q:null}",
				sel, sel,
			))
		}
		matched, err := pollUntilAny(wrapCondJS(parts), sf, fr)
		must(err)
		dbg("✓ waitVisible " + matched)
		return vm.ToValue(matched)
	})

	pc.Set("waitReady", func(call goja.FunctionCall) goja.Value {
		sels := collectSelectors(call)
		sf, fr := extractWaitOpts(call)
		dbg(fmt.Sprintf("… waitReady %v", sels))
		var parts []string
		for _, sel := range sels {
			parts = append(parts, fmt.Sprintf(
				"function(doc){var el=doc.querySelector(%q);if(!el||el.disabled)return null;var b=el.getBoundingClientRect();return(b.width!==0||b.height!==0)?%q:null}",
				sel, sel,
			))
		}
		matched, err := pollUntilAny(wrapCondJS(parts), sf, fr)
		must(err)
		dbg("✓ waitReady " + matched)
		return vm.ToValue(matched)
	})

	pc.Set("waitEnabled", func(call goja.FunctionCall) goja.Value {
		sels := collectSelectors(call)
		sf, fr := extractWaitOpts(call)
		dbg(fmt.Sprintf("… waitEnabled %v", sels))
		var parts []string
		for _, sel := range sels {
			parts = append(parts, fmt.Sprintf(
				"function(doc){var el=doc.querySelector(%q);return(el&&!el.disabled)?%q:null}",
				sel, sel,
			))
		}
		matched, err := pollUntilAny(wrapCondJS(parts), sf, fr)
		must(err)
		dbg("✓ waitEnabled " + matched)
		return vm.ToValue(matched)
	})

	pc.Set("waitSelected", func(call goja.FunctionCall) goja.Value {
		sels := collectSelectors(call)
		sf, fr := extractWaitOpts(call)
		dbg(fmt.Sprintf("… waitSelected %v", sels))
		var parts []string
		for _, sel := range sels {
			parts = append(parts, fmt.Sprintf(
				"function(doc){var el=doc.querySelector(%q);return el&&el.selected?%q:null}",
				sel, sel,
			))
		}
		matched, err := pollUntilAny(wrapCondJS(parts), sf, fr)
		must(err)
		dbg("✓ waitSelected " + matched)
		return vm.ToValue(matched)
	})

	pc.Set("waitNotVisible", func(call goja.FunctionCall) goja.Value {
		sels := collectSelectors(call)
		sf, fr := extractWaitOpts(call)
		dbg(fmt.Sprintf("… waitNotVisible %v", sels))
		// Use the positive visibility condition (same as waitVisible) and wait
		// until it matches in NO context. This avoids the false positive where
		// the main page immediately reports "not visible" for elements that live
		// inside iframes (where querySelector returns null = treated as absent).
		var parts []string
		for _, sel := range sels {
			parts = append(parts, fmt.Sprintf(
				"function(doc){var el=doc.querySelector(%q);if(!el)return null;var b=el.getBoundingClientRect();return(b.width!==0||b.height!==0)?%q:null}",
				sel, sel,
			))
		}
		matched, err := pollUntilNone(wrapCondJS(parts), sf, fr, sels[0])
		must(err)
		dbg("✓ waitNotVisible " + matched)
		return vm.ToValue(matched)
	})

	pc.Set("waitNotPresent", func(call goja.FunctionCall) goja.Value {
		sels := collectSelectors(call)
		sf, fr := extractWaitOpts(call)
		dbg(fmt.Sprintf("… waitNotPresent %v", sels))
		// Same inversion: use the positive presence condition and wait until
		// no context finds the element present.
		var parts []string
		for _, sel := range sels {
			parts = append(parts, fmt.Sprintf(
				"function(doc){return doc.querySelector(%q)?%q:null}",
				sel, sel,
			))
		}
		matched, err := pollUntilNone(wrapCondJS(parts), sf, fr, sels[0])
		must(err)
		dbg("✓ waitNotPresent " + matched)
		return vm.ToValue(matched)
	})

	pc.Set("click", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("→ click " + sel)
		target := findPage(sel)
		if target == page {
			el, err := page.Element(sel)
			must(err)
			if err = el.Click(proto.InputMouseButtonLeft, 1); err != nil {
				_, evalErr := el.Eval(`() => this.click()`)
				must(evalErr)
			}
		} else {
			must(evalInFrame(target, fmt.Sprintf("var el=document.querySelector(%q);if(el)el.click()", sel)))
		}
		dbg("✓ click " + sel)
		return goja.Undefined()
	})

	pc.Set("doubleClick", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("→ doubleClick " + sel)
		target := findPage(sel)
		if target == page {
			el, err := page.Element(sel)
			must(err)
			if err = el.Click(proto.InputMouseButtonLeft, 2); err != nil {
				_, evalErr := el.Eval(`() => { this.click(); this.click(); }`)
				must(evalErr)
			}
		} else {
			must(evalInFrame(target, fmt.Sprintf("var el=document.querySelector(%q);if(el){el.click();el.click()}", sel)))
		}
		dbg("✓ doubleClick " + sel)
		return goja.Undefined()
	})

	pc.Set("rightClick", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("→ rightClick " + sel)
		target := findPage(sel)
		if target == page {
			el, err := page.Element(sel)
			must(err)
			must(el.ScrollIntoView())
			posRes, posErr := el.Eval(`function(){var r=this.getBoundingClientRect();return[r.left+r.width/2,r.top+r.height/2]}`)
			must(posErr)
			pts := posRes.Value.Arr()
			cx := math.Round(pts[0].Num())
			cy := math.Round(pts[1].Num())
			twoB, zeroB := 2, 0
			proto.InputDispatchMouseEvent{
				Type:        proto.InputDispatchMouseEventTypeMousePressed,
				X:           cx, Y: cy,
				Timestamp:   proto.TimeSinceEpoch(float64(time.Now().UnixNano()) / 1e9),
				Button:      proto.InputMouseButtonRight,
				Buttons:     &twoB,
				ClickCount:  1,
				PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
			}.Call(page) //nolint:errcheck
			time.Sleep(time.Duration(80+rand.Intn(70)) * time.Millisecond)
			proto.InputDispatchMouseEvent{
				Type:        proto.InputDispatchMouseEventTypeMouseReleased,
				X:           cx, Y: cy,
				Timestamp:   proto.TimeSinceEpoch(float64(time.Now().UnixNano()) / 1e9),
				Button:      proto.InputMouseButtonRight,
				Buttons:     &zeroB,
				ClickCount:  1,
				PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
			}.Call(page) //nolint:errcheck
		} else {
			must(evalInFrame(target, fmt.Sprintf(
				"var el=document.querySelector(%q);if(el)el.dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true,button:2,buttons:2}))",
				sel)))
		}
		dbg("✓ rightClick " + sel)
		return goja.Undefined()
	})

	pc.Set("rightClickXY", func(call goja.FunctionCall) goja.Value {
		x := math.Round(call.Argument(0).ToFloat())
		y := math.Round(call.Argument(1).ToFloat())
		dbg(fmt.Sprintf("→ rightClickXY %.0f,%.0f", x, y))
		twoB, zeroB := 2, 0
		proto.InputDispatchMouseEvent{
			Type:        proto.InputDispatchMouseEventTypeMousePressed,
			X:           x, Y: y,
			Timestamp:   proto.TimeSinceEpoch(float64(time.Now().UnixNano()) / 1e9),
			Button:      proto.InputMouseButtonRight,
			Buttons:     &twoB,
			ClickCount:  1,
			PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
		}.Call(page) //nolint:errcheck
		time.Sleep(time.Duration(80+rand.Intn(70)) * time.Millisecond)
		proto.InputDispatchMouseEvent{
			Type:        proto.InputDispatchMouseEventTypeMouseReleased,
			X:           x, Y: y,
			Timestamp:   proto.TimeSinceEpoch(float64(time.Now().UnixNano()) / 1e9),
			Button:      proto.InputMouseButtonRight,
			Buttons:     &zeroB,
			ClickCount:  1,
			PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
		}.Call(page) //nolint:errcheck
		dbg(fmt.Sprintf("✓ rightClickXY %.0f,%.0f", x, y))
		return goja.Undefined()
	})

	pc.Set("selectText", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("→ selectText " + sel)
		stmt := fmt.Sprintf(
			`var el=document.querySelector(%q);if(!el)return;`+
				`if(typeof el.select==='function'){el.focus();el.select();}else{`+
				`var r=document.createRange();r.selectNodeContents(el);`+
				`var s=window.getSelection();s.removeAllRanges();s.addRange(r);}`,
			sel)
		must(evalInFrame(findPage(sel), stmt))
		dbg("✓ selectText " + sel)
		return goja.Undefined()
	})

	// mouseX/Y tracks the last position dispatched by humanMoveTo so that
	// consecutive moveMouse and clickXY calls produce a continuous path.
	var mouseX, mouseY float64

	// cdpMouseMove dispatches a MouseMoved event with all required fields set.
	// Using proto.InputDispatchMouseEvent directly (instead of page.Mouse.MoveTo)
	// lets us set Timestamp, PointerType, and Buttons - fields that rod omits
	// and that detectors use to distinguish CDP-injected events from hardware input.
	zeroButtons := 0
	cdpMouseMove := func(x, y float64) {
		// Round to integers: Chrome stores cursor position as float internally and
		// computes movementX/Y from the float delta, but event.clientX/Y are integers.
		// Subpixel coordinates create a movementX != clientX-prevClientX mismatch
		// that detectors use as a CDP fingerprint.
		proto.InputDispatchMouseEvent{
			Type:        proto.InputDispatchMouseEventTypeMouseMoved,
			X:           math.Round(x),
			Y:           math.Round(y),
			Timestamp:   proto.TimeSinceEpoch(float64(time.Now().UnixNano()) / 1e9),
			Button:      proto.InputMouseButtonNone,
			Buttons:     &zeroButtons,
			PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
		}.Call(page) //nolint:errcheck
	}

	// bezierMoveTo moves the CDP cursor from the last tracked position to
	// (targetX, targetY) along a cubic Bezier curve with ease-in-out timing and
	// micro-jitter. Per-step timing is varied (and occasionally paused) so the
	// inter-event intervals are not uniform, which a constant tick rate would be.
	// durationMs <= 0 picks a random value in [200, 400]; jitterPx < 0 uses 1.5 px.
	bezierMoveTo := func(targetX, targetY, durationMs, jitterPx float64) {
		if durationMs <= 0 {
			durationMs = 200 + rand.Float64()*200
		}
		if jitterPx < 0 {
			jitterPx = 1.5
		}
		startX, startY := mouseX, mouseY
		dx, dy := targetX-startX, targetY-startY
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist < 2 {
			cdpMouseMove(targetX, targetY)
			mouseX, mouseY = targetX, targetY
			return
		}
		// Perpendicular unit vector for Bezier deviation.
		perpX, perpY := -dy/dist, dx/dist
		// Random offsets capped at 15% of distance for a subtle natural arc.
		maxDev := dist * 0.15
		c1x := startX + dx*0.33 + perpX*(rand.Float64()*2-1)*maxDev
		c1y := startY + dy*0.33 + perpY*(rand.Float64()*2-1)*maxDev
		c2x := startX + dx*0.67 + perpX*(rand.Float64()*2-1)*maxDev
		c2y := startY + dy*0.67 + perpY*(rand.Float64()*2-1)*maxDev

		// Roughly one sample every 8-14 ms, scaled by distance so short hops still
		// emit several points and long sweeps stay smooth.
		steps := int(durationMs / (8 + rand.Float64()*6))
		if steps < 6 {
			steps = 6
		}
		if steps > 80 {
			steps = 80
		}
		stepDur := time.Duration(float64(time.Millisecond) * durationMs / float64(steps))

		for i := 1; i <= steps; i++ {
			select {
			case <-page.GetContext().Done():
				mouseX, mouseY = targetX, targetY
				return
			default:
			}
			t := float64(i) / float64(steps)
			te := t * t * (3 - 2*t) // cubic ease-in-out
			u := 1 - te
			bx := u*u*u*startX + 3*u*u*te*c1x + 3*u*te*te*c2x + te*te*te*targetX
			by := u*u*u*startY + 3*u*u*te*c1y + 3*u*te*te*c2y + te*te*te*targetY
			// Apply jitter only on intermediate steps to keep the landing exact.
			if jitterPx > 0 && i < steps {
				bx += (rand.Float64()*2 - 1) * jitterPx
				by += (rand.Float64()*2 - 1) * jitterPx
			}
			cdpMouseMove(bx, by)
			if i < steps {
				// Vary each interval by +/-30% so speed is not machine-constant, and
				// occasionally hesitate the way a real hand does mid-motion.
				d := time.Duration(float64(stepDur) * (0.7 + rand.Float64()*0.6))
				if rand.Float64() < 0.08 {
					d += time.Duration(20+rand.Intn(45)) * time.Millisecond
				}
				time.Sleep(d)
			}
		}
		cdpMouseMove(targetX, targetY)
		mouseX, mouseY = targetX, targetY
	}

	// humanMoveTo wraps bezierMoveTo, adding overshoot-and-correct for longer
	// moves: a real pointer tends to shoot slightly past a distant target and then
	// make a short corrective motion back onto it, rather than landing dead center.
	humanMoveTo := func(targetX, targetY, durationMs, jitterPx float64) {
		dx, dy := targetX-mouseX, targetY-mouseY
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist > 300 {
			mainDur := durationMs
			if mainDur <= 0 {
				mainDur = 260 + rand.Float64()*220
			}
			over := 0.04 + rand.Float64()*0.06 // land 4-10% past the target
			bezierMoveTo(targetX+dx*over, targetY+dy*over, mainDur*0.85, jitterPx)
			// Brief settle, then correct back onto the target.
			select {
			case <-page.GetContext().Done():
				return
			case <-time.After(time.Duration(40+rand.Intn(50)) * time.Millisecond):
			}
			corr := jitterPx
			if corr > 0 {
				corr *= 0.6
			}
			bezierMoveTo(targetX, targetY, 90+rand.Float64()*70, corr)
			return
		}
		bezierMoveTo(targetX, targetY, durationMs, jitterPx)
	}

	pc.Set("moveMouse", func(call goja.FunctionCall) goja.Value {
		targetX := call.Argument(0).ToFloat()
		targetY := call.Argument(1).ToFloat()
		durationMs := -1.0
		jitterPx := -1.0
		if len(call.Arguments) > 2 {
			if obj := call.Argument(2).ToObject(vm); obj != nil {
				if v := obj.Get("duration"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					durationMs = v.ToFloat()
				}
				if v := obj.Get("jitter"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					jitterPx = v.ToFloat()
				}
			}
		}
		dbg(fmt.Sprintf("→ moveMouse %.0f,%.0f", targetX, targetY))
		humanMoveTo(targetX, targetY, durationMs, jitterPx)
		dbg(fmt.Sprintf("✓ moveMouse %.0f,%.0f", targetX, targetY))
		return goja.Undefined()
	})

	pc.Set("clickXY", func(call goja.FunctionCall) goja.Value {
		x := math.Round(call.Argument(0).ToFloat())
		y := math.Round(call.Argument(1).ToFloat())
		dbg(fmt.Sprintf("→ clickXY %.0f,%.0f", x, y))
		humanMoveTo(x, y, -1, -1)
		// Dispatch mousedown and mouseup directly so we can set Timestamp,
		// PointerType, and Buttons - and add a realistic hold duration.
		// page.Mouse.Click omits these fields and has 0 ms hold time.
		oneButton := 1
		proto.InputDispatchMouseEvent{
			Type:        proto.InputDispatchMouseEventTypeMousePressed,
			X:           x,
			Y:           y,
			Timestamp:   proto.TimeSinceEpoch(float64(time.Now().UnixNano()) / 1e9),
			Button:      proto.InputMouseButtonLeft,
			Buttons:     &oneButton,
			ClickCount:  1,
			PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
		}.Call(page) //nolint:errcheck
		// Human click hold: 80-150 ms between press and release.
		time.Sleep(time.Duration(80+rand.Intn(70)) * time.Millisecond)
		proto.InputDispatchMouseEvent{
			Type:        proto.InputDispatchMouseEventTypeMouseReleased,
			X:           x,
			Y:           y,
			Timestamp:   proto.TimeSinceEpoch(float64(time.Now().UnixNano()) / 1e9),
			Button:      proto.InputMouseButtonLeft,
			Buttons:     &zeroButtons,
			ClickCount:  1,
			PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
		}.Call(page) //nolint:errcheck
		// Sync rod's internal position tracker so any subsequent rod operations
		// (e.g. el.Click) see the correct cursor location.
		page.Mouse.MoveTo(proto.Point{X: x, Y: y}) //nolint:errcheck
		dbg(fmt.Sprintf("✓ clickXY %.0f,%.0f", x, y))
		return goja.Undefined()
	})

	// humanScroll scrolls the page by deltaY CSS pixels (positive = down) using
	// eased mouse-wheel events with jittered step sizes and intervals, instead of a
	// single instant jump. An options object may set { duration } in ms.
	pc.Set("humanScroll", func(call goja.FunctionCall) goja.Value {
		totalY := call.Argument(0).ToFloat()
		durationMs := 350.0 + rand.Float64()*350
		if len(call.Arguments) > 1 {
			if obj := call.Argument(1).ToObject(vm); obj != nil {
				if v := obj.Get("duration"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
					durationMs = v.ToFloat()
				}
			}
		}
		dbg(fmt.Sprintf("→ humanScroll %.0f", totalY))
		steps := int(math.Abs(totalY)/90) + 4
		if steps > 40 {
			steps = 40
		}
		stepDur := time.Duration(float64(time.Millisecond) * durationMs / float64(steps))
		var done float64
		for i := 1; i <= steps; i++ {
			select {
			case <-page.GetContext().Done():
				return goja.Undefined()
			default:
			}
			t := float64(i) / float64(steps)
			te := t * t * (3 - 2*t) // ease-in-out
			target := totalY * te
			delta := target - done
			done = target
			// Jitter each wheel notch so deltas are not perfectly smooth.
			delta += (rand.Float64()*2 - 1) * math.Abs(delta) * 0.15
			proto.InputDispatchMouseEvent{
				Type:        proto.InputDispatchMouseEventTypeMouseWheel,
				X:           math.Round(mouseX),
				Y:           math.Round(mouseY),
				DeltaX:      0,
				DeltaY:      delta,
				Timestamp:   proto.TimeSinceEpoch(float64(time.Now().UnixNano()) / 1e9),
				PointerType: proto.InputDispatchMouseEventPointerTypeMouse,
			}.Call(page) //nolint:errcheck
			if i < steps {
				d := time.Duration(float64(stepDur) * (0.7 + rand.Float64()*0.6))
				time.Sleep(d)
			}
		}
		dbg(fmt.Sprintf("✓ humanScroll %.0f", totalY))
		return goja.Undefined()
	})

	// humanIdle spends about ms milliseconds producing low-amplitude pointer drift
	// interleaved with pauses, the way a person rests a hand on the mouse. Use it to
	// accumulate natural behavioural signal on pages that classify inactivity as bot.
	pc.Set("humanIdle", func(call goja.FunctionCall) goja.Value {
		ms := call.Argument(0).ToInteger()
		dbg(fmt.Sprintf("→ humanIdle %dms", ms))
		deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
		for time.Now().Before(deadline) {
			select {
			case <-page.GetContext().Done():
				return goja.Undefined()
			default:
			}
			nx := mouseX + (rand.Float64()*2-1)*40
			ny := mouseY + (rand.Float64()*2-1)*30
			humanMoveTo(nx, ny, 120+rand.Float64()*180, -1)
			select {
			case <-page.GetContext().Done():
				return goja.Undefined()
			case <-time.After(time.Duration(200+rand.Intn(600)) * time.Millisecond):
			}
		}
		dbg(fmt.Sprintf("✓ humanIdle %dms", ms))
		return goja.Undefined()
	})

	pc.Set("scrollIntoView", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("→ scrollIntoView " + sel)
		target := findPage(sel)
		if target == page {
			el, err := page.Element(sel)
			must(err)
			must(el.ScrollIntoView())
		} else {
			must(evalInFrame(target, fmt.Sprintf("var el=document.querySelector(%q);if(el)el.scrollIntoView()", sel)))
		}
		dbg("✓ scrollIntoView " + sel)
		return goja.Undefined()
	})

	pc.Set("sendKeys", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		text := argStr(call.Argument(1))
		dbg(fmt.Sprintf("→ sendKeys %s (%d chars)", sel, len(text)))
		// Focus in whichever frame owns the element, then insert text via Input.insertText
		// which Chrome applies to the focused element regardless of frame.
		must(evalInFrame(findPage(sel), fmt.Sprintf("var el=document.querySelector(%q);if(el)el.focus()", sel)))
		must(page.InsertText(text))
		dbg("✓ sendKeys " + sel)
		return goja.Undefined()
	})

	// namedKeys maps CDP/browser key name strings to rod input.Key constants.
	// For single-character keys the rune value is used directly as a fallback.
	namedKeys := map[string]input.Key{
		"Enter": input.Enter, "Return": input.Enter,
		"Tab": input.Tab, "Escape": input.Escape, "Backspace": input.Backspace,
		"Delete": input.Delete, "Insert": input.Insert,
		"Home": input.Home, "End": input.End,
		"PageUp": input.PageUp, "PageDown": input.PageDown,
		"ArrowLeft": input.ArrowLeft, "ArrowRight": input.ArrowRight,
		"ArrowUp": input.ArrowUp, "ArrowDown": input.ArrowDown,
		" ": input.Space, "Space": input.Space,
		"F1": input.F1, "F2": input.F2, "F3": input.F3, "F4": input.F4,
		"F5": input.F5, "F6": input.F6, "F7": input.F7, "F8": input.F8,
		"F9": input.F9, "F10": input.F10, "F11": input.F11, "F12": input.F12,
		"ShiftLeft": input.ShiftLeft, "ShiftRight": input.ShiftRight,
		"ControlLeft": input.ControlLeft, "ControlRight": input.ControlRight,
		"AltLeft": input.AltLeft, "AltRight": input.AltRight,
	}

	pc.Set("keyEvent", func(call goja.FunctionCall) goja.Value {
		key := argStr(call.Argument(0))
		dbg("→ keyEvent " + key)
		if k, ok := namedKeys[key]; ok {
			must(page.Keyboard.Press(k))
		} else {
			runes := []rune(key)
			if len(runes) > 0 {
				must(page.Keyboard.Press(input.Key(runes[0])))
			}
		}
		dbg("✓ keyEvent " + key)
		return goja.Undefined()
	})

	pc.Set("clear", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("→ clear " + sel)
		stmt := fmt.Sprintf(
			`var el=document.querySelector(%q);if(!el)return;`+
				`var n=Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype,'value')`+
				`||Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype,'value');`+
				`if(n&&n.set){n.set.call(el,'')}else{el.value=''};`+
				`el.dispatchEvent(new Event('input',{bubbles:true}));`+
				`el.dispatchEvent(new Event('change',{bubbles:true}))`,
			sel)
		must(evalInFrame(findPage(sel), stmt))
		dbg("✓ clear " + sel)
		return goja.Undefined()
	})

	pc.Set("focus", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("→ focus " + sel)
		target := findPage(sel)
		if target == page {
			el, err := page.Element(sel)
			must(err)
			must(el.Focus())
		} else {
			must(evalInFrame(target, fmt.Sprintf("var el=document.querySelector(%q);if(el)el.focus()", sel)))
		}
		dbg("✓ focus " + sel)
		return goja.Undefined()
	})

	pc.Set("blur", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("→ blur " + sel)
		must(evalInFrame(findPage(sel), fmt.Sprintf("var el=document.querySelector(%q);if(el)el.blur()", sel)))
		dbg("✓ blur " + sel)
		return goja.Undefined()
	})

	pc.Set("submit", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("→ submit " + sel)
		must(evalInFrame(findPage(sel), fmt.Sprintf("var el=document.querySelector(%q);if(el)el.submit()", sel)))
		dbg("✓ submit " + sel)
		return goja.Undefined()
	})

	pc.Set("setValue", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		val := argStr(call.Argument(1))
		dbg(fmt.Sprintf("→ setValue %s = %q", sel, val))
		target := findPage(sel)
		if target == page {
			el, err := page.Element(sel)
			must(err)
			must(el.Input(val))
		} else {
			stmt := fmt.Sprintf(
				`var el=document.querySelector(%q);if(!el)return;`+
					`var n=Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype,'value')`+
					`||Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype,'value');`+
					`if(n&&n.set){n.set.call(el,%q)}else{el.value=%q};`+
					`el.dispatchEvent(new Event('input',{bubbles:true}));`+
					`el.dispatchEvent(new Event('change',{bubbles:true}))`,
				sel, val, val)
			must(evalInFrame(target, stmt))
		}
		dbg("✓ setValue " + sel)
		return goja.Undefined()
	})

	pc.Set("getValue", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("? getValue " + sel)
		target := findPage(sel)
		var val string
		if target == page {
			rPage, rCancel := readPage()
			defer rCancel()
			el, err := rPage.Element(sel)
			must(err)
			prop, err := el.Property("value")
			must(err)
			val = prop.Str()
		} else {
			var err error
			val, err = readFromFrame(target, fmt.Sprintf("(document.querySelector(%q)||{value:''}).value", sel))
			must(err)
		}
		dbg(fmt.Sprintf("= getValue %s %q", sel, val))
		return vm.ToValue(val)
	})

	pc.Set("getText", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("? getText " + sel)
		target := findPage(sel)
		var text string
		if target == page {
			rPage, rCancel := readPage()
			defer rCancel()
			el, err := rPage.Element(sel)
			must(err)
			var err2 error
			text, err2 = el.Text()
			must(err2)
		} else {
			var err error
			text, err = readFromFrame(target, fmt.Sprintf("(document.querySelector(%q)||{innerText:''}).innerText", sel))
			must(err)
		}
		dbg(fmt.Sprintf("= getText %s %q", sel, text))
		return vm.ToValue(text)
	})

	pc.Set("getTextContent", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("? getTextContent " + sel)
		text, err := readFromFrame(findPage(sel), fmt.Sprintf("(document.querySelector(%q)||{textContent:''}).textContent", sel))
		must(err)
		dbg(fmt.Sprintf("= getTextContent %s %q", sel, text))
		return vm.ToValue(text)
	})

	pc.Set("getInnerHTML", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("? getInnerHTML " + sel)
		html, err := readFromFrame(findPage(sel), fmt.Sprintf("(document.querySelector(%q)||{innerHTML:''}).innerHTML", sel))
		must(err)
		dbg(fmt.Sprintf("= getInnerHTML %s (%d bytes)", sel, len(html)))
		return vm.ToValue(html)
	})

	pc.Set("getOuterHTML", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("? getOuterHTML " + sel)
		target := findPage(sel)
		var html string
		if target == page {
			rPage, rCancel := readPage()
			defer rCancel()
			el, err := rPage.Element(sel)
			must(err)
			var err2 error
			html, err2 = el.HTML()
			must(err2)
		} else {
			var err error
			html, err = readFromFrame(target, fmt.Sprintf("(document.querySelector(%q)||{outerHTML:''}).outerHTML", sel))
			must(err)
		}
		dbg(fmt.Sprintf("= getOuterHTML %s (%d bytes)", sel, len(html)))
		return vm.ToValue(html)
	})

	pc.Set("getAttribute", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		attr := argStr(call.Argument(1))
		dbg(fmt.Sprintf("? getAttribute %s[%s]", sel, attr))
		target := findPage(sel)
		if target == page {
			rPage, rCancel := readPage()
			defer rCancel()
			el, err := rPage.Element(sel)
			must(err)
			val, err := el.Attribute(attr)
			must(err)
			if val == nil {
				return goja.Null()
			}
			dbg(fmt.Sprintf("= getAttribute %s[%s] %q", sel, attr, *val))
			return vm.ToValue(*val)
		}
		res, err := (proto.RuntimeEvaluate{
			Expression: fmt.Sprintf(
				"(function(){var el=document.querySelector(%q);if(!el||!el.hasAttribute(%q))return null;return el.getAttribute(%q)})()",
				sel, attr, attr),
			ReturnByValue: true,
		}).Call(target)
		must(err)
		if res == nil || res.Result.Value.Nil() {
			return goja.Null()
		}
		s := res.Result.Value.Str()
		if s == "" || s == "null" || s == "<nil>" {
			return goja.Null()
		}
		dbg(fmt.Sprintf("= getAttribute %s[%s] %q", sel, attr, s))
		return vm.ToValue(s)
	})

	pc.Set("getAttributes", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("? getAttributes " + sel)
		target := findPage(sel)
		if target == page {
			rPage, rCancel := readPage()
			defer rCancel()
			res, err := rPage.Eval(fmt.Sprintf(
				"() => { const el=document.querySelector(%q); if(!el)return {}; return Object.fromEntries([...el.attributes].map(a=>[a.name,a.value])) }",
				sel))
			must(err)
			return vm.ToValue(res.Value.Val())
		}
		res, err := (proto.RuntimeEvaluate{
			Expression: fmt.Sprintf(
				"(function(){var el=document.querySelector(%q);if(!el)return{};return Object.fromEntries([...el.attributes].map(function(a){return[a.name,a.value]}))})()",
				sel),
			ReturnByValue: true,
		}).Call(target)
		must(err)
		return vm.ToValue(res.Result.Value.Val())
	})

	pc.Set("setAttribute", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		attr := argStr(call.Argument(1))
		val := argStr(call.Argument(2))
		dbg(fmt.Sprintf("→ setAttribute %s[%s] = %q", sel, attr, val))
		must(evalInFrame(findPage(sel), fmt.Sprintf("var el=document.querySelector(%q);if(el)el.setAttribute(%q,%q)", sel, attr, val)))
		dbg(fmt.Sprintf("✓ setAttribute %s[%s]", sel, attr))
		return goja.Undefined()
	})

	pc.Set("removeAttribute", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		attr := argStr(call.Argument(1))
		dbg(fmt.Sprintf("→ removeAttribute %s[%s]", sel, attr))
		must(evalInFrame(findPage(sel), fmt.Sprintf("var el=document.querySelector(%q);if(el)el.removeAttribute(%q)", sel, attr)))
		dbg(fmt.Sprintf("✓ removeAttribute %s[%s]", sel, attr))
		return goja.Undefined()
	})

	pc.Set("getJSAttribute", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		attr := argStr(call.Argument(1))
		dbg(fmt.Sprintf("? getJSAttribute %s.%s", sel, attr))
		target := findPage(sel)
		if target == page {
			rPage, rCancel := readPage()
			defer rCancel()
			el, err := rPage.Element(sel)
			must(err)
			prop, err := el.Property(attr)
			must(err)
			dbg(fmt.Sprintf("= getJSAttribute %s.%s %v", sel, attr, prop.Val()))
			return vm.ToValue(prop.Val())
		}
		res, err := (proto.RuntimeEvaluate{
			Expression:    fmt.Sprintf("(document.querySelector(%q)||{})[%q]", sel, attr),
			ReturnByValue: true,
		}).Call(target)
		must(err)
		if res == nil || res.Result.Value.Nil() {
			return goja.Undefined()
		}
		return vm.ToValue(res.Result.Value.Val())
	})

	pc.Set("setJSAttribute", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		attr := argStr(call.Argument(1))
		val := argStr(call.Argument(2))
		dbg(fmt.Sprintf("→ setJSAttribute %s.%s = %q", sel, attr, val))
		must(evalInFrame(findPage(sel), fmt.Sprintf("var el=document.querySelector(%q);if(el)el[%q]=%q", sel, attr, val)))
		dbg(fmt.Sprintf("✓ setJSAttribute %s.%s", sel, attr))
		return goja.Undefined()
	})

	pc.Set("getNodeCount", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		dbg("? getNodeCount " + sel)
		target := findPage(sel)
		if target == page {
			rPage, rCancel := readPage()
			defer rCancel()
			els, err := rPage.Elements(sel)
			if err != nil {
				return vm.ToValue(0)
			}
			dbg(fmt.Sprintf("= getNodeCount %s %d", sel, len(els)))
			return vm.ToValue(len(els))
		}
		s, err := readFromFrame(target, fmt.Sprintf("document.querySelectorAll(%q).length", sel))
		if err != nil {
			return vm.ToValue(0)
		}
		var n int
		fmt.Sscanf(s, "%d", &n)
		dbg(fmt.Sprintf("= getNodeCount %s %d", sel, n))
		return vm.ToValue(n)
	})

	pc.Set("evaluate", func(call goja.FunctionCall) goja.Value {
		dbg("→ evaluate")
		expr := argStr(call.Argument(0))
		rPage, rCancel := readPage()
		defer rCancel()
		res, err := proto.RuntimeEvaluate{Expression: expr}.Call(rPage)
		must(err)
		result := res.Result.Value.Val()
		dbg(fmt.Sprintf("= evaluate %v", result))
		return vm.ToValue(result)
	})

	pc.Set("screenshot", func(call goja.FunctionCall) goja.Value {
		name := argStr(call.Argument(0))
		dbg("→ screenshot " + name)
		rPage, rCancel := readPage()
		defer rCancel()
		info, _ := rPage.Info()
		buf, err := rPage.Screenshot(true, nil)
		if err != nil {
			if emitter != nil {
				emitter.log(fmt.Sprintf("[screenshot] %s: %s", name, err))
			}
			return goja.Undefined()
		}
		pageURL := ""
		if info != nil {
			pageURL = info.URL
		}
		if emitter != nil {
			emitter.screenshot(name, buf, pageURL)
		}
		dbg("✓ screenshot " + name)
		return goja.Undefined()
	})

	pc.Set("screenshotElement", func(call goja.FunctionCall) goja.Value {
		sel := argStr(call.Argument(0))
		name := argStr(call.Argument(1))
		dbg(fmt.Sprintf("→ screenshotElement %s as %s", sel, name))
		rPage, rCancel := readPage()
		defer rCancel()
		info, _ := rPage.Info()
		el, err := rPage.Element(sel)
		must(err)
		must(el.WaitVisible())
		buf, err := el.Screenshot("", 0)
		if err != nil {
			if emitter != nil {
				emitter.log(fmt.Sprintf("[screenshot] %s: %s", name, err))
			}
			return goja.Undefined()
		}
		pageURL := ""
		if info != nil {
			pageURL = info.URL
		}
		if emitter != nil {
			emitter.screenshot(name, buf, pageURL)
		}
		dbg("✓ screenshotElement " + name)
		return goja.Undefined()
	})

	pc.Set("domDump", func(call goja.FunctionCall) goja.Value {
		name := argStr(call.Argument(0))
		dbg("→ domDump " + name)
		rPage, rCancel := readPage()
		defer rCancel()
		info, _ := rPage.Info()
		html, err := rPage.HTML()
		if err != nil {
			if emitter != nil {
				emitter.log(fmt.Sprintf("[domDump] %s: %s", name, err))
			}
			return goja.Undefined()
		}
		pageURL := ""
		if info != nil {
			pageURL = info.URL
		}
		if emitter != nil {
			emitter.domDump(name, html, pageURL)
		}
		dbg(fmt.Sprintf("✓ domDump %s (%d bytes)", name, len(html)))
		return goja.Undefined()
	})

	pc.Set("setViewport", func(call goja.FunctionCall) goja.Value {
		w := call.Argument(0).ToInteger()
		h := call.Argument(1).ToInteger()
		dbg(fmt.Sprintf("→ setViewport %dx%d", w, h))
		// Override the screen dimensions to match the viewport. Otherwise screen.*
		// stays at the headless window default (800x600) while innerWidth/Height take
		// the new size, which detectors flag as an impossible viewport-exceeds-screen.
		sw, sh := int(w), int(h)
		must(proto.EmulationSetDeviceMetricsOverride{
			Width: int(w), Height: int(h), DeviceScaleFactor: 1,
			ScreenWidth: &sw, ScreenHeight: &sh,
		}.Call(page))
		dbg(fmt.Sprintf("✓ setViewport %dx%d", w, h))
		return goja.Undefined()
	})

	pc.Set("setViewportMobile", func(call goja.FunctionCall) goja.Value {
		w := call.Argument(0).ToInteger()
		h := call.Argument(1).ToInteger()
		dbg(fmt.Sprintf("→ setViewportMobile %dx%d", w, h))
		sw, sh := int(w), int(h)
		must(proto.EmulationSetDeviceMetricsOverride{
			Width: int(w), Height: int(h), DeviceScaleFactor: 1,
			Mobile:      true,
			ScreenWidth: &sw, ScreenHeight: &sh,
		}.Call(page))
		must(proto.EmulationSetTouchEmulationEnabled{Enabled: true}.Call(page))
		dbg(fmt.Sprintf("✓ setViewportMobile %dx%d", w, h))
		return goja.Undefined()
	})

	pc.Set("resetViewport", func(call goja.FunctionCall) goja.Value {
		dbg("→ resetViewport")
		must(proto.EmulationClearDeviceMetricsOverride{}.Call(page))
		dbg("✓ resetViewport")
		return goja.Undefined()
	})

	pc.Set("setUserAgent", func(call goja.FunctionCall) goja.Value {
		ua := argStr(call.Argument(0))
		dbg("→ setUserAgent " + ua)
		// Set platform and client-hint metadata alongside the UA string. A string-only
		// override leaves navigator.platform and Sec-CH-UA untouched, so they contradict
		// the UA. Two limits remain, both unavoidable at the page level:
		//   - This override is page scoped and does not reach the service worker, which
		//     keeps the process (launch-flag) identity. For an identity consistent across
		//     every context, pass userAgent to newSession() instead of calling this.
		//   - navigator.platform in worker contexts follows the host OS and cannot be
		//     overridden, so a cross-OS UA (e.g. macOS on a Linux host) still mismatches
		//     there. Prefer a UA whose OS matches the host.
		must(proto.EmulationSetUserAgentOverride{
			UserAgent:         ua,
			Platform:          uaPlatform(ua),
			UserAgentMetadata: uaMetadata(ua),
		}.Call(page))
		dbg("✓ setUserAgent")
		return goja.Undefined()
	})

	// setAcceptLanguage sets the Accept-Language HTTP header and overrides
	// navigator.language / navigator.languages in the main frame via CDP.
	// For true worker consistency, prefer the lang option in newSession() which
	// sets Chrome's --lang flag at process level. This method is useful for
	// remote-mode sessions where launch flags cannot be changed.
	pc.Set("setAcceptLanguage", func(call goja.FunctionCall) goja.Value {
		lang := argStr(call.Argument(0))
		dbg("→ setAcceptLanguage " + lang)
		// Preserve the current UA - EmulationSetUserAgentOverride requires it.
		uaRes, err := page.Eval("navigator.userAgent")
		if err != nil {
			panic(vm.NewGoError(err))
		}
		must(proto.EmulationSetUserAgentOverride{
			UserAgent:      uaRes.Value.String(),
			AcceptLanguage: lang,
		}.Call(page))
		dbg("✓ setAcceptLanguage")
		return goja.Undefined()
	})

	pc.Set("wait", func(call goja.FunctionCall) goja.Value {
		ms := call.Argument(0).ToInteger()
		dbg(fmt.Sprintf("→ wait %dms", ms))
		select {
		case <-page.GetContext().Done():
			must(page.GetContext().Err())
		case <-time.After(time.Duration(ms) * time.Millisecond):
		}
		dbg(fmt.Sprintf("✓ wait %dms", ms))
		return goja.Undefined()
	})

	// disableFidoUI enables the CDP WebAuthn virtual authenticator environment.
	// In this mode Chrome intercepts WebAuthn/FIDO requests via CDP instead of
	// showing the native "Passkeys & Security Keys" browser dialog, so DOM
	// interactions remain possible while on the FIDO page.
	pc.Set("disableFidoUI", func(call goja.FunctionCall) goja.Value {
		dbg("→ disableFidoUI")
		must(proto.WebAuthnEnable{}.Call(page))
		dbg("✓ disableFidoUI")
		return goja.Undefined()
	})

	// injectScript registers a JS snippet that runs before any page scripts on
	// every subsequent navigation (CDP Page.addScriptToEvaluateOnNewDocument).
	// Scoped to this page target only - does not affect other tabs or sessions.
	pc.Set("injectScript", func(call goja.FunctionCall) goja.Value {
		js := argStr(call.Argument(0))
		if js == "" {
			panic(vm.NewTypeError("injectScript: script string required"))
		}
		dbg("→ injectScript")
		_, err := proto.PageAddScriptToEvaluateOnNewDocument{Source: js}.Call(page)
		must(err)
		dbg("✓ injectScript")
		return goja.Undefined()
	})
}
