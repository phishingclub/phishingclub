package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// loaderTemplate is a template in English with an email, a before page, a
// landing page and an after proxy, and a German row
type loaderTemplate struct {
	id    uuid.UUID
	email uuid.UUID
	page  uuid.UUID
	proxy uuid.UUID
	de    *model.CampaignTemplateLanguage
}

func (f *templateLanguageFixture) loaderTemplate() loaderTemplate {
	f.t.Helper()
	lt := loaderTemplate{proxy: f.proxy()}
	tmpl := f.languageTemplate(&f.c1)
	tmpl.BeforeLandingPageID = nullable.NewNullableWithValue(f.page(&f.c1))
	tmpl.AfterLandingPageID = nullable.Nullable[uuid.UUID]{}
	tmpl.AfterLandingProxyID = nullable.NewNullableWithValue(lt.proxy)
	lt.email = tmpl.EmailID.MustGet()
	lt.page = tmpl.LandingPageID.MustGet()
	lt.de = &model.CampaignTemplateLanguage{
		Language:            "de",
		EmailID:             nullable.NewNullableWithValue(f.email(&f.c1, "hallo", "betreff")),
		BeforeLandingPageID: nullable.NewNullableWithValue(f.page(&f.c1)),
		LandingPageID:       nullable.NewNullableWithValue(f.page(&f.c1)),
	}
	tmpl.Languages = nullable.NewNullableWithValue([]*model.CampaignTemplateLanguage{lt.de})
	lt.id = f.createTemplate(tmpl)
	return lt
}

// the option sets of the load paths that must use the loader: sending, the
// copied email body and preview, the lure and phishing pages, and the deny page
var loaderPaths = map[string]*repository.CampaignTemplateOption{
	"send": {
		WithDomain: true, WithEmail: true, WithSMTPConfiguration: true, WithAPISender: true, WithIdentifier: true,
	},
	"email body": {WithEmail: true, WithDomain: true, WithIdentifier: true},
	"page": {
		WithDomain: true, WithBeforeLandingPage: true, WithLandingPage: true, WithAfterLandingPage: true,
		WithBeforeLandingProxy: true, WithLandingProxy: true, WithAfterLandingProxy: true, WithIdentifier: true,
	},
	"deny":     {WithEmail: true, WithDomain: true},
	"ids only": {},
}

// TestLanguageLoaderSwapsContent checks the loader puts the email and page ids
// and the loaded email and pages of the stamped language in place, keeps the
// proxy stages, and gives the template's own content for its language or no
// language
func TestLanguageLoaderSwapsContent(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	lt := f.loaderTemplate()
	deEmail := lt.de.EmailID.MustGet()
	deBefore := lt.de.BeforeLandingPageID.MustGet()
	dePage := lt.de.LandingPageID.MustGet()
	for name, opts := range loaderPaths {
		got, err := f.repo.GetByIDForLanguage(f.ctx, &lt.id, "de", opts)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.EmailID.MustGet() != deEmail || got.LandingPageID.MustGet() != dePage || got.BeforeLandingPageID.MustGet() != deBefore {
			t.Errorf("%s: ids not swapped", name)
		}
		if got.AfterLandingProxyID.MustGet() != lt.proxy {
			t.Errorf("%s: proxy stage changed", name)
		}
		if opts.WithEmail && (got.Email == nil || got.Email.ID.MustGet() != deEmail) {
			t.Errorf("%s: loaded email not swapped", name)
		}
		if !opts.WithEmail && got.Email != nil {
			t.Errorf("%s: email loaded without the option", name)
		}
		if opts.WithLandingPage && (got.LandingPage == nil || got.LandingPage.ID.MustGet() != dePage) {
			t.Errorf("%s: loaded landing page not swapped", name)
		}
		if opts.WithBeforeLandingPage && (got.BeforeLandingePage == nil || got.BeforeLandingePage.ID.MustGet() != deBefore) {
			t.Errorf("%s: loaded before page not swapped", name)
		}
		if opts.WithAfterLandingProxy && (got.AfterLandingProxy == nil || got.AfterLandingProxy.ID.MustGet() != lt.proxy) {
			t.Errorf("%s: after proxy not loaded", name)
		}
		if got.AfterLandingPageID.IsSpecified() && !got.AfterLandingPageID.IsNull() {
			t.Errorf("%s: after page set on a proxy stage", name)
		}
		// the template language and no language give the template's own content
		for _, code := range []string{"en", ""} {
			own, err := f.repo.GetByIDForLanguage(f.ctx, &lt.id, code, opts)
			if err != nil {
				t.Fatalf("%s %q: %v", name, code, err)
			}
			if own.EmailID.MustGet() != lt.email || own.LandingPageID.MustGet() != lt.page {
				t.Errorf("%s %q: not the template content", name, code)
			}
			if opts.WithEmail && own.Email.ID.MustGet() != lt.email {
				t.Errorf("%s %q: loaded email is not the template email", name, code)
			}
		}
	}
}

// TestLanguageLoaderMissingLanguage checks a language the template does not
// have, or has on another template only, returns the typed error and never
// the default content
func TestLanguageLoaderMissingLanguage(t *testing.T) {
	f := newTemplateLanguageFixture(t)
	lt := f.loaderTemplate()
	// another template has French, this one does not
	f.createTemplate(f.languageTemplate(&f.c1, f.row("fr", &f.c1)))
	for _, code := range []string{"fr", "xx"} {
		got, err := f.repo.GetByIDForLanguage(f.ctx, &lt.id, code, &repository.CampaignTemplateOption{WithEmail: true})
		if !errors.Is(err, repository.ErrTemplateLanguageUnavailable) {
			t.Fatalf("%s: want ErrTemplateLanguageUnavailable, got %v", code, err)
		}
		if got != nil {
			t.Fatalf("%s: content returned with the error", code)
		}
	}
	// a template that is gone is not found, as with GetByID
	missing := uuid.New()
	if _, err := f.repo.GetByIDForLanguage(f.ctx, &missing, "de", nil); err == nil || errors.Is(err, repository.ErrTemplateLanguageUnavailable) {
		t.Fatalf("missing template: %v", err)
	}
}
