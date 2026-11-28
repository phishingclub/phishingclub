package data

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Language is a language a template can be written in. Code is the lowercase
// ISO 639-1 code that is stored, Name is the English name shown in the UI.
type Language struct {
	Code string
	Name string
}

// Languages is the fixed list of languages a template can use. The frontend
// keeps the same list in src/lib/utils/languages.js, a test checks they match.
var Languages = []Language{
	// official languages of the EU
	{Code: "bg", Name: "Bulgarian"},
	{Code: "hr", Name: "Croatian"},
	{Code: "cs", Name: "Czech"},
	{Code: "da", Name: "Danish"},
	{Code: "nl", Name: "Dutch"},
	{Code: "en", Name: "English"},
	{Code: "et", Name: "Estonian"},
	{Code: "fi", Name: "Finnish"},
	{Code: "fr", Name: "French"},
	{Code: "de", Name: "German"},
	{Code: "el", Name: "Greek"},
	{Code: "hu", Name: "Hungarian"},
	{Code: "ga", Name: "Irish"},
	{Code: "it", Name: "Italian"},
	{Code: "lv", Name: "Latvian"},
	{Code: "lt", Name: "Lithuanian"},
	{Code: "mt", Name: "Maltese"},
	{Code: "pl", Name: "Polish"},
	{Code: "pt", Name: "Portuguese"},
	{Code: "ro", Name: "Romanian"},
	{Code: "sk", Name: "Slovak"},
	{Code: "sl", Name: "Slovenian"},
	{Code: "es", Name: "Spanish"},
	{Code: "sv", Name: "Swedish"},
	// other languages of Europe
	{Code: "no", Name: "Norwegian"},
	{Code: "is", Name: "Icelandic"},
	{Code: "sr", Name: "Serbian"},
	{Code: "bs", Name: "Bosnian"},
	{Code: "mk", Name: "Macedonian"},
	{Code: "sq", Name: "Albanian"},
	{Code: "uk", Name: "Ukrainian"},
	{Code: "ru", Name: "Russian"},
	{Code: "tr", Name: "Turkish"},
	// other languages common in companies
	{Code: "ar", Name: "Arabic"},
	{Code: "he", Name: "Hebrew"},
	{Code: "hi", Name: "Hindi"},
	{Code: "zh", Name: "Chinese"},
	{Code: "ja", Name: "Japanese"},
	{Code: "ko", Name: "Korean"},
	{Code: "th", Name: "Thai"},
	{Code: "vi", Name: "Vietnamese"},
	{Code: "id", Name: "Indonesian"},
	{Code: "ms", Name: "Malay"},
}

var languageNames = func() map[string]string {
	m := make(map[string]string, len(Languages))
	for _, l := range Languages {
		m[l.Code] = l.Name
	}
	return m
}()

// IsValidLanguage reports whether code is in the fixed language list. The
// match is exact, codes are lowercase.
func IsValidLanguage(code string) bool {
	_, ok := languageNames[code]
	return ok
}

// LanguageName returns the English name of a language code, or the code
// itself when it is not in the list.
func LanguageName(code string) string {
	if name, ok := languageNames[code]; ok {
		return name
	}
	return code
}

// languageAliases are codes and names that mean a language of the list under
// another code: the Norwegian written forms and old codes still sent by some
// systems
var languageAliases = map[string]string{
	"nb":               "no",
	"nn":               "no",
	"norwegianbokmal":  "no",
	"norwegianbokmål":  "no",
	"norwegiannynorsk": "no",
	"bokmal":           "no",
	"bokmål":           "no",
	"nynorsk":          "no",
	"iw":               "he",
	"in":               "id",
}

// languageByName maps each English name, lower case with no spaces, to its code
var languageByName = func() map[string]string {
	m := make(map[string]string, len(Languages))
	for _, l := range Languages {
		m[strings.ToLower(l.Name)] = l.Code
	}
	return m
}()

// MaxLanguageInputLength is the longest text NormalizeLanguage looks at
const MaxLanguageInputLength = 64

// NormalizeLanguage returns the code of the fixed list language that v names.
// v can be a code (da), a language tag whose first part is a code (da-DK,
// da_DK) or an English name (Danish). Case and spaces do not matter. Empty
// input gives an empty code and true. A value that names no language of the
// list gives an empty code and false. Zero width and other format characters
// are removed and the text is composed (NFC), so a name typed with a separate
// accent mark (as macOS often gives) matches.
func NormalizeLanguage(v string) (string, bool) {
	if len(v) > MaxLanguageInputLength {
		return "", false
	}
	key := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return unicode.ToLower(r)
	}, v)
	key = norm.NFC.String(key)
	if key == "" {
		return "", true
	}
	if IsValidLanguage(key) {
		return key, true
	}
	if code, ok := languageAliases[key]; ok {
		return code, true
	}
	if code, ok := languageByName[key]; ok {
		return code, true
	}
	// a language tag, only the first part names the language
	if i := strings.IndexAny(key, "-_"); i > 0 {
		primary := key[:i]
		if IsValidLanguage(primary) {
			return primary, true
		}
		if code, ok := languageAliases[primary]; ok {
			return code, true
		}
	}
	return "", false
}
