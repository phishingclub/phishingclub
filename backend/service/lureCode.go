package service

import (
	"context"

	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/lure"
	"github.com/phishingclub/phishingclub/repository"
	"go.uber.org/zap"
)

// allocateRounds bounds the redraws when some drawn codes are already taken.
// Each round clears all but a vanishing fraction of a healthy space, so running
// out means the space is too small rather than the draw unlucky.
const allocateRounds = 3

// LureCode allocates the short identifiers used in lure URLs.
type LureCode struct {
	CampaignRecipientRepository *repository.CampaignRecipient
	Logger                      *zap.SugaredLogger
}

// AllocateBatch returns n distinct codes that are free at the time of the call.
//
// The batch is drawn up front and checked in chunked queries rather than
// inserting each code and retrying on conflict, so a ten thousand recipient
// campaign costs a couple of dozen queries.
//
// Running out of rounds is the only exhaustion signal. A capacity threshold
// would need a count over every live code and would still only estimate what
// the next draw hits.
func (l *LureCode) AllocateBatch(
	ctx context.Context,
	algorithm lure.Algorithm,
	length int,
	n int,
) ([]lure.Code, error) {
	if n <= 0 {
		return []lure.Code{}, nil
	}
	if !lure.IsValidAlgorithm(algorithm) {
		return nil, errs.NewValidationError(
			errors.Errorf("unknown lure code algorithm: %s", algorithm),
		)
	}
	if length < lure.MinLength || length > lure.MaxLength {
		return nil, errs.NewValidationError(
			errors.Errorf(
				"lure code length must be between %d and %d",
				lure.MinLength,
				lure.MaxLength,
			),
		)
	}
	// keyed on the code, the column uniqueness sits on, so internal duplicates
	// are rejected on the same terms the database will
	pool := make(map[string]lure.Code, n)
	for round := 0; round < allocateRounds; round++ {
		// the shortfall in one pass. this repeats only for codes the map rejected
		// as internal duplicates
		for len(pool) < n {
			codes, err := lure.GenerateBatch(algorithm, length, n-len(pool))
			if err != nil {
				return nil, errs.Wrap(err)
			}
			for _, code := range codes {
				pool[code.Display] = code
			}
		}
		candidates := make([]string, 0, len(pool))
		for display := range pool {
			candidates = append(candidates, display)
		}
		taken, err := l.CampaignRecipientRepository.FindTakenLureCodes(ctx, candidates)
		if err != nil {
			l.Logger.Errorw("failed to check taken lure codes", "error", err)
			return nil, errs.Wrap(err)
		}
		if len(taken) == 0 {
			codes := make([]lure.Code, 0, len(pool))
			for _, code := range pool {
				codes = append(codes, code)
			}
			return codes, nil
		}
		for _, display := range taken {
			delete(pool, display)
		}
	}
	return nil, errs.NewValidationError(
		errors.Errorf(
			"could not allocate %d unique lure codes at length %d, use a longer code length",
			n,
			length,
		),
	)
}
