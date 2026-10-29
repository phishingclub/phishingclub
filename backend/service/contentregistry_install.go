package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/vo"
	"gopkg.in/yaml.v3"
)

// installAttachmentYAML is one attachment entry. It is shared by the top level
// attachments block and the per email attachments list. Inline applies only when
// the attachment is nested under an email, where it sets the per link inline flag
// so the file can be referenced from the email HTML as cid:filename.
type installAttachmentYAML struct {
	Name        string `yaml:"name"`
	File        string `yaml:"file"`
	Description string `yaml:"description"`
	Embedded    bool   `yaml:"embedded"`
	Calendar    bool   `yaml:"calendar"`
	Inline      bool   `yaml:"inline"`
}

// installHeaderYAML is one header on an smtp configuration or an api sender.
type installHeaderYAML struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

// installDataYAML is the subset of a package data.yaml the consumer installs.
// Only content entity blocks are modelled; other blocks are skipped for now.
type installDataYAML struct {
	Name  string `yaml:"name"`
	Pages []struct {
		Name string `yaml:"name"`
		File string `yaml:"file"`
	} `yaml:"pages"`
	Emails []struct {
		Name         string                  `yaml:"name"`
		File         string                  `yaml:"file"`
		Subject      string                  `yaml:"subject"`
		From         string                  `yaml:"from"`
		EnvelopeFrom string                  `yaml:"envelope from"`
		Attachments  []installAttachmentYAML `yaml:"attachments"`
	} `yaml:"emails"`
	Scripts []struct {
		Name string `yaml:"name"`
		File string `yaml:"file"`
	} `yaml:"scripts"`
	ReportTemplates []struct {
		Name       string `yaml:"name"`
		File       string `yaml:"file"`
		IsTraining bool   `yaml:"isTraining"`
	} `yaml:"reportTemplates"`
	Proxies []struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		StartURL    string `yaml:"startURL"`
		File        string `yaml:"file"`
	} `yaml:"proxies"`
	RemoteBrowsers []struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		File        string   `yaml:"file"`
		Mode        string   `yaml:"mode"`
		Remote      string   `yaml:"remote"`
		Proxy       string   `yaml:"proxy"`
		Headless    bool     `yaml:"headless"`
		Timeout     int      `yaml:"timeout"`
		Lang        string   `yaml:"lang"`
		ExtraFlags  []string `yaml:"extraFlags"`
	} `yaml:"remoteBrowsers"`
	Domains []struct {
		Name              string `yaml:"name"`
		Type              string `yaml:"type"`
		ProxyTargetDomain string `yaml:"proxyTargetDomain"`
		HostWebsite       bool   `yaml:"hostWebsite"`
		ManagedTLS        bool   `yaml:"managedTLS"`
		SelfSignedTLS     bool   `yaml:"selfSignedTLS"`
		OwnManagedTLS     bool   `yaml:"ownManagedTLS"`
		OwnManagedTLSKey  string `yaml:"ownManagedTLSKey"`
		OwnManagedTLSPem  string `yaml:"ownManagedTLSPem"`
		RedirectURL       string `yaml:"redirectURL"`
		Content           string `yaml:"content"`
		NotFound          string `yaml:"notFound"`
	} `yaml:"domains"`
	AllowDenies []struct {
		Name         string `yaml:"name"`
		Allowed      bool   `yaml:"allowed"`
		Cidrs        string `yaml:"cidrs"`
		CountryCodes string `yaml:"countryCodes"`
		Asns         string `yaml:"asns"`
		JA4          string `yaml:"ja4"`
		Headers      []struct {
			KeyRegex   string `yaml:"keyRegex"`
			ValueRegex string `yaml:"valueRegex"`
		} `yaml:"headers"`
	} `yaml:"allowDenies"`
	SMTPConfigurations []struct {
		Name             string              `yaml:"name"`
		Host             string              `yaml:"host"`
		Port             int                 `yaml:"port"`
		Username         string              `yaml:"username"`
		Password         string              `yaml:"password"`
		IgnoreCertErrors bool                `yaml:"ignoreCertErrors"`
		Helo             string              `yaml:"helo"`
		Headers          []installHeaderYAML `yaml:"headers"`
	} `yaml:"smtpConfigurations"`
	Webhooks []struct {
		Name   string `yaml:"name"`
		URL    string `yaml:"url"`
		Secret string `yaml:"secret"`
	} `yaml:"webhooks"`
	// OAuthProviders ship the reusable config (endpoints, scopes) plus the client
	// id/secret as @@VAR@@ tokens. The provider is created unauthorized; no tokens ship.
	OAuthProviders []struct {
		Name         string `yaml:"name"`
		AuthURL      string `yaml:"authURL"`
		TokenURL     string `yaml:"tokenURL"`
		Scopes       string `yaml:"scopes"`
		ClientID     string `yaml:"clientID"`
		ClientSecret string `yaml:"clientSecret"`
	} `yaml:"oauthProviders"`
	APISenders  []installAPISenderYAML `yaml:"apiSenders"`
	Identifiers []struct {
		Name string `yaml:"name"`
	} `yaml:"identifiers"`
	Attachments       []installAttachmentYAML       `yaml:"attachments"`
	CampaignTemplates []installCampaignTemplateYAML `yaml:"campaignTemplates"`
}

// installAPISenderYAML is the request template for a third party send API. An
// optional oauthProvider names a provider resolved by name. Send time template
// vars like {{.OAuthAccessToken}} ship verbatim; install time @@VAR@@ tokens
// are substituted.
type installAPISenderYAML struct {
	Name                       string              `yaml:"name"`
	RequestMethod              string              `yaml:"requestMethod"`
	RequestURL                 string              `yaml:"requestURL"`
	RequestHeaders             []installHeaderYAML `yaml:"requestHeaders"`
	RequestBody                string              `yaml:"requestBody"`
	ExpectedResponseStatusCode int                 `yaml:"expectedResponseStatusCode"`
	ExpectedResponseHeaders    []installHeaderYAML `yaml:"expectedResponseHeaders"`
	ExpectedResponseBody       string              `yaml:"expectedResponseBody"`
	APIKey                     string              `yaml:"apiKey"`
	OAuthProvider              string              `yaml:"oauthProvider"`
}

