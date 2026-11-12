package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/lure"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// TemplateRand is the random source used to spread templates over recipients.
// Tests set a seeded one.
type TemplateRand interface {
	Shuffle(n int, swap func(i, j int))
	IntN(n int) int
}

type globalTemplateRand struct{}

func (globalTemplateRand) Shuffle(n int, swap func(i, j int)) { rand.Shuffle(n, swap) }
func (globalTemplateRand) IntN(n int) int                     { return rand.IntN(n) }

func (c *Campaign) templateRand() TemplateRand {
	if c.TemplateRand != nil {
		return c.TemplateRand
	}
	return globalTemplateRand{}
}

// assignTemplates gives each recipient one template from the pool. keys holds
// each recipient's value of the campaign's "Balance per" field, in schedule
// order.
//
// First every group is given its share of each template: the pool is shuffled
// once into a cycle, and one counter hands out the cycle across the groups, so
// each group and the totals stay within one of even.
//
// Then the shares are handed out going through the recipients in order. Each
// one gets a template its group still has left. In turn it prefers one that
// differs from the last template its group got, one after which the group can
// still avoid the same template twice in a row, one that differs from the
// template given just before, the one its group has the most of left, and the
// next one in the cycle after the template given just before. Recipients scheduled close together
// therefore get different templates, also when groups are interleaved. The
// result is indexed like keys.
func assignTemplates(keys []string, pool []uuid.UUID, rng TemplateRand) []uuid.UUID {
	out := make([]uuid.UUID, len(keys))
	if len(pool) == 0 {
		return out
	}
	if len(pool) == 1 {
		for i := range out {
			out[i] = pool[0]
		}
		return out
	}
	cycle := append([]uuid.UUID{}, pool...)
	rng.Shuffle(len(cycle), func(i, j int) { cycle[i], cycle[j] = cycle[j], cycle[i] })
	size := len(cycle)

	// each group's share of each template, by position in the cycle
	groupOf := make([]int, len(keys))
	index := map[string]int{}
	groupKeys := []string{}
	groupSize := []int{}
	for i, k := range keys {
		g, ok := index[k]
		if !ok {
			g = len(groupKeys)
			index[k] = g
			groupKeys = append(groupKeys, k)
			groupSize = append(groupSize, 0)
		}
		groupOf[i] = g
		groupSize[g]++
	}
	order := make([]int, len(groupKeys))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return groupKeys[order[a]] < groupKeys[order[b]] })
	rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	left := make([][]int, len(groupKeys))
	rest := make([]int, len(groupKeys))
	counter := 0
	for _, g := range order {
		left[g] = make([]int, size)
		for n := 0; n < groupSize[g]; n++ {
			left[g][counter%size]++
			counter++
		}
		rest[g] = groupSize[g]
	}

	// hand the shares out in schedule order
	groupLast := make([]int, len(groupKeys))
	for g := range groupLast {
		groupLast[g] = -1
	}
	previous := -1
	candidates := make([]int, 0, size)
	filtered := make([]int, 0, size)
	// narrow keeps the candidates that pass keep, unless none do
	narrow := func(keep func(int) bool) {
		filtered = filtered[:0]
		for _, c := range candidates {
			if keep(c) {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) > 0 {
			candidates, filtered = filtered, candidates
		}
	}
	for i := range keys {
		g := groupOf[i]
		counts := left[g]
		// the highest and second highest counts left, and how many hold the highest
		top, second, atTop := 0, 0, 0
		for _, n := range counts {
			switch {
			case n > top:
				second, top, atTop = top, n, 1
			case n == top:
				atTop++
			case n > second:
				second = n
			}
		}
		after := rest[g] - 1
		half := (after + 1) / 2
		candidates = candidates[:0]
		for c, n := range counts {
			if n > 0 {
				candidates = append(candidates, c)
			}
		}
		narrow(func(c int) bool { return c != groupLast[g] })
		// after this pick the group must still be able to avoid the same template
		// twice in a row: no template may need more than every other of the rest,
		// and one that needs exactly that on an odd rest must not be this pick
		narrow(func(c int) bool {
			n := counts[c] - 1
			others := top
			if counts[c] == top && atTop == 1 {
				others = second
			}
			if others > half || n > half {
				return false
			}
			return !(after%2 == 1 && n == half)
		})
		narrow(func(c int) bool { return c != previous })
		most := 0
		for _, c := range candidates {
			most = max(most, counts[c])
		}
		narrow(func(c int) bool { return counts[c] == most })
		// the next one in the cycle after the template given just before
		pick, best := candidates[0], size+1
		for _, c := range candidates {
			distance := c - previous
			if distance <= 0 {
				distance += size
			}
			if distance < best {
				pick, best = c, distance
			}
		}
		out[i] = cycle[pick]
		counts[pick]--
		rest[g]--
		groupLast[g] = pick
		previous = pick
	}
	return out
}

