package repository

import "testing"

// TestMergeLanguageStatRows checks rows of the same language are summed, a
// row with no stamp counts in its template language, a template with no
// language gives the unspecified group, and groups are ordered by language
// name with the unspecified group last
func TestMergeLanguageStatRows(t *testing.T) {
	en, de, empty := "en", "de", ""
	got := mergeLanguageStatRows([]languageStatRow{
		{Language: &de, TemplateLanguage: &en, Total: 2, Clicked: 1},
		{Language: nil, TemplateLanguage: &en, Total: 3, Clicked: 1},
		{Language: &en, TemplateLanguage: &de, Total: 1},
		{Language: nil, TemplateLanguage: &empty, Total: 1},
		{Language: nil, TemplateLanguage: nil, Total: 1},
	})
	if len(got) != 3 {
		t.Fatalf("want 3 groups, got %+v", got)
	}
	if got[0].GroupID != "en" || got[0].Total != 4 || got[0].Clicked != 1 || got[0].Group != "English" {
		t.Fatalf("english first: %+v", got)
	}
	if got[1].GroupID != "de" || got[1].Total != 2 || got[1].Clicked != 1 {
		t.Fatalf("german: %+v", got)
	}
	if got[2].GroupID != "" || got[2].Group != UnspecifiedLanguageGroup || got[2].Total != 2 {
		t.Fatalf("unspecified last: %+v", got)
	}
}