// installCampaignTemplateYAML is one campaign template declared in a package. It
// is named rather than anonymous so install and the update reconcile can build
// the entity from it through one shared helper.
type installCampaignTemplateYAML struct {
	Name                        string `yaml:"name"`
	Domain                      string `yaml:"domain"`
	Email                       string `yaml:"email"`
	LandingPage                 string `yaml:"landingPage"`
	LandingProxy                string `yaml:"landingProxy"`
	BeforeLandingPage           string `yaml:"beforeLandingPage"`
	AfterLandingPage            string `yaml:"afterLandingPage"`
	AfterLandingPageRedirectURL string `yaml:"afterLandingPageRedirectURL"`
	SMTPConfiguration           string `yaml:"smtpConfiguration"`
	APISender                   string `yaml:"apiSender"`
	URLIdentifier               string `yaml:"urlIdentifier"`
	StateIdentifier             string `yaml:"stateIdentifier"`
	URLPath                     string `yaml:"urlPath"`
	LureURLMode                 string `yaml:"lureURLMode"`
	LureCodeAlgo                string `yaml:"lureCodeAlgo"`
	LureCodeLength              int    `yaml:"lureCodeLength"`
	IsTraining                  bool   `yaml:"isTraining"`
}

// installPriority installs a package that is named by another one first: an oauth
// provider before the api sender that names it, everything else after.
func installPriority(pkgType string) int {
	switch pkgType {
	case "oauthprovider":
		return 0
	case "apisender":
		return 1
	default:
		return 2
	}
}

// memberRemovePriority is the reverse of installPriority, so a referencing package
// (an api sender) is removed before the one it references (its oauth provider).
func memberRemovePriority(ctx context.Context, s *ContentRegistry, m *uuid.UUID) int {
	if m == nil {
		return 99
	}
	mp, err := s.RegistryRepository.GetInstalledPackageByID(ctx, m)
	if err != nil || mp == nil {
		return 99
	}
	// negate install priority so a higher install priority removes first.
	return -installPriority(mp.PackageType)
}

func splitPackageID(id string) (string, string) {
	if i := strings.IndexByte(id, '/'); i >= 0 {
		return id[:i], id[i+1:]
	}
	return "", ""
}

// requiredMissing returns the required variables (no default) across all packages
// that were not supplied in vars, with their declared type and description so the
// UI can render a meaningful prompt.
func requiredMissing(pkgs []*registry.VerifiedPackage, vars map[string]string, share bool) []MissingVariable {
	var missing []MissingVariable
	seen := map[string]bool{}
	for _, p := range pkgs {
		for _, v := range p.Manifest.Variables {
			if !v.Required || v.Default != nil {
				continue
			}
			key := varKey(p.Manifest.ID, v.Name, share)
			if seen[key] {
				continue
			}
			if _, ok := vars[key]; !ok {
				missing = append(missing, missingVar(p.Manifest.ID, v, share))
				seen[key] = true
			}
		}
	}
	return missing
}

// validateEnumValues rejects a supplied value for an enum variable that is not one
// of the options the author declared, so an install cannot substitute an off list
// value. Only supplied, non empty values are checked; a default is validated to be
// one of the options by Bait Box when the package is published.
func validateEnumValues(pkgs []*registry.VerifiedPackage, vars map[string]string, share bool) error {
	for _, p := range pkgs {
		for _, v := range p.Manifest.Variables {
			if v.Type != "enum" {
				continue
			}
			val, ok := vars[varKey(p.Manifest.ID, v.Name, share)]
			if !ok || val == "" {
				continue
			}
			allowed := false
			for _, o := range v.Options {
				if o == val {
					allowed = true
					break
				}
			}
			if !allowed {
				return errs.NewValidationError(fmt.Errorf("value %q for %q is not an allowed option", val, v.Name))
			}
		}
	}
	return nil
}

// checkSharedPicks refuses a shared variable name that one package declares as a
// pick and another declares as a plain value or a pick of another kind. One value
// fills them all, so a plain default given for one would otherwise count as the
// operator's choice for the other.
func checkSharedPicks(pkgs []*registry.VerifiedPackage, share bool) error {
	if !share {
		return nil
	}
	type decl struct{ pick, pkgID string }
	seen := map[string]decl{}
	for _, p := range pkgs {
		for _, v := range p.Manifest.Variables {
			d, ok := seen[v.Name]
			if !ok {
				seen[v.Name] = decl{pick: v.Pick, pkgID: p.Manifest.ID}
				continue
			}
			if d.pick != v.Pick {
				return errs.NewValidationError(fmt.Errorf(
					"the packages %s and %s declare the shared variable %q differently, and a variable "+
						"that picks an existing item must pick the same kind in every package that shares it",
					d.pkgID, p.Manifest.ID, v.Name,
				))
			}
		}
	}
	return nil
}

// effectiveVarsFor returns the name->value map to substitute into one package's
// content, honoring the share mode: a package's declared default overlaid by the
// supplied value under its key (the bare name when shared, a scoped key when
// each referenced package keeps its own value).
func effectiveVarsFor(p *registry.VerifiedPackage, vars map[string]string, share bool) map[string]string {
	eff := map[string]string{}
	for _, v := range p.Manifest.Variables {
		if v.Default != nil {
			eff[v.Name] = *v.Default
		}
		if val, ok := vars[varKey(p.Manifest.ID, v.Name, share)]; ok {
			eff[v.Name] = val
		}
	}
	return eff
}