// pickLeastUsed returns the pool template with the fewest recipients, ties broken
// at random, and counts the pick
func pickLeastUsed(counts map[uuid.UUID]int, pool []uuid.UUID, rng TemplateRand) uuid.UUID {
	if len(pool) == 1 {
		counts[pool[0]]++
		return pool[0]
	}
	least := -1
	candidates := []uuid.UUID{}
	for _, id := range pool {
		n := counts[id]
		switch {
		case least == -1 || n < least:
			least = n
			candidates = []uuid.UUID{id}
		case n == least:
			candidates = append(candidates, id)
		}
	}
	pick := candidates[rng.IntN(len(candidates))]
	counts[pick]++
	return pick
}

// pickForGroup returns the pool template the group has least of, then the one
// least used in the whole campaign, ties broken at random, and counts the pick
// in both
func pickForGroup(group map[uuid.UUID]int, totals map[uuid.UUID]int, pool []uuid.UUID, rng TemplateRand) uuid.UUID {
	least := -1
	candidates := []uuid.UUID{}
	for _, id := range pool {
		n := group[id]
		switch {
		case least == -1 || n < least:
			least = n
			candidates = []uuid.UUID{id}
		case n == least:
			candidates = append(candidates, id)
		}
	}
	pick := pickLeastUsed(totals, candidates, rng)
	group[pick]++
	return pick
}

// groupByKey orders indexes so recipients with the same mix key are next to
// each other, groups and members shuffled. Used for self managed campaigns and
// pool edits, before pickLeastUsed, so each group gets a mix.
func groupByKey(mixKeys []string, rng TemplateRand) []int {
	groups := map[string][]int{}
	keys := []string{}
	for i, d := range mixKeys {
		if _, ok := groups[d]; !ok {
			keys = append(keys, d)
		}
		groups[d] = append(groups[d], i)
	}
	sort.Strings(keys)
	rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
	out := make([]int, 0, len(mixKeys))
	for _, key := range keys {
		idx := groups[key]
		rng.Shuffle(len(idx), func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
		out = append(out, idx...)
	}
	return out
}

// recipientMixValue returns the recipient's value of the mix field, or empty.
// With none every recipient shares one empty key, so the spread is just even.
func recipientMixValue(recipient *model.Recipient, mixBy string) string {
	if recipient == nil {
		return ""
	}
	var field nullable.Nullable[vo.OptionalString127]
	switch mixBy {
	case model.TemplateMixDepartment:
		field = recipient.Department
	case model.TemplateMixPosition:
		field = recipient.Position
	case model.TemplateMixCity:
		field = recipient.City
	case model.TemplateMixCountry:
		field = recipient.Country
	case model.TemplateMixMisc:
		field = recipient.Misc
	default:
		return ""
	}
	if v, err := field.Get(); err == nil {
		return v.String()
	}
	return ""
}

// assignmentMixValue returns the value of the mix field from a stored assignment
func assignmentMixValue(row repository.TemplateAssignment, mixBy string) string {
	switch mixBy {
	case model.TemplateMixDepartment:
		return row.Department
	case model.TemplateMixPosition:
		return row.Position
	case model.TemplateMixCity:
		return row.City
	case model.TemplateMixCountry:
		return row.Country
	case model.TemplateMixMisc:
		return row.Misc
	}
	return ""
}

// templatePool returns the campaign's stored pool, or its single template for a
// campaign that has no pool rows
func (c *Campaign) templatePool(ctx context.Context, campaign *model.Campaign) ([]uuid.UUID, error) {
	if id, err := campaign.ID.Get(); err == nil {
		pool, err := c.CampaignRepository.GetTemplatePoolIDs(ctx, &id)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		if len(pool) > 0 {
			return pool, nil
		}
	}
	if v, err := campaign.TemplateID.Get(); err == nil {
		return []uuid.UUID{v}, nil
	}
	return nil, errors.New("campaign has no template")
}

// lureSettingsOf returns the lure values of a template the way the campaign
// snapshot reads them, so invalid values compare as the defaults
func lureSettingsOf(t *model.CampaignTemplate) (mode string, algorithm string, length int, isTraining bool) {
	mode = data.LureURLModeQuery
	algorithm = string(lure.DefaultAlgorithm)
	length = lure.DefaultLength
	if v, err := t.LureURLMode.Get(); err == nil && data.IsValidLureURLMode(v) {
		mode = v
	}
	if v, err := t.LureCodeAlgo.Get(); err == nil && lure.IsValidAlgorithm(lure.Algorithm(v)) {
		algorithm = v
	}
	if v, err := t.LureCodeLength.Get(); err == nil && v >= lure.MinLength && v <= lure.MaxLength {
		length = v
	}
	if v, err := t.IsTraining.Get(); err == nil {
		isTraining = v
	}
	return mode, algorithm, length, isTraining
}

// validateTemplatePool checks that every pool template exists, is usable and
// shares the settings the campaign copies from its first template. Returns the
// templates in pool order. Company scope is checked by ValidateCompanyRefs.
func (c *Campaign) validateTemplatePool(
	ctx context.Context,
	campaign *model.Campaign,
) ([]*model.CampaignTemplate, error) {
	if err := campaign.ValidateTemplatePool(); err != nil {
		return nil, err
	}
	pool := campaign.TemplatePool()
	templates := make([]*model.CampaignTemplate, 0, len(pool))
	for _, id := range pool {
		t, err := c.CampaignTemplateService.CampaignTemplateRepository.GetByID(
			ctx,
			&id,
			&repository.CampaignTemplateOption{},
		)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, validate.WrapErrorWithField(
				fmt.Errorf("template %s not found", id.String()),
				"templateIDs",
			)
		}
		if err != nil {
			return nil, errs.Wrap(err)
		}
		if usable, err := t.IsUsable.Get(); err != nil || !usable {
			return nil, validate.WrapErrorWithField(
				fmt.Errorf("template '%s' is not usable", templateName(t)),
				"templateIDs",
			)
		}
		templates = append(templates, t)
	}
	firstMode, firstAlgo, firstLength, firstTraining := lureSettingsOf(templates[0])
	for _, t := range templates[1:] {
		mode, algo, length, training := lureSettingsOf(t)
		field := ""
		switch {
		case mode != firstMode:
			field = "lure URL mode"
		case algo != firstAlgo:
			field = "lure code algorithm"
		case length != firstLength:
			field = "lure code length"
		case training != firstTraining:
			field = "training setting"
		}
		if field != "" {
			return nil, validate.WrapErrorWithField(
				fmt.Errorf(
					"all templates must have the same %s, '%s' differs from '%s'",
					field,
					templateName(t),
					templateName(templates[0]),
				),
				"templateIDs",
			)
		}
	}
	return templates, nil
}

