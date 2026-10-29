package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// pageFingerprint and emailFingerprint cover the name as well as the content, so
// a rename reads as drift too; the entity is still matched by its source name.
// A NUL joins the fields so values cannot run together.
func pageFingerprint(p *model.Page) string {
	return contentHash(nullableString64(p.Name) + "\x00" + pageContentOf(p))
}

func emailMetaOf(e *model.Email) (subject, from, envelope string) {
	if e.MailHeaderSubject.IsSpecified() && !e.MailHeaderSubject.IsNull() {
		subject = e.MailHeaderSubject.MustGet().String()
	}
	if e.MailHeaderFrom.IsSpecified() && !e.MailHeaderFrom.IsNull() {
		from = e.MailHeaderFrom.MustGet().String()
	}
	if e.MailEnvelopeFrom.IsSpecified() && !e.MailEnvelopeFrom.IsNull() {
		envelope = e.MailEnvelopeFrom.MustGet().String()
	}
	return
}

func emailFingerprint(e *model.Email) string {
	subject, from, envelope := emailMetaOf(e)
	return contentHash(strings.Join([]string{
		nullableString64(e.Name), subject, from, envelope, emailContentOf(e),
	}, "\x00"))
}

// The fingerprints below cover the meaningful content fields of each type. They
// are read from the stored entity (see entitySpec.storedHash), so an install time
// hash and a later drift hash are comparable.

// hashParts joins the fields with a NUL and hashes them, so values cannot run
// together into a false match.
func hashParts(parts ...string) string {
	return contentHash(strings.Join(parts, "\x00"))
}

// optStr reads any nullable vo value that has a String method, returning the
// empty string when the field is unset or null.
func optStr[T interface{ String() string }](n nullable.Nullable[T]) string {
	if n.IsSpecified() && !n.IsNull() {
		return n.MustGet().String()
	}
	return ""
}

func optRawStr(n nullable.Nullable[string]) string {
	if n.IsSpecified() && !n.IsNull() {
		return n.MustGet()
	}
	return ""
}

func optBool(n nullable.Nullable[bool]) string {
	if n.IsSpecified() && !n.IsNull() {
		if n.MustGet() {
			return "1"
		}
		return "0"
	}
	return ""
}

func optInt(n nullable.Nullable[int]) string {
	if n.IsSpecified() && !n.IsNull() {
		return strconv.Itoa(n.MustGet())
	}
	return ""
}

func optUUID(n nullable.Nullable[uuid.UUID]) string {
	if n.IsSpecified() && !n.IsNull() {
		return n.MustGet().String()
	}
	return ""
}

func optPort(n nullable.Nullable[vo.Port]) string {
	if n.IsSpecified() && !n.IsNull() {
		return strconv.Itoa(int(n.MustGet().Uint16()))
	}
	return ""
}