// substitute replaces @@NAME@@ tokens with the supplied values in a single
// pass from left to right (strings.Replacer), so the result does not depend on
// map iteration order and a value that happens to contain another token is not
// expanded again.
func substitute(s string, vars map[string]string) string {
	if len(vars) == 0 {
		return s
	}
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "@@"+k+"@@", v)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// Install returns ErrMissingVariables when a required value is absent.
//
// A report template package overwrites the global report template of its kind
// in place. confirmReportTemplates lists the kinds (simulation, training) the
// user agreed to overwrite; a kind not listed stops the install with
// ErrReportTemplateOverwrite before anything is written.
func (s *ContentRegistry) Install(
	ctx context.Context,
	session *model.Session,
	registryID *uuid.UUID,
	namespace, name string,
	vars map[string]string,
	confirmReportTemplates []string,
) (_ *InstallResult, err error) {
	ae, err := s.authorize(session, "Registry.Install")
	if err != nil {
		return nil, err
	}
	defer s.auditFailure(ae, &err)
	ae.Details["registryId"] = registryID.String()
	ae.Details["packageId"] = namespace + "/" + name
	reg, err := s.RegistryRepository.GetRegistryByID(ctx, registryID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	client := s.clientFor(reg)
	defer client.CloseIdle()
	cache := newPackageCache(func(id string) (*registry.VerifiedPackage, error) {
		return s.fetchPackage(ctx, client, id)
	})

	// download and check everything before taking the lock, so a slow registry
	// never keeps other installs, updates or uninstalls waiting.
	if _, err := s.planInstall(ctx, registryID, namespace, name, vars, confirmReportTemplates, cache.get); err != nil {
		return nil, err
	}
	unlock, err := s.lockOps(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	// what is installed may have changed while downloading, so plan again under the
	// lock. Only the packages already downloaded are used here.
	cache.offline = true
	plan, err := s.planInstall(ctx, registryID, namespace, name, vars, confirmReportTemplates, cache.get)
	if err != nil {
		return nil, err
	}
	if len(plan.overwrites) > 0 {
		ae.Details["reportTemplateOverwrite"] = reportTemplateOverwriteAudit(plan.overwrites)
	}
	// once writing starts it runs to the end even if the request ends, so an
	// install is never cut off halfway.
	return s.applyInstall(context.WithoutCancel(ctx), session, ae, registryID, plan, vars)
}

// installPlan is everything an install writes, resolved and checked before the
// first write.
type installPlan struct {
	root         *registry.VerifiedPackage
	toInstall    []*registry.VerifiedPackage
	reuseMembers []*uuid.UUID
	share        bool
	mergedVars   map[string]string
	overwrites   []ReportTemplateOverwrite
}

// planInstall resolves a package and the packages it references through fetch
// and runs every check an install needs, without writing anything.
func (s *ContentRegistry) planInstall(
	ctx context.Context,
	registryID *uuid.UUID,
	namespace, name string,
	vars map[string]string,
	confirmReportTemplates []string,
	fetch func(id string) (*registry.VerifiedPackage, error),
) (*installPlan, error) {
	// refuse a duplicate install of the same package from the same registry; the
	// user should update or remove the existing one instead of stacking copies.
	if existing, _ := s.RegistryRepository.GetInstalledPackage(ctx, registryID, namespace+"/"+name); existing != nil {
		return nil, errs.NewValidationError(
			fmt.Errorf("%s/%s is already installed from this Bait Box", namespace, name),
		)
	}
	root, err := fetch(namespace + "/" + name)
	if err != nil {
		return nil, registryError("could not fetch the package", err)
	}

	// a bundle references other packages. resolve each at its current (latest)
	// version from the same registry: one already installed from this registry is
	// reused (linked, not reinstalled); the rest are installed as dependencies.
	// the visibility rule enforced at publish guarantees each reference resolves.
	// the manifest check already caps the reference count; the byte budget bounds
	// how much unpacked payload the referenced packages may hold in memory together.
	if len(root.Manifest.Dependencies) > maxBundleDependencies {
		return nil, errs.NewValidationError(fmt.Errorf("package references too many packages"))
	}
	plan := &installPlan{root: root}
	reusedByID := map[string]*uuid.UUID{}
	bundleBytes := packageBytes(root)
	for _, d := range root.Manifest.Dependencies {
		if ex, _ := s.RegistryRepository.GetInstalledPackage(ctx, registryID, d.ID); ex != nil {
			plan.reuseMembers = append(plan.reuseMembers, ex.ID)
			reusedByID[d.ID] = ex.ID
			continue
		}
		dp, derr := fetch(d.ID)
		if derr != nil {
			return nil, registryError("could not fetch referenced package "+d.ID, derr)
		}
		bundleBytes += packageBytes(dp)
		if bundleBytes > maxBundleBytes {
			return nil, errs.NewValidationError(fmt.Errorf(
				"referenced packages exceed the %d MiB install size limit", maxBundleBytes>>20,
			))
		}
		plan.toInstall = append(plan.toInstall, dp)
	}
	// install an oauth provider before the api sender that names it; the stable sort
	// keeps the author's order within the same priority.
	sort.SliceStable(plan.toInstall, func(i, j int) bool {
		return installPriority(plan.toInstall[i].Manifest.Type) < installPriority(plan.toInstall[j].Manifest.Type)
	})

	// collect variables across the bundle and the dependencies it will install. By
	// default a name shared across them is filled once (requiredMissing dedups by
	// name); the shareVariables flag is read for the per package option.
	// a bundle shares variables by name across its packages unless the author turned
	// it off; a plain package has one package, so the mode has no effect.
	plan.share = root.Manifest.ShareVariables == nil || *root.Manifest.ShareVariables
	varPkgs := append([]*registry.VerifiedPackage{root}, plan.toInstall...)
	if err := checkSharedPicks(varPkgs, plan.share); err != nil {
		return nil, err
	}
	if missing := requiredMissing(varPkgs, vars, plan.share); len(missing) > 0 {
		return nil, ErrMissingVariables{Variables: missing}
	}
	if err := validateEnumValues(varPkgs, vars, plan.share); err != nil {
		return nil, err
	}
	// a global asset the user added at a path a package ships is never adopted or
	// overwritten; report every such path before anything is created.
	conflicts, err := s.assetPathConflicts(ctx, varPkgs)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if len(conflicts) > 0 {
		return nil, assetConflictErr(conflicts)
	}
	// every attachment an install creates needs a file name no attachment uses yet,
	// including another attachment in the same install.
	attConflicts, err := s.attachmentNameConflicts(ctx, installAttachmentDecls(varPkgs), nil, false)
	if err != nil {
		return nil, err
	}
	if len(attConflicts) > 0 {
		return nil, attachmentNamesConflictErr(attConflicts)
	}
	plan.overwrites, err = s.reportTemplateInstallCheck(ctx, varPkgs, confirmReportTemplates)
	if err != nil {
		return nil, err
	}

	// the bundle's campaign template can reference a dependency entity whose name is
	// itself a variable (a domain named @@DOMAIN@@, say). The bundle declares no such
	// variable, so resolve the template with the merged values across every package,
	// matching the name the dependency actually created. A reused member was
	// installed earlier with its own values, so its names come from what it bound
	// then. The order is the one orderMembers describes, which Update also applies.
	plan.mergedVars = effectiveVarsFor(root, vars, plan.share)
	installedNow := map[string]*registry.VerifiedPackage{}
	for _, pk := range plan.toInstall {
		installedNow[pk.Manifest.ID] = pk
	}
	for _, d := range root.Manifest.Dependencies {
		var mv map[string]string
		if pk, ok := installedNow[d.ID]; ok {
			mv = effectiveVarsFor(pk, vars, plan.share)
		} else if m, ok := reusedByID[d.ID]; ok {
			var err error
			if mv, err = s.memberVarsFor(ctx, fetch, []*uuid.UUID{m}); err != nil {
				return nil, err
			}
		}
		for k, v := range mv {
			plan.mergedVars[k] = v
		}
	}
	targets := make([]oauthCheckTarget, 0, len(plan.toInstall)+1)
	for _, pk := range plan.toInstall {
		targets = append(targets, oauthCheckTarget{pkg: pk, vars: effectiveVarsFor(pk, vars, plan.share)})
	}
	targets = append(targets, oauthCheckTarget{pkg: plan.root, vars: plan.mergedVars})
	if err := s.checkOAuthBindings(ctx, registryID, targets, vars, plan.share); err != nil {
		return nil, err
	}
	return plan, nil
}

// oauthCheckTarget is a package whose API senders checkOAuthBindings checks, with
// the values its content is substituted with.
type oauthCheckTarget struct {
	pkg  *registry.VerifiedPackage
	vars map[string]string
}

// checkOAuthBindings refuses, before any write, an API sender that names an OAuth
// provider it may not bind. A provider the targets create binds only from the
// same namespace, and the write checks again when it binds. A pick counts only
// with a value in supplied.
func (s *ContentRegistry) checkOAuthBindings(
	ctx context.Context, registryID *uuid.UUID, targets []oauthCheckTarget, supplied map[string]string, share bool,
) error {
	// created maps a provider name the targets create to the namespaces that
	// declare it.
	created := map[string]map[string]bool{}
	docs := make([][]contentDoc, len(targets))
	for i, t := range targets {
		d, err := contentDocs(t.pkg)
		if err != nil {
			return s.contentDocsErr(err)
		}
		for _, doc := range d {
			for _, op := range doc.dy.OAuthProviders {
				n := strings.TrimSpace(op.Name)
				if created[n] == nil {
					created[n] = map[string]bool{}
				}
				created[n][t.pkg.Manifest.Namespace()] = true
			}
		}
		docs[i] = d
	}
	for i, t := range targets {
		b := &oauthBinding{registryID: registryID, manifest: t.pkg.Manifest, supplied: supplied, share: share}
		for _, d := range docs[i] {
			for _, as := range d.dy.APISenders {
				ref := b.oauthRef(as.OAuthProvider, t.vars)
				if nss, ok := created[ref]; ok {
					if _, picked := b.picked(as.OAuthProvider); picked || nss[b.namespace()] {
						continue
					}
					return foreignOAuthProviderErr(as.Name, ref)
				}
				p, err := s.lookupPackageOAuthProvider(ctx, b, as.OAuthProvider, t.vars)
				if err != nil {
					return err
				}
				if p.foreign {
					return foreignOAuthProviderErr(as.Name, p.ref)
				}
			}
		}
	}
	return nil
}

// applyInstall writes a planned install. The caller holds the lock. On any
// failure it removes what it created.
func (s *ContentRegistry) applyInstall(
	ctx context.Context,
	session *model.Session,
	ae *AuditEvent,
	registryID *uuid.UUID,
	plan *installPlan,
	vars map[string]string,
) (*InstallResult, error) {
	root := plan.root
	share := plan.share
	boundVars, _ := json.Marshal(vars)

	bundleIP, err := s.RegistryRepository.InsertInstalledPackage(ctx, &database.InstalledPackage{
		RegistryID: registryID, PackageID: root.Manifest.ID, PackageType: root.Manifest.Type,
		Version: root.Manifest.Version, ContentHash: root.Manifest.ContentHash,
		VariablesScoped: !share, OAuthPicks: suppliedOAuthPicks(root.Manifest, vars, share),
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// everything created in this call, newest last, so a failure unwinds it in
	// reverse. reused (already installed) members are not listed; they are left in
	// place on rollback.
	installed := []*uuid.UUID{bundleIP}
	result := &InstallResult{PackageID: root.Manifest.ID}
	// fail unwinds the members first, then the bundle. When anything could not be
	// removed the bundle row and its member links stay, so the leftover shows as
	// installed and removing the bundle cleans up the rest.
	fail := func(e error) (*InstallResult, error) {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		clean := true
		for i := len(installed) - 1; i >= 1; i-- {
			if !s.rollbackInstall(rctx, session, installed[i], false) {
				clean = false
			}
		}
		if !s.rollbackInstall(rctx, session, bundleIP, !clean) {
			clean = false
		}
		if clean {
			_ = s.RegistryRepository.DeletePackageMembersByBundle(rctx, bundleIP)
			ae.Details["rollback"] = "complete"
		} else {
			ae.Details["rollback"] = "partial"
			e = fmt.Errorf("%w (rollback incomplete: some created items could not be removed "+
				"and stay listed as installed)", e)
		}
		return nil, e
	}

	// install each referenced package as its own snapshot, then link it to the
	// bundle so uninstall can ref count it. a leaf content package carries no
	// campaign template, but the second pass is harmless if it does.
	for _, dp := range plan.toInstall {
		depIP, derr := s.RegistryRepository.InsertInstalledPackage(ctx, &database.InstalledPackage{
			RegistryID: registryID, PackageID: dp.Manifest.ID, PackageType: dp.Manifest.Type,
			Version: dp.Manifest.Version, ContentHash: dp.Manifest.ContentHash, InstalledAsDependency: true,
			VariablesScoped: !share, OAuthPicks: suppliedOAuthPicks(dp.Manifest, vars, share),
		})
		if derr != nil {
			return fail(errs.Wrap(derr))
		}
		installed = append(installed, depIP)
		depVars := effectiveVarsFor(dp, vars, share)
		if e := s.installEntities(ctx, session, dp, depVars, depIP, string(boundVars), vars, result); e != nil {
			return fail(e)
		}
		if e := s.installCampaignTemplates(ctx, session, dp, depVars, depIP, string(boundVars), result); e != nil {
			return fail(e)
		}
		if e := s.RegistryRepository.InsertPackageMember(ctx, bundleIP, depIP); e != nil {
			return fail(errs.Wrap(e))
		}
	}
	for _, m := range plan.reuseMembers {
		if e := s.RegistryRepository.InsertPackageMember(ctx, bundleIP, m); e != nil {
			return fail(errs.Wrap(e))
		}
	}

	// the bundle's own content: for a reference bundle this is just the campaign
	// template, which resolves its pieces by name against the entities installed
	// above, using the merged values planInstall worked out. a plain package
	// installs its own entities here with no dependencies.
	if e := s.installEntities(
		ctx, session, root, plan.mergedVars, bundleIP, string(boundVars), vars, result,
	); e != nil {
		return fail(e)
	}
	e := s.installCampaignTemplates(
		ctx, session, root, plan.mergedVars, bundleIP, string(boundVars), result,
	)
	if e != nil {
		return fail(e)
	}

	ae.Details["installedPackageId"] = bundleIP.String()
	ae.Details["version"] = root.Manifest.Version
	s.AuditLogAuthorized(ae)
	return result, nil
}

// contentDoc is one content folder in a package: its path prefix (ending in a
// slash, e.g. "content/login/") and the parsed data.yaml found in it.
type contentDoc struct {
	prefix string
	dy     *installDataYAML
}

// contentDocs finds every data.yaml under content/ and parses each. baitbox lays
// a package out as content/<folder>/data.yaml plus that folder's files, and a
// bundle has several such folders, so install must resolve each folder on its own
// rather than assume a single flat content/data.yaml.
func contentDocs(pkg *registry.VerifiedPackage) ([]contentDoc, error) {
	paths := make([]string, 0, 4)
	for p := range pkg.Files {
		if strings.HasPrefix(p, "content/") && strings.HasSuffix(p, "/data.yaml") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	out := make([]contentDoc, 0, len(paths))
	for _, p := range paths {
		var dy installDataYAML
		if err := yaml.Unmarshal(pkg.Files[p], &dy); err != nil {
			return nil, &dataYAMLError{path: p, err: err}
		}
		// the prefix is the folder holding this data.yaml, so its file refs resolve
		// relative to it (prefix keeps the trailing slash).
		out = append(out, contentDoc{prefix: strings.TrimSuffix(p, "data.yaml"), dy: &dy})
	}
	return out, nil
}

// dataYAMLError is a data.yaml that does not parse. The message names only the
// file, as the parser text is not for the user.
type dataYAMLError struct {
	path string
	err  error
}

func (e *dataYAMLError) Error() string {
	return fmt.Sprintf("the package file %s could not be read", e.path)
}

func (e *dataYAMLError) Unwrap() error { return e.err }

// contentDocsErr logs the parser error behind a contentDocs failure and returns
// the error for the user.
func (s *ContentRegistry) contentDocsErr(err error) error {
	var de *dataYAMLError
	if errors.As(err, &de) {
		s.Logger.Warnw("package data.yaml could not be parsed", "path", de.path, "error", de.err)
	}
	return errs.NewValidationError(err)
}

// installEntities creates the content entities declared in one verified package.
// Assets install once (shared across folders); each content folder's data.yaml is
// then installed with its files resolved relative to that folder. An OAuth
// provider pick counts only with a value in picks.
func (s *ContentRegistry) installEntities(
	ctx context.Context,
	session *model.Session,
	pkg *registry.VerifiedPackage,
	vars map[string]string,
	ipID *uuid.UUID,
	boundVars string,
	picks map[string]string,
	result *InstallResult,
) error {
	// assets first so page/email references resolve on disk.
	for path, b := range pkg.Files {
		if !strings.HasPrefix(path, "assets/") {
			continue
		}
		rel := strings.TrimPrefix(path, "assets/")
		if rel == "" {
			continue
		}
		if perr := checkAssetRel(rel); perr != nil {
			return perr
		}
		id, created, err := s.installAsset(ctx, session, ipID, rel, b)
		if err != nil {
			return err
		}
		// installAsset returns an asset this install created or one another installed
		// package owns; a user asset at the path fails above and is never adopted.
		// record provenance for both. uninstall ref counts these rows so a shared
		// asset is deleted only when no other install still references it. the result
		// lists only assets this install newly created.
		if id != nil {
			if created {
				result.Assets = append(result.Assets, rel)
			}
			if err := s.recordResource(ctx, ipID, "asset", rel, id, contentHash(string(b)), boundVars); err != nil {
				// do not leave an untracked file this install just created.
				if created {
					s.cleanupUntracked(ctx, session, "asset", id)
				}
				return err
			}
		}
	}
	docs, err := contentDocs(pkg)
	if err != nil {
		return s.contentDocsErr(err)
	}
	for _, doc := range docs {
		err := s.installDocEntities(ctx, session, pkg, doc.prefix, doc.dy, vars, ipID, boundVars, picks, result)
		if err != nil {
			return err
		}
	}
	return nil
}

// installDocEntities creates the entities declared in one content folder's
// data.yaml, reading each file relative to that folder's prefix. picks is as for
// installEntities.
func (s *ContentRegistry) installDocEntities(
	ctx context.Context,
	session *model.Session,
	pkg *registry.VerifiedPackage,
	prefix string,
	dy *installDataYAML,
	vars map[string]string,
	ipID *uuid.UUID,
	boundVars string,
	picks map[string]string,
	result *InstallResult,
) error {
	for _, p := range dy.Pages {
		body, ok := pkg.Files[prefix+p.File]
		if !ok {
			result.Skipped = append(result.Skipped, "page "+p.Name+": file missing")
			continue
		}
		content := substitute(string(body), vars)
		id, err := s.createPage(ctx, session, p.Name, content)
		if err != nil {
			return collisionErr("page", p.Name, err)
		}
		if err := s.recordResource(ctx, ipID, "page", p.Name, id, s.storedPageHash(ctx, id), boundVars); err != nil {
			// do not leave the entity without a provenance row; rollback only deletes
			// recorded resources, so delete this one here.
			s.cleanupUntracked(ctx, session, "page", id)
			return err
		}
		result.Pages = append(result.Pages, p.Name)
	}
	for _, e := range dy.Emails {
		body, ok := pkg.Files[prefix+e.File]
		if !ok {
			result.Skipped = append(result.Skipped, "email "+e.Name+": file missing")
			continue
		}
		content := substitute(string(body), vars)
		id, err := s.createEmail(
			ctx, session, e.Name, content, substitute(e.Subject, vars),
			substitute(e.From, vars), substitute(e.EnvelopeFrom, vars),
		)
		if err != nil {
			return collisionErr("email", e.Name, err)
		}
		err = s.recordResource(ctx, ipID, "email", e.Name, id, s.storedHash(ctx, "email", id), boundVars)
		if err != nil {
			s.cleanupUntracked(ctx, session, "email", id)
			return err
		}
		result.Emails = append(result.Emails, e.Name)
		for _, at := range e.Attachments {
			body, ok := pkg.Files[prefix+at.File]
			if !ok {
				result.Skipped = append(result.Skipped, "attachment "+at.Name+": file missing")
				continue
			}
			atID, cerr := s.createAttachment(
				ctx, session, substitute(at.Name, vars), at.File, substitute(at.Description, vars),
				at.Embedded, at.Calendar, body,
			)
			if cerr != nil {
				return cerr
			}
			rerr := s.recordResource(
				ctx, ipID, "attachment", at.Name, atID, contentHash(string(body)), boundVars,
			)
			if rerr != nil {
				s.cleanupUntracked(ctx, session, "attachment", atID)
				return rerr
			}
			if lerr := s.EmailRepository.AddAttachment(ctx, id, atID, at.Inline); lerr != nil {
				s.cleanupUntracked(ctx, session, "attachment", atID)
				return errs.Wrap(lerr)
			}
			result.Others = append(result.Others, "attachment "+at.Name)
		}
	}

	// record provenance for a newly created non core entity, cleaning it up if the
	// provenance insert fails so no untracked entity is left behind.
	record := func(resourceType, sourceName string, id *uuid.UUID, fp, label string) error {
		if err := s.recordResource(ctx, ipID, resourceType, sourceName, id, fp, boundVars); err != nil {
			s.cleanupUntracked(ctx, session, resourceType, id)
			return err
		}
		result.Others = append(result.Others, label)
		return nil
	}

	doc := contentDoc{prefix: prefix, dy: dy}
	if err := scriptSpec.install(s, ctx, session, pkg, doc, vars, ipID, boundVars, result); err != nil {
		return err
	}
	for _, rt := range dy.ReportTemplates {
		body, ok := pkg.Files[prefix+rt.File]
		if !ok {
			result.Skipped = append(result.Skipped, "report template "+rt.Name+": file missing")
			continue
		}
		content := substitute(string(body), vars)
		id, replaced, cerr := s.installReportTemplate(ctx, session, content, rt.IsTraining)
		if cerr != nil {
			return cerr
		}
		err := s.recordResourceReplacing(
			ctx, ipID, "reporttemplate", rt.Name, id,
			s.storedReportTemplateHash(ctx, id), boundVars, replaced,
		)
		if err != nil {
			// write the previous content back, or delete a template created here, so
			// nothing is left changed without provenance.
			if rerr := s.releaseResourceEntity(ctx, session, "reporttemplate", id, replaced); rerr != nil {
				s.Logger.Errorw("install: could not restore a report template left without provenance",
					"id", id.String(), "error", rerr)
			}
			return err
		}
		result.Others = append(result.Others, "report template "+rt.Name)
	}
	// oauth providers install before api senders so a sender declared in the same
	// folder can resolve its provider by name.
	specs := []resourceSpec{
		proxySpec, remoteBrowserSpec, domainSpec, allowDenySpec, smtpSpec, webhookSpec, oauthProviderSpec,
	}
	for _, sp := range specs {
		if err := sp.install(s, ctx, session, pkg, doc, vars, ipID, boundVars, result); err != nil {
			return err
		}
	}
	var binding *oauthBinding
	for _, as := range dy.APISenders {
		var oauthID *uuid.UUID
		if strings.TrimSpace(as.OAuthProvider) != "" {
			if binding == nil {
				b, berr := s.oauthBindingFor(ctx, ipID, pkg.Manifest, picks)
				if berr != nil {
					return berr
				}
				binding = b
			}
			oid, e := s.resolvePackageOAuthProviderID(ctx, binding, as.Name, as.OAuthProvider, vars)
			if e != nil {
				return e
			}
			oauthID = oid
		}
		m, berr := apiSenderModel(as, vars, oauthID)
		if berr != nil {
			return berr
		}
		id, cerr := s.APISender.Create(ctx, session, m)
		if cerr != nil {
			return cerr
		}
		err := record("apisender", as.Name, id, s.storedHash(ctx, "apisender", id), "api sender "+as.Name)
		if err != nil {
			return err
		}
	}
	for _, idn := range dy.Identifiers {
		id, created, cerr := s.createIdentifier(ctx, idn.Name)
		if cerr != nil {
			return cerr
		}
		if created && id != nil {
			if err := record("identifier", idn.Name, id, contentHash(idn.Name), "identifier "+idn.Name); err != nil {
				return err
			}
		}
	}
	for _, at := range dy.Attachments {
		body, ok := pkg.Files[prefix+at.File]
		if !ok {
			result.Skipped = append(result.Skipped, "attachment "+at.Name+": file missing")
			continue
		}
		id, cerr := s.createAttachment(
			ctx, session, at.Name, at.File, substitute(at.Description, vars), at.Embedded, at.Calendar, body,
		)
		if cerr != nil {
			return cerr
		}
		if err := record("attachment", at.Name, id, contentHash(string(body)), "attachment "+at.Name); err != nil {
			return err
		}
	}
	return nil
}

func (s *ContentRegistry) recordResource(
	ctx context.Context,
	ipID *uuid.UUID,
	resourceType, sourceName string,
	resourceID *uuid.UUID,
	hash, boundVars string,
) error {
	return s.recordResourceReplacing(ctx, ipID, resourceType, sourceName, resourceID, hash, boundVars, nil)
}

// recordResourceReplacing records provenance like recordResource and keeps the
// content the entity had before the install overwrote it, nil when the install
// created the entity.
func (s *ContentRegistry) recordResourceReplacing(
	ctx context.Context,
	ipID *uuid.UUID,
	resourceType, sourceName string,
	resourceID *uuid.UUID,
	hash, boundVars string,
	replaced *string,
) error {
	_, err := s.insertResourceRow(ctx, ipID, resourceType, sourceName, resourceID, hash, boundVars, replaced)
	return err
}

// insertResourceRow inserts one provenance row and returns its id.
func (s *ContentRegistry) insertResourceRow(
	ctx context.Context,
	ipID *uuid.UUID,
	resourceType, sourceName string,
	resourceID *uuid.UUID,
	hash, boundVars string,
	replaced *string,
) (*uuid.UUID, error) {
	return s.RegistryRepository.InsertInstalledResource(ctx, &database.InstalledResource{
		InstalledPackageID:   ipID,
		ResourceType:         resourceType,
		ResourceID:           resourceID,
		SourceName:           sourceName,
		InstalledContentHash: hash,
		BoundVariables:       boundVars,
		ReplacedContent:      replaced,
	})
}

// cleanupUntracked deletes an entity an install just created when its provenance
// could not be recorded. Rollback only sees recorded entities, so a failed delete
// here is logged with the id for manual cleanup.
func (s *ContentRegistry) cleanupUntracked(
	ctx context.Context, session *model.Session, resourceType string, id *uuid.UUID,
) {
	if err := s.deleteResourceEntity(ctx, session, resourceType, id); err != nil {
		s.Logger.Errorw("install: could not delete an entity left without provenance",
			"type", resourceType, "id", id.String(), "error", err)
	}
}

// installCampaignTemplates resolves every referenced entity by name, so it must
// run after all other entities are installed.
func (s *ContentRegistry) installCampaignTemplates(
	ctx context.Context,
	session *model.Session,
	pkg *registry.VerifiedPackage,
	vars map[string]string,
	ipID *uuid.UUID,
	boundVars string,
	result *InstallResult,
) error {
	docs, err := contentDocs(pkg)
	if err != nil {
		return s.contentDocsErr(err)
	}
	for _, doc := range docs {
		for _, ct := range doc.dy.CampaignTemplates {
			m, berr := s.buildCampaignTemplateModel(ctx, ct, vars)
			if berr != nil {
				return berr
			}
			id, cerr := s.CampaignTemplate.Create(ctx, session, m)
			if cerr != nil {
				name := strings.TrimSpace(substitute(ct.Name, vars))
				var ve errs.ValidationError
				if errors.As(cerr, &ve) && !strings.Contains(cerr.Error(), "not unique") {
					return errs.NewValidationError(fmt.Errorf("campaign template %q: %w", name, cerr))
				}
				return collisionErr("campaign template", name, errs.Wrap(cerr))
			}
			rerr := s.recordResource(
				ctx, ipID, "campaigntemplate", ct.Name, id,
				campaignTemplateDeclFingerprint(ct, vars), boundVars,
			)
			if rerr != nil {
				s.cleanupUntracked(ctx, session, "campaigntemplate", id)
				return rerr
			}
			result.Others = append(result.Others, "campaign template "+ct.Name)
		}
	}
	return nil
}

// buildCampaignTemplateModel resolves a campaign template declaration into a
// model: every reference is resolved by name against the installed entities, and
// @@VAR@@ is substituted in each (and the name) so a reference to an entity whose
// name is a variable matches what install created. Returns refResolveErr when a
// reference does not resolve or is a company domain. Shared by install and the
// update reconcile.
func (s *ContentRegistry) buildCampaignTemplateModel(
	ctx context.Context, ct installCampaignTemplateYAML, vars map[string]string,
) (*model.CampaignTemplate, error) {
	m := &model.CampaignTemplate{}
	sub := func(s string) string { return substitute(s, vars) }
	n, nerr := vo.NewString64(sub(ct.Name))
	if nerr != nil {
		return nil, errs.NewValidationError(nerr)
	}
	m.Name = nn(*n)
	for _, r := range campaignTemplateRefs(ct, vars) {
		id, e := s.resolveReferenceID(ctx, r.resourceType, r.name)
		if e != nil {
			return nil, refResolveErr(r.label, r.name, e)
		}
		r.set(m, id)
	}
	if ct.AfterLandingPageRedirectURL != "" {
		if u, e := vo.NewOptionalString255(sub(ct.AfterLandingPageRedirectURL)); e == nil {
			m.AfterLandingPageRedirectURL = nn(*u)
		}
	}
	if ct.URLPath != "" {
		if up, e := vo.NewURLPath(ct.URLPath); e == nil {
			m.URLPath = nn(*up)
		}
	}
	if ct.LureURLMode != "" {
		m.LureURLMode = nn(ct.LureURLMode)
	}
	if ct.LureCodeAlgo != "" {
		m.LureCodeAlgo = nn(ct.LureCodeAlgo)
	}
	if ct.LureCodeLength > 0 {
		m.LureCodeLength = nn(ct.LureCodeLength)
	}
	m.IsTraining = nn(ct.IsTraining)
	return m, nil
}

// campaignTemplateDeclFingerprint hashes a campaign template's declared wiring
// (the substituted reference names and scalar settings), so an update can tell
// whether the new version changed the template. A campaign template points at
// other managed entities rather than carrying content of its own, so its drift
// is measured from the declaration, not a read back entity.
func campaignTemplateDeclFingerprint(ct installCampaignTemplateYAML, vars map[string]string) string {
	sub := func(s string) string { return substitute(s, vars) }
	return hashParts(
		sub(ct.Name), sub(ct.Domain), sub(ct.Email), sub(ct.LandingPage), sub(ct.LandingProxy),
		sub(ct.BeforeLandingPage), sub(ct.AfterLandingPage), sub(ct.AfterLandingPageRedirectURL),
		sub(ct.SMTPConfiguration), sub(ct.APISender), sub(ct.URLIdentifier), sub(ct.StateIdentifier),
		ct.URLPath, ct.LureURLMode, ct.LureCodeAlgo, strconv.Itoa(ct.LureCodeLength), strconv.FormatBool(ct.IsTraining),
	)
}

// maxBundleDependencies caps how many packages one bundle install fetches.
const maxBundleDependencies = 100

// maxBundleBytes caps the unpacked payload of a bundle and the referenced packages
// it installs, since they are all held in memory until the install finishes.
const maxBundleBytes = 64 << 20

// packageBytes is the unpacked payload size of a verified package.
func packageBytes(pkg *registry.VerifiedPackage) int64 {
	var n int64
	for _, b := range pkg.Files {
		n += int64(len(b))
	}
	return n
}

// mergeBoundVars loads the variables bound at install time and overlays newly
// supplied ones, so an update does not ask again for unchanged secrets.
func (s *ContentRegistry) mergeBoundVars(
	ctx context.Context, installedPackageID *uuid.UUID, vars map[string]string,
) map[string]string {
	merged := map[string]string{}
	resources, err := s.RegistryRepository.GetInstalledResourcesByPackage(ctx, installedPackageID)
	if err == nil {
		for _, r := range resources {
			if r.BoundVariables == "" {
				continue
			}
			var m map[string]string
			if json.Unmarshal([]byte(r.BoundVariables), &m) == nil {
				for k, v := range m {
					merged[k] = v
				}
			}
		}
	}
	for k, v := range vars {
		merged[k] = v
	}
	return merged
}

// memberVarsFor returns, by bare name, the variable values a set of installed
// bundle members were installed with: each member's declared defaults (read from
// its manifest through fetch, when reachable) overlaid by the values bound in its
// provenance. The member's entities were named from these values, so a bundle's
// campaign template resolves a reference such as @@DOMAIN@@ to the entity the
// member actually created, whether the member was installed with the bundle or
// reused from an earlier install.
//
// A member that was not downloaded before the lock was taken returns
// errNotDownloaded, as every other read from the offline cache does. A member
// whose download failed (gone from the registry, unreachable) is read from its
// bound values only, the same on the first pass and under the lock.
func (s *ContentRegistry) memberVarsFor(
	ctx context.Context,
	fetch func(id string) (*registry.VerifiedPackage, error),
	members []*uuid.UUID,
) (map[string]string, error) {
	out := map[string]string{}
	for _, m := range members {
		mp, err := s.RegistryRepository.GetInstalledPackageByID(ctx, m)
		if err != nil || mp == nil {
			continue
		}
		var mpkg *registry.VerifiedPackage
		if fetch != nil {
			p, ferr := fetch(mp.PackageID)
			switch {
			case ferr == nil:
				mpkg = p
			case errors.Is(ferr, errNotDownloaded):
				return nil, errs.NewValidationError(errNotDownloaded)
			default:
				s.Logger.Debugw("could not read member manifest, using bound values only",
					"package", mp.PackageID, "error", ferr)
			}
		}
		for k, v := range boundNamesFor(mp.PackageID, mpkg, s.mergeBoundVars(ctx, m, nil)) {
			out[k] = v
		}
	}
	return out, nil
}

// boundNamesFor maps the values bound for one installed package to the bare
// variable names its content substituted. A value bound under a package scoped
// key counts only for that package. With the manifest known, only declared names
// are kept and their defaults fill the gaps; without it every bare key is kept.
func boundNamesFor(pkgID string, p *registry.VerifiedPackage, bound map[string]string) map[string]string {
	out := map[string]string{}
	declared := map[string]bool{}
	known := p != nil && p.Manifest != nil
	if known {
		for _, v := range p.Manifest.Variables {
			declared[v.Name] = true
			if v.Default != nil {
				out[v.Name] = *v.Default
			}
		}
	}
	scoped := map[string]string{}
	for k, v := range bound {
		if i := strings.Index(k, varScopeSep); i >= 0 {
			if k[:i] == pkgID {
				scoped[k[i+len(varScopeSep):]] = v
			}
			continue
		}
		if !known || declared[k] {
			out[k] = v
		}
	}
	for k, v := range scoped {
		if !known || declared[k] {
			out[k] = v
		}
	}
	return out
}

// orderMembers sorts installed bundle members into the precedence of variable
// values for a bundle's own content. The order, lowest first, is: the bundle's
// own values, then each referenced package in the order the bundle manifest lists
// it, a later one overriding an earlier one on a shared name. A referenced
// package installed with the bundle contributes the values supplied then; one
// reused from an earlier install contributes the values it bound itself. Install
// and Update both follow this order. A member the manifest no longer lists comes
// last, ordered by package id.
func (s *ContentRegistry) orderMembers(
	ctx context.Context, deps []registry.Dependency, members []*uuid.UUID,
) []*uuid.UUID {
	pos := map[string]int{}
	for i, d := range deps {
		if _, seen := pos[d.ID]; !seen {
			pos[d.ID] = i
		}
	}
	type keyed struct {
		id    *uuid.UUID
		rank  int
		pkgID string
	}
	ks := make([]keyed, 0, len(members))
	for _, m := range members {
		k := keyed{id: m, rank: len(deps)}
		if mp, err := s.RegistryRepository.GetInstalledPackageByID(ctx, m); err == nil && mp != nil {
			k.pkgID = mp.PackageID
			if p, ok := pos[mp.PackageID]; ok {
				k.rank = p
			}
		}
		ks = append(ks, k)
	}
	sort.SliceStable(ks, func(i, j int) bool {
		if ks[i].rank != ks[j].rank {
			return ks[i].rank < ks[j].rank
		}
		return ks[i].pkgID < ks[j].pkgID
	})
	out := make([]*uuid.UUID, len(ks))
	for i, k := range ks {
		out[i] = k.id
	}
	return out
}

// packageCache downloads each package id at most once and remembers the answer,
// a failure too. With offline set it answers only from what it already holds,
// so work done under the lock never waits on the registry.
type packageCache struct {
	fetch   func(id string) (*registry.VerifiedPackage, error)
	entries map[string]packageCacheEntry
	offline bool
}

type packageCacheEntry struct {
	pkg *registry.VerifiedPackage
	err error
}

// errNotDownloaded is returned for a package that was not downloaded before the
// lock was taken, which happens when what is installed changed in between.
var errNotDownloaded = errors.New("the installed content changed while this was prepared, try again")

func newPackageCache(fetch func(id string) (*registry.VerifiedPackage, error)) *packageCache {
	return &packageCache{fetch: fetch, entries: map[string]packageCacheEntry{}}
}

func (c *packageCache) get(id string) (*registry.VerifiedPackage, error) {
	if e, ok := c.entries[id]; ok {
		return e.pkg, e.err
	}
	if c.offline || c.fetch == nil {
		return nil, errNotDownloaded
	}
	p, err := c.fetch(id)
	c.entries[id] = packageCacheEntry{p, err}
	return p, err
}

// getManifest downloads a package like get but keeps only its manifest, for a
// package whose files are never read. A later get answers with the manifest
// only, which has no files map (see manifestOnly).
func (c *packageCache) getManifest(id string) (*registry.VerifiedPackage, error) {
	p, err := c.get(id)
	if err != nil || p == nil || manifestOnly(p) {
		return p, err
	}
	slim := &registry.VerifiedPackage{Manifest: p.Manifest}
	c.entries[id] = packageCacheEntry{pkg: slim}
	return slim, nil
}

// manifestOnly reports whether a package was kept without its files.
func manifestOnly(p *registry.VerifiedPackage) bool {
	return p != nil && p.Files == nil
}

// memoFetch wraps a package fetch so each package id is downloaded at most once
// for the life of the returned function. A failed fetch is remembered too.
func memoFetch(
	fetch func(id string) (*registry.VerifiedPackage, error),
) func(id string) (*registry.VerifiedPackage, error) {
	return newPackageCache(fetch).get
}
