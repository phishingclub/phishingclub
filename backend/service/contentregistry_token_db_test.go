package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/registry"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// tokenRegistry is a fake Bait Box that accepts only the bearer token want and
// records the last Authorization header it saw.
type tokenRegistry struct {
	srv  *httptest.Server
	mu   sync.Mutex
	last string
}

func newTokenRegistry(t *testing.T, want string) *tokenRegistry {
	t.Helper()
	tr := &tokenRegistry{}
	tr.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr.mu.Lock()
		tr.last = r.Header.Get("Authorization")
		tr.mu.Unlock()
		if want != "" && r.Header.Get("Authorization") != "Bearer "+want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/v1/info" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.URL.Path == "/api/v1/index" {
			_, _ = w.Write([]byte(`{"packages":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(tr.srv.Close)
	return tr
}

func (tr *tokenRegistry) lastAuth() string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.last
}

func TestRegistryTokenTrimmedOnAdd(t *testing.T) {
	h := newRegFixture(t)
	reg := newTokenRegistry(t, "secret")
	id, err := h.s.AddRegistry(h.ctx, h.session, "Box", reg.srv.URL, "  secret \n", true)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	row, err := h.s.RegistryRepository.GetRegistryByID(h.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if row.Token != "secret" {
		t.Fatalf("token not trimmed: %q", row.Token)
	}
}

func TestRegistryAddRefusedTokenMessage(t *testing.T) {
	h := newRegFixture(t)
	reg := newTokenRegistry(t, "secret")
	_, err := h.s.AddRegistry(h.ctx, h.session, "Box", reg.srv.URL, "wrong", true)
	if err == nil {
		t.Fatal("expected a refused token")
	}
	var ve errs.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected a validation error, got %T", err)
	}
	if !strings.Contains(err.Error(), "refused the token") || strings.Contains(err.Error(), "/api/") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestRegistryAddTransportErrorIsShort(t *testing.T) {
	h := newRegFixture(t)
	reg := newTokenRegistry(t, "")
	// without the insecure flag the self signed test cert is refused.
	_, err := h.s.AddRegistry(h.ctx, h.session, "Box", reg.srv.URL, "", false)
	if err == nil {
		t.Fatal("expected a TLS error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Allow self signed TLS") ||
		strings.Contains(msg, "x509") || strings.Contains(msg, "/api/") {
		t.Fatalf("unexpected message: %v", msg)
	}
}

func TestRegistryUpdateTokenRules(t *testing.T) {
	h := newRegFixture(t)
	regA := newTokenRegistry(t, "")
	regB := newTokenRegistry(t, "")
	n := 0
	insert := func(url, token string, insecure bool) *database.Registry {
		n++
		id, err := h.s.RegistryRepository.InsertRegistry(h.ctx, &database.Registry{
			Name: "Box " + strconv.Itoa(n), URL: url, Token: token, InsecureTLS: insecure,
		})
		if err != nil {
			t.Fatal(err)
		}
		row, err := h.s.RegistryRepository.GetRegistryByID(h.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	stored := func(r *database.Registry) string {
		row, err := h.s.RegistryRepository.GetRegistryByID(h.ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		return row.Token
	}

	t.Run("empty keeps the stored token", func(t *testing.T) {
		r := insert(regA.srv.URL, "old", true)
		err := h.s.UpdateRegistry(
			h.ctx, h.session, r.ID,
			UpdateRegistryInput{Name: r.Name, URL: regA.srv.URL + "/", Insecure: true},
		)
		if err != nil {
			t.Fatal(err)
		}
		if stored(r) != "old" || regA.lastAuth() != "Bearer old" {
			t.Fatalf("stored token not kept: %q, sent %q", stored(r), regA.lastAuth())
		}
	})

	t.Run("new token is trimmed", func(t *testing.T) {
		r := insert(regA.srv.URL, "old", true)
		in := UpdateRegistryInput{Name: r.Name, Token: " new\t", Insecure: true}
		err := h.s.UpdateRegistry(h.ctx, h.session, r.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if stored(r) != "new" {
			t.Fatalf("got %q", stored(r))
		}
	})

	t.Run("clear removes the stored token", func(t *testing.T) {
		r := insert(regA.srv.URL, "old", true)
		in := UpdateRegistryInput{Name: r.Name, ClearToken: true, Insecure: true}
		err := h.s.UpdateRegistry(h.ctx, h.session, r.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if stored(r) != "" || regA.lastAuth() != "" {
			t.Fatalf("token not cleared: %q, sent %q", stored(r), regA.lastAuth())
		}
	})

	t.Run("clear with a new token is refused", func(t *testing.T) {
		r := insert(regA.srv.URL, "old", true)
		err := h.s.UpdateRegistry(
			h.ctx, h.session, r.ID,
			UpdateRegistryInput{Name: r.Name, Token: "x", ClearToken: true, Insecure: true},
		)
		var ve errs.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("expected validation error, got %v", err)
		}
	})

	t.Run("host change keeps the stored token", func(t *testing.T) {
		r := insert(regA.srv.URL, "old", false)
		in := UpdateRegistryInput{Name: r.Name, URL: regB.srv.URL, Insecure: true}
		err := h.s.UpdateRegistry(h.ctx, h.session, r.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		if stored(r) != "old" || regB.lastAuth() != "Bearer old" {
			t.Fatalf("stored token not kept: %q, sent %q", stored(r), regB.lastAuth())
		}
	})
}

// fakeDepRegistry serves pkgs, refuses the token for any package under
// acme/locked and answers 404 for anything it does not know.
func fakeDepRegistry(t *testing.T, pkgs map[string]fakeArchive) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/info" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		rest, ok := strings.CutPrefix(r.URL.Path, "/api/v1/packages/")
		parts := strings.Split(rest, "/")
		if !ok || len(parts) < 3 {
			http.NotFound(w, r)
			return
		}
		if parts[0]+"/"+parts[1] == "acme/locked" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p, ok := pkgs[parts[0]+"/"+parts[1]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 4 && parts[3] == "download" {
			_, _ = w.Write(p.archive)
			return
		}
		_ = json.NewEncoder(w).Encode(
			registry.VersionResponse{Version: p.version, ContentHash: p.hash, ArchiveSHA: p.sha},
		)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestInspectRefusedDependencyIsNotBroken: a referenced package the token may
// not read is reported as refused with the token message, not as a package
// that no longer exists.
func TestInspectRefusedDependencyIsNotBroken(t *testing.T) {
	h := newRegFixture(t)
	pkgs := map[string]fakeArchive{
		"acme/ok": buildFakeArchive(t, "acme/ok", "page", nil, map[string]string{"README.md": "ok"}),
		"acme/bundle": buildFakeArchive(t, "acme/bundle", "bundle",
			[]string{"acme/ok", "acme/locked", "acme/gone"}, map[string]string{"README.md": "b"}),
	}
	srv := fakeDepRegistry(t, pkgs)
	id, err := h.s.RegistryRepository.InsertRegistry(
		h.ctx, &database.Registry{Name: "Box", URL: srv.URL, InsecureTLS: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.s.Inspect(h.ctx, h.session, id, "acme", "bundle")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.BrokenDependencies) != 1 || res.BrokenDependencies[0] != "acme/gone" {
		t.Fatalf("broken: %v", res.BrokenDependencies)
	}
	if len(res.RefusedDependencies) != 1 || res.RefusedDependencies[0] != "acme/locked" {
		t.Fatalf("refused: %v", res.RefusedDependencies)
	}
	if res.RefusedMessage != tokenRefusedMessage {
		t.Fatalf("refused message: %q", res.RefusedMessage)
	}
}

// TestInspectUnansweredDependencyIsNotBroken: a reference the Bait Box failed
// to answer for, by a server error or a dropped connection, is never reported
// as deleted. The inspect fails with a short reason instead.
func TestInspectUnansweredDependencyIsNotBroken(t *testing.T) {
	pkgs := map[string]fakeArchive{
		"acme/bundle": buildFakeArchive(t, "acme/bundle", "bundle",
			[]string{"acme/flaky", "acme/a", "acme/b", "acme/c", "acme/d", "acme/e"},
			map[string]string{"README.md": "b"}),
	}
	inner := fakeDepRegistry(t, pkgs)
	for _, c := range []struct {
		name, want string
		fail       func(w http.ResponseWriter)
	}{
		{"server error", "HTTP 502", func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) }},
		{"dropped connection", "the connection failed", func(w http.ResponseWriter) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newRegFixture(t)
			// every dependency lookup fails; the bundle itself is served.
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/api/v1/packages/acme/bundle/") {
					proxyTo(t, inner, w, r)
					return
				}
				c.fail(w)
			}))
			t.Cleanup(srv.Close)
			id, err := h.s.RegistryRepository.InsertRegistry(h.ctx, &database.Registry{
				Name: "Box", URL: srv.URL, InsecureTLS: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			res, err := h.s.Inspect(h.ctx, h.session, id, "acme", "bundle")
			if err == nil {
				t.Fatalf("expected the inspect to fail, broken %v", res.BrokenDependencies)
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "could not inspect package: ") || !strings.Contains(msg, c.want) {
				t.Fatalf("unexpected message: %q", msg)
			}
			if strings.Contains(msg, "/api/") || strings.Contains(msg, "no longer") {
				t.Fatalf("message leaks detail or reports a deletion: %q", msg)
			}
		})
	}
}

// proxyTo replays r against srv and copies the answer to w.
func proxyTo(t *testing.T, srv *httptest.Server, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, srv.URL+r.URL.Path, nil)
	if err != nil {
		t.Error(err)
		return
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Error(err)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// TestListInstalledStatusPerPackage: each package status comes from its own
// lookup, so one refused or missing package never changes another's status.
func TestListInstalledStatusPerPackage(t *testing.T) {
	h := newRegFixture(t)
	pkgs := map[string]fakeArchive{}
	for i := 0; i < 8; i++ {
		id := "acme/ok" + strconv.Itoa(i)
		pkgs[id] = buildFakeArchive(t, id, "page", nil, map[string]string{"README.md": id})
	}
	srv := fakeDepRegistry(t, pkgs)
	rid, err := h.s.RegistryRepository.InsertRegistry(
		h.ctx, &database.Registry{Name: "Box", URL: srv.URL, InsecureTLS: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	add := func(pid, hash string) {
		if _, err := h.s.RegistryRepository.InsertInstalledPackage(h.ctx, &database.InstalledPackage{
			RegistryID: rid, PackageID: pid, PackageType: "page", Version: "1.0.0", ContentHash: hash,
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("acme/locked", "x")
	add("acme/gone", "x")
	for id, p := range pkgs {
		hash := p.hash
		if id == "acme/ok0" {
			hash = "older"
		}
		add(id, hash)
	}
	views, err := h.s.ListInstalled(h.ctx, h.session)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 10 {
		t.Fatalf("expected 10 rows, got %d", len(views))
	}
	for _, v := range views {
		want := installedStatusCurrent
		switch v.PackageID {
		case "acme/locked":
			want = installedStatusTokenRefused
		case "acme/gone":
			want = installedStatusUnknown
		case "acme/ok0":
			want = installedStatusUpdate
		}
		if v.Status != want {
			t.Errorf("%s: status %q, want %q", v.PackageID, v.Status, want)
		}
		if (v.Status == installedStatusTokenRefused) != (v.StatusMessage == tokenRefusedMessage) {
			t.Errorf("%s: status message %q", v.PackageID, v.StatusMessage)
		}
	}
}

// auditCapture swaps the fixture logger for one that keeps the audit events.
func auditCapture(t *testing.T, h *regFixture) func(name string) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.Lock(zapcore.AddSync(&buf)),
		zapcore.InfoLevel,
	)
	h.s.Logger = zap.New(core).Sugar()
	return func(name string) []map[string]any {
		var out []map[string]any
		sc := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
		for sc.Scan() {
			var line map[string]any
			if json.Unmarshal(sc.Bytes(), &line) != nil || line["name"] != name {
				continue
			}
			d, _ := line["details"].(map[string]any)
			out = append(out, d)
		}
		return out
	}
}

func TestRegistryAuditNamesTheBaitBox(t *testing.T) {
	h := newRegFixture(t)
	events := auditCapture(t, h)
	reg := newTokenRegistry(t, "secret")
	id, err := h.s.RegistryRepository.InsertRegistry(h.ctx, &database.Registry{
		Name: "Box", URL: reg.srv.URL, Token: "secret", InsecureTLS: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	const typed = "tok-typed-XYZ123"
	err = h.s.UpdateRegistry(h.ctx, h.session, id, UpdateRegistryInput{Name: "Box", Token: typed, Insecure: true})
	if err == nil {
		t.Fatal("expected the refused token to fail the edit")
	}
	up := events("Registry.Update")
	if len(up) != 1 || up[0]["registryId"] != id.String() ||
		up[0]["tokenChanged"] != true || up[0]["result"] != "failed" {
		t.Fatalf("update failure event: %v", up)
	}
	if err := h.s.UpdateRegistry(
		h.ctx, h.session, id, UpdateRegistryInput{Name: "Box", Token: "secret", Insecure: true},
	); err != nil {
		t.Fatal(err)
	}
	up = events("Registry.Update")
	if len(up) != 2 || up[1]["registryId"] != id.String() || up[1]["tokenChanged"] != true || up[1]["result"] != nil {
		t.Fatalf("update success event: %v", up)
	}
	for _, e := range up {
		for k, v := range e {
			if s, ok := v.(string); ok && (strings.Contains(s, typed) || strings.Contains(s, "secret")) {
				t.Fatalf("audit detail %q leaks the token", k)
			}
		}
	}
	if err := h.db.Migrator().DropTable(&database.Registry{}); err != nil {
		t.Fatal(err)
	}
	if err := h.s.DeleteRegistry(h.ctx, h.session, id); err == nil {
		t.Fatal("expected delete to fail without the table")
	}
	del := events("Registry.Delete")
	if len(del) != 1 || del[0]["registryId"] != id.String() || del[0]["result"] != "failed" {
		t.Fatalf("delete failure event: %v", del)
	}
}

// TestRegistryAuditKeys: every registry event names what it acted on with the
// same keys, set before any step that can fail, and never carries the token.
func TestRegistryAuditKeys(t *testing.T) {
	h := newRegFixture(t)
	events := auditCapture(t, h)
	reg := newTokenRegistry(t, "secret")

	// add: the url is recorded even when the connection test fails.
	if _, err := h.s.AddRegistry(h.ctx, h.session, "Box", reg.srv.URL+" ", "wrong-token", true); err == nil {
		t.Fatal("expected the refused token to fail the add")
	}
	add := events("Registry.Add")
	if len(add) != 1 || add[0]["url"] != reg.srv.URL || add[0]["result"] != "failed" {
		t.Fatalf("add failure event: %v", add)
	}
	rid, err := h.s.AddRegistry(h.ctx, h.session, "Box", reg.srv.URL, "secret", true)
	if err != nil {
		t.Fatal(err)
	}
	add = events("Registry.Add")
	if len(add) != 2 || add[1]["registryId"] != rid.String() || add[1]["url"] != reg.srv.URL {
		t.Fatalf("add success event: %v", add)
	}

	// install: a failed fetch still records the Bait Box and the package address.
	if _, err := h.s.Install(h.ctx, h.session, rid, "acme", "missing", nil, nil); err == nil {
		t.Fatal("expected the install to fail")
	}
	in := events("Registry.Install")
	if len(in) != 1 || in[0]["registryId"] != rid.String() || in[0]["packageId"] != "acme/missing" ||
		in[0]["result"] != "failed" {
		t.Fatalf("install failure event: %v", in)
	}
	if _, ok := in[0]["id"]; ok {
		t.Fatalf("install event still uses the id key: %v", in[0])
	}

	// update: a failed update records the install, its Bait Box and package.
	h.withUpdateDeps("acme/page")
	if _, err := h.s.Update(h.ctx, h.session, h.ipID, nil, nil, nil); err == nil {
		t.Fatal("expected the update to fail")
	}
	up := events("Registry.PackageUpdate")
	if len(up) != 1 || up[0]["installedPackageId"] != h.ipID.String() || up[0]["packageId"] != "acme/page" ||
		up[0]["registryId"] == nil || up[0]["result"] != "failed" {
		t.Fatalf("update failure event: %v", up)
	}

	// uninstall: the install uuid is under installedPackageId, never id.
	_ = h.s.Remove(h.ctx, h.session, h.ipID)
	rm := events("Registry.Remove")
	if len(rm) == 0 || rm[0]["installedPackageId"] != h.ipID.String() {
		t.Fatalf("remove event: %v", rm)
	}
	if _, ok := rm[0]["id"]; ok {
		t.Fatalf("remove event still uses the id key: %v", rm[0])
	}

	for _, name := range []string{"Registry.Add", "Registry.Install", "Registry.PackageUpdate", "Registry.Remove"} {
		for _, e := range events(name) {
			for k, v := range e {
				str, ok := v.(string)
				if ok && (strings.Contains(str, "secret") || strings.Contains(str, "wrong-token")) {
					t.Fatalf("%s detail %q leaks the token", name, k)
				}
			}
		}
	}
}

func TestRegistryStatusTokenRefused(t *testing.T) {
	h := newRegFixture(t)
	reg := newTokenRegistry(t, "secret")
	id, err := h.s.RegistryRepository.InsertRegistry(h.ctx, &database.Registry{
		Name: "Box", URL: reg.srv.URL, Token: "revoked", InsecureTLS: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := h.s.RegistryStatus(h.ctx, h.session, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Connected || !st.TokenRefused || st.ConnectError != tokenRefusedMessage {
		t.Fatalf("unexpected status: %+v", st)
	}
	if _, err := h.s.Browse(h.ctx, h.session, id); err == nil || err.Error() != tokenRefusedMessage {
		t.Fatalf("browse: %v", err)
	}
	if _, err := h.s.Inspect(h.ctx, h.session, id, "acme", "x"); err == nil || err.Error() != tokenRefusedMessage {
		t.Fatalf("inspect: %v", err)
	}
}

func TestRegistryErrorMapping(t *testing.T) {
	if got := connErrReason(registry.ErrUnauthorized); got != tokenRefusedMessage {
		t.Fatalf("connErrReason: %q", got)
	}
	err := registryError("could not fetch the package", registry.ErrUnauthorized)
	if err.Error() != tokenRefusedMessage {
		t.Fatalf("registryError: %v", err)
	}
	other := registryError("could not fetch the package", errors.New("boom"))
	if !strings.Contains(other.Error(), "boom") {
		t.Fatalf("registryError lost the cause: %v", other)
	}
	transport := func(msg string) error {
		return &neturl.Error{Op: "Get", URL: "https://box.example/api/v1/info", Err: errors.New(msg)}
	}
	for _, c := range []struct {
		err  error
		want string
	}{
		{transport("EOF"), "the connection failed"},
		{transport("read tcp 10.0.0.1:5555->10.0.0.2:443: connection reset by peer"), "the connection failed"},
		{transport("remote error: tls: handshake failure"), "TLS handshake failed"},
		{errors.New("unexpected EOF"), "the connection failed"},
		{registry.ErrUnexpectedAnswer, registry.ErrUnexpectedAnswer.Error()},
		{timeoutErr{}, "timed out"},
		{&registry.StatusError{What: "a lookup", Code: 502}, "the Bait Box answered a lookup with HTTP 502"},
	} {
		if got := connErrReason(c.err); got != c.want {
			t.Errorf("connErrReason(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

// timeoutErr is a net.Error like the one a body read returns when the client
// timeout passes.
type timeoutErr struct{}

func (timeoutErr) Error() string {
	return "net/http: request canceled (Client.Timeout or context cancellation while reading body)"
}
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// TestRegistryErrorHidesReaderText: a body read timeout, an early end of the
// body and a corrupt archive reach the user as short plain reasons.
func TestRegistryErrorHidesReaderText(t *testing.T) {
	var _ net.Error = timeoutErr{}
	for _, c := range []struct {
		err       error
		want, bad string
	}{
		{timeoutErr{}, "could not fetch the package: timed out", "Client.Timeout"},
		{
			fmt.Errorf("read body: %w", io.ErrUnexpectedEOF),
			"could not fetch the package: the connection failed", "read body",
		},
		{context.DeadlineExceeded, "could not fetch the package: timed out", "deadline"},
		{registry.ErrBadArchive, "could not fetch the package: the package archive could not be read", "gzip"},
		{registry.ErrBadManifest, "could not fetch the package: the package manifest could not be read", "invalid character"},
	} {
		got := registryError("could not fetch the package", c.err)
		if got.Error() != c.want || strings.Contains(got.Error(), c.bad) {
			t.Errorf("registryError(%v) = %q, want %q", c.err, got, c.want)
		}
		var ve errs.ValidationError
		if !errors.As(got, &ve) {
			t.Errorf("registryError(%v) is not a validation error", c.err)
		}
	}
}
