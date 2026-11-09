package service

import (
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
)

func poolOf(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

// TestAssignTemplatesBalancedAndMixed checks totals per template differ by at
// most one and every department large enough gets every template
func TestAssignTemplatesBalancedAndMixed(t *testing.T) {
	sizes := map[string]int{"sales": 40, "it": 31, "hr": 20, "": 9}
	departments := []string{}
	for d, n := range sizes {
		for i := 0; i < n; i++ {
			departments = append(departments, d)
		}
	}
	for seed := uint64(1); seed <= 20; seed++ {
		pool := poolOf(3)
		got := assignTemplates(departments, pool, rand.New(rand.NewPCG(seed, seed)))
		totals := map[uuid.UUID]int{}
		perDept := map[string]map[uuid.UUID]int{}
		for i, id := range got {
			totals[id]++
			if perDept[departments[i]] == nil {
				perDept[departments[i]] = map[uuid.UUID]int{}
			}
			perDept[departments[i]][id]++
		}
		if len(totals) != 3 {
			t.Fatalf("seed %d: want 3 templates used, got %d", seed, len(totals))
		}
		min, max := len(departments), 0
		for _, n := range totals {
			if n < min {
				min = n
			}
			if n > max {
				max = n
			}
		}
		if max-min > 1 {
			t.Fatalf("seed %d: totals not balanced %v", seed, totals)
		}
		for d, counts := range perDept {
			if len(counts) != 3 {
				t.Fatalf("seed %d: department %q got %d templates, want 3", seed, d, len(counts))
			}
			// inside a department the spread is also within one
			lo, hi := sizes[d], 0
			for _, n := range counts {
				if n < lo {
					lo = n
				}
				if n > hi {
					hi = n
				}
			}
			if hi-lo > 1 {
				t.Fatalf("seed %d: department %q not mixed evenly %v", seed, d, counts)
			}
		}
	}
}

// TestAssignTemplatesAlternatesInSendOrder checks recipients sent one after
// another get different templates, with no field to balance on and within each
// group when there is one
func TestAssignTemplatesAlternatesInSendOrder(t *testing.T) {
	for seed := uint64(1); seed <= 20; seed++ {
		rng := rand.New(rand.NewPCG(seed, seed))
		// no value: every run of len(pool) sends holds each template once
		pool := poolOf(3)
		got := assignTemplates(make([]string, 30), pool, rng)
		for i := 0; i+3 <= len(got); i++ {
			if got[i] == got[i+1] || got[i] == got[i+2] || got[i+1] == got[i+2] {
				t.Fatalf("seed %d: sends %d to %d repeat a template", seed, i, i+2)
			}
		}
		// two groups in mixed send order: each group alternates on its own
		keys := []string{}
		for i := 0; i < 20; i++ {
			if i%3 == 0 {
				keys = append(keys, "sales")
			} else {
				keys = append(keys, "it")
			}
		}
		pool2 := poolOf(2)
		got2 := assignTemplates(keys, pool2, rng)
		last := map[string]uuid.UUID{}
		for i, k := range keys {
			if prev, ok := last[k]; ok && prev == got2[i] {
				t.Fatalf("seed %d: two %s recipients in a row got the same template", seed, k)
			}
			last[k] = got2[i]
		}
		// interleaved groups whose sizes are a multiple of the pool size must
		// not line up on the same template
		inter := []string{}
		for i := 0; i < 9; i++ {
			inter = append(inter, []string{"a", "b", "c"}[i%3])
		}
		got3 := assignTemplates(inter, pool, rng)
		repeats := 0
		for i := 1; i < len(got3); i++ {
			if got3[i] == got3[i-1] {
				repeats++
			}
		}
		if repeats > 0 {
			t.Fatalf("seed %d: interleaved groups gave %d repeated neighbours", seed, repeats)
		}
	}
}

// TestAssignTemplatesStartVaries checks the first recipient does not always get
// the first template
func TestAssignTemplatesStartVaries(t *testing.T) {
	pool := poolOf(2)
	firsts := map[uuid.UUID]bool{}
	for seed := uint64(1); seed <= 20; seed++ {
		firsts[assignTemplates(make([]string, 4), pool, rand.New(rand.NewPCG(seed, seed)))[0]] = true
	}
	if len(firsts) != 2 {
		t.Fatal("the first recipient always gets the same template")
	}
}

func TestAssignTemplatesSinglePool(t *testing.T) {
	pool := poolOf(1)
	got := assignTemplates([]string{"a", "b", ""}, pool, rand.New(rand.NewPCG(1, 1)))
	for i, id := range got {
		if id != pool[0] {
			t.Fatalf("recipient %d: got %s, want the only template", i, id)
		}
	}
}

// TestPickLeastUsed checks a new recipient gets the least used template and a
// run of picks stays balanced
func TestPickLeastUsed(t *testing.T) {
	pool := poolOf(3)
	rng := rand.New(rand.NewPCG(3, 3))
	counts := map[uuid.UUID]int{pool[0]: 5, pool[1]: 2, pool[2]: 4}
	if got := pickLeastUsed(counts, pool, rng); got != pool[1] {
		t.Fatalf("got %s, want least used", got)
	}
	if counts[pool[1]] != 3 {
		t.Fatalf("pick not counted: %v", counts)
	}
	for i := 0; i < 20; i++ {
		pickLeastUsed(counts, pool, rng)
	}
	min, max := 1000, 0
	for _, n := range counts {
		if n < min {
			min = n
		}
		if n > max {
			max = n
		}
	}
	if max-min > 1 {
		t.Fatalf("picks not balanced: %v", counts)
	}
	// ties are broken at random, not always the first
	seen := map[uuid.UUID]bool{}
	for seed := uint64(0); seed < 30; seed++ {
		even := map[uuid.UUID]int{}
		seen[pickLeastUsed(even, pool, rand.New(rand.NewPCG(seed, seed+1)))] = true
	}
	if len(seen) < 2 {
		t.Fatalf("ties always broken the same way: %v", seen)
	}
}

// TestSendTemplateGroupsContinuesAfterError checks a failing template does not
// stop the next one, and a template that closed the campaign stops the rest
func TestSendTemplateGroupsContinuesAfterError(t *testing.T) {
	c := &Campaign{Common: Common{Logger: zap.NewNop().Sugar()}}
	order := poolOf(3)
	campaign := &model.Campaign{}
	tried := []uuid.UUID{}
	err := c.sendTemplateGroups(campaign, order, func(id uuid.UUID) error {
		tried = append(tried, id)
		if id == order[0] {
			return errors.New("broken template")
		}
		return nil
	})
	if err == nil || len(tried) != 3 {
		t.Fatalf("want an error and all 3 groups tried, got %v and %d", err, len(tried))
	}

	tried = tried[:0]
	err = c.sendTemplateGroups(campaign, order, func(id uuid.UUID) error {
		tried = append(tried, id)
		// the first template closes the campaign
		campaign.ClosedAt = nullable.NewNullableWithValue(time.Now().Add(-time.Second))
		return nil
	})
	if err != nil || len(tried) != 1 {
		t.Fatalf("closed campaign should stop after one group, got %v and %d", err, len(tried))
	}
}

// TestRecipientMixValue checks each mix field reads the matching recipient
// field and none or an unknown field gives one shared empty key
func TestRecipientMixValue(t *testing.T) {
	r := &model.Recipient{}
	set := func(f *nullable.Nullable[vo.OptionalString127], v string) {
		s, err := vo.NewOptionalString127(v)
		if err != nil {
			t.Fatalf("value %q: %v", v, err)
		}
		f.Set(*s)
	}
	set(&r.Department, "sales")
	set(&r.Position, "manager")
	set(&r.City, "aarhus")
	set(&r.Country, "dk")
	set(&r.Misc, "team a")
	want := map[string]string{
		model.TemplateMixDepartment: "sales",
		model.TemplateMixPosition:   "manager",
		model.TemplateMixCity:       "aarhus",
		model.TemplateMixCountry:    "dk",
		model.TemplateMixMisc:       "team a",
		model.TemplateMixNone:       "",
		"email":                     "",
	}
	for mixBy, w := range want {
		if got := recipientMixValue(r, mixBy); got != w {
			t.Fatalf("mix by %q: got %q, want %q", mixBy, got, w)
		}
	}
	row := repository.TemplateAssignment{Department: "sales", Position: "manager", City: "aarhus", Country: "dk", Misc: "team a"}
	for mixBy, w := range want {
		if got := assignmentMixValue(row, mixBy); got != w {
			t.Fatalf("stored row, mix by %q: got %q, want %q", mixBy, got, w)
		}
	}
}

// TestTemplateMixByAllowlist checks only the listed fields are accepted and an
// empty value reads as department
func TestTemplateMixByAllowlist(t *testing.T) {
	for _, v := range []string{"", "department", "position", "city", "country", "misc", "none"} {
		if !model.IsValidTemplateMixBy(v) {
			t.Fatalf("%q refused", v)
		}
	}
	for _, v := range []string{"email", "Department", "name;--", "first_name"} {
		c := &model.Campaign{}
		c.TemplateMixBy.Set(v)
		if err := c.ValidateTemplateMixBy(); err == nil {
			t.Fatalf("%q accepted", v)
		}
	}
	if got := (&model.Campaign{}).TemplateMixField(); got != model.TemplateMixDepartment {
		t.Fatalf("default mix field %q", got)
	}
}

// TestAssignTemplatesRandomGroups checks, over many random mixes of group sizes
// and send orders, that every group stays within one of even, totals stay within
// one, and recipients scheduled one after another rarely share a template
func TestAssignTemplatesRandomGroups(t *testing.T) {
	sends, repeats := 0, 0
	for seed := uint64(1); seed <= 200; seed++ {
		rng := rand.New(rand.NewPCG(seed, seed*7))
		pool := poolOf(2 + rng.IntN(3))
		keys := []string{}
		for g := 0; g < 1+rng.IntN(5); g++ {
			for n := 1 + rng.IntN(30); n > 0; n-- {
				keys = append(keys, string(rune('a'+g)))
			}
		}
		rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
		got := assignTemplates(keys, pool, rng)
		totals := map[uuid.UUID]int{}
		perGroup := map[string]map[uuid.UUID]int{}
		for i, id := range got {
			totals[id]++
			if perGroup[keys[i]] == nil {
				perGroup[keys[i]] = map[uuid.UUID]int{}
			}
			perGroup[keys[i]][id]++
			if i > 0 {
				sends++
				if got[i] == got[i-1] {
					repeats++
				}
			}
		}
		spread := func(counts map[uuid.UUID]int) int {
			lo, hi := -1, 0
			for _, id := range pool {
				n := counts[id]
				if lo == -1 || n < lo {
					lo = n
				}
				if n > hi {
					hi = n
				}
			}
			return hi - lo
		}
		for g, counts := range perGroup {
			if spread(counts) > 1 {
				t.Fatalf("seed %d: group %s not even %v", seed, g, counts)
			}
			// inside a group, only the repeats its shares force are allowed
			size, largest := 0, 0
			for _, n := range counts {
				size += n
				largest = max(largest, n)
			}
			var last uuid.UUID
			seen, groupRepeats := false, 0
			for i, k := range keys {
				if k != g {
					continue
				}
				if seen && got[i] == last {
					groupRepeats++
				}
				last, seen = got[i], true
			}
			if least := max(0, 2*largest-size-1); groupRepeats != least {
				t.Fatalf("seed %d: group %s has %d repeats, %d are needed", seed, g, groupRepeats, least)
			}
		}
		if spread(totals) > 1 {
			t.Fatalf("seed %d: totals not even %v", seed, totals)
		}
	}
	if rate := float64(repeats) / float64(sends); rate > 0.15 {
		t.Fatalf("back to back repeats %.2f, want at most 0.15", rate)
	} else {
		t.Logf("back to back repeats %.3f over %d sends", rate, sends)
	}
}

// BenchmarkAssignTemplates measures a large campaign: 30k recipients, 50
// templates, 20 groups
func BenchmarkAssignTemplates(b *testing.B) {
	pool := poolOf(50)
	keys := make([]string, 30000)
	for i := range keys {
		keys[i] = string(rune('a' + i%20))
	}
	rng := rand.New(rand.NewPCG(1, 1))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		assignTemplates(keys, pool, rng)
	}
}
