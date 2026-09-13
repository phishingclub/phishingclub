package remotebrowser

import (
	"strings"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// Header rewriting lets an operator script add, replace, or drop HTTP headers on
// the traffic between the remote browser and the sites it talks to. It is driven
// by four script methods (setRequestHeader, removeRequestHeader,
// setResponseHeader, removeResponseHeader) that register rules here.
//
// The rewrite runs over the CDP Fetch domain using continueRequest and
// continueResponse only. The browser still opens its own connection and loads
// the body itself; this code only edits the header list as it passes. Responses
// are never re-fetched or synthesized, so the real protocol, transfer sizes, and
// connection timing stay intact and a normal login is not disturbed.

// headerDir is the traffic direction a rule applies to.
type headerDir int

const (
	headerDirRequest headerDir = iota
	headerDirResponse
)

// headerRule is one add-or-replace (set) or drop (remove) instruction, scoped to
// a set of URL globs. An empty targets list matches every request.
type headerRule struct {
	key     string
	value   string // used only when set is true
	set     bool   // true = add or overwrite, false = remove
	targets []string
}

// headerRules holds the per-session rule set and the Fetch interception state.
// It is read from the Fetch event goroutine and written from the script (goja)
// thread, so every access is guarded by mu.
type headerRules struct {
	mu        sync.Mutex
	request   []headerRule
	response  []headerRule
	installed bool
}

// add appends a rule for one direction.
func (hr *headerRules) add(dir headerDir, r headerRule) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	if dir == headerDirResponse {
		hr.response = append(hr.response, r)
	} else {
		hr.request = append(hr.request, r)
	}
}

// ensureInstalled enables Fetch interception and starts the event loop the first
// time any rule is registered. It is safe to call on every rule add. Callers
// register rules before navigation, so at install time the page is about:blank
// with no requests in flight and none can be missed while the loop starts.
//
// Both the request and the response stage are enabled so request-direction and
// response-direction rules both fire. That pauses each request twice; the cost
// is one extra continue per request, paid only once header rewriting is used.
func (hr *headerRules) ensureInstalled(page *rod.Page, emitter *channelEmitter) error {
	hr.mu.Lock()
	if hr.installed {
		hr.mu.Unlock()
		return nil
	}
	hr.installed = true
	hr.mu.Unlock()

	// EachEvent subscribes and enables Fetch (request stage) synchronously, then
	// returns a wait function that drains events. The explicit FetchEnable below
	// runs after it to add the response stage; the later enable sets the patterns.
	wait := page.EachEvent(func(e *proto.FetchRequestPaused) {
		hr.handle(page, e)
	})

	err := proto.FetchEnable{
		Patterns: []*proto.FetchRequestPattern{
			{URLPattern: "*", RequestStage: proto.FetchRequestStageRequest},
			{URLPattern: "*", RequestStage: proto.FetchRequestStageResponse},
		},
	}.Call(page)
	if err != nil {
		hr.mu.Lock()
		hr.installed = false
		hr.mu.Unlock()
		return err
	}

	go wait()

	if emitter != nil {
		emitter.log("[session] header rewriting enabled (Fetch interception)")
	}
	return nil
}

// handle continues one paused request, editing headers when a rule matches. It
// must continue every request it sees, matched or not, or the request stalls.
func (hr *headerRules) handle(page *rod.Page, e *proto.FetchRequestPaused) {
	url := ""
	if e.Request != nil {
		url = e.Request.URL
	}

	// Response stage: the request is at the response stage when a status code or
	// an error reason is present. Only a real response (status code set) can have
	// its headers rewritten; an errored response is passed through untouched.
	if e.ResponseStatusCode != nil || e.ResponseErrorReason != "" {
		hr.mu.Lock()
		rules := hr.response
		hr.mu.Unlock()

		if e.ResponseStatusCode == nil || !anyRuleMatches(rules, url) {
			_ = proto.FetchContinueResponse{RequestID: e.RequestID}.Call(page)
			return
		}
		existing := entriesFromHeaderList(e.ResponseHeaders)
		newHeaders := applyHeaderRules(rules, url, existing)
		// continueResponse requires the response code alongside modified headers.
		code := *e.ResponseStatusCode
		_ = proto.FetchContinueResponse{
			RequestID:       e.RequestID,
			ResponseCode:    &code,
			ResponsePhrase:  e.ResponseStatusText, // empty lets CDP fill the standard phrase
			ResponseHeaders: newHeaders,
		}.Call(page)
		return
	}

	// Request stage.
	hr.mu.Lock()
	rules := hr.request
	hr.mu.Unlock()

	if e.Request == nil || !anyRuleMatches(rules, url) {
		_ = proto.FetchContinueRequest{RequestID: e.RequestID}.Call(page)
		return
	}
	existing := entriesFromNetworkHeaders(e.Request.Headers)
	newHeaders := applyHeaderRules(rules, url, existing)
	_ = proto.FetchContinueRequest{
		RequestID: e.RequestID,
		Headers:   newHeaders,
	}.Call(page)
}

