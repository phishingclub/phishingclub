package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
)

// ProgramGroupByLanguage groups the results of a program by the language of
// each send
const ProgramGroupByLanguage = "language"

// factTemplateLanguages returns the template language of each template key of
// program facts, the template id as text. A template that is gone or has no
// language is left out.
func (s *Program) factTemplateLanguages(ctx context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	if s.CampaignTemplateService == nil || s.CampaignTemplateService.CampaignTemplateRepository == nil {
		return out, nil
	}
	ids := make([]uuid.UUID, 0, len(keys))
	for _, key := range keys {
		if id, err := uuid.Parse(key); err == nil {
			ids = append(ids, id)
		}
	}
	sets, err := s.CampaignTemplateService.CampaignTemplateRepository.GetLanguageSetsByIDs(ctx, ids)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for id, set := range sets {
		if set.Language != "" {
			out[id.String()] = set.Language
		}
	}
	return out, nil
}

// sendLanguage returns the language a send was made in: its stamp, or the
// language of its template when it has none. Empty when neither is set.
// The caller holds the read lock.
func (st *programFactsStore) sendLanguage(sd *programFactSend, templateLanguages map[string]string) string {
	if code := st.languages[sd.language]; code != "" {
		return code
	}
	return templateLanguages[st.templates[sd.template]]
}

// programLanguageGroups computes the results of a loaded program grouped by
// the language of each send. Program sends are stamped like campaign sends;
// a send with no stamp counts in its template language. A named program gets
// exact counts. An anonymous program, or a named one whose Anonymize All
// began, gets sizes only: an anonymous program counts the sends of its whole
// settled months, and a row of fewer than programMinGroup people, in total or
// in one of its months, is withheld. A withheld language row leaves the month
// totals as they are, a language says nothing of the outcomes.
func (s *Program) programLanguageGroups(
	ctx context.Context,
	program *model.Program,
	now time.Time,
) (*ProgramResultGroups, error) {
	out := &ProgramResultGroups{By: ProgramGroupByLanguage, Rows: []*ProgramResultRow{}}
	sizesOnly := program.IsAnonymousOn() || programPersonByPseudonym(program)
	out.Anonymous = sizesOnly
	facts, err := s.programFactsFor(ctx, program, now)
	if err != nil {
		return nil, err
	}
	facts.mu.RLock()
	keys := make([]string, len(facts.templates))
	copy(keys, facts.templates)
	facts.mu.RUnlock()
	templateLanguages, err := s.factTemplateLanguages(ctx, keys)
	if err != nil {
		return nil, err
	}
	kinds, err := s.stepKinds(ctx, program)
	if err != nil {
		return nil, err
	}
	facts.mu.RLock()
	defer facts.mu.RUnlock()
	if !sizesOnly {
		hasPhishing := kinds.phishing || facts.hasPhishing
		hasTraining := kinds.training || facts.hasTraining
		out.Rows = facts.languageRows(templateLanguages, hasPhishing, hasTraining)
		return out, nil
	}
	var anon *programAnonymousFacts
	if facts.anonymous {
		anon = facts.anonymousFacts(now, program.Location())
	}
	out.Rows = facts.languageSizeRows(templateLanguages, anon)
	return out, nil
}

// languageRowKey returns the key and the name of a language row
func languageRowKey(code string) (string, string) {
	if code == "" {
		return "unspecified", programGroupUnspecified
	}
	return "language:" + code, data.LanguageName(code)
}

