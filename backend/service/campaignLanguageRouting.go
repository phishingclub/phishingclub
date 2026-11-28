package service

import (
	"context"
	"errors"
	"maps"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// mergeLanguageRouting copies the language fields an update sets onto the
// stored campaign and audits the change. Routes left out are kept, unless the
// update changes the field they are keyed on, then they are cleared. The route
// keys are recipient values, so only their count is logged. It reports
// whether the field, the default or the routes changed.
func mergeLanguageRouting(
	ctx context.Context,
	campaigns *repository.Campaign,
	id *uuid.UUID,
	current *model.Campaign,
	incoming *model.Campaign,
	ae *AuditEvent,
) (bool, error) {
	if !incoming.LanguageRouteBy.IsSpecified() && !incoming.LanguageDefault.IsSpecified() && !incoming.LanguageRoutes.IsSpecified() {
		return false, nil
	}
	previousRouteBy := current.LanguageRouteField()
	previousDefault := ""
	if v, err := current.LanguageDefault.Get(); err == nil {
		previousDefault = v
	}
	previousRoutes, err := campaigns.GetLanguageRoutesByCampaignID(ctx, id)
	if err != nil {
		return false, err
	}
	if incoming.LanguageRouteBy.IsSpecified() {
		current.LanguageRouteBy = nullable.NewNullableWithValue(incoming.LanguageRouteField())
	}
	if incoming.LanguageDefault.IsSpecified() {
		v, _ := incoming.LanguageDefault.Get()
		current.LanguageDefault = nullable.NewNullableWithValue(v)
	}
	if incoming.LanguageRoutes.IsSpecified() {
		current.LanguageRoutes = incoming.LanguageRoutes
	} else if current.LanguageRouteField() != previousRouteBy {
		// the stored routes are keyed on values of the old field
		current.LanguageRoutes = nullable.NewNullableWithValue(map[string]string{})
		if len(previousRoutes) > 0 {
			ae.Details["languageRoutesCleared"] = len(previousRoutes)
		}
	}
	if err := current.ValidateLanguageRouting(); err != nil {
		return false, err
	}
	newDefault := ""
	if v, err := current.LanguageDefault.Get(); err == nil {
		newDefault = v
	}
	// routes equal to the stored ones are not written again
	routesChanged := false
	if current.LanguageRoutes.IsSpecified() {
		routes, _ := current.LanguageRoutes.Get()
		if maps.Equal(routes, previousRoutes) {
			current.LanguageRoutes = nullable.Nullable[map[string]string]{}
		} else {
			routesChanged = true
		}
	}
	routeCount := len(previousRoutes)
	if current.LanguageRoutes.IsSpecified() {
		routeCount = model.LanguageRouteCount(current.LanguageRoutes)
	}
	changed := previousRouteBy != current.LanguageRouteField() || previousDefault != newDefault
	if routesChanged && current.LanguageRouteField() != "" {
		changed = true
	}
	if previousRouteBy != current.LanguageRouteField() || previousDefault != newDefault || routesChanged {
		ae.Details["previousLanguageRouteBy"] = previousRouteBy
		ae.Details["languageRouteBy"] = current.LanguageRouteField()
		ae.Details["previousLanguageDefault"] = previousDefault
		ae.Details["languageDefault"] = newDefault
		ae.Details["previousLanguageRouteCount"] = len(previousRoutes)
		ae.Details["languageRouteCount"] = routeCount
	}
	return changed, nil
}

// anonymousRoutingLockedText is the refusal of a language routing change on
// an anonymous campaign that has recipients
const anonymousRoutingLockedText = "Language by, the default language and the language routes can not be changed on an anonymous campaign once it has recipients"

// refuseAnonymousRoutingChange refuses a change of Language by, the default
// language or the routes on an anonymous campaign whose rows are kept. Events
// of an anonymous campaign carry a pseudonym, so a restamp could not tell a
// row that has events from one that has none, and whether a row moved to
// another language must not show whether that person did anything. So the
// rows keep their language and routing is locked. A self managed campaign
// keeps its rows once it has any. A scheduled campaign rebuilds all its rows
// on update, so it is locked only once a row was sent or claimed or an event
// was saved. Fails closed: when the anonymity flag can not be read the
// campaign counts as anonymous, and a failed read refuses the change.
func (c *Campaign) refuseAnonymousRoutingChange(
	ctx context.Context,
	id *uuid.UUID,
	campaign *model.Campaign,
) error {
	if !campaignAnonymous(campaign) {
		return nil
	}
	locked := false
	var err error
	if campaign.IsSelfManaged() {
		locked, err = c.CampaignRecipientRepository.HasRecipientsByCampaignID(ctx, id)
	} else {
		locked, err = c.CampaignRecipientRepository.HasSendActivityByCampaignID(ctx, id)
	}
	if err != nil {
		c.Logger.Errorw("failed to check recipients before changing language routing", "error", err)
		return errs.Wrap(err)
	}
	if locked {
		return validate.WrapErrorWithField(errors.New(anonymousRoutingLockedText), "languageRouteBy")
	}
	return nil
}

// updateRestampingUnsentLanguages writes the campaign like
// CampaignRepository.UpdateByID and, in the same transaction, gives the rows
// of a self managed campaign that are not sent yet the language its routing
// picks now, see updateWithPoolAndRestamp. It returns how many rows changed.
func (c *Campaign) updateRestampingUnsentLanguages(
	ctx context.Context,
	id *uuid.UUID,
	campaign *model.Campaign,
	pool []uuid.UUID,
) (int64, error) {
	_, restamped, err := c.updateWithPoolAndRestamp(ctx, id, campaign, pool, nil, true)
	return restamped, err
}

// updateWithPoolAndRestamp writes the campaign like
// CampaignRepository.UpdateByID. When plan is set, it plans a new template
// pool and its recipient moves and writes them in the same transaction. With
// restamp the rows of a self managed campaign that are not sent yet get the
// language its routing picks now, also in that transaction. Only rows that
// have no event, are not cancelled and are not claimed by a sender are
// restamped, so a row keeps the language it was sent in. When any part fails
// nothing is written, so the stored pool and routing never differ from the
// templates and stamps they gave. The template lock is held from the plan to
// the commit. Rows of an anonymous campaign are never restamped, see
// refuseAnonymousRoutingChange. It returns the written pool change, if any,
// and how many rows were restamped.
func (c *Campaign) updateWithPoolAndRestamp(
	ctx context.Context,
	id *uuid.UUID,
	campaign *model.Campaign,
	pool []uuid.UUID,
	plan func() (*poolChange, error),
	restamp bool,
) (*poolChange, int64, error) {
	restamp = restamp && !campaignAnonymous(campaign)
	if plan == nil && !restamp {
		return nil, 0, c.CampaignRepository.UpdateByID(ctx, id, campaign)
	}
	// a manual send reads and sends a row under the same lock, so a row is
	// never sent with a template or language other than the stored ones
	unlock := c.lockCampaignTemplates(*id)
	defer unlock()
	var change *poolChange
	if plan != nil {
		var err error
		if change, err = plan(); err != nil {
			return nil, 0, err
		}
	}
	// read before the write: routes left unset on the campaign equal the
	// stored ones
	var languages *campaignLanguages
	if restamp {
		var err error
		if languages, err = c.campaignLanguagesFor(ctx, campaign, pool); err != nil {
			return nil, 0, err
		}
		// the emails of the languages the unsent rows can get. The rows are
		// read again in the write below, a recipient's language does not
		// change with a move
		if languages.routeBy == model.LanguageRouteLanguage {
			rows, err := c.CampaignRecipientRepository.GetTemplateAssignments(ctx, id)
			if err != nil {
				return nil, 0, err
			}
			raws := []string{}
			for _, row := range rows {
				if row.Restampable {
					raws = append(raws, row.RecipientLanguage)
				}
			}
			if err := c.checkStampEmails(ctx, languages, raws); err != nil {
				return nil, 0, err
			}
		}
	}
	var first *uuid.UUID
	if v, err := campaign.TemplateID.Get(); err == nil {
		first = &v
	}
	var restamped int64
	err := c.CampaignRepository.UpdateByIDWith(ctx, id, campaign, func(tx *gorm.DB) error {
		if change != nil {
			if err := change.writeTx(tx, id); err != nil {
				return err
			}
		}
		if languages == nil {
			return nil
		}
		// the rows are read after the moves, so a moved row is stamped by its
		// new template
		txRepo := &repository.CampaignRecipient{DB: tx}
		rows, err := txRepo.GetTemplateAssignments(ctx, id)
		if err != nil {
			return err
		}
		stamps := map[uuid.UUID]*string{}
		for _, row := range rows {
			if !row.Restampable {
				continue
			}
			templateID := row.TemplateID
			if templateID == nil {
				templateID = first
			}
			if templateID == nil {
				continue
			}
			var code *string
			if v, err := languages.languageForValue(assignmentRouteValue(row, languages.routeBy), *templateID).Get(); err == nil {
				code = &v
			}
			if (code == nil && row.Language == nil) || (code != nil && row.Language != nil && *code == *row.Language) {
				continue
			}
			stamps[row.ID] = code
		}
		restamped, err = txRepo.RestampUnsentLanguages(ctx, id, stamps)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return change, restamped, nil
}