// headerKV is one header name and value while rules are applied.
type headerKV struct {
	name  string
	value string
}

func entriesFromHeaderList(list []*proto.FetchHeaderEntry) []headerKV {
	out := make([]headerKV, 0, len(list))
	for _, h := range list {
		if h == nil {
			continue
		}
		out = append(out, headerKV{name: h.Name, value: h.Value})
	}
	return out
}

func entriesFromNetworkHeaders(h proto.NetworkHeaders) []headerKV {
	out := make([]headerKV, 0, len(h))
	for name, v := range h {
		out = append(out, headerKV{name: name, value: v.Str()})
	}
	return out
}

// applyHeaderRules rebuilds the header list for one request with every matching
// rule applied. Removes are applied first, then sets, so a remove followed by a
// set of the same key yields a single clean value. A set replaces any existing
// value for its key rather than adding a second one.
func applyHeaderRules(rules []headerRule, url string, existing []headerKV) []*proto.FetchHeaderEntry {
	remove := map[string]bool{}
	setOrder := []headerKV{}
	setIndex := map[string]int{}

	for _, r := range rules {
		if !ruleMatches(r, url) {
			continue
		}
		lk := strings.ToLower(r.key)
		if r.set {
			if i, ok := setIndex[lk]; ok {
				setOrder[i] = headerKV{name: r.key, value: r.value}
			} else {
				setIndex[lk] = len(setOrder)
				setOrder = append(setOrder, headerKV{name: r.key, value: r.value})
			}
			delete(remove, lk) // a set wins over an earlier remove of the same key
		} else {
			remove[lk] = true
		}
	}

	out := make([]*proto.FetchHeaderEntry, 0, len(existing)+len(setOrder))
	for _, e := range existing {
		lk := strings.ToLower(e.name)
		if remove[lk] {
			continue
		}
		if _, ok := setIndex[lk]; ok {
			continue // re-added below with the operator value
		}
		out = append(out, &proto.FetchHeaderEntry{Name: e.name, Value: e.value})
	}
	for _, s := range setOrder {
		out = append(out, &proto.FetchHeaderEntry{Name: s.name, Value: s.value})
	}
	return out
}

func anyRuleMatches(rules []headerRule, url string) bool {
	for _, r := range rules {
		if ruleMatches(r, url) {
			return true
		}
	}
	return false
}

func ruleMatches(r headerRule, url string) bool {
	if len(r.targets) == 0 {
		return true
	}
	for _, t := range r.targets {
		if globMatch(t, url) {
			return true
		}
	}
	return false
}

// globMatch reports whether s matches a pattern where "*" stands for zero or
// more characters. Matching is case sensitive; the parts between stars must
// appear in order, and the pattern is anchored at each end unless it starts or
// ends with a star.
func globMatch(pattern, s string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	parts := strings.Split(pattern, "*")
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(s[pos:], part)
		if idx < 0 {
			return false
		}
		if i == 0 && !strings.HasPrefix(pattern, "*") && idx != 0 {
			return false
		}
		pos += idx + len(part)
	}
	if !strings.HasSuffix(pattern, "*") {
		last := parts[len(parts)-1]
		if last != "" && !strings.HasSuffix(s, last) {
			return false
		}
	}
	return true
}

// headerEditForbidden reports headers that frame or route the message. Editing
// them corrupts the request or response, so the script methods reject them
// outright rather than let a rule break the connection.
func headerEditForbidden(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "content-length", "transfer-encoding", "host", "connection":
		return true
	}
	// HTTP/2 pseudo-headers.
	return strings.HasPrefix(strings.TrimSpace(key), ":")
}

// headerEditRisky reports headers that are safe to edit at the protocol level but
// commonly break a login flow when changed (cookies and CORS). The script
// methods allow these but log a warning.
func headerEditRisky(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "set-cookie", "cookie",
		"access-control-allow-origin",
		"access-control-allow-credentials",
		"access-control-allow-headers",
		"access-control-allow-methods":
		return true
	}
	return false
}