// languageRowBefore orders rows by language name with the unspecified row
// last, like the results by language of a campaign
func languageRowBefore(a, b *ProgramResultRow) bool {
	if ua, ub := a.Key == "unspecified", b.Key == "unspecified"; ua != ub {
		return ub
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.Key < b.Key
}

// languageRows counts the sends of a named program by language, ordered by
// language name. The caller holds the read lock.
func (st *programFactsStore) languageRows(
	templateLanguages map[string]string,
	hasPhishing, hasTraining bool,
) []*ProgramResultRow {
	counts := map[string]*programRowSends{}
	for i := range st.sends {
		sd := &st.sends[i]
		code := st.sendLanguage(sd, templateLanguages)
		c, ok := counts[code]
		if !ok {
			c = &programRowSends{}
			counts[code] = c
		}
		c.add(sd)
	}
	type languageRow struct {
		row  *ProgramResultRow
		sent int
	}
	list := make([]languageRow, 0, len(counts))
	for code, c := range counts {
		key, name := languageRowKey(code)
		r := &ProgramResultRow{Key: key, Name: name}
		if hasPhishing {
			r.Sent, r.Clicked, r.Submitted, r.Reported = intPtr(c.sent), intPtr(c.clicked), intPtr(c.submitted), intPtr(c.reported)
		}
		if hasTraining {
			r.TrainingSent = intPtr(c.trainingSent)
			r.TrainingStarted = intPtr(c.trainingStarted)
			r.TrainingCompleted = intPtr(c.trainingCompleted)
		}
		list = append(list, languageRow{r, c.sent + c.trainingSent})
	}
	// by language name, like the results by language of a campaign
	sort.Slice(list, func(i, j int) bool { return languageRowBefore(list[i].row, list[j].row) })
	out := make([]*ProgramResultRow, 0, len(list))
	for _, r := range list {
		out = append(out, r.row)
	}
	return out
}

// languageGroupCount returns how many rows the results grouped by language
// have, see programLanguageGroups. With sizesOnly only phishing sends count,
// for an anonymous program only those of its whole settled months. The
// caller holds the read lock.
func (st *programFactsStore) languageGroupCount(
	templateLanguages map[string]string,
	sizesOnly bool,
	now time.Time,
	loc *time.Location,
) int {
	var anon *programAnonymousFacts
	if sizesOnly && st.anonymous {
		anon = st.anonymousFacts(now, loc)
	}
	codes := map[string]struct{}{}
	for i := range st.sends {
		sd := &st.sends[i]
		if sizesOnly && sd.training {
			continue
		}
		if anon != nil && anon.monthOf(sd.sentAt) < 0 {
			continue
		}
		codes[st.sendLanguage(sd, templateLanguages)] = struct{}{}
		if len(codes) > 1 {
			break
		}
	}
	return len(codes)
}

// languageSizeRows counts the phishing sends of each language with the size
// only. anon, when set, limits the count to the whole settled months of an
// anonymous program and withholds a row with a month of fewer than
// programMinGroup people. A row of fewer than programMinGroup people is
// withheld. The caller holds the read lock.
func (st *programFactsStore) languageSizeRows(
	templateLanguages map[string]string,
	anon *programAnonymousFacts,
) []*ProgramResultRow {
	type languageCount struct {
		sent        int
		people      map[int32]struct{}
		monthPeople map[int]map[int32]struct{}
	}
	counts := map[string]*languageCount{}
	for i := range st.sends {
		sd := &st.sends[i]
		if sd.training {
			continue
		}
		month := 0
		if anon != nil {
			month = anon.monthOf(sd.sentAt)
			if month < 0 {
				continue
			}
		}
		code := st.sendLanguage(sd, templateLanguages)
		c, ok := counts[code]
		if !ok {
			c = &languageCount{people: map[int32]struct{}{}, monthPeople: map[int]map[int32]struct{}{}}
			counts[code] = c
		}
		c.sent++
		c.people[sd.person] = struct{}{}
		if anon != nil {
			if c.monthPeople[month] == nil {
				c.monthPeople[month] = map[int32]struct{}{}
			}
			c.monthPeople[month][sd.person] = struct{}{}
		}
	}
	type languageRow struct {
		row  *ProgramResultRow
		sent int
	}
	list := make([]languageRow, 0, len(counts))
	for code, c := range counts {
		key, name := languageRowKey(code)
		r := &ProgramResultRow{Key: key, Name: name, OutcomesHidden: true}
		withheld := len(c.people) < programMinGroup
		for _, people := range c.monthPeople {
			if len(people) < programMinGroup {
				withheld = true
			}
		}
		if withheld {
			r.Suppressed = true
		} else {
			r.Sent = intPtr(c.sent)
		}
		list = append(list, languageRow{r, c.sent})
	}
	// by language name, like the results by language of a campaign, so the
	// place of a withheld row tells nothing of its size
	sort.Slice(list, func(i, j int) bool { return languageRowBefore(list[i].row, list[j].row) })
	out := make([]*ProgramResultRow, 0, len(list))
	for _, r := range list {
		out = append(out, r.row)
	}
	return out
}

// errProgramGroupBy is returned for a grouping the program results do not have
var errProgramGroupBy = errors.New("must be department, position or language")