func templateName(t *model.CampaignTemplate) string {
	if v, err := t.Name.Get(); err == nil {
		return v.String()
	}
	if v, err := t.ID.Get(); err == nil {
		return v.String()
	}
	return ""
}

// attachTemplatePools sets Templates and TemplateIDs on each campaign from its
// pool, in one query
func (c *Campaign) attachTemplatePools(ctx context.Context, campaigns ...*model.Campaign) error {
	ids := make([]*uuid.UUID, 0, len(campaigns))
	for _, campaign := range campaigns {
		if campaign == nil {
			continue
		}
		if id, err := campaign.ID.Get(); err == nil {
			ids = append(ids, &id)
		}
	}
	pools, err := c.CampaignRepository.GetTemplatePools(ctx, ids)
	if err != nil {
		return errs.Wrap(err)
	}
	for _, campaign := range campaigns {
		if campaign == nil {
			continue
		}
		id, err := campaign.ID.Get()
		if err != nil {
			continue
		}
		refs := pools[id]
		if len(refs) == 0 {
			// a campaign whose template was deleted has an empty pool
			refs = []*model.CampaignTemplateRef{}
		}
		campaign.Templates = refs
		templateIDs := make([]*uuid.UUID, 0, len(refs))
		for _, ref := range refs {
			tid := ref.ID
			templateIDs = append(templateIDs, &tid)
		}
		campaign.TemplateIDs = nullable.NewNullableWithValue(templateIDs)
	}
	return nil
}

// attachRecipientTemplates sets Template on each listed recipient. The names come
// from the campaign's pool, plus one query for templates no longer in it.
func (c *Campaign) attachRecipientTemplates(
	ctx context.Context,
	campaign *model.Campaign,
	rows []*model.CampaignRecipient,
) error {
	names := map[uuid.UUID]string{}
	for _, ref := range campaign.Templates {
		names[ref.ID] = ref.Name
	}
	missing := []*uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for _, row := range rows {
		id, err := row.TemplateID.Get()
		if err != nil {
			continue
		}
		if _, ok := names[id]; ok || seen[id] {
			continue
		}
		seen[id] = true
		missing = append(missing, &id)
	}
	if len(missing) > 0 {
		found, err := c.CampaignRepository.GetTemplateNamesByIDs(ctx, missing)
		if err != nil {
			return errs.Wrap(err)
		}
		for id, name := range found {
			names[id] = name
		}
	}
	for _, row := range rows {
		id, err := row.TemplateID.Get()
		if err != nil {
			continue
		}
		name, ok := names[id]
		if !ok {
			name = "(deleted template)"
		}
		row.Template = &model.CampaignTemplateRef{ID: id, Name: name}
	}
	return nil
}

