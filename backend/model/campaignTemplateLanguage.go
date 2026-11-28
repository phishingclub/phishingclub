package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/validate"
)

// MaxTemplateLanguages is the most language rows a template can hold, one per
// language of the fixed list
var MaxTemplateLanguages = len(data.Languages)

// CampaignTemplateLanguage is one more language of a campaign template, with
// the email and the pages of that language. Proxy stages and every other
// setting stay on the template.
type CampaignTemplateLanguage struct {
	ID                 nullable.Nullable[uuid.UUID] `json:"id"`
	CreatedAt          *time.Time                   `json:"createdAt"`
	UpdatedAt          *time.Time                   `json:"updatedAt"`
	CampaignTemplateID nullable.Nullable[uuid.UUID] `json:"campaignTemplateID"`

	// Language is a code of data.Languages
	Language string `json:"language"`

	EmailID             nullable.Nullable[uuid.UUID] `json:"emailID"`
	BeforeLandingPageID nullable.Nullable[uuid.UUID] `json:"beforeLandingPageID"`
	LandingPageID       nullable.Nullable[uuid.UUID] `json:"landingPageID"`
	AfterLandingPageID  nullable.Nullable[uuid.UUID] `json:"afterLandingPageID"`

	// IsUsable is computed on save, see TemplateStages.Usable. Read only.
	IsUsable bool `json:"isUsable"`

	// names of the email and pages, set when a template is read in full.
	// Read only.
	EmailName             string `json:"emailName,omitempty"`
	BeforeLandingPageName string `json:"beforeLandingPageName,omitempty"`
	LandingPageName       string `json:"landingPageName,omitempty"`
	AfterLandingPageName  string `json:"afterLandingPageName,omitempty"`
}

// TemplateStages tells which page flow stages of a template are pages. A
// language row fills exactly these stages, the others are proxies or not used
// and stay shared.
type TemplateStages struct {
	BeforeLandingPage bool
	LandingPage       bool
	AfterLandingPage  bool
}

// StagesOf returns the stages t has as pages
func StagesOf(t *CampaignTemplate) TemplateStages {
	_, before := t.BeforeLandingPageID.Get()
	_, landing := t.LandingPageID.Get()
	_, after := t.AfterLandingPageID.Get()
	return TemplateStages{
		BeforeLandingPage: before == nil,
		LandingPage:       landing == nil,
		AfterLandingPage:  after == nil,
	}
}

// Usable reports whether a language row has an email and every stage the
// template has as a page
func (s TemplateStages) Usable(l *CampaignTemplateLanguage) bool {
	if _, err := l.EmailID.Get(); err != nil {
		return false
	}
	for _, st := range s.stages(l) {
		if _, err := st.id.Get(); st.page && err != nil {
			return false
		}
	}
	return true
}

// languageStage is one page stage of a language row. name is the stage as
// the template page names it.
type languageStage struct {
	name string
	page bool
	id   nullable.Nullable[uuid.UUID]
}

func (s TemplateStages) stages(l *CampaignTemplateLanguage) []languageStage {
	return []languageStage{
		{"Before Landing", s.BeforeLandingPage, l.BeforeLandingPageID},
		{"Landing", s.LandingPage, l.LandingPageID},
		{"After Landing", s.AfterLandingPage, l.AfterLandingPageID},
	}
}

// MissingStage returns the name of the first stage the template has as a page
// that l has no page for, empty when there is none
func (s TemplateStages) MissingStage(l *CampaignTemplateLanguage) string {
	for _, st := range s.stages(l) {
		if _, err := st.id.Get(); st.page && err != nil {
			return st.name
		}
	}
	return ""
}

// NormalizeLanguageCode trims a language code and makes it lower case
func NormalizeLanguageCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

// Validate checks a language row of a template written in templateLanguage
// with the page stages in stages. The code must be in the fixed list and
// differ from the template language. The row must set an email and a page for
// every stage the template has as a page, and no page for any other stage.
func (l *CampaignTemplateLanguage) Validate(templateLanguage string, stages TemplateStages) error {
	if !data.IsValidLanguage(l.Language) {
		return validate.WrapErrorWithField(
			fmt.Errorf("unknown language %q", l.Language),
			"languages",
		)
	}
	name := data.LanguageName(l.Language)
	if l.Language == templateLanguage {
		return validate.WrapErrorWithField(
			fmt.Errorf("%s is the main language, it can not also be added as a language", name),
			"languages",
		)
	}
	if _, err := l.EmailID.Get(); err != nil {
		return validate.WrapErrorWithField(
			fmt.Errorf("%s needs an email", name),
			"languages",
		)
	}
	for _, st := range stages.stages(l) {
		_, err := st.id.Get()
		if st.page && err != nil {
			return validate.WrapErrorWithField(
				fmt.Errorf("%s needs a %s page, like the template", name, st.name),
				"languages",
			)
		}
		if !st.page && err == nil {
			return validate.WrapErrorWithField(
				fmt.Errorf("%s sets a %s page, but the template has no %s page", name, st.name, st.name),
				"languages",
			)
		}
	}
	return nil
}

// ValidateTemplateLanguages checks the language rows of a template: the
// template language is set when there are rows, each row is valid and no code
// is listed twice. Codes are normalized in place.
// ValidateTemplateLanguageCount refuses more than MaxTemplateLanguages rows
// and empty rows. It is cheap, so it runs before any row is read further.
func ValidateTemplateLanguageCount(languages []*CampaignTemplateLanguage) error {
	if len(languages) > MaxTemplateLanguages {
		return validate.WrapErrorWithField(
			fmt.Errorf("a template can have at most %d languages", MaxTemplateLanguages),
			"languages",
		)
	}
	for _, l := range languages {
		if l == nil {
			return validate.WrapErrorWithField(errors.New("empty language row"), "languages")
		}
	}
	return nil
}

func ValidateTemplateLanguages(templateLanguage string, stages TemplateStages, languages []*CampaignTemplateLanguage) error {
	if len(languages) == 0 {
		return nil
	}
	if templateLanguage == "" {
		return validate.WrapErrorWithField(
			errors.New("the main language is required when the template has languages"),
			"language",
		)
	}
	if len(languages) > MaxTemplateLanguages {
		return validate.WrapErrorWithField(
			fmt.Errorf("a template can have at most %d languages", MaxTemplateLanguages),
			"languages",
		)
	}
	seen := map[string]bool{}
	for _, l := range languages {
		if l == nil {
			return validate.WrapErrorWithField(errors.New("empty language row"), "languages")
		}
		l.Language = NormalizeLanguageCode(l.Language)
		if err := l.Validate(templateLanguage, stages); err != nil {
			return err
		}
		if seen[l.Language] {
			return validate.WrapErrorWithField(
				fmt.Errorf("%s is listed more than once", data.LanguageName(l.Language)),
				"languages",
			)
		}
		seen[l.Language] = true
	}
	return nil
}
