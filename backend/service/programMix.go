package service

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// programMix is what the advance batches of one tick know about the template
// mixing of one program: the mix value of each person in a batch and how many
// sends of each step people with that value have around now. The picks made
// in a batch are added as they are made, so later people in the same batch see
// them. The sends are read once per tick: after a batch is stored its picks
// are kept only when they were stored and fall in the window, which is what a
// new read would find.
type programMix struct {
	values map[uuid.UUID]string
	counts repository.ProgramMixCounts
	mixBy  string
	from   time.Time
	to     time.Time
	picks  []programMixPick
}

// programMixPick is one step picked for a person in the current batch
type programMixPick struct {
	enrollmentID uuid.UUID
	value        string
	stepID       uuid.UUID
	at           time.Time
}

// programMixWindow returns the sends that count as colleagues getting a
// template around the same time: those planned or sent from half the time
// between templates ago up to the end of the spread ahead. Half the interval
// back holds every send of the current round when the spread is at most half
// the interval, and never one of the round before it, as a person is sent at
// most once in that time. The spread ahead holds the sends of this round that
// are planned but not sent yet, for example after a resume.
func programMixWindow(program *model.Program, now time.Time) (time.Time, time.Time) {
	return now.Add(-program.IntervalOrDefault() / 2), now.Add(program.SpreadOrDefault())
}

// programMixFor returns the mixing state of a program for the people of one
// batch. cached is the state of an earlier batch of the same tick, nil when
// there is none. It is used again when the program still mixes by the same
// field over the same window, and only the values of the new people are read.
// Otherwise the sends are read again. It returns nil when the program does not
// mix templates.
func (s *Program) programMixFor(
	ctx context.Context,
	cached *programMix,
	program *model.Program,
	due []*database.ProgramEnrollment,
	now time.Time,
) (*programMix, error) {
	mixBy := program.TemplateMixInEffect()
	if mixBy == "" {
		return nil, nil
	}
	programID := program.ID.MustGet()
	from, to := programMixWindow(program, now)
	mix := cached
	if mix == nil || mix.mixBy != mixBy || !mix.from.Equal(from) || !mix.to.Equal(to) {
		counts, err := s.ProgramRepository.GetMixCounts(ctx, &programID, mixBy, from, to)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		mix = &programMix{values: map[uuid.UUID]string{}, counts: counts, mixBy: mixBy, from: from, to: to}
	}
	recipientIDs := []*uuid.UUID{}
	for _, e := range due {
		if e.ProgramID != nil && *e.ProgramID == programID && e.RecipientID != nil {
			if _, ok := mix.values[*e.RecipientID]; !ok {
				recipientIDs = append(recipientIDs, e.RecipientID)
			}
		}
	}
	values, err := s.ProgramRepository.GetRecipientMixValues(ctx, recipientIDs, mixBy)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for id, v := range values {
		mix.values[id] = v
	}
	return mix, nil
}

// countsFor returns the send counts per step of the people who share the
// person's mix value. It returns nil on a nil mix.
func (m *programMix) countsFor(recipientID *uuid.UUID) map[uuid.UUID]int {
	if m == nil || recipientID == nil {
		return nil
	}
	value := m.values[*recipientID]
	if m.counts[value] == nil {
		m.counts[value] = map[uuid.UUID]int{}
	}
	return m.counts[value]
}

// add counts a step picked for a person in this batch
func (m *programMix) add(recipientID *uuid.UUID, stepID uuid.UUID) {
	if counts := m.countsFor(recipientID); counts != nil {
		counts[stepID]++
	}
}

// record counts a step picked for a person in this batch, see add, and keeps
// the pick until settle
func (m *programMix) record(enrollmentID *uuid.UUID, recipientID *uuid.UUID, stepID uuid.UUID, at time.Time) {
	if m == nil || recipientID == nil || enrollmentID == nil {
		return
	}
	m.add(recipientID, stepID)
	m.picks = append(m.picks, programMixPick{
		enrollmentID: *enrollmentID,
		value:        m.values[*recipientID],
		stepID:       stepID,
		at:           at,
	})
}

// settle ends a batch: a pick that was not stored, or whose send falls outside
// the window, is taken back out of the counts, so the next batch of the tick
// sees what a new read of the sends would find
func (m *programMix) settle(failed map[uuid.UUID]bool) {
	if m == nil {
		return
	}
	for _, p := range m.picks {
		if failed[p.enrollmentID] || p.at.Before(m.from) || p.at.After(m.to) {
			m.counts[p.value][p.stepID]--
		}
	}
	m.picks = nil
}

// programPickStepMixed is programPickStep that, in random order with counts,
// picks among the candidates sent least to the person's colleagues, at random
// among those. Without counts or in sequential order it is programPickStep.
func programPickStepMixed(
	program *model.Program,
	candidates []*model.ProgramStep,
	rng *rand.Rand,
	counts map[uuid.UUID]int,
) *model.ProgramStep {
	if counts == nil || len(candidates) == 0 || program.OrderModeOrDefault() != model.ProgramOrderShuffle {
		return programPickStep(program, candidates, rng)
	}
	least := []*model.ProgramStep{}
	fewest := 0
	for _, c := range candidates {
		n := counts[c.ID.MustGet()]
		switch {
		case len(least) == 0 || n < fewest:
			least = []*model.ProgramStep{c}
			fewest = n
		case n == fewest:
			least = append(least, c)
		}
	}
	return least[rng.Intn(len(least))]
}