// planTemplateMoves works out how a pool change applies to the recipients of a
// self managed campaign. It only reads; the result is written together with the
// new pool. A recipient whose template is still in the new pool keeps it. A
// recipient already sent to, or with any event, keeps the template it got even
// when that template was removed, so its delivered links and its results stay
// with the email it received. Any other recipient on a removed template is given
// a new pool template, the least used first with recipients sharing the
// "Balance per" value taken together, so totals stay balanced. Rows with no
// template count as the campaign's previous first template; the pool write
// stamps them with it in one statement before any move, so none resolves
// through the campaign row afterwards. With keepSent false everyone on a removed
// template moves; an anonymous self managed campaign uses this, since it may
// hold only one template. Returns the stamps, which give a recipient a template
// whether or not it was sent to, and the moves, which only apply while it was
// not.
func (c *Campaign) planTemplateMoves(
	ctx context.Context,
	campaignID *uuid.UUID,
	previousTemplateID *uuid.UUID,
	newPool []uuid.UUID,
	mixBy string,
	keepSent bool,
) (stamps map[uuid.UUID][]uuid.UUID, moves map[uuid.UUID][]uuid.UUID, err error) {
	if len(newPool) == 0 {
		return nil, nil, nil
	}
	rows, err := c.CampaignRecipientRepository.GetTemplateAssignments(ctx, campaignID)
	if err != nil {
		return nil, nil, errs.Wrap(err)
	}
	inPool := make(map[uuid.UUID]bool, len(newPool))
	for _, id := range newPool {
		inPool[id] = true
	}
	counts := map[uuid.UUID]int{}
	perGroup := map[string]map[uuid.UUID]int{}
	groupOf := func(key string) map[uuid.UUID]int {
		if perGroup[key] == nil {
			perGroup[key] = map[uuid.UUID]int{}
		}
		return perGroup[key]
	}
	stamps = map[uuid.UUID][]uuid.UUID{}
	moves = map[uuid.UUID][]uuid.UUID{}
	movers := []repository.TemplateAssignment{}
	for _, row := range rows {
		effective := row.TemplateID
		if effective == nil {
			effective = previousTemplateID
		}
		if effective != nil && (inPool[*effective] || (keepSent && row.Sent)) {
			if inPool[*effective] {
				counts[*effective]++
				groupOf(assignmentMixValue(row, mixBy))[*effective]++
			}
			continue
		}
		movers = append(movers, row)
	}
	keys := make([]string, len(movers))
	for i, row := range movers {
		keys[i] = assignmentMixValue(row, mixBy)
	}
	rng := c.templateRand()
	for _, i := range groupByKey(keys, rng) {
		pick := pickForGroup(groupOf(keys[i]), counts, newPool, rng)
		if keepSent {
			moves[pick] = append(moves[pick], movers[i].ID)
		} else {
			stamps[pick] = append(stamps[pick], movers[i].ID)
		}
	}
	return stamps, moves, nil
}

// checkPoolKeepsTraining refuses a new pool whose training setting differs from
// the campaign's once the campaign has recipients. The setting is copied from the
// template only before the first recipient, so a change after that would not
// apply to the people already in the campaign.
func (c *Campaign) checkPoolKeepsTraining(
	ctx context.Context,
	campaignID *uuid.UUID,
	campaign *model.Campaign,
	first *model.CampaignTemplate,
) error {
	stored, err := campaign.IsTraining.Get()
	if err != nil {
		return nil
	}
	_, _, _, training := lureSettingsOf(first)
	if training == stored {
		return nil
	}
	hasRecipients, err := c.CampaignRecipientRepository.HasRecipientsByCampaignID(ctx, campaignID)
	if err != nil {
		return errs.Wrap(err)
	}
	if !hasRecipients {
		return nil
	}
	return validate.WrapErrorWithField(
		fmt.Errorf(
			"the campaign already has recipients, so its templates must keep the same training setting, '%s' differs",
			templateName(first),
		),
		"templateIDs",
	)
}

// uuidStrings returns the ids as strings for audit details
func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
