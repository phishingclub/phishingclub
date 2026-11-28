package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/vo"
)

func asValidation(err error, target *errs.ValidationError) bool {
	return errors.As(err, target)
}

func TestNormalizeLanguageKey(t *testing.T) {
	cases := map[string]string{
		"Denmark":         "denmark",
		"  denmark ":      "denmark",
		"\u00a0Denmark\t": "denmark",
		"\ufeffDenmark":   "denmark",
		"DÄNEMARK":        "dänemark",
		// a decomposed umlaut is the same key as the composed one
		"Da\u0308nemark":    "dänemark",
		"Österreich":        "österreich",
		"ΣΟΦΟΣ":             "σοφος",
		"":                  "",
		"   ":               "",
		"Côte d'Ivoire":     "côte d'ivoire",
		"İstanbul":          "i\u0307stanbul",
		"Straße":            "straße",
		"DEPT / Sales":      "dept / sales",
		"\u3000tokyo\u3000": "tokyo",
	}
	for in, want := range cases {
		if got := NormalizeLanguageKey(in); got != want {
			t.Errorf("NormalizeLanguageKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func routingCampaign(routeBy, def string, routes map[string]string) *Campaign {
	c := &Campaign{}
	if routeBy != "-" {
		c.LanguageRouteBy = nullable.NewNullableWithValue(routeBy)
	}
	if def != "-" {
		c.LanguageDefault = nullable.NewNullableWithValue(def)
	}
	if routes != nil {
		c.LanguageRoutes = nullable.NewNullableWithValue(routes)
	}
	return c
}

func TestLanguageRoutingValidate(t *testing.T) {
	ok := []*Campaign{
		routingCampaign("-", "-", nil),
		routingCampaign("", "", nil),
		routingCampaign("country", "en", nil),
		routingCampaign("city", "de", map[string]string{"Berlin": "de"}),
		routingCampaign("department", "en", map[string]string{}),
		routingCampaign("position", "en", map[string]string{"cto": "en"}),
		routingCampaign("misc", "en", map[string]string{"x": "da"}),
		routingCampaign("country", " DE ", map[string]string{"Austria": "DE"}),
	}
	for i, c := range ok {
		if err := c.ValidateLanguageRouting(); err != nil {
			t.Errorf("case %d: %v", i, err)
		}
	}
	bad := map[string]*Campaign{
		"unknown field":         routingCampaign("email", "en", nil),
		"field from mix list":   routingCampaign("none", "en", nil),
		"no default":            routingCampaign("country", "", nil),
		"default not set":       routingCampaign("country", "-", nil),
		"unknown default":       routingCampaign("country", "xx", nil),
		"region default":        routingCampaign("country", "pt-br", nil),
		"unknown route code":    routingCampaign("country", "en", map[string]string{"Denmark": "dk"}),
		"empty key":             routingCampaign("country", "en", map[string]string{"  ": "da"}),
		"duplicate after norm":  routingCampaign("country", "en", map[string]string{"Denmark": "da", " denmark ": "da"}),
		"duplicate nfd":         routingCampaign("country", "en", map[string]string{"Dänemark": "da", "Da\u0308nemark": "da"}),
		"key too long":          routingCampaign("country", "en", map[string]string{strings.Repeat("a", 128): "da"}),
		"default without field": routingCampaign("-", "en", nil),
		"routes without field":  routingCampaign("-", "-", map[string]string{"a": "en"}),
	}
	for name, c := range bad {
		if err := c.ValidateLanguageRouting(); !asValidation(err, new(errs.ValidationError)) {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
	// at most 500 values
	many := map[string]string{}
	for i := 0; i < MaxLanguageRoutes; i++ {
		many[fmt.Sprintf("v%d", i)] = "en"
	}
	if err := routingCampaign("country", "en", many).ValidateLanguageRouting(); err != nil {
		t.Errorf("500 routes refused: %v", err)
	}
	many["one more"] = "en"
	if err := routingCampaign("country", "en", many).ValidateLanguageRouting(); err == nil {
		t.Error("501 routes accepted")
	}
	// a key of 127 characters, counted as characters not bytes
	if err := routingCampaign("country", "en", map[string]string{strings.Repeat("ä", 127): "de"}).ValidateLanguageRouting(); err != nil {
		t.Errorf("127 character key refused: %v", err)
	}
}

func TestLanguageRoutingNormalizesAndClears(t *testing.T) {
	c := routingCampaign("country", "DA", map[string]string{"  DENMARK ": "DA", "Germany": "de"})
	if err := c.ValidateLanguageRouting(); err != nil {
		t.Fatal(err)
	}
	routes := c.LanguageRoutes.MustGet()
	if len(routes) != 2 || routes["denmark"] != "da" || routes["germany"] != "de" {
		t.Fatalf("routes not normalized: %v", routes)
	}
	if c.LanguageDefault.MustGet() != "da" {
		t.Fatalf("default not normalized: %q", c.LanguageDefault.MustGet())
	}
	// off clears the default and the routes
	off := routingCampaign("", "de", map[string]string{"Germany": "de"})
	if err := off.ValidateLanguageRouting(); err != nil {
		t.Fatal(err)
	}
	if off.LanguageDefault.MustGet() != "" || len(off.LanguageRoutes.MustGet()) != 0 {
		t.Fatalf("off kept routing: %q %v", off.LanguageDefault.MustGet(), off.LanguageRoutes.MustGet())
	}
	m := off.ToDBMap()
	if m["language_route_by"] != "" || m["language_default"] != "" {
		t.Fatalf("db map: %v %v", m["language_route_by"], m["language_default"])
	}
}

func TestProgramLanguageRouting(t *testing.T) {
	p := &Program{
		LanguageRouteBy: nullable.NewNullableWithValue("country"),
		LanguageDefault: nullable.NewNullableWithValue("en"),
		LanguageRoutes:  nullable.NewNullableWithValue(map[string]string{"Denmark ": "da"}),
	}
	if err := p.ValidateLanguageRouting(); err != nil {
		t.Fatal(err)
	}
	if p.LanguageRoutes.MustGet()["denmark"] != "da" {
		t.Fatalf("routes: %v", p.LanguageRoutes.MustGet())
	}
	m := p.ToDBMap()
	if m["language_route_by"] != "country" || m["language_default"] != "en" {
		t.Fatalf("db map: %v", m)
	}
	// a program allows country for languages, which its template mix does not
	if IsValidProgramTemplateMixBy("country") {
		t.Fatal("the mix list must stay separate")
	}
	bad := &Program{LanguageRouteBy: nullable.NewNullableWithValue("country")}
	if err := bad.ValidateLanguageRouting(); err == nil {
		t.Fatal("program routing without default accepted")
	}
}

func langRow(code string, email bool, before, landing, after bool) *CampaignTemplateLanguage {
	l := &CampaignTemplateLanguage{Language: code}
	set := func(on bool) nullable.Nullable[uuid.UUID] {
		if on {
			return nullable.NewNullableWithValue(uuid.New())
		}
		return nullable.Nullable[uuid.UUID]{}
	}
	l.EmailID = set(email)
	l.BeforeLandingPageID = set(before)
	l.LandingPageID = set(landing)
	l.AfterLandingPageID = set(after)
	return l
}

func TestValidateTemplateLanguages(t *testing.T) {
	stages := TemplateStages{LandingPage: true, AfterLandingPage: true}
	if err := ValidateTemplateLanguages("en", stages, []*CampaignTemplateLanguage{
		langRow("de", true, false, true, true),
		langRow(" DA ", true, false, true, true),
	}); err != nil {
		t.Fatalf("valid rows refused: %v", err)
	}
	bad := map[string][]*CampaignTemplateLanguage{
		"unknown code":          {langRow("xx", true, false, true, true)},
		"region code":           {langRow("pt-br", true, false, true, true)},
		"template language":     {langRow("en", true, false, true, true)},
		"no email":              {langRow("de", false, false, true, true)},
		"missing page stage":    {langRow("de", true, false, true, false)},
		"page on a proxy stage": {langRow("de", true, true, true, true)},
		"duplicate":             {langRow("de", true, false, true, true), langRow("DE", true, false, true, true)},
	}
	for name, rows := range bad {
		if err := ValidateTemplateLanguages("en", stages, rows); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := ValidateTemplateLanguages("", stages, []*CampaignTemplateLanguage{langRow("de", true, false, true, true)}); err == nil {
		t.Error("rows without a template language accepted")
	}
	if err := ValidateTemplateLanguages("", stages, nil); err != nil {
		t.Errorf("no rows and no template language refused: %v", err)
	}
	// usability follows the stages of the template
	row := langRow("de", true, false, true, false)
	if stages.Usable(row) {
		t.Error("row without the after page is usable")
	}
	if !(TemplateStages{LandingPage: true}).Usable(row) {
		t.Error("row with every page stage is not usable")
	}
}

func TestTemplateLanguageField(t *testing.T) {
	tmpl := newTemplateForLanguageTest()
	tmpl.Language = nullable.NewNullableWithValue(" DE ")
	if err := tmpl.Validate(); err != nil {
		t.Fatal(err)
	}
	if tmpl.TemplateLanguage() != "de" || tmpl.ToDBMap()["language"] != "de" {
		t.Fatalf("language not normalized: %q", tmpl.TemplateLanguage())
	}
	tmpl.Language = nullable.NewNullableWithValue("deu")
	if err := tmpl.Validate(); err == nil {
		t.Fatal("unknown template language accepted")
	}
	// the stage shape is read from the template
	tmpl.Language = nullable.NewNullableWithValue("en")
	tmpl.Languages = nullable.NewNullableWithValue([]*CampaignTemplateLanguage{langRow("de", true, false, true, false)})
	if err := tmpl.Validate(); err != nil {
		t.Fatalf("row matching the template stages refused: %v", err)
	}
	tmpl.AfterLandingPageID = nullable.NewNullableWithValue(uuid.New())
	if err := tmpl.Validate(); err == nil {
		t.Fatal("row missing the after page accepted")
	}
	// a template without languages keeps working without a language
	plain := newTemplateForLanguageTest()
	if err := plain.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.ToDBMap()["language"]; ok {
		t.Fatal("language written when not set")
	}
}

func newTemplateForLanguageTest() *CampaignTemplate {
	a, b := uuid.New(), uuid.New()
	return &CampaignTemplate{
		Name:              nullable.NewNullableWithValue(*vo.NewString64Must("language test")),
		URLIdentifierID:   nullable.NewNullableWithValue(&a),
		StateIdentifierID: nullable.NewNullableWithValue(&b),
		LandingPageID:     nullable.NewNullableWithValue(uuid.New()),
	}
}
