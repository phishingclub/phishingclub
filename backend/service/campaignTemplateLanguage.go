package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// templateLanguageCompanyRefs lists the emails and pages of language rows
func templateLanguageCompanyRefs(languages []*model.CampaignTemplateLanguage) []CompanyRef {
	refs := companyRefs{}
	for _, l := range languages {
		if l == nil {
			continue
		}
		refs.add(database.EMAIL_TABLE, "languages", l.EmailID)
		refs.add(database.PAGE_TABLE, "languages", l.BeforeLandingPageID)
		refs.add(database.PAGE_TABLE, "languages", l.LandingPageID)
		refs.add(database.PAGE_TABLE, "languages", l.AfterLandingPageID)
	}
	return refs
}

// languagesOf returns the language rows set on t, nil when not set
func languagesOf(t *model.CampaignTemplate) []*model.CampaignTemplateLanguage {
	if v, err := t.Languages.Get(); err == nil {
		return v
	}
	return nil
}

// languageEmailChanged lists the codes of rows whose email is new or not the
// one stored for that code
func languageEmailChanged(stored, incoming []*model.CampaignTemplateLanguage) []*model.CampaignTemplateLanguage {
	storedEmail := map[string]uuid.UUID{}
	for _, l := range stored {
		if v, err := l.EmailID.Get(); err == nil {
			storedEmail[l.Language] = v
		}
	}
	out := []*model.CampaignTemplateLanguage{}
	for _, l := range incoming {
		v, err := l.EmailID.Get()
		if err != nil {
			continue
		}
		if prev, ok := storedEmail[l.Language]; ok && prev == v {
			continue
		}
		out = append(out, l)
	}
	return out
}

// validateLanguageEmails checks the email of every given language row: it must
// exist, have content that parses and runs with example data, and a subject
// that parses. A broken language email is refused when it is saved, not only
// when it is sent.
func (c *CampaignTemplate) validateLanguageEmails(
	ctx context.Context,
	companyID *uuid.UUID,
	languages []*model.CampaignTemplateLanguage,
) error {
	emails := &repository.Email{DB: c.CampaignTemplateRepository.DB}
	for _, l := range languages {
		emailID, err := l.EmailID.Get()
		if err != nil {
			continue
		}
		name := data.LanguageName(l.Language)
		email, err := emails.GetByID(ctx, &emailID, &repository.EmailOption{})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return validate.WrapErrorWithField(fmt.Errorf("the %s email was not found", name), "languages")
		}
		if err != nil {
			return err
		}
		content, err := email.Content.Get()
		if err != nil {
			return validate.WrapErrorWithField(fmt.Errorf("the %s email has no content", name), "languages")
		}
		if c.TemplateService != nil {
			if err := c.TemplateService.ValidateEmailTemplate(content.String()); err != nil {
				return validate.WrapErrorWithField(fmt.Errorf("the %s email: %s", name, err.Error()), "languages")
			}
		} else if _, err := template.New("email").Funcs(TemplateFuncs()).Parse(content.String()); err != nil {
			return validate.WrapErrorWithField(fmt.Errorf("the %s email does not parse: %s", name, err.Error()), "languages")
		}
		subject, err := email.MailHeaderSubject.Get()
		if err != nil {
			return validate.WrapErrorWithField(fmt.Errorf("the %s email has no subject", name), "languages")
		}
		funcs := TemplateFuncs()
		if c.TemplateService != nil {
			funcs = c.TemplateService.TemplateFuncsWithCompany(ctx, companyID)
		}
		if _, err := template.New("subject").Funcs(funcs).Parse(subject.String()); err != nil {
			return validate.WrapErrorWithField(fmt.Errorf("the %s email subject does not parse: %s", name, err.Error()), "languages")
		}
	}
	return nil
}

// languageChanges compares the stored language rows with the incoming ones
type languageChanges struct {
	added   []string
	removed []string
	// changed holds the new email and page ids of every code whose ids differ
	changed map[string]map[string]string
}

func languageIDs(l *model.CampaignTemplateLanguage) map[string]string {
	out := map[string]string{}
	set := func(key string, id nullable.Nullable[uuid.UUID]) {
		if v, err := id.Get(); err == nil {
			out[key] = v.String()
		}
	}
	set("emailId", l.EmailID)
	set("beforeLandingPageId", l.BeforeLandingPageID)
	set("landingPageId", l.LandingPageID)
	set("afterLandingPageId", l.AfterLandingPageID)
	return out
}