func optCidrs(n nullable.Nullable[vo.IPNetSlice]) string {
	if n.IsSpecified() && !n.IsNull() {
		sl := n.MustGet()
		parts := make([]string, 0, len(sl))
		for _, c := range sl {
			parts = append(parts, c.String())
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func optRBConfig(n nullable.Nullable[model.RemoteBrowserConfig]) string {
	if n.IsSpecified() && !n.IsNull() {
		if b, err := json.Marshal(n.MustGet()); err == nil {
			return string(b)
		}
	}
	return ""
}

func optHeaders(n nullable.Nullable[model.APISenderHeaders]) string {
	if n.IsSpecified() && !n.IsNull() {
		return n.MustGet().String()
	}
	return ""
}

func scriptFingerprint(m *model.Script) string {
	return hashParts(optStr(m.Name), optStr(m.Script))
}

func reportTemplateFingerprint(m *model.ReportTemplate) string {
	return hashParts(optStr(m.Content), optBool(m.IsTraining))
}

func proxyFingerprint(m *model.Proxy) string {
	return hashParts(optStr(m.Name), optStr(m.Description), optStr(m.StartURL), optStr(m.ProxyConfig))
}

func remoteBrowserFingerprint(m *model.RemoteBrowser) string {
	return hashParts(optStr(m.Name), optStr(m.Description), optStr(m.Script), optRBConfig(m.Config))
}

func domainFingerprint(m *model.Domain) string {
	return hashParts(optStr(m.Name), optStr(m.PageContent), optStr(m.PageNotFoundContent), optStr(m.RedirectURL),
		optBool(m.HostWebsite), optBool(m.ManagedTLS), optBool(m.SelfSignedTLS), optBool(m.OwnManagedTLS),
		optRawStr(m.OwnManagedTLSKey), optRawStr(m.OwnManagedTLSPem))
}

// allowDenyFingerprint omits Allowed on purpose: the AllowDeny service refuses to
// change it on update, so it is not reconcilable content.
func allowDenyFingerprint(m *model.AllowDeny) string {
	return hashParts(
		optStr(m.Name), optCidrs(m.Cidrs), optRawStr(m.CountryCodes),
		optRawStr(m.Asns), optRawStr(m.JA4Fingerprints), optRawStr(m.Headers),
	)
}

// smtpFieldsFingerprint hashes the smtp configuration fields without its headers.
func smtpFieldsFingerprint(m *model.SMTPConfiguration) string {
	return hashParts(
		optStr(m.Name), optStr(m.Host), optPort(m.Port), optStr(m.Username),
		optStr(m.Password), optBool(m.IgnoreCertErrors), optStr(m.Helo),
	)
}

// smtpFingerprint hashes the fields plus the custom headers in a stable order.
// A configuration with no headers hashes the same as the fields alone, so a
// baseline recorded before headers were part of the hash still matches.
func smtpFingerprint(m *model.SMTPConfiguration) string {
	if len(m.Headers) == 0 {
		return smtpFieldsFingerprint(m)
	}
	hs := make([]string, 0, len(m.Headers))
	for _, h := range m.Headers {
		if h == nil {
			continue
		}
		hs = append(hs, optStr(h.Key)+"\x01"+optStr(h.Value))
	}
	sort.Strings(hs)
	return hashParts(smtpFieldsFingerprint(m), strings.Join(hs, "\x02"))
}

// smtpHeaderModels turns declared headers into header models with the value
// substituted, rejecting a key or value the header type does not accept.
func smtpHeaderModels(headers []installHeaderYAML, vars map[string]string) ([]*model.SMTPHeader, error) {
	out := make([]*model.SMTPHeader, 0, len(headers))
	for _, h := range headers {
		hk, err := vo.NewString127(h.Key)
		if err != nil {
			return nil, errs.NewValidationError(fmt.Errorf("smtp header %q: %w", h.Key, err))
		}
		hv, err := vo.NewString255(substitute(h.Value, vars))
		if err != nil {
			return nil, errs.NewValidationError(fmt.Errorf("smtp header %q value: %w", h.Key, err))
		}
		out = append(out, &model.SMTPHeader{Key: nn(*hk), Value: nn(*hv)})
	}
	return out, nil
}

func webhookFingerprint(m *model.Webhook) string {
	return hashParts(optStr(m.Name), optStr(m.URL), optStr(m.Secret))
}

// oauthProviderFingerprint omits ClientSecret: it is write only and never read
// back, so including it would always read as drift.
func oauthProviderFingerprint(m *model.OAuthProvider) string {
	return hashParts(optStr(m.Name), optStr(m.AuthURL), optStr(m.TokenURL), optStr(m.Scopes), optStr(m.ClientID))
}

func apiSenderFingerprint(m *model.APISender) string {
	return hashParts(optStr(m.Name), optStr(m.RequestMethod), optStr(m.RequestURL), optHeaders(m.RequestHeaders),
		optStr(m.RequestBody), optInt(m.ExpectedResponseStatusCode), optHeaders(m.ExpectedResponseHeaders),
		optStr(m.ExpectedResponseBody), optStr(m.APIKey), optUUID(m.OAuthProviderID))
}

func nullableString64(n nullable.Nullable[vo.String64]) string {
	if n.IsSpecified() && !n.IsNull() {
		return n.MustGet().String()
	}
	return ""
}

func pageContentOf(p *model.Page) string {
	if p.Content.IsSpecified() && !p.Content.IsNull() {
		return p.Content.MustGet().String()
	}
	return ""
}

func emailContentOf(e *model.Email) string {
	if e.Content.IsSpecified() && !e.Content.IsNull() {
		return e.Content.MustGet().String()
	}
	return ""
}

// upstreamPageFP is the fingerprint the new version would install for a page,
// built the same way pageFingerprint reads a stored page (vo TrimSpace only), so
// it is comparable to the recorded install baseline.
func upstreamPageFP(name, content string) string {
	p := &model.Page{}
	if n, err := vo.NewString64(name); err == nil {
		p.Name = nn(*n)
	}
	if c, err := vo.NewOptionalString1MB(content); err == nil {
		p.Content = nn(*c)
	}
	return pageFingerprint(p)
}

// upstreamEmailFP mirrors createEmail, including the tracking pixel the Email
// service adds, so the hash compares against the stored (pixel added) baseline.
func (s *ContentRegistry) upstreamEmailFP(name string, ec emailContent) string {
	e := &model.Email{}
	if n, err := vo.NewString64(name); err == nil {
		e.Name = nn(*n)
	}
	withPixel := s.Email.TemplateService.AddTrackingPixel(strings.TrimSpace(ec.content))
	if c, err := vo.NewOptionalString1MB(withPixel); err == nil {
		e.Content = nn(*c)
	}
	setEmailMeta(e, ec)
	return emailFingerprint(e)
}