func compareLanguages(stored, incoming []*model.CampaignTemplateLanguage) languageChanges {
	ch := languageChanges{added: []string{}, removed: []string{}, changed: map[string]map[string]string{}}
	byCode := map[string]*model.CampaignTemplateLanguage{}
	for _, l := range stored {
		byCode[l.Language] = l
	}
	seen := map[string]bool{}
	for _, l := range incoming {
		seen[l.Language] = true
		prev, ok := byCode[l.Language]
		if !ok {
			ch.added = append(ch.added, l.Language)
			continue
		}
		before, after := languageIDs(prev), languageIDs(l)
		same := len(before) == len(after)
		for k, v := range after {
			if before[k] != v {
				same = false
			}
		}
		if !same {
			ch.changed[l.Language] = after
		}
	}
	for _, l := range stored {
		if !seen[l.Language] {
			ch.removed = append(ch.removed, l.Language)
		}
	}
	sort.Strings(ch.added)
	sort.Strings(ch.removed)
	return ch
}

// goneLanguages returns the codes that recipients could be stamped with before
// an update and that have no content after it: language rows that are removed
// and a template language that changes, unless the code is still present as a
// row or as the new template language
func goneLanguages(storedLanguage, newLanguage string, storedCodes, newCodes []string) []string {
	before := append([]string{}, storedCodes...)
	if storedLanguage != "" {
		before = append(before, storedLanguage)
	}
	after := append([]string{}, newCodes...)
	if newLanguage != "" {
		after = append(after, newLanguage)
	}
	out := []string{}
	for _, code := range before {
		if !slices.Contains(after, code) && !slices.Contains(out, code) {
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out
}

// refuseGoneLanguagesInUse refuses an update that takes away languages that an
// open campaign uses (recipients stamped with it, sent or not, or its default
// or routes pick it) or that an open program, draft or started, picks for its
// people. nullStamp is the code that recipients with no language are stamped
// with by the update; when it is gone those recipients use it too. The
// refusal names only the languages in use, and only the campaigns and
// programs of the template's company (global ones for a global template);
// the others are counted.
func (c *CampaignTemplate) refuseGoneLanguagesInUse(
	ctx context.Context,
	templateID *uuid.UUID,
	companyID *uuid.UUID,
	gone []string,
	nullStamp string,
) error {
	if len(gone) == 0 {
		return nil
	}
	listed := func(names []string, others int64) string {
		shown := names
		if len(shown) > 3 {
			shown = shown[:3]
		}
		text := strings.Join(shown, ", ")
		if len(names) > 3 {
			text += " and more"
		}
		if others > 0 {
			if text == "" {
				return fmt.Sprintf("%d in other companies", others)
			}
			text += fmt.Sprintf(" and %d in other companies", others)
		}
		return text
	}
	refusal := func(codes []string, users string, names []string, others int64) error {
		languageNames := make([]string, 0, len(codes))
		for _, code := range codes {
			languageNames = append(languageNames, data.LanguageName(code))
		}
		pronoun := "it"
		if len(codes) > 1 {
			pronoun = "them"
		}
		return validate.WrapErrorWithField(
			fmt.Errorf(
				"%s can not be removed while %s %s: %s",
				strings.Join(languageNames, ", "),
				users,
				pronoun,
				listed(names, others),
			),
			"languages",
		)
	}
	// each gone code is checked alone so the refusal names only the codes in
	// use. gone holds at most the languages of one template.
	usedCodes := []string{}
	campaignNames := []string{}
	var otherCampaigns int64
	for _, code := range gone {
		names, others, err := c.CampaignRepository.GetOpenCampaignNamesUsingLanguage(
			ctx, templateID, []string{code}, code == nullStamp, companyID, 4,
		)
		if err != nil {
			c.Logger.Errorw("failed to check campaigns using the template languages", "error", err)
			return err
		}
		if len(names) == 0 && others == 0 {
			continue
		}
		usedCodes = append(usedCodes, code)
		otherCampaigns = max(otherCampaigns, others)
		for _, name := range names {
			if !slices.Contains(campaignNames, name) {
				campaignNames = append(campaignNames, name)
			}
		}
	}
	if len(usedCodes) > 1 && otherCampaigns > 0 {
		// a campaign of another company can use more than one of the codes,
		// so the codes are counted together
		_, others, err := c.CampaignRepository.GetOpenCampaignNamesUsingLanguage(
			ctx, templateID, usedCodes, slices.Contains(usedCodes, nullStamp), companyID, 1,
		)
		if err != nil {
			c.Logger.Errorw("failed to count campaigns using the template languages", "error", err)
			return err
		}
		otherCampaigns = others
	}
	if len(usedCodes) > 0 {
		return refusal(usedCodes, "open campaigns use", campaignNames, otherCampaigns)
	}
	if c.ProgramRepository == nil {
		return nil
	}
	programNames := []string{}
	otherPrograms := map[uuid.UUID]bool{}
	for _, code := range gone {
		programs, err := c.ProgramRepository.GetOpenUsingTemplateLanguages(ctx, templateID, []string{code})
		if err != nil {
			c.Logger.Errorw("failed to check programs using the template languages", "error", err)
			return err
		}
		if len(programs) == 0 {
			continue
		}
		usedCodes = append(usedCodes, code)
		for _, p := range programs {
			if !sameCompany(companyID, p.CompanyID) {
				otherPrograms[p.ID] = true
				continue
			}
			if !slices.Contains(programNames, p.Name) {
				programNames = append(programNames, p.Name)
			}
		}
	}
	if len(usedCodes) > 0 {
		return refusal(usedCodes, "programs that are not closed pick", programNames, int64(len(otherPrograms)))
	}
	return nil
}

// sameCompany reports whether two company ids are the same, nil being global
func sameCompany(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// checkStoredLanguages checks the stored language rows against an update that
// does not send languages: a row can not take the code of the new template
// language, and the template language stays required while rows exist
func checkStoredLanguages(templateLanguage string, stored []*model.CampaignTemplateLanguage) error {
	if len(stored) == 0 {
		return nil
	}
	if templateLanguage == "" {
		return validate.WrapErrorWithField(
			errors.New("the main language is required when the template has languages"),
			"language",
		)
	}
	for _, l := range stored {
		if l.Language == templateLanguage {
			return validate.WrapErrorWithField(
				fmt.Errorf("%s is already a language of the template, remove it first", data.LanguageName(templateLanguage)),
				"language",
			)
		}
	}
	return nil
}

// checkStoredLanguageStages checks the stored language rows against the page
// stages of an update that does not send languages. A usable row that has no
// page for a stage the template now has as a page would stop being usable, so
// the update is refused and the client must send the languages with a page for
// that stage. A stage that is no longer a page is cleared on the rows instead.
func checkStoredLanguageStages(stages model.TemplateStages, stored []*model.CampaignTemplateLanguage) error {
	for _, l := range stored {
		if l == nil || !l.IsUsable || stages.Usable(l) {
			continue
		}
		stage := stages.MissingStage(l)
		return validate.WrapErrorWithField(
			fmt.Errorf(
				"%s has no %s page, send the languages with a %s page for every language",
				data.LanguageName(l.Language), stage, stage,
			),
			"languages",
		)
	}
	return nil
}

// templateContent is the email and the page ids of one language of a
// template. A stage that is a proxy or not used has no page id.
type templateContent struct {
	email, before, landing, after *uuid.UUID
}

func idOrNil(v nullable.Nullable[uuid.UUID]) *uuid.UUID {
	if id, err := v.Get(); err == nil {
		return &id
	}
	return nil
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (t templateContent) equal(o templateContent) bool {
	return sameID(t.email, o.email) && sameID(t.before, o.before) &&
		sameID(t.landing, o.landing) && sameID(t.after, o.after)
}

// contentOfTemplate returns the template's own email and pages
func contentOfTemplate(t *model.CampaignTemplate) templateContent {
	return templateContent{
		email:   idOrNil(t.EmailID),
		before:  idOrNil(t.BeforeLandingPageID),
		landing: idOrNil(t.LandingPageID),
		after:   idOrNil(t.AfterLandingPageID),
	}
}

// contentOfLanguage returns the email and pages of a language row
func contentOfLanguage(l *model.CampaignTemplateLanguage) templateContent {
	return templateContent{
		email:   idOrNil(l.EmailID),
		before:  idOrNil(l.BeforeLandingPageID),
		landing: idOrNil(l.LandingPageID),
		after:   idOrNil(l.AfterLandingPageID),
	}
}

// nullStampLanguage returns the code that recipients with no language on
// this template must be stamped with when an update moves the template's own
// content to another code, so they keep the content they were getting. A
// recipient with no language gets the template language and its own email
// and pages. Empty means the rows can stay as they are.
//
// When the template language does not change, the rows keep their meaning.
// Otherwise the old template language is used when it is still present. When
// it was empty or is gone, the code whose email and pages now equal the old
// template fields is used. When no content matches, the old template language
// is used, as it names what those people were sent. When that was empty too
// the rows stay as they are, like an edit of the email without languages.
func nullStampLanguage(
	storedLanguage, newLanguage string,
	stored templateContent,
	updated *model.CampaignTemplate,
	finalLanguages []*model.CampaignTemplateLanguage,
) string {
	if storedLanguage == newLanguage {
		return ""
	}
	if storedLanguage != "" {
		for _, l := range finalLanguages {
			if l.Language == storedLanguage {
				return storedLanguage
			}
		}
	}
	if newLanguage != "" && stored.equal(contentOfTemplate(updated)) {
		return newLanguage
	}
	for _, l := range finalLanguages {
		if stored.equal(contentOfLanguage(l)) {
			return l.Language
		}
	}
	return storedLanguage
}
