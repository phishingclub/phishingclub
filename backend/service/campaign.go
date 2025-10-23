package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"

	go_errors "github.com/go-errors/errors"
	"gopkg.in/yaml.v3"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/build"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/log"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"github.com/wneessen/go-mail"
	"gorm.io/gorm"
)

// Campaign is the Campaign service
type Campaign struct {
	Common
	CampaignRepository          *repository.Campaign
	CampaignRecipientRepository *repository.CampaignRecipient
	RecipientRepository         *repository.Recipient
	RecipientGroupRepository    *repository.RecipientGroup
	AllowDenyRepository         *repository.AllowDeny
	WebhookRepository           *repository.Webhook
	CampaignTemplateService     *CampaignTemplate
	TemplateService             *Template
	DomainService               *Domain
	RecipientService            *Recipient
	MailService                 *Email
	APISenderService            *APISender
	SMTPConfigService           *SMTPConfiguration
	WebhookService              *Webhook
	AttachmentPath              string
}

// Create creates a new campaign
func (c *Campaign) Create(
	ctx context.Context,
	session *model.Session,
	campaign *model.Campaign,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("Campaign.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	if len(campaign.RecipientGroupIDs) == 0 {
		return nil, validate.WrapErrorWithField(errors.New("no groups provided"), "Recipient Groups")
	}
	// if the schedule type is scheduled, set the start time to start of day and end to the end of the last day
	if campaign.ConstraintWeekDays.IsSpecified() && !campaign.ConstraintWeekDays.IsNull() {
		if err := campaign.ValidateSendTimesSet(); err != nil {
			return nil, errs.Wrap(err)
		}
		if err := c.updateSchedulesCampaignStartAndEndDates(campaign); err != nil {
			return nil, errs.Wrap(err)
		}
	}
	// validate
	if err := campaign.Validate(); err != nil {
		return nil, errs.Wrap(err)
	}
	// check the template is usable
	templateID := campaign.TemplateID.MustGet()
	cTemplate, err := c.CampaignTemplateService.GetByID(
		ctx,
		session,
		&templateID,
		&repository.CampaignTemplateOption{
			UsableOnly: true,
		},
	)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if cTemplate == nil {
		return nil, errors.New("attempted to create campaign with unusable template")
	}
	// check uniqueness
	var companyID *uuid.UUID
	if cid, err := campaign.CompanyID.Get(); err == nil {
		companyID = &cid
	}
	name := campaign.Name.MustGet()
	isOK, err := repository.CheckNameIsUnique(
		ctx,
		c.CampaignRepository.DB,
		"campaigns",
		name.String(),
		companyID,
		nil,
	)
	if err != nil {
		c.Logger.Errorw("failed to check campaign uniqueness", "error", err)
		return nil, errs.Wrap(err)
	}
	if !isOK {
		c.Logger.Debugw("campaign name is already taken", "error", name.String())
		return nil, validate.WrapErrorWithField(errors.New("is not unique"), "name")
	}
	// validate allow deny list selections
	if err := c.validateAllowDenyIsSameTypeByIDs(ctx, campaign); err != nil {
		return nil, errs.Wrap(err)
	}
	// check there is atleast one valid group
	// and remove any empty groups
	validGroups := []*uuid.UUID{}
	for _, groupID := range campaign.RecipientGroupIDs.MustGet() {
		count, err := c.RecipientGroupRepository.GetRecipientCount(ctx, groupID)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		if count > 0 {
			validGroups = append(validGroups, groupID)
		}
	}
	if len(validGroups) == 0 {
		return nil, errs.NewValidationError(
			errors.New("Selected groups have no recipients"),
		)
	}
	campaign.RecipientGroupIDs.Set(validGroups)
	// save
	id, err := c.CampaignRepository.Insert(ctx, campaign)
	if err != nil {
		c.Logger.Errorw("failed to create campaign", "error", err)
		return nil, errs.Wrap(err)
	}
	createdCampaign, err := c.CampaignRepository.GetByID(
		ctx,
		id,
		&repository.CampaignOption{
			WithRecipientGroups: true,
		},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id", "error", err)
		return nil, errs.Wrap(err)
	}
	err = c.schedule(ctx, session, createdCampaign)
	if err != nil {
		c.Logger.Errorw("failed to schedule campaign", "error", err)
		// TODO we should delete the campaign as it was not scheduled
		return nil, errs.Wrap(err)
	}
	ae.Details["id"] = id.String()
	c.AuditLogAuthorized(ae)
	return id, nil
}

// schedule campaign schedules the campaign
// this is a service method that does not perform auth, use with consideration
func (c *Campaign) schedule(
	ctx context.Context,
	session *model.Session,
	campaign *model.Campaign,
) error {
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.Logger.Errorw("failed to create campaign", "error", err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		return errs.ErrAuthorizationFailed
	}

	// get all recipients and remove duplicates
	recipients := []*model.Recipient{}
	dubMap := map[string]bool{}
	if campaign.RecipientGroupIDs.IsSpecified() && !campaign.RecipientGroupIDs.IsNull() {
		for _, groupID := range campaign.RecipientGroupIDs.MustGet() {
			group, err := c.RecipientGroupRepository.GetByID(
				ctx,
				groupID,
				&repository.RecipientGroupOption{
					WithRecipients: true,
				},
			)
			if err != nil {
				c.Logger.Errorw("failed to get recipient group by id", "error", err)
				return errs.Wrap(err)
			}
			recps := group.Recipients
			if recps == nil {
				c.Logger.Error("recipient group did not load recipients")
				return errors.New("recipient group did not load recipients")
			}
			// collect all and remove duplicates
			for _, recp := range recps {
				id := recp.ID.MustGet().String()
				if _, ok := dubMap[id]; ok {
					continue
				}
				dubMap[id] = true
				recipients = append(recipients, recp)
			}
		}
	}
	// handle self managed campaign
	// if this is a self-managed campaign, we must not remove existing
	// campaign-recipients when rescheduling as this would give them new IDs
	// which would mean previous sent links will not work anymore.
	// instead we must only add new recipients and remove the ones that are no longer in the recipient groups
	if campaign.IsSelfManaged() {
		if err := campaign.ValidateNoSendTimesSet(); err != nil {
			return errs.Wrap(err)
		}
		// sort by email when self managed
		sort.Slice(recipients, func(a, b int) bool {
			if v, err := recipients[a].Email.Get(); err == nil {
				if v2, err := recipients[b].Email.Get(); err == nil {
					return strings.ToLower(v.String()) > strings.ToLower(v2.String())
				}
			}
			return false
		})
		campaignID := campaign.ID.MustGet()
		// remove campaign-recipients that are not supplied in a re-schedule
		recipientIDs := make([]*uuid.UUID, len(recipients))
		for i, recp := range recipients {
			rid := recp.ID.MustGet()
			recipientIDs[i] = &rid
		}
		// c.Logger.Debugw("keeping recpient IDs", recipientIDs)
		err := c.CampaignRecipientRepository.DeleteRecipientsNotIn(
			ctx,
			&campaignID,
			recipientIDs,
		)
		if err != nil {
			c.Logger.Errorw("failed to delete campaign recipients", "error", err)
			return errs.Wrap(err)
		}
		// insert campaign-recipients that are not already in the schedule
		campaignRecipients := make([]*model.CampaignRecipient, len(recipients))
		for i, recipient := range recipients {
			// check if already exists
			rid := recipient.ID.MustGet()
			_, err := c.CampaignRecipientRepository.GetByCampaignAndRecipientID(
				ctx,
				&campaignID,
				&rid,
				&repository.CampaignRecipientOption{},
			)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				c.Logger.Errorw("failed to get campaign recipient by campaign and recipient id",
					"error", err,
				)
				return errs.Wrap(err)
			}
			// if exists, skip it
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			recpID := nullable.NewNullableWithValue(recipient.ID.MustGet())
			campaignRecipients[i] = &model.CampaignRecipient{
				RecipientID: recpID,
				CampaignID:  nullable.NewNullableWithValue(campaignID),
				SelfManaged: nullable.NewNullableWithValue(true),
			}
			// save campaign-recipient
			_, err = c.CampaignRecipientRepository.Insert(ctx, campaignRecipients[i])
			if err != nil {
				c.Logger.Errorw("failed to create campaign", "error", err)
				return errs.Wrap(err)
			}
		}
		err = c.setMostNotableCampaignEvent(
			ctx,
			campaign,
			data.EVENT_CAMPAIGN_SELF_MANAGED,
		)
		if err != nil {
			// err is logged in method call
			return errs.Wrap(err)
		}

		return nil
	}
	if err := campaign.ValidateSendTimesSet(); err != nil {
		return errs.Wrap(err)
	}
	// set schedule with a even spread between startAt and end time
	startAt := campaign.SendStartAt.MustGet()
	endAt := campaign.SendEndAt.MustGet()
	recipientsCount := len(recipients)
	// no recipients, no schedule
	if recipientsCount == 0 {
		return fmt.Errorf("no recipients to schedule")
	}
	campaignRecipients := make([]*model.CampaignRecipient, recipientsCount)
	// sort the recipients by the selected sort field and order
	sortOrder := campaign.SortOrder.MustGet().String()
	sortField := campaign.SortField.MustGet().String()
	recipients = sortRecipients(recipients, sortOrder, sortField)
	// schedule the emails
	if recipientsCount == 0 {
		return fmt.Errorf("no recipients to schedule for '%s'", campaign.Name.MustGet())
	}
	scheduledEvent := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SCHEDULED]
	// handle single recipient
	if recipientsCount == 1 {
		recpID := nullable.NewNullableWithValue(recipients[0].ID.MustGet())
		campaignID := nullable.NewNullableWithValue(campaign.ID.MustGet())
		campaignRecipient := &model.CampaignRecipient{
			RecipientID:    recpID,
			CampaignID:     campaignID,
			SendAt:         nullable.NewNullableWithValue(startAt),
			NotableEventID: nullable.NewNullableWithValue(*scheduledEvent),
		}
		_, err := c.CampaignRecipientRepository.Insert(ctx, campaignRecipient)
		if err != nil {
			c.Logger.Errorw("failed to create campaign", "error", err)
			return errs.Wrap(err)
		}
		err = c.setMostNotableCampaignEvent(
			ctx,
			campaign,
			data.EVENT_CAMPAIGN_SCHEDULED,
		)
		if err != nil {
			// err is logged in method call
			return errs.Wrap(err)
		}
		return nil
	}
	// handle delivery schedule with specific week days
	// it is when there a start and end date, specific weekdays and times ranges to send in.
	if campaign.ConstraintWeekDays.IsSpecified() && !campaign.ConstraintWeekDays.IsNull() {
		c.Logger.Debugw("schedule campaign with contraints",
			"campaignName", campaign.Name.MustGet(),
		)
		campaignDuration := time.Duration(0)
		// first we calculate the diff, so we know the offset for we need to push between each in range moment
		currentDate := startAt
		toFind := campaign.ConstraintWeekDays.MustGet().AsSlice()
		// iterate over each day in the period
		for currentDate.Before(endAt) || currentDate.Equal(endAt) {
			// check if the current day is in the week days
			if slices.Contains(toFind, int(currentDate.Weekday())) {
				// calculate the number of minutes the campaigns spans on this week day
				dayStartTime := campaign.ConstraintStartTime.MustGet()
				dayEndTime := campaign.ConstraintEndTime.MustGet()
				diff := dayStartTime.DiffMinutes(dayEndTime)
				campaignDuration += diff

			}
			currentDate = currentDate.AddDate(0, 0, 1)
		}
		//  interval is the minutes between each recipient schedule
		//interval := int(campaignDuration.Minutes()) / (recipientsCount - 1) // -1 as the first send it placed at the start time
		interval := time.Duration(campaignDuration.Nanoseconds() / int64(recipientsCount-1))
		dayStartTime := campaign.ConstraintStartTime.MustGet()
		dayEndTime := campaign.ConstraintEndTime.MustGet()
		// schedule each recipient
		// iterate through the time again and progess on each interval until all recipients are set
		currentDate = startAt
		c.Logger.Debugw("campaign interval", "interval", interval)
		for currentDate.Before(endAt) || currentDate.Equal(endAt) {
			c.Logger.Debugw("schedule check date", "currentDate", currentDate)
			// check if the current day is in the week days
			if slices.Contains(toFind, int(currentDate.Weekday())) {
				c.Logger.Debugw("scheduling date", "currentDate", currentDate)
				// iterate over the hours in the day and jump each interval
				// if over the end time, break and skip to next day, saving the surplus of interval minutes
				// to be added to next send
				currentDayStart := currentDate.Truncate(24 * time.Hour).Add(dayStartTime.Minutes())
				currentDayEnd := currentDate.Truncate(24 * time.Hour).Add(dayEndTime.Minutes())
				for currentDayStart.Before(currentDayEnd) || currentDayStart.Equal(currentDayEnd) {
					c.Logger.Debugw("scheduling date at", "currentDayStart", currentDayStart)
					// check if we have any recipients left
					if len(recipients) == 0 {
						break
					}
					// get the next recipient
					recipient := recipients[0]
					recipients = recipients[1:]
					// save
					campaignRecipient := &model.CampaignRecipient{
						RecipientID:    recipient.ID,
						CampaignID:     campaign.ID,
						SendAt:         nullable.NewNullableWithValue(currentDayStart),
						NotableEventID: nullable.NewNullableWithValue(*scheduledEvent),
					}
					_, err := c.CampaignRecipientRepository.Insert(ctx, campaignRecipient)
					if err != nil {
						c.Logger.Errorw("failed to create campaign", "error", err)
						return errs.Wrap(err)
					}
					// check if we are over the end time
					currentDayStart = currentDayStart.Add(interval * time.Duration(1))

				}
			}
			// check the next day within the start and end date range
			currentDate = currentDate.AddDate(0, 0, 1)
		}
		err = c.setMostNotableCampaignEvent(
			ctx,
			campaign,
			data.EVENT_CAMPAIGN_SCHEDULED,
		)
		if err != nil {
			// err is logged in method call
			return errs.Wrap(err)
		}

		return nil
	}

	// handle basic delivery schedule
	// it is when there is no constraints, equal distribution between start and end datetime
	campaignDuration := endAt.Sub(startAt)
	// Calculate interval between emails
	// TODO make this work in minutes
	interval := time.Duration(campaignDuration.Nanoseconds() / int64(recipientsCount-1))
	for i, recipient := range recipients {
		sentAt := startAt
		if i > 0 {
			sa := campaignRecipients[i-1].SendAt.MustGet().Add(interval * time.Duration(1))
			sentAt = sa
		}
		// todo perhaps this array is unnecesssary
		//recpID := recipient.ID.MustGet()
		//campaignID := campaign.ID.MustGet()
		campaignRecipients[i] = &model.CampaignRecipient{
			RecipientID:    recipient.ID,
			CampaignID:     campaign.ID,
			SendAt:         nullable.NewNullableWithValue(sentAt),
			NotableEventID: nullable.NewNullableWithValue(*scheduledEvent),
		}
		// save
		_, err = c.CampaignRecipientRepository.Insert(ctx, campaignRecipients[i])
		if err != nil {
			c.Logger.Errorw("failed to create campaign", "error", err)
			return errs.Wrap(err)
		}
	}
	err = c.setMostNotableCampaignEvent(
		ctx,
		campaign,
		data.EVENT_CAMPAIGN_SCHEDULED,
	)
	if err != nil {
		// err is logged in method call
		return errs.Wrap(err)
	}
	return nil
}

// sortRecipients sorts the recipients by the selected sort field and order
func sortRecipients(recipients []*model.Recipient, sortOrder, sortField string) []*model.Recipient {
	switch sortOrder {
	case "random":
		sort.Slice(recipients, func(i, j int) bool {
			// return a random bool
			// #nosec
			return rand.Float32() < 0.5
		})
	case "desc":
		sort.Slice(recipients, func(a, b int) bool {
			// TODO implements the rest of the fields
			switch sortField {
			case "email":
				if v, err := recipients[a].Email.Get(); err == nil {
					if v2, err := recipients[b].Email.Get(); err == nil {
						return strings.ToLower(v.String()) > strings.ToLower(v2.String())
					}
				}
				return false
			case "first_name":
				if v, err := recipients[a].FirstName.Get(); err == nil {
					if v2, err := recipients[b].FirstName.Get(); err == nil {
						return strings.ToLower(v.String()) > strings.ToLower(v2.String())
					}
				}
				return false
			case "last_name":
				if v, err := recipients[a].LastName.Get(); err == nil {
					if v2, err := recipients[b].LastName.Get(); err == nil {
						return strings.ToLower(v.String()) > strings.ToLower(v2.String())
					}
				}
				return false
			case "phone":
				if v, err := recipients[a].Phone.Get(); err == nil {
					if v2, err := recipients[b].Phone.Get(); err == nil {
						return strings.ToLower(v.String()) > strings.ToLower(v2.String())
					}
				}
				return false
			case "position":
				if v, err := recipients[a].Position.Get(); err == nil {
					if v2, err := recipients[b].Position.Get(); err == nil {
						return strings.ToLower(v.String()) > strings.ToLower(v2.String())
					}
				}
				return false
			case "department":
				if v, err := recipients[a].Department.Get(); err == nil {
					if v2, err := recipients[b].Department.Get(); err == nil {
						return strings.ToLower(v.String()) > strings.ToLower(v2.String())
					}
				}
				return false
			case "city":
				if v, err := recipients[a].City.Get(); err == nil {
					if v2, err := recipients[b].City.Get(); err == nil {
						return strings.ToLower(v.String()) > strings.ToLower(v2.String())
					}
				}
				return false
			case "country":
				if v, err := recipients[a].Country.Get(); err == nil {
					if v2, err := recipients[b].Country.Get(); err == nil {
						return strings.ToLower(v.String()) > strings.ToLower(v2.String())
					}
				}
				return false
			case "misc":
				if v, err := recipients[a].Misc.Get(); err == nil {
					if v2, err := recipients[b].Misc.Get(); err == nil {
						return strings.ToLower(v.String()) > strings.ToLower(v2.String())
					}
				}
				return false
			case "extraID":
				if v, err := recipients[a].ExtraIdentifier.Get(); err == nil {
					if v2, err := recipients[b].ExtraIdentifier.Get(); err == nil {
						return strings.ToLower(v.String()) > strings.ToLower(v2.String())
					}
				}
				return false
			default:
				panic("unknown sort field")
			}
		})
	case "asc":
		sort.Slice(recipients, func(a, b int) bool {
			switch sortField {
			case "email":
				if v, err := recipients[a].Email.Get(); err == nil {
					if v2, err := recipients[b].Email.Get(); err == nil {
						return strings.ToLower(v.String()) < strings.ToLower(v2.String())
					}
				}
				return false
			case "firstName":
				if v, err := recipients[a].FirstName.Get(); err == nil {
					if v2, err := recipients[b].FirstName.Get(); err == nil {
						return strings.ToLower(v.String()) < strings.ToLower(v2.String())
					}
				}
				return false
			case "lastName":
				if v, err := recipients[a].LastName.Get(); err == nil {
					if v2, err := recipients[b].LastName.Get(); err == nil {
						return strings.ToLower(v.String()) < strings.ToLower(v2.String())
					}
				}
				return false
			case "phone":
				if v, err := recipients[a].Phone.Get(); err == nil {
					if v2, err := recipients[b].Phone.Get(); err == nil {
						return strings.ToLower(v.String()) < strings.ToLower(v2.String())
					}
				}
				return false
			case "position":
				if v, err := recipients[a].Position.Get(); err == nil {
					if v2, err := recipients[b].Position.Get(); err == nil {
						return strings.ToLower(v.String()) < strings.ToLower(v2.String())
					}
				}
				return false
			case "department":
				if v, err := recipients[a].Department.Get(); err == nil {
					if v2, err := recipients[b].Department.Get(); err == nil {
						return strings.ToLower(v.String()) < strings.ToLower(v2.String())
					}
				}
				return false
			case "city":
				if v, err := recipients[a].City.Get(); err == nil {
					if v2, err := recipients[b].City.Get(); err == nil {
						return strings.ToLower(v.String()) < strings.ToLower(v2.String())
					}
				}
				return false
			case "country":
				if v, err := recipients[a].Country.Get(); err == nil {
					if v2, err := recipients[b].Country.Get(); err == nil {
						return strings.ToLower(v.String()) < strings.ToLower(v2.String())
					}
				}
				return false
			case "misc":
				if v, err := recipients[a].Misc.Get(); err == nil {
					if v2, err := recipients[b].Misc.Get(); err == nil {
						return strings.ToLower(v.String()) < strings.ToLower(v2.String())
					}
				}
				return false
			case "extraID":
				if v, err := recipients[a].ExtraIdentifier.Get(); err == nil {
					if v2, err := recipients[b].ExtraIdentifier.Get(); err == nil {
						return strings.ToLower(v.String()) < strings.ToLower(v2.String())
					}
				}
				return false
			default:
				panic("unknown sort field")
			}
		})
	}
	return recipients
}

// GetByID gets a campaign by its id
func (c *Campaign) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	options *repository.CampaignOption,
) (*model.Campaign, error) {
	ae := NewAuditEvent("Campaign.GetById", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	campaign, err := c.CampaignRepository.GetByID(ctx, id, options)
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return campaign, nil
}

// GetByName gets a campaign by its name
func (c *Campaign) GetByName(
	ctx context.Context,
	session *model.Session,
	name string,
	companyID *uuid.UUID,
	options *repository.CampaignOption,
) (*model.Campaign, error) {
	ae := NewAuditEvent("Campaign.GetByName", session)
	ae.Details["name"] = name
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	campaign, err := c.CampaignRepository.GetByNameAndCompanyID(ctx, name, companyID, options)
	if err != nil {
		c.Logger.Errorw("failed to get campaign by name", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return campaign, nil
}

// GetByCompanyID gets a campaigns by it company id
func (c *Campaign) GetByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	ae := NewAuditEvent("Campaign.GetByCompanyID", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = c.CampaignRepository.GetAllByCompanyID(ctx, companyID, options)
	if err != nil {
		c.Logger.Errorw("failed to get campaigns by company id", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetStats gets stats for a campaign
// if no company id is given it retrieves stats for global including all companies
func (c *Campaign) GetStats(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	includeTestCampaigns bool,
) (*model.CampaignsStatView, error) {
	ae := NewAuditEvent("Campaign.GetStats", session)
	if companyID != nil {
		ae.Details["companyID"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get stats
	active, err := c.CampaignRepository.GetActiveCount(ctx, companyID, includeTestCampaigns)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	upcoming, err := c.CampaignRepository.GetUpcomingCount(ctx, companyID, includeTestCampaigns)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	finished, err := c.CampaignRepository.GetFinishedCount(ctx, companyID, includeTestCampaigns)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &model.CampaignsStatView{
		Active:   active,
		Upcoming: upcoming,
		Finished: finished,
	}, nil
}

// GetResultStats gets results stats for a campaign
func (c *Campaign) GetResultStats(
	ctx context.Context,
	session *model.Session,
	campaignID *uuid.UUID,
) (*model.CampaignResultView, error) {
	ae := NewAuditEvent("Campaign.GetResultStats", session)
	ae.Details["campaignId"] = campaignID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get stats
	stats, err := c.CampaignRepository.GetResultStats(ctx, campaignID)
	if err != nil {
		c.Logger.Errorw("failed to get campaign results statistics", "error", err)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return stats, nil
}

// GetRecipientsByCampaignID gets all recipients for a campaign
func (c *Campaign) GetRecipientsByCampaignID(
	ctx context.Context,
	session *model.Session,
	campaignID *uuid.UUID,
	options *repository.CampaignRecipientOption,
) ([]*model.CampaignRecipient, error) {
	ae := NewAuditEvent("Campaign.GetRecipientsByCampaignID", session)
	ae.Details["campaignId"] = campaignID.String()
	recipients := []*model.CampaignRecipient{}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return recipients, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get all recipients
	if options.OrderBy == "" {
		options.OrderBy = "campaign_recipients.sent_at"
	}
	recipients, err = c.CampaignRecipientRepository.GetByCampaignID(
		ctx,
		campaignID,
		options,
	)
	if err != nil {
		c.Logger.Errorw("failed to get recipients by campaign id", "error", err)
		return recipients, errs.Wrap(err)
	}
	// no audit on read
	return recipients, nil
}

// GetEventsByCampaignID gets all events for a campaign
func (c *Campaign) GetEventsByCampaignID(
	ctx context.Context,
	session *model.Session,
	campaignID *uuid.UUID,
	queryArgs *vo.QueryArgs,
	since *time.Time,
	eventTypeIDs []string,
) (*model.Result[model.CampaignEvent], error) {
	result := model.NewEmptyResult[model.CampaignEvent]()
	ae := NewAuditEvent("Campaign.GetEventsByCampaignID", session)
	ae.Details["campaignId"] = campaignID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = c.CampaignRepository.GetEventsByCampaignID(
		ctx,
		campaignID,
		&repository.CampaignEventOption{
			QueryArgs:    queryArgs,
			WithUser:     true,
			EventTypeIDs: eventTypeIDs,
		},
		since,
	)
	if err != nil {
		c.Logger.Errorw("failed to get events by campaign id", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetAll gets all campaigns using pagination
func (c *Campaign) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	ae := NewAuditEvent("Campaign.GetAll", session)
	if companyID != nil {
		ae.Details["companyID"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = c.CampaignRepository.GetAll(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		c.Logger.Errorw("failed to get all campaigns", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetAllWithinDates gets all campaigns active, scheduled or self managed within dates
func (c *Campaign) GetAllWithinDates(
	ctx context.Context,
	session *model.Session,
	startDate time.Time,
	endDate time.Time,
	companyID *uuid.UUID,
	options *repository.CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	ae := NewAuditEvent("Campaign.GetAllWithinDates", session)
	if companyID != nil {
		ae.Details["companyID"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = c.CampaignRepository.GetAllCampaignWithinDates(
		ctx,
		companyID,
		startDate,
		endDate,
		options,
	)
	if err != nil {
		c.Logger.Errorw("failed to get all campaigns between dates", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetAllActive gets all active campaigns using pagination
func (c *Campaign) GetAllActive(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	ae := NewAuditEvent("Campaign.GetAllActive", session)
	if companyID != nil {
		ae.Details["companyID"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = c.CampaignRepository.GetAllActive(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		c.Logger.Errorw("failed to get all active campaigns", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetAllUpcoming gets all upcoming campaigns using pagination
func (c *Campaign) GetAllUpcoming(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	ae := NewAuditEvent("Campaign.GetAllUpcoming", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = c.CampaignRepository.GetAllUpcoming(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		c.Logger.Errorw("failed to get all upcoming campaigns", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// GetAllFinished gets all finished campaigns using pagination
func (c *Campaign) GetAllFinished(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	options *repository.CampaignOption,
) (*model.Result[model.Campaign], error) {
	result := model.NewEmptyResult[model.Campaign]()
	ae := NewAuditEvent("Campaign.GetAllFinished", session)
	if companyID != nil {
		ae.Details["companyId"] = companyID.String()
	}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	result, err = c.CampaignRepository.GetAllFinished(
		ctx,
		companyID,
		options,
	)
	if err != nil {
		c.Logger.Errorw("failed to get all finished campaigns", "error", err)
		return result, errs.Wrap(err)
	}
	// no audit on read
	return result, nil
}

// SaveTrackingPixelLoaded saves the tracking pixel event for a campaign recipient
// no permissions to check - this endpoint is public
// only a campaign recipient id is required
func (c *Campaign) SaveTrackingPixelLoaded(
	ctx *gin.Context,
	campaignRecipientID *uuid.UUID,
) error {
	// get the campaign campaignRecipient
	campaignRecipient, err := c.CampaignRecipientRepository.GetByCampaignRecipientID(
		ctx.Request.Context(),
		campaignRecipientID,
	)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.Logger.Debugw("campaign recipient not found for tracking pixel", "campaign_recipient_id", campaignRecipientID.String())
			return err
		}
		c.Logger.Errorw("failed to get campaign recipient by id", "error", err)
		return errs.Wrap(err)
	}
	recipientID := campaignRecipient.RecipientID.MustGet()
	campaignID := campaignRecipient.CampaignID.MustGet()

	campaign, err := c.CampaignRepository.GetByID(
		ctx,
		&campaignID,
		&repository.CampaignOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id", "error", err)
		return errs.Wrap(err)
	}
	trackingPixelLoadedEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ]
	newEventID := uuid.New()
	var campaignEvent *model.CampaignEvent
	if campaign.IsAnonymous.MustGet() {
		userAgent := vo.NewEmptyOptionalString255()
		campaignEvent = &model.CampaignEvent{
			ID:          &newEventID,
			CampaignID:  &campaignID,
			RecipientID: nil,
			IP:          vo.NewEmptyOptionalString64(),
			UserAgent:   userAgent,
			EventID:     trackingPixelLoadedEventID,
			Data:        vo.NewOptionalString1MBMust(""),
		}
	} else {
		ip := vo.NewOptionalString64Must(utils.ExtractClientIP(ctx.Request))
		ua := ctx.Request.UserAgent()
		if len(ua) > 255 {
			ua = strings.TrimSpace(ua[:255])
		}
		userAgent := vo.NewOptionalString255Must(ua)
		campaignEvent = &model.CampaignEvent{
			ID:          &newEventID,
			CampaignID:  &campaignID,
			RecipientID: &recipientID,
			IP:          ip,
			UserAgent:   userAgent,
			EventID:     cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ],
			Data:        vo.NewOptionalString1MBMust(""),
		}
	}
	err = c.CampaignRepository.SaveEvent(ctx, campaignEvent)
	if err != nil {
		c.Logger.Errorw("failed to save tracking pixel loaded event", "error", err)
		return errs.Wrap(err)
	}
	// handle most notable event
	err = c.SetNotableCampaignRecipientEvent(
		ctx,
		campaignRecipient,
		data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ,
	)
	if err != nil {
		// logging was done in the previous call
		return errs.Wrap(err)
	}
	// handle webhook
	webhookID, err := c.CampaignRepository.GetWebhookIDByCampaignID(ctx, &campaignID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Logger.Errorw("failed to get webhook id by campaign id", "error", err)
		return errs.Wrap(err)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || webhookID == nil {
		return nil
	}
	err = c.HandleWebhook(
		ctx,
		webhookID,
		&campaignID,
		&recipientID,
		data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_READ,
	)
	if err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// UpdateByID updates a campaign by id
func (c *Campaign) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	incoming *model.Campaign,
) error {
	ae := NewAuditEvent("Campaign.UpdateById", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if len(incoming.RecipientGroupIDs) == 0 {
		return validate.WrapErrorWithField(errors.New("no groups provided"), "Recipient Groups")
	}
	// if the schedule type is scheduled, set the start time to start of day and end to the end of the last day
	if incoming.ConstraintWeekDays.IsSpecified() && !incoming.ConstraintWeekDays.IsNull() {
		if err := incoming.ValidateScheduledTimes(); err != nil {
			return errs.Wrap(err)
		}
		if err := c.updateSchedulesCampaignStartAndEndDates(incoming); err != nil {
			return errs.Wrap(err)
		}
	}
	// validate allow deny list selections
	if err := c.validateAllowDenyIsSameTypeByIDs(ctx, incoming); err != nil {
		return errs.Wrap(err)
	}
	// get campaign and change the values
	current, err := c.CampaignRepository.GetByID(
		ctx,
		id,
		&repository.CampaignOption{
			WithRecipientGroups: true,
		},
	)
	if err != nil {
		c.Logger.Errorw("failed to campaign by id", "error", err)
		return errs.Wrap(err)
	}
	// check if the campaign is within the allowed time frame for editing
	// we allow editing up until 5 minutes before the campaigns start time
	if sendStartAt, err := current.SendStartAt.Get(); err == nil {
		nowPlus5 := time.Now().Add(5 * time.Minute)
		// c.Logger.Debugw("now (+5min): %s campaign: %s", nowPlus5.String(), sendStartAt.String())
		if nowPlus5.After(sendStartAt) {
			c.Logger.Debugw(
				"campaign too close to start to edit",
				"campaignID", current.ID.MustGet().String(),
				"nowPlus5", nowPlus5,
				"sendStartAt", sendStartAt,
			)
			return validate.WrapErrorWithField(
				errors.New("Campaign already started or too close to start time"),
				"Not allowed",
			)
		}
	}

	// update the values
	if v, err := incoming.Name.Get(); err == nil {
		// check uniqueness
		var companyID *uuid.UUID
		if cid, err := incoming.CompanyID.Get(); err == nil {
			companyID = &cid
		}
		name := incoming.Name.MustGet()
		isOK, err := repository.CheckNameIsUnique(
			ctx,
			c.CampaignRepository.DB,
			"campaigns",
			name.String(),
			companyID,
			id,
		)
		if err != nil {
			c.Logger.Errorw("failed to check campaign uniqueness", "error", err)
			return errs.Wrap(err)
		}
		if !isOK {
			c.Logger.Debugw("campaign name not unique", "name", name.String())
			return validate.WrapErrorWithField(errors.New("is not unique"), "name")
		}

		current.Name.Set(v)
	}
	// update values
	if v, err := incoming.SaveSubmittedData.Get(); err == nil {
		current.SaveSubmittedData.Set(v)
	}
	if v, err := incoming.IsAnonymous.Get(); err == nil {
		current.IsAnonymous.Set(v)
	}
	if v, err := incoming.IsTest.Get(); err == nil {
		current.IsTest.Set(v)
	}
	if v, err := incoming.SortField.Get(); err == nil {
		current.SortField.Set(v)
	}
	if v, err := incoming.SortOrder.Get(); err == nil {
		current.SortOrder.Set(v)
	}
	if v, err := incoming.SendStartAt.Get(); err == nil {
		current.SendStartAt.Set(v.Truncate(time.Minute))
	}
	if v, err := incoming.SendEndAt.Get(); err == nil {
		current.SendEndAt.Set(v.Truncate(time.Minute))
	}
	if v, err := incoming.ConstraintWeekDays.Get(); err == nil {
		current.ConstraintWeekDays.Set(v)
	}
	if v, err := incoming.ConstraintStartTime.Get(); err == nil {
		current.ConstraintStartTime.Set(v)
	}
	if v, err := incoming.ConstraintEndTime.Get(); err == nil {
		current.ConstraintEndTime.Set(v)
	}
	if v, err := incoming.CloseAt.Get(); err == nil {
		current.CloseAt.Set(v.Truncate(time.Minute))
	}
	if v, err := incoming.AnonymizeAt.Get(); err == nil {
		current.AnonymizeAt.Set(v.Truncate(time.Minute))
	}
	if v, err := incoming.ClosedAt.Get(); err == nil {
		current.ClosedAt.Set(v.Truncate(time.Minute))
	}
	if v, err := incoming.AnonymizedAt.Get(); err == nil {
		current.AnonymizedAt.Set(v.Truncate(time.Minute))
	}
	if v, err := incoming.TemplateID.Get(); err == nil {
		current.TemplateID.Set(v)
	}
	if v, err := incoming.RecipientGroupIDs.Get(); err == nil {
		current.RecipientGroupIDs.Set(v)
	}
	if v, err := incoming.WebhookID.Get(); err == nil {
		current.WebhookID.Set(v)
	}

	// check there is atleast one valid group
	// and remove any empty groups
	validGroups := []*uuid.UUID{}
	for _, groupID := range current.RecipientGroupIDs.MustGet() {
		count, err := c.RecipientGroupRepository.GetRecipientCount(ctx, groupID)
		if err != nil {
			return errs.Wrap(err)
		}
		if count > 0 {
			validGroups = append(validGroups, groupID)
		}
	}
	if len(validGroups) == 0 {
		return errs.NewValidationError(
			errors.New("Selected groups have no recipients"),
		)
	}
	// overwrite the allow / deny
	current.AllowDenyIDs = incoming.AllowDenyIDs
	if _, err := current.AllowDenyIDs.Get(); err == nil {
		if incoming.DenyPageID.IsSpecified() {
			if incoming.DenyPageID.IsNull() {
				current.DenyPageID.SetNull()
			} else {
				current.DenyPageID.Set(incoming.DenyPageID.MustGet())
			}
		}

	}

	// handle evasion page ID
	if incoming.EvasionPageID.IsSpecified() {
		if incoming.EvasionPageID.IsNull() {
			current.EvasionPageID.SetNull()
		} else {
			current.EvasionPageID.Set(incoming.EvasionPageID.MustGet())
		}
	}
	// validate and update
	if err := current.Validate(); err != nil {
		return errs.Wrap(err)
	}
	err = c.CampaignRepository.UpdateByID(ctx, id, current)
	if err != nil {
		c.Logger.Errorw("failed to update campaign by id", "error", err)
		return errs.Wrap(err)
	}
	// re-schedule the campaign
	// TODO should this all be in the schedule method
	// remove all existing schedules if the campaign is not self-managed
	if !incoming.IsSelfManaged() {
		err = c.CampaignRecipientRepository.DeleteByCampaigID(
			ctx,
			id,
		)
		if err != nil {
			c.Logger.Errorw("failed to remove recipient groups", "error", err)
			return errs.Wrap(err)
		}
		err = c.CampaignRepository.RemoveCampaignRecipientGroups(
			ctx,
			id,
		)
		if err != nil {
			c.Logger.Errorw("failed to remove campaignrecipient groups", "error", err)
			return errs.Wrap(err)
		}
	} else {
		// if self managed remove only the campaign recipients groups
		err = c.CampaignRepository.RemoveCampaignRecipientGroups(
			ctx,
			id,
		)
		if err != nil {
			c.Logger.Errorw("failed to remove recipient groups", "error", err)
			return errs.Wrap(err)
		}
	}
	if incoming.RecipientGroupIDs.IsSpecified() && !incoming.RecipientGroupIDs.IsNull() {
		recipientGroupIDs := incoming.RecipientGroupIDs.MustGet()
		err = c.CampaignRepository.AddRecipientGroups(
			ctx,
			id,
			recipientGroupIDs,
		)
	}
	if err != nil {
		c.Logger.Errorw("failed to add recipient groups", "error", err)
		return errs.Wrap(err)
	}
	err = c.schedule(ctx, session, current)
	if err != nil {
		c.Logger.Errorw("failed to re-schedule campaign", "error", err)
		return errs.Wrap(err)
	}
	c.AuditLogAuthorized(ae)
	return nil
}

// validateAllowDenyIsSameType checks if the allow and deny lists are of the same type
// allow and deny are mutually exclusive
func (c *Campaign) validateAllowDenyIsSameTypeByIDs(
	ctx context.Context,
	campaign *model.Campaign,
) error {
	if campaign.AllowDenyIDs.IsSpecified() && !campaign.AllowDenyIDs.IsNull() {
		allowDenyIDs := campaign.AllowDenyIDs.MustGet()
		if len(allowDenyIDs) == 0 {
			return nil
		}
		isAllowList := false
		for i, id := range allowDenyIDs {
			entry, err := c.AllowDenyRepository.GetByID(ctx, id, &repository.AllowDenyOption{})
			if err != nil {
				c.Logger.Errorw("failed to get allow deny by id", "error", err)
			}
			allowed := entry.Allowed.MustGet()
			if i == 0 {
				isAllowList = allowed
				continue
			}
			if isAllowList != allowed {
				return validate.WrapErrorWithField(errors.New("allow and deny list are mutually exclusive"), "allowDenyIDs")
			}
		}
	}
	return nil
}

// updateSchedulesCampaignStartAndEndDates updates the schedules for a campaign
// it uses the first and last selected weekday and along with sending times to adjust
// the start and end of the campaign
func (c *Campaign) updateSchedulesCampaignStartAndEndDates(
	campaign *model.Campaign,
) error {
	// get the first and last selected weekday
	campaignWeekDays := campaign.ConstraintWeekDays.MustGet().AsSlice()
	startAt := campaign.SendStartAt.MustGet()
	endAt := campaign.SendEndAt.MustGet()
	startTime := campaign.ConstraintStartTime.MustGet()
	endTime := campaign.ConstraintEndTime.MustGet()
	// find the first day and start time of sending
	currentDate := startAt
	startFound := false
	startDay := time.Time{}
	lastDay := time.Time{}
	for currentDate.Before(endAt) || currentDate.Equal(endAt) {
		if slices.Contains(campaignWeekDays, int(currentDate.Weekday())) {
			if !startFound {
				startDay = currentDate
				startFound = true
			}
			lastDay = currentDate
		}
		currentDate = currentDate.AddDate(0, 0, 1)
	}
	campaign.SendStartAt.Set(startDay.Truncate(24 * time.Hour).Add(startTime.Minutes()))
	campaign.SendEndAt.Set(lastDay.Truncate(24 * time.Hour).Add(endTime.Minutes()))
	return nil
}

// DeleteByID deletes a campaign by id
func (c *Campaign) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Campaign.DeleteById", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// delete all campaign-allowDeny relations to the campaign
	err = c.CampaignRepository.RemoveAllowDenyListsByCampaignID(ctx, id)
	if err != nil {
		c.Logger.Errorw("failed to delete campaign allow deny by campaign id", "error", err)
		return errs.Wrap(err)
	}
	// remove all related events
	err = c.CampaignRepository.DeleteEventsByCampaignID(ctx, id)
	if err != nil {
		c.Logger.Errorw("failed to delete campaign events by campaign id", "error", err)
		return errs.Wrap(err)
	}
	// delete the relation between the campaign and the recipient groups
	err = c.CampaignRepository.RemoveCampaignRecipientGroups(ctx, id)
	if err != nil {
		c.Logger.Errorw("failed to delete campaign recipient groups by campaign id",
			"campaignID", id.String(),
			"error", err,
		)
		return errs.Wrap(err)
	}
	err = c.CampaignRecipientRepository.DeleteByCampaigID(
		ctx,
		id,
	)
	if err != nil {
		c.Logger.Errorw("failed to remove recipient groups", "error", err)
		return errs.Wrap(err)
	}
	// delete campaign
	err = c.CampaignRepository.DeleteByID(ctx, id)
	if err != nil {
		c.Logger.Errorw("failed to delete campaign by id", "error", err)
		return errs.Wrap(err)
	}
	c.AuditLogAuthorized(ae)
	return nil
}

// SendNextBatch sends the next batch of emails
// atm this is only audit logged on auth failures
func (c *Campaign) SendNextBatch(
	ctx context.Context,
	session *model.Session,
) error {
	ae := NewAuditEvent("Campaign.SendNextBatch", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get next batch
	campaignRecipients, err := c.CampaignRecipientRepository.GetUnsendRecipientsForSending(
		ctx,
		1000, // limit
		&repository.CampaignRecipientOption{
			WithRecipient: true,
		},
	)
	if err != nil {
		c.Logger.Errorw("failed to get next batch", "error", err)
		return errs.Wrap(err)
	}
	// group campaignrecipients by campaign so if it is send via SMTP we can reuse
	// the connection
	campaignMap := map[string][]*model.CampaignRecipient{}
	for _, campaignRecipient := range campaignRecipients {
		campaignID := campaignRecipient.CampaignID.MustGet().String()
		campaignMap[campaignID] = append(campaignMap[campaignID], campaignRecipient)
	}
	// iterate each campaign and send the messages
	for campaignID, campaignRecipients := range campaignMap {
		err = c.sendCampaignMessages(ctx, session, campaignID, campaignRecipients)
		if err != nil {
			c.Logger.Errorw("failed to send campaign messages", "error", err)
			continue
		}
	}

	return errs.Wrap(err)
}

func (c *Campaign) sendCampaignMessages(
	ctx context.Context,
	session *model.Session,
	cid string,
	campaignRecipients []*model.CampaignRecipient,
) error {
	campaignID := uuid.MustParse(cid)
	// fetch the campaign to ensure that it is still active and to fetch details for sending
	campaign, err := c.CampaignRepository.GetByID(
		ctx,
		&campaignID,
		&repository.CampaignOption{
			WithCampaignTemplate: false,
		},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id",
			"campaignID", campaignID,
			"error", err,
		)
		return errs.Wrap(err)
	}
	// check if the campaign has been close while sending is being processed
	if !campaign.IsActive() {
		c.Logger.Debugw("campaign is not active",
			"campaignID", campaign.ID.MustGet().String(),
		)
		return errors.New("skipping send, campaign is not active")
	}
	// fetch the campaign cTemplate to get the sender and message to send
	templateID, err := campaign.TemplateID.Get()
	if err != nil {
		c.Logger.Infow("campaign has no template", "error", err)
		return errs.Wrap(errors.New("skipping send, campaign has no template"))
	}
	cTemplate, err := c.CampaignTemplateService.GetByID(
		ctx,
		session,
		&templateID,
		&repository.CampaignTemplateOption{
			WithDomain:             true,
			WithSMTPConfiguration:  true,
			WithIdentifier:         true,
			WithBeforeLandingProxy: true,
			WithLandingProxy:       true,
		},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign template by id", "error", err)
		closeErr := c.closeCampaign(
			ctx,
			session,
			&campaignID,
			campaign,
			"failed get email",
		)
		return errs.Wrap(errors.Join(err, closeErr))
	}
	// domain
	domain := cTemplate.Domain
	if domain == nil {
		// if the domain has been removed from the campaign template used in this campaign, close the campaign
		closeErr := c.closeCampaign(
			ctx,
			session,
			&campaignID,
			campaign,
			"Campaign does not have a domain relation",
		)
		if closeErr != nil {
			return errs.Wrap(errors.Join(err, closeErr))
		}
		c.Logger.Warnw("Running campaign does not have a domain relation - cancelling campaign",
			"campaignID", campaignID.String(),
		)
		return nil
	}
	// get email details
	emailID, err := cTemplate.EmailID.Get()
	if err != nil {
		c.Logger.Warnw("Running campaign does not have a email relation - cancelling campaign",
			"campaignID", campaignID.String(),
		)
		// if the email relation has been removed from the campaign template used in this campagin, close the campaign
		closeErr := c.closeCampaign(
			ctx,
			session,
			&campaignID,
			campaign,
			"Campaign does not have a email relation",
		)
		if closeErr != nil {
			return errs.Wrap(errors.Join(err, closeErr))
		}
		return nil
	}
	// get campaign's company context for attachment filtering
	var campaignCompanyID *uuid.UUID
	if campaign.CompanyID.IsSpecified() && !campaign.CompanyID.IsNull() {
		companyID := campaign.CompanyID.MustGet()
		campaignCompanyID = &companyID
	}

	email, err := c.MailService.GetByID(
		ctx,
		session,
		&emailID,
		campaignCompanyID,
	)
	if err != nil {
		closeErr := c.closeCampaign(
			ctx,
			session,
			&campaignID,
			campaign,
			"failed get email",
		)
		return errs.Wrap(errors.Join(err, closeErr))
	}
	content, err := email.Content.Get()
	if err != nil {
		// if mail templates fails to parse, close the campaign
		closeErr := c.closeCampaign(
			ctx,
			session,
			&campaignID,
			campaign,
			"failed get email content",
		)
		return errs.Wrap(errors.Join(err, closeErr))
	}
	t := template.New("email")
	t = t.Funcs(TemplateFuncs())
	mailTmpl, err := t.Parse(content.String())
	if err != nil {
		// if mail templates fails to parse, close the campaign
		closeErr := c.closeCampaign(
			ctx,
			session,
			&campaignID,
			campaign,
			"failed to parse the template",
		)
		return errs.Wrap(errors.Join(err, closeErr))
	}
	// check if sending is API or SMTP
	isSmtpCampaign := cTemplate.SMTPConfigurationID.IsSpecified() && !cTemplate.SMTPConfigurationID.IsNull()
	isAPISenderCampaign := cTemplate.APISenderID.IsSpecified() && !cTemplate.APISenderID.IsNull()
	// close the campaign
	if !isSmtpCampaign && !isAPISenderCampaign {
		c.Logger.Warnw("Running campaign does not have a SMTP or API sender relation - cancelling campaign",
			"campaignID", campaignID.String(),
		)
		// if there is no smtp config or api sender, then one of them has been removed from the campaigns template
		return c.closeCampaign(
			ctx,
			session,
			&campaignID,
			campaign,
			"Campaign does not have a either an SMTP configuration or an API Sender",
		)
	}
	if isAPISenderCampaign {
		// send via API
		for _, campaignRecipient := range campaignRecipients {
			// update the last attempt at timestamp so we do not accidently try sending the same
			// email again if a panic or error happens in a 3. party lib.
			campaignRecipientID := campaignRecipient.ID.MustGet()
			campaignRecipient.LastAttemptAt = nullable.NewNullableWithValue(time.Now())
			err := c.CampaignRecipientRepository.UpdateByID(
				ctx,
				&campaignRecipientID,
				campaignRecipient,
			)
			if err != nil {
				c.Logger.Errorw("CRITICAL - failed to update last attempted at - aborting",
					"error", err,
				)
				return errs.Wrap(
					fmt.Errorf("failed to update last attempted at: %s \nThis is critical for sending, aborting...", err),
				)
			}
			// generate custom campaign URL if first page is MITM
			recipientID := campaignRecipient.ID.MustGet()
			customCampaignURL, urlErr := c.GetLandingPageURLByCampaignRecipientID(ctx, session, &recipientID)
			if urlErr != nil {
				c.Logger.Errorw("failed to get campaign url for API sender", "error", urlErr)
				customCampaignURL = ""
			}

			// send via API with custom URL (domain and template stay the same for assets)
			err = c.APISenderService.SendWithCustomURL(
				ctx,
				session,
				cTemplate,
				campaignRecipient,
				domain,
				mailTmpl,
				email,
				customCampaignURL,
			)
			if err != nil {
				c.Logger.Errorw("failed to send message via. API", "error", err)
			}
			err = c.saveSendingResult(
				ctx,
				campaignRecipient,
				err,
			)
			if err != nil {
				c.Logger.Errorw("failed to save sending result", "error", err)
				return errs.Wrap(err)
			}
		}
		err = c.setMostNotableCampaignEvent(
			ctx,
			campaign,
			data.EVENT_CAMPAIGN_ACTIVE,
		)
		if err != nil {
			// err is logged in method call
			return errs.Wrap(err)
		}
		return nil
	}
	if !isSmtpCampaign {
		c.Logger.Error("no sender configuration found")
		return errors.New("no sender configuration found")
	}
	// get the SMTP configuration
	smtpConfigID, err := cTemplate.SMTPConfigurationID.Get()
	if err != nil {
		c.Logger.Infow(
			"failed to get SMTP configuration from template - template no longer usable",
			"smtpConfigID", smtpConfigID,
		)
		return errs.Wrap(err)
	}
	smtpConfig, err := c.SMTPConfigService.GetByID(
		ctx,
		session,
		&smtpConfigID,
		&repository.SMTPConfigurationOption{
			WithHeaders: true,
		},
	)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Logger.Errorw("smtp configuration did not load", "error", err)
		return errs.Wrap(err)
	}
	smtpPort, err := smtpConfig.Port.Get()
	if err != nil {
		c.Logger.Errorw("failed to get smtp port", "error", err)
		return errs.Wrap(err)
	}
	smtpHost, err := smtpConfig.Host.Get()
	if err != nil {
		c.Logger.Errorw("failed to get smtp host", "error", err)
		return errs.Wrap(err)
	}
	smtpIgnoreCertErrors, err := smtpConfig.IgnoreCertErrors.Get()
	if err != nil {
		c.Logger.Errorw("failed to get smtp ignore cert errors", "error", err)
		return errs.Wrap(err)
	}
	emailOptions := []mail.Option{
		mail.WithPort(smtpPort.Int()),
		mail.WithTLSConfig(
			&tls.Config{
				ServerName: smtpHost.String(),
				// #nosec
				InsecureSkipVerify: smtpIgnoreCertErrors,
				// MinVersion:         tls.VersionTLS12,
			},
		),
	}
	// setup authentication if provided
	username, err := smtpConfig.Username.Get()
	if err != nil {
		c.Logger.Errorw("failed to get smtp username", "error", err)
		return errs.Wrap(err)
	}
	password, err := smtpConfig.Password.Get()
	if err != nil {
		c.Logger.Errorw("failed to get smtp password", "error", err)
		return errs.Wrap(err)
	}
	if un := username.String(); len(un) > 0 {
		emailOptions = append(
			emailOptions,
			mail.WithUsername(
				un,
			),
		)
		if pw := password.String(); len(pw) > 0 {
			emailOptions = append(
				emailOptions,
				mail.WithPassword(
					pw,
				),
			)
		}
	}
	// prepare all messages
	messageOptions := []mail.MsgOption{
		mail.WithNoDefaultUserAgent(),
	}
	// create maps between recipients and messages
	// and prepare all messages
	messages := []*mail.Msg{}
	mailToCampaignRecipient := make(map[string]*model.CampaignRecipient, len(campaignRecipients))
	for _, campaignRecipient := range campaignRecipients {
		// update the last attempt at timestamp so we do not accidently try sending the same
		// email again if a panic or error happens in a 3. party lib.
		campaignRecipientID := campaignRecipient.ID.MustGet()
		campaignRecipient.LastAttemptAt = nullable.NewNullableWithValue(time.Now())
		err := c.CampaignRecipientRepository.UpdateByID(
			ctx,
			&campaignRecipientID,
			campaignRecipient,
		)
		if err != nil {
			c.Logger.Errorw("CRITICAL - failed to update last attempted at - aborting",
				"error", err,
			)
			return fmt.Errorf("failed to update last attempted at: %s \nThis is critical for sending, aborting...", err)
		}
		m := mail.NewMsg(messageOptions...)
		/* TODO at the moment the mail envelope from is a email, so it can not be empty by definition
		if envelopefrom.string() == "" {
			// extract the email only from mail.mailheaderfrom
			// and use that as the envelope from
			// this is a fallback if the envelope from is not set
			address, err := netmail.parseaddress(email.mailheaderfrom)
			if err != nil {
				c.logger.errorw("failed to parse mail header 'from'", "error", err)
				return false,errs.Wrap(err)
			}
			err = m.envelopefrom(address.address)
			if err != nil {
				c.logger.errorw("failed to set envelope from", "error", err)
				return false,errs.Wrap(err)
			}
		} else {
			err = m.envelopefrom(email.mailenvelopefrom)
			if err != nil {
				c.logger.errorw("failed to set envelope from", "error", err)
				return false,errs.Wrap(err)
			}
		}
		*/
		err = m.EnvelopeFrom(email.MailEnvelopeFrom.MustGet().String())
		if err != nil {
			c.Logger.Errorw("failed to set envelope from", "error", err)
			return errs.Wrap(err)
		}
		// headers
		err = m.From(email.MailHeaderFrom.MustGet().String())
		if err != nil {
			c.Logger.Errorw("failed to set mail header 'From'", "error", err)
			return errs.Wrap(err)
		}
		// handle a race where the recipient has been removed/anonymized
		if campaignRecipient.Recipient == nil {
			crid := campaignRecipient.ID.MustGet()
			err := c.CampaignRecipientRepository.Cancel(
				ctx,
				[]*uuid.UUID{&crid},
			)
			if err != nil {
				return errors.New("Missing recipient from campaign recipient")
			}
			c.Logger.Info("A campaign recipient had no recipient - cancelled - this can happend in rare race conditions or curruption bugs")
			continue
		}
		recpEmail := campaignRecipient.Recipient.Email.MustGet().String()
		err = m.To(recpEmail)
		if err != nil {
			c.Logger.Errorw("failed to set mail header 'To'", "error", err)
			return errs.Wrap(err)
		}
		// store a map between recipient email and message
		// so we can later save the sending result
		mailToCampaignRecipient[m.GetToString()[0]] = campaignRecipient
		// custom headers
		if headers := smtpConfig.Headers; headers != nil {
			for _, header := range headers {
				key := header.Key.MustGet()
				value := header.Value.MustGet()
				m.SetGenHeader(
					mail.Header(key.String()),
					value.String(),
				)
			}
		}
		m.Subject(email.MailHeaderSubject.MustGet().String())
		urlIdentifier := cTemplate.URLIdentifier
		if urlIdentifier == nil {
			c.Logger.Error("url identifier is MUST be loaded for the campaign template")
			return fmt.Errorf("url identifier is MUST be loaded for the campaign template")
		}

		// get template domain for assets and tracking pixel
		domainName, err := domain.Name.Get()
		if err != nil {
			c.Logger.Errorw("failed to get domain name", "error", err)
			return errs.Wrap(err)
		}
		urlPath := cTemplate.URLPath.MustGet().String()

		// generate custom campaign URL if first page is MITM
		recipientID := campaignRecipient.ID.MustGet()
		customCampaignURL, err := c.GetLandingPageURLByCampaignRecipientID(ctx, session, &recipientID)
		if err != nil {
			c.Logger.Errorw("failed to get campaign url", "error", err)
			return errs.Wrap(err)
		}

		t := c.TemplateService.CreateMail(
			domainName.String(),
			urlIdentifier.Name.MustGet(),
			urlPath,
			campaignRecipient,
			email,
			nil,
		)

		// override campaign URL if it's different from template domain URL
		templateURL := fmt.Sprintf("https://%s%s?%s=%s", domainName.String(), urlPath, urlIdentifier.Name.MustGet(), recipientID.String())
		if customCampaignURL != templateURL {
			(*t)["URL"] = customCampaignURL
		}
		var bodyBuffer bytes.Buffer
		err = mailTmpl.Execute(&bodyBuffer, t)
		if err != nil {
			c.Logger.Errorw("failed to execute mail template", "error", err)
			return errs.Wrap(err)
		}
		m.SetBodyString("text/html", bodyBuffer.String())
		// attachments
		attachments := email.Attachments
		for _, attachment := range attachments {
			p, err := c.MailService.AttachmentService.GetPath(attachment)
			if err != nil {
				return fmt.Errorf("failed to get attachment path: %s", err)
			}
			if !attachment.EmbeddedContent.MustGet() {
				m.AttachFile(p.String())
			} else {
				attachmentContent, err := os.ReadFile(p.String())
				if err != nil {
					return errs.Wrap(err)
				}
				// hacky setup of attachment for executing as email template
				attachmentAsEmail := model.Email{
					ID:                email.ID,
					CreatedAt:         email.CreatedAt,
					UpdatedAt:         email.UpdatedAt,
					Name:              email.Name,
					MailEnvelopeFrom:  email.MailEnvelopeFrom,
					MailHeaderFrom:    email.MailHeaderFrom,
					MailHeaderSubject: email.MailHeaderSubject,
					Content:           email.Content,
					AddTrackingPixel:  email.AddTrackingPixel,
					CompanyID:         email.CompanyID,
					Attachments:       email.Attachments,
					Company:           email.Company,
				}
				// really hacky / unsafe
				attachmentAsEmail.Content = nullable.NewNullableWithValue(
					*vo.NewUnsafeOptionalString1MB(string(attachmentContent)),
				)
				// generate custom campaign URL for attachment
				recipientID := campaignRecipient.ID.MustGet()
				customCampaignURL, err := c.GetLandingPageURLByCampaignRecipientID(ctx, session, &recipientID)
				if err != nil {
					c.Logger.Errorw("failed to get campaign url for attachment", "error", err)
					return errs.Wrap(err)
				}

				attachmentStr, err := c.TemplateService.CreateMailBodyWithCustomURL(
					urlIdentifier.Name.MustGet(),
					urlPath,
					domain,
					campaignRecipient,
					&attachmentAsEmail,
					nil,
					customCampaignURL,
				)
				if err != nil {
					return errs.Wrap(fmt.Errorf("failed to setup attachment with embedded content: %s", err))
				}
				m.AttachReadSeeker(
					filepath.Base(p.String()),
					strings.NewReader(attachmentStr),
				)
			}
		}
		messages = append(messages, m)
	}

	// send the messages
	// the client sends all the messages and ensure that all messages are sent
	// in the same connection
	var mc *mail.Client

	// Try different authentication methods based on configuration
	// If username is provided, use authentication; otherwise try without auth first
	if un := username.String(); len(un) > 0 {
		// Try CRAM-MD5 first when credentials are provided
		emailOptionsCRAM5 := append(emailOptions, mail.WithSMTPAuth(mail.SMTPAuthCramMD5))
		mc, _ = mail.NewClient(smtpHost.String(), emailOptionsCRAM5...)
		mc.SetLogger(log.NewGoMailLoggerAdapter(c.Logger))
		mc.SetDebugLog(true)
		if build.Flags.Production {
			mc.SetTLSPolicy(mail.TLSMandatory)
		} else {
			mc.SetTLSPolicy(mail.TLSOpportunistic)
		}
		err = mc.DialAndSendWithContext(ctx, messages...)

		// Check if it's an authentication error and try PLAIN auth
		if err != nil && (strings.Contains(err.Error(), "535 ") ||
			strings.Contains(err.Error(), "534 ") ||
			strings.Contains(err.Error(), "538 ") ||
			strings.Contains(err.Error(), "CRAM-MD5") ||
			strings.Contains(err.Error(), "authentication failed")) {
			c.Logger.Debugf("CRAM-MD5 authentication failed, trying PLAIN auth", "error", err)
			emailOptionsBasic := emailOptions
			emailOptionsBasic = append(emailOptions, mail.WithSMTPAuth(mail.SMTPAuthPlain))
			mc, _ = mail.NewClient(smtpHost.String(), emailOptionsBasic...)
			mc.SetLogger(log.NewGoMailLoggerAdapter(c.Logger))
			mc.SetDebugLog(true)
			if build.Flags.Production {
				mc.SetTLSPolicy(mail.TLSMandatory)
			} else {
				mc.SetTLSPolicy(mail.TLSOpportunistic)
			}
			err = mc.DialAndSendWithContext(ctx, messages...)
		}
	} else {
		// No credentials provided, try without authentication (e.g., local postfix)
		mc, _ = mail.NewClient(smtpHost.String(), emailOptions...)
		mc.SetLogger(log.NewGoMailLoggerAdapter(c.Logger))
		mc.SetDebugLog(true)
		if build.Flags.Production {
			mc.SetTLSPolicy(mail.TLSMandatory)
		} else {
			mc.SetTLSPolicy(mail.TLSOpportunistic)
		}
		err = mc.DialAndSendWithContext(ctx, messages...)

		// If no-auth fails and we get an auth-related error, log it appropriately
		if err != nil && (strings.Contains(err.Error(), "530 ") ||
			strings.Contains(err.Error(), "535 ") ||
			strings.Contains(err.Error(), "authentication required") ||
			strings.Contains(err.Error(), "AUTH")) {
			c.Logger.Warnw("Server requires authentication but no credentials provided", "error", err)
		}
	}
	if err != nil {
		c.Logger.Errorw("failed to send test email", "error", err)
	}
	// check each message if has been sent and save the result for each
	for _, m := range messages {
		var sendError error = nil
		if m.HasSendError() {
			sendError = m.SendError()
		}
		// deref 0 as only a single recipient in each mail
		to := m.GetToString()[0]
		campaignRecipient := mailToCampaignRecipient[to]
		err := c.saveSendingResult(
			ctx,
			campaignRecipient,
			sendError,
		)
		if err != nil {
			c.Logger.Errorw("failed to save sending result", "error", err)
		}
	}
	// check if most notable event
	err = c.setMostNotableCampaignEvent(
		ctx,
		campaign,
		data.EVENT_CAMPAIGN_ACTIVE,
	)
	if err != nil {
		// err is logged in method call
		return errs.Wrap(err)
	}
	return nil
}

// saveSendingResult saves a result from a send campaign atttempts
func (c *Campaign) saveSendingResult(
	ctx context.Context,
	campaignRecipient *model.CampaignRecipient,
	sendError error,
) error {
	if sendError == nil {
		campaignRecipient.SentAt = nullable.NewNullableWithValue(time.Now())
	}
	campaignRecipientID := campaignRecipient.ID.MustGet()
	err := c.CampaignRecipientRepository.UpdateByID(
		ctx,
		&campaignRecipientID,
		campaignRecipient,
	)
	if err != nil {
		c.Logger.Errorw("failed to update campaign recipient by id", "error", err)
	}
	// persist the event
	id := uuid.New()
	eventName := data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT
	if sendError != nil {
		eventName = data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_FAILED
	}
	eventID := cache.EventIDByName[eventName]
	data := vo.NewEmptyOptionalString1MB()
	if sendError != nil {
		data, err = vo.NewOptionalString1MB(sendError.Error())
		if err != nil {
			return errs.Wrap(fmt.Errorf("failed to create data: %s", err))
		}
	}
	campaignID := campaignRecipient.CampaignID.MustGet()
	recipientID := campaignRecipient.RecipientID.MustGet()
	campaign, err := c.CampaignRepository.GetByID(
		ctx,
		&campaignID,
		&repository.CampaignOption{},
	)
	if err != nil {
		return errs.Wrap(err)
	}
	var campaignEvent *model.CampaignEvent
	if !campaign.IsAnonymous.MustGet() {
		campaignEvent = &model.CampaignEvent{
			ID:          &id,
			CampaignID:  &campaignID,
			RecipientID: &recipientID,
			IP:          vo.NewOptionalString64Must(""),
			UserAgent:   vo.NewOptionalString255Must(""),
			EventID:     eventID,
			Data:        data,
		}
	} else {
		campaignEvent = &model.CampaignEvent{
			ID:          &id,
			CampaignID:  &campaignID,
			RecipientID: nil,
			IP:          vo.NewOptionalString64Must(""),
			UserAgent:   vo.NewOptionalString255Must(""),
			EventID:     eventID,
			Data:        data,
		}
	}
	err = c.CampaignRepository.SaveEvent(ctx, campaignEvent)
	if err != nil {
		return fmt.Errorf("failed to save event: %s", err)
	}
	// handle most notable event
	err = c.SetNotableCampaignRecipientEvent(
		ctx,
		campaignRecipient,
		cache.EventNameByID[eventID.String()],
	)
	if err != nil {
		// logging was done in the previous call
		return errs.Wrap(err)
	}
	// handle webhook
	webhookID, err := c.CampaignRepository.GetWebhookIDByCampaignID(ctx, &campaignID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Logger.Errorw("failed to get webhook id by campaign id", "error", err)
		return errs.Wrap(err)
	}
	if webhookID == nil {
		return nil
	}
	err = c.HandleWebhook(
		ctx,
		webhookID,
		&campaignID,
		&recipientID,
		eventName,
	)
	if err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// saveEventCampaignClose saves an event about closing a campaign
func (c *Campaign) saveEventCampaignClose(
	ctx context.Context,
	campaignID *uuid.UUID,
	reason string,
) error {
	// persist the event
	id := uuid.New()
	r, err := vo.NewOptionalString1MB(reason)
	if err != nil {
		return errs.Wrap(err)
	}
	campaignEvent := &model.CampaignEvent{
		ID:          &id,
		CampaignID:  campaignID,
		RecipientID: nil,
		IP:          vo.NewOptionalString64Must(""),
		UserAgent:   vo.NewOptionalString255Must(""),
		EventID:     cache.EventIDByName[data.EVENT_CAMPAIGN_CLOSED],
		Data:        r,
	}
	err = c.CampaignRepository.SaveEvent(ctx, campaignEvent)
	if err != nil {
		return fmt.Errorf("failed to save event: %s", err)
	}
	// handle webhook
	webhookID, err := c.CampaignRepository.GetWebhookIDByCampaignID(ctx, campaignID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Logger.Errorw("failed to get webhook id by campaign id", "error", err)
		return errs.Wrap(err)
	}
	if webhookID == nil {
		return nil
	}
	err = c.HandleWebhook(
		ctx,
		webhookID,
		campaignID,
		nil,
		data.EVENT_CAMPAIGN_CLOSED,
	)
	if err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// HandleCloseCampaigns closes campaigns that are past their end time
func (c *Campaign) HandleCloseCampaigns(
	ctx context.Context,
	session *model.Session,
) error {
	ae := NewAuditEvent("Campaign.HandleCloseCampaigns", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get all campaigns that are past their end time
	// and not yet closed
	campaigns, err := c.CampaignRepository.GetAllReadyToClose(
		ctx,
		&repository.CampaignOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get closed campaigns", "error", err)
		return errs.Wrap(err)
	}
	// close the campaigns
	closedCampaignIDs := []string{}
	for _, campaign := range campaigns.Rows {
		campaignID := campaign.ID.MustGet()
		closedCampaignIDs = append(closedCampaignIDs, campaignID.String())
		c.Logger.Debugw("closing campaign with id", "campaignID", campaignID)
		var err error
		// if there is no campaign template closing is due to missing template
		campaignTemplateID, err := campaign.TemplateID.Get()
		if err != nil {
			err = c.closeCampaign(
				ctx,
				session,
				&campaignID,
				campaign,
				"Campaign closed due to missing campaign template",
			)
			c.handleCloseError(err, &campaignID)
			return errs.Wrap(err)
		}
		// check if the template is unusable
		cTemplate, err := c.CampaignTemplateService.GetByID(
			ctx,
			session,
			&campaignTemplateID,
			&repository.CampaignTemplateOption{},
		)
		if cTemplate == nil || err != nil {
			err = c.closeCampaign(
				ctx,
				session,
				&campaignID,
				campaign,
				"Campaign closed due to unusable template",
			)
			c.handleCloseError(err, &campaignID)
			return errs.Wrap(err)
		}
		err = c.closeCampaign(
			ctx,
			session,
			&campaignID,
			campaign,
			"Campaign closed due to over close time",
		)
	}
	if len(closedCampaignIDs) > 0 {
		ae.Details["closedCampaignIds"] = closedCampaignIDs
		c.AuditLogAuthorized(ae)
	}
	return nil
}

func (c *Campaign) handleCloseError(err error, campaignID *uuid.UUID) {
	if err != nil && !errors.Is(err, errs.ErrCampaignAlreadyClosed) {
		c.Logger.Errorw("failed to close campaign by id", "error", err)
		return
	}
	if go_errors.Is(err, errs.ErrCampaignAlreadyClosed) {
		c.Logger.Debugw("campaign already closed", "error", err)
		return
	}
	c.Logger.Debugw("closed campaign with id", "campaignID", campaignID)
}

// HandleAnonymizeCampaigns anonymizes campaigns are ready for anonymization
func (c *Campaign) HandleAnonymizeCampaigns(
	ctx context.Context,
	session *model.Session,
) error {
	ae := NewAuditEvent("Campaign.HandleAnonymizeCampaigns", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get all campaigns that are past their end time
	// and not yet closed
	campaigns, err := c.CampaignRepository.GetReadyToAnonymize(
		ctx,
		&repository.CampaignOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get ready to anonymize campaigns", "error", err)
		return errs.Wrap(err)
	}
	// close and anonymize the campaigns
	affectedIds := []string{}
	for _, campaign := range campaigns.Rows {
		campaignID := campaign.ID.MustGet()
		affectedIds = append(affectedIds, campaignID.String())
		c.Logger.Debugw("anonymizing campaign with id", "campaignID", campaignID)
		err = c.AnonymizeByID(ctx, session, &campaignID)

		if err != nil && !errors.Is(err, errs.ErrCampaignAlreadyClosed) {
			c.Logger.Errorw("failed to anonymize campaign by id", "error", err)
			continue
		}
		if errors.Is(err, errs.ErrCampaignAlreadyAnonymized) {
			c.Logger.Debugw("campaign already anonymized", "error", err)
			continue
		}
		c.Logger.Debugw("anonymized campaign with id", "campaignID", campaignID)
	}
	if len(affectedIds) > 0 {
		ae.Details["anonymizedCampaignIds"] = affectedIds
		c.AuditLogAuthorized(ae)
	}
	return nil
}

// CloseCampaignByID closes a campaign by id
func (c *Campaign) CloseCampaignByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Campaign.CloseCampaignByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.Wrap(errs.ErrAuthorizationFailed)
	}
	// get the campaign
	campaign, err := c.CampaignRepository.GetByID(
		ctx,
		id,
		&repository.CampaignOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id: %s", err)
		return errs.Wrap(err)
	}
	err = c.closeCampaign(ctx, session, id, campaign, "Manually closed")
	if err != nil {
		c.Logger.Errorw("failed to close campaign by id", "error", err)
		return errs.Wrap(err)
	}
	c.AuditLogAuthorized(ae)
	return nil
}

// closeCampaign closes a campaign
func (c *Campaign) closeCampaign(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	campaign *model.Campaign,
	reason string,
) error {
	if campaign == nil {
		return errs.NewCustomError(errors.New("campaign is nil"))
	}
	c.Logger.Debugw("closing campaign with id", "id", id.String())
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		return errs.ErrAuthorizationFailed
	}
	// find all recipients that are not sent and cancel them
	campaignRecipients, err := c.CampaignRecipientRepository.GetUnsendRecipients(
		ctx,
		repository.NO_LIMIT,
		&repository.CampaignRecipientOption{},
	)
	c.Logger.Debugw("found unsent recipients to cancel", "count", len(campaignRecipients))
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Logger.Errorw("failed to get unsent recipients", "error", err)
		return errs.Wrap(err)
	}
	campaignRecipientUUIDs := []*uuid.UUID{}
	for _, cr := range campaignRecipients {
		campaignRecipientID := cr.ID.MustGet()
		campaignRecipientUUIDs = append(campaignRecipientUUIDs, &campaignRecipientID)
	}
	err = c.CampaignRecipientRepository.Cancel(ctx, campaignRecipientUUIDs)
	if err != nil {
		c.Logger.Errorw("failed to cancel recipients", "error", err)
		return errs.Wrap(err)
	}
	err = campaign.Closed()
	if go_errors.Is(err, errs.ErrCampaignAlreadyClosed) {
		c.Logger.Debugw("campaign already closed", "error", err)
		return errs.Wrap(err)
	}
	if err != nil {
		c.Logger.Errorw("failed to close campaign by id", "error", err)
		return errs.Wrap(err)
	}
	err = c.CampaignRepository.UpdateByID(ctx, id, campaign)
	if err != nil {
		c.Logger.Errorw("failed to close campaign by id", "error", err)
		return errs.Wrap(err)
	}
	err = c.saveEventCampaignClose(
		ctx,
		id,
		reason,
	)
	if err != nil {
		c.Logger.Errorw("failed to save event about closing campaign", "error", err)
	}
	err = c.setMostNotableCampaignEvent(
		ctx,
		campaign,
		data.EVENT_CAMPAIGN_CLOSED,
	)
	if err != nil {
		// err is logged in method call
		return errs.Wrap(err)
	}

	// Generate campaign statistics when closing (skip test campaigns)
	if !campaign.IsTest.MustGet() {
		c.Logger.Debugf("generating campaign statistics", "campaignID", id.String())
		err = c.GenerateCampaignStats(ctx, session, id)
		if err != nil {
			c.Logger.Errorw("failed to generate campaign statistics", "error", err, "campaignID", id.String())
			// Don't fail the close operation if stats generation fails
		} else {
			c.Logger.Debugf("successfully generated campaign statistics", "campaignID", id.String())
		}
	} else {
		c.Logger.Debugf("skipping stats generation for test campaign", "campaignID", id.String())
	}

	return nil
}

// GetCampaignEmailBody returns the rendered email for a self managed campaign recipient
func (c *Campaign) GetCampaignEmailBody(
	ctx context.Context,
	session *model.Session,
	campaignRecipientID *uuid.UUID,
) (string, error) {
	ae := NewAuditEvent("Campaign.GetCampaignEmailBody", session)
	ae.Details["campaignRecipientId"] = campaignRecipientID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return "", errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return "", errs.ErrAuthorizationFailed
	}
	// check recipient is in a active campaign
	campaignRecipient, err := c.CampaignRecipientRepository.GetByID(
		ctx,
		campaignRecipientID,
		&repository.CampaignRecipientOption{
			WithRecipient: true,
		},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign recipient by id", "error", err)
		return "", errs.Wrap(err)
	}
	if campaignRecipient.RecipientID.IsNull() {
		return "", errs.NewCustomError(
			errors.New("recipient is anonymized"),
		)
	}
	campaignID := campaignRecipient.CampaignID.MustGet()
	campaign, err := c.CampaignRepository.GetByID(
		ctx,
		&campaignID,
		&repository.CampaignOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id", "error", err)
		return "", errs.Wrap(err)
	}
	templateID, err := campaign.TemplateID.Get()
	if err != nil {
		c.Logger.Errorw("failed to get template from campaign, has no template", "error", err)
		return "", errs.Wrap(err)
	}
	cTemplate, err := c.CampaignTemplateService.GetByID(
		ctx,
		session,
		&templateID,
		&repository.CampaignTemplateOption{
			WithIdentifier:         true,
			WithBeforeLandingProxy: true,
			WithLandingProxy:       true,
		},
	)
	emailID, err := cTemplate.EmailID.Get()
	if err != nil {
		c.Logger.Infow("failed email from template - template ID", "templateID", templateID)
		return "", errs.NewValidationError(
			errors.New("Campaign template has no email"),
		)
	}
	// get the email
	// get campaign's company context for attachment filtering
	var campaignCompanyID *uuid.UUID
	if campaign.CompanyID.IsSpecified() && !campaign.CompanyID.IsNull() {
		companyID := campaign.CompanyID.MustGet()
		campaignCompanyID = &companyID
	}

	email, err := c.MailService.GetByID(
		ctx,
		session,
		&emailID,
		campaignCompanyID,
	)
	if err != nil {
		c.Logger.Errorw("failed to get message by id", "error", err)
		return "", errs.Wrap(err)
	}
	urlIdentifier := cTemplate.URLIdentifier
	if urlIdentifier == nil {
		return "", errors.New("url identifier is nil")
	}

	// get template domain for assets and tracking pixel
	domainID, err := cTemplate.DomainID.Get()
	if err != nil {
		c.Logger.Infow("failed domain from template - template ID", "templateID", templateID)
		return "", errs.NewValidationError(
			errors.New("Campaign template has no domain"),
		)
	}
	domain, err := c.DomainService.GetByID(
		ctx,
		session,
		&domainID,
		&repository.DomainOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get domain by id", "error", err)
		return "", errs.Wrap(err)
	}
	urlPath := cTemplate.URLPath.MustGet().String()

	// generate custom campaign URL if first page is MITM
	customCampaignURL, err := c.GetLandingPageURLByCampaignRecipientID(ctx, session, campaignRecipientID)
	if err != nil {
		c.Logger.Errorw("failed to get campaign url", "error", err)
		return "", errs.Wrap(err)
	}

	// no audit on read
	return c.TemplateService.CreateMailBodyWithCustomURL(
		urlIdentifier.Name.MustGet(),
		urlPath,
		domain,
		campaignRecipient,
		email,
		nil,
		customCampaignURL,
	)
}

// GetLandingPageURLByCampaignRecipientID generates the lure URL for a campaign recipient.
// if the first page in the campaign flow is a mitm proxy, the url goes directly to the
// mitm domain instead of redirecting through the template domain.
func (c *Campaign) GetLandingPageURLByCampaignRecipientID(
	ctx context.Context,
	session *model.Session,
	campaignRecipientID *uuid.UUID,
) (string, error) {
	ae := NewAuditEvent("Campaign.GetLandingPageURLByCampaignRecipientID", session)
	ae.Details["campaignRecipientId"] = campaignRecipientID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return "", errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return "", errs.ErrAuthorizationFailed
	}
	campaignRecipient, err := c.CampaignRecipientRepository.GetByID(
		ctx,
		campaignRecipientID,
		&repository.CampaignRecipientOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign recipient by id", "error", err)
		return "", errs.Wrap(err)
	}
	campaignID := campaignRecipient.CampaignID.MustGet()
	campaign, err := c.CampaignRepository.GetByID(
		ctx,
		&campaignID,
		&repository.CampaignOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id", "error", err)
		return "", errs.Wrap(err)
	}
	templateID, err := campaign.TemplateID.Get()
	if err != nil {
		c.Logger.Errorw("failed to get campaign template, campaign has no template", "error", err)
		return "", errs.Wrap(err)
	}
	cTemplate, err := c.CampaignTemplateService.GetByID(
		ctx,
		session,
		&templateID,
		&repository.CampaignTemplateOption{
			WithIdentifier:         true,
			WithBeforeLandingProxy: true,
			WithLandingProxy:       true,
		},
	)
	// determine if we should use mitm domain for first page
	var baseURL string
	var urlPath string
	idIdentifier := cTemplate.URLIdentifier.Name.MustGet()

	// check if first page is a mitm proxy
	firstPageProxy := c.getFirstPageProxy(cTemplate)
	if firstPageProxy != nil {
		// get the phishing domain for this proxy
		phishingDomain, err := c.getPhishingDomainForProxy(ctx, firstPageProxy)
		if err != nil {
			c.Logger.Errorw("failed to get phishing domain for first page proxy", "error", err)
			// fallback to template domain
			firstPageProxy = nil
		} else {
			// use phishing domain directly
			startURL, err := firstPageProxy.StartURL.Get()
			if err != nil {
				c.Logger.Errorw("failed to get start url from first page proxy", "error", err)
				return "", errs.Wrap(err)
			}
			parsedStartURL, err := url.Parse(startURL.String())
			if err != nil {
				c.Logger.Errorw("failed to parse start url from first page proxy", "error", err)
				return "", errs.Wrap(err)
			}
			baseURL = "https://" + phishingDomain
			urlPath = parsedStartURL.Path
		}
	}

	if firstPageProxy == nil {
		// use template domain (current behavior)
		domainID, err := cTemplate.DomainID.Get()
		if err != nil {
			c.Logger.Infow("failed email from template - template ID", "templateID", templateID)
			return "", errs.NewValidationError(
				errors.New("Campaign template has no email"),
			)
		}
		domain, err := c.DomainService.GetByID(
			ctx,
			session,
			&domainID,
			&repository.DomainOption{},
		)
		if err != nil {
			c.Logger.Errorw("failed to get domain by id", err)
			return "", errs.Wrap(err)
		}
		urlPath = cTemplate.URLPath.MustGet().String()
		baseURL = "https://" + domain.Name.MustGet().String()
	}

	// build final url
	separator := "?"
	if strings.Contains(baseURL, "?") {
		separator = "&"
	}
	url := fmt.Sprintf("%s%s%s%s=%s", baseURL, urlPath, separator, idIdentifier, campaignRecipientID.String())
	// no audit on read
	return url, nil
}

// getFirstPageProxy returns the proxy for the first page in the campaign flow
// returns nil if the first page is not a proxy
func (c *Campaign) getFirstPageProxy(template *model.CampaignTemplate) *model.Proxy {
	if template.BeforeLandingPageID.IsNull() != true {
		return nil
	}
	if template.BeforeLandingProxyID.IsNull() != true {
		return template.BeforeLandingProxy
	}
	if template.LandingPageID.IsNull() != true {
		return nil
	}
	if template.LandingProxyID.IsNull() != true {
		return template.LandingProxy
	}
	if template.AfterLandingPageID.IsNull() != true {
		return nil
	}
	if template.AfterLandingProxyID.IsNull() != true {
		return template.AfterLandingProxy
	}
	return nil
}

// getPhishingDomainForProxy finds the phishing domain that maps to the proxy's start url
func (c *Campaign) getPhishingDomainForProxy(ctx context.Context, proxy *model.Proxy) (string, error) {
	startURL, err := proxy.StartURL.Get()
	if err != nil {
		return "", fmt.Errorf("failed to get start url from proxy: %w", err)
	}

	proxyConfig, err := proxy.ProxyConfig.Get()
	if err != nil {
		return "", fmt.Errorf("failed to get proxy config: %w", err)
	}

	// parse the proxy configuration to find domain mappings
	var rawConfig map[string]interface{}
	err = yaml.Unmarshal([]byte(proxyConfig.String()), &rawConfig)
	if err != nil {
		return "", fmt.Errorf("failed to parse proxy config yaml: %w", err)
	}

	// parse the start URL to get the target domain
	parsedStartURL, err := url.Parse(startURL.String())
	if err != nil {
		return "", fmt.Errorf("failed to parse start url: %w", err)
	}
	startDomain := parsedStartURL.Host

	// find the phishing domain mapping for the start URL domain
	for originalHost, domainData := range rawConfig {
		if originalHost == "proxy" || originalHost == "global" {
			continue
		}
		if originalHost == startDomain {
			if domainMap, ok := domainData.(map[string]interface{}); ok {
				if to, exists := domainMap["to"]; exists {
					if toStr, ok := to.(string); ok {
						return toStr, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("no phishing domain mapping found for start url domain: %s", startDomain)
}

// SetSentAtByCampaignRecipientID sets the sent at time for a recipient
func (c *Campaign) SetSentAtByCampaignRecipientID(
	ctx context.Context,
	session *model.Session,
	campaignRecipientID *uuid.UUID,
) error {
	ae := NewAuditEvent("Campaign.SetSentAtByCampaignRecipientID", session)
	ae.Details["campaignRecipientId"] = campaignRecipientID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get campaignRecipient
	campaignRecipient, err := c.CampaignRecipientRepository.GetByID(
		ctx,
		campaignRecipientID,
		&repository.CampaignRecipientOption{
			WithCampaign: true,
		},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign recipient by id", "error", err)
		return errs.Wrap(err)
	}
	campaign := campaignRecipient.Campaign
	// check if the campaign recipient is in a active campaign
	if !campaign.IsActive() {
		c.Logger.Debugw("failed to cancel campaign recipient by id: campaign is inactive",
			"campaignID", campaign.ID.MustGet().String(),
		)
		return errors.New("campaign is inactive")
	}

	if !campaignRecipient.CancelledAt.IsNull() &&
		campaignRecipient.CancelledAt.MustGet().Before(time.Now()) {
		c.Logger.Debugw("failed to cancel campaign recipient by id: already cancelled",
			"campaignrecipientID", campaignRecipientID.String(),
		)
		return errors.New("campaign recipient already cancelled")
	}
	campaignRecipient.SentAt = nullable.NewNullableWithValue(time.Now())
	err = c.CampaignRecipientRepository.UpdateByID(
		ctx,
		campaignRecipientID,
		campaignRecipient,
	)
	if err != nil {
		c.Logger.Errorw("wailed to cancel campaign recipient by recipient id", "error", err)
		return errs.Wrap(err)
	}
	// create an event for the sent email
	id := uuid.New()
	campaignID := campaignRecipient.CampaignID.MustGet()
	recipientID := campaignRecipient.RecipientID.MustGet()

	var campaignEvent *model.CampaignEvent

	if campaign.IsAnonymous.MustGet() {
		campaignEvent = &model.CampaignEvent{
			ID:          &id,
			CampaignID:  &campaignID,
			RecipientID: nil,
			IP:          vo.NewOptionalString64Must(""),
			UserAgent:   vo.NewOptionalString255Must(""),
			EventID:     cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT],
			Data:        vo.NewEmptyOptionalString1MB(),
		}
	} else {
		campaignEvent = &model.CampaignEvent{
			ID:          &id,
			CampaignID:  &campaignID,
			RecipientID: &recipientID,
			IP:          vo.NewOptionalString64Must(""),
			UserAgent:   vo.NewOptionalString255Must(""),
			EventID:     cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT],
			Data:        vo.NewEmptyOptionalString1MB(),
		}
	}

	err = c.CampaignRepository.SaveEvent(ctx, campaignEvent)
	if err != nil {
		return errs.Wrap(err)
	}
	c.AuditLogAuthorized(ae)
	// handle webhook
	webhookID, err := c.CampaignRepository.GetWebhookIDByCampaignID(ctx, &campaignID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		c.Logger.Errorw("failed to get webhook id by campaign id", "error", err)
		return errs.Wrap(err)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || webhookID == nil {
		return nil
	}
	err = c.HandleWebhook(
		ctx,
		webhookID,
		&campaignID,
		&recipientID,
		data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT,
	)
	if err != nil {
		return errs.Wrap(err)
	}
	return nil
}

// HandleWebhook handles a webhook
// it must only be called from secure contexts as it is not checked for permissions
func (c *Campaign) HandleWebhook(
	ctx context.Context,
	webhookID *uuid.UUID,
	campaignID *uuid.UUID,
	recipientID *uuid.UUID,
	eventName string,
) error {
	campaignName, err := c.CampaignRepository.GetNameByID(ctx, campaignID)
	if err != nil {
		return errs.Wrap(err)
	}
	email, err := c.RecipientRepository.GetEmailByID(ctx, recipientID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return errs.Wrap(err)
	}
	webhook, err := c.WebhookRepository.GetByID(ctx, webhookID)
	if err != nil {
		return errs.Wrap(err)
	}
	now := time.Now()
	webhookReq := WebhookRequest{
		Time:         &now,
		CampaignName: campaignName,
		Event:        eventName,
	}
	if email != nil {
		webhookReq.Email = email.String()
	}
	// the webhook is handles as a different go routine
	// so we don't block the campaign handling thread
	go func() {
		c.Logger.Debugw("sending webhook", "url", webhook.URL.MustGet().String())
		_, err := c.WebhookService.Send(ctx, webhook, &webhookReq)
		if err != nil {
			c.Logger.Errorw("failed to send webhook", "error", err)
		}
		c.Logger.Debugw("sending webhook completed", "url", webhook.URL.MustGet().String())
	}()
	return nil
}

// AnonymizeByID anonymizes a campaign including the events
func (c *Campaign) AnonymizeByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Campaign.AnonymizeByID", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		return errs.ErrAuthorizationFailed
	}
	// get campaign to check it exists and etc
	campaign, err := c.CampaignRepository.GetByID(
		ctx,
		id,
		&repository.CampaignOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id", "error", err)
		return errs.Wrap(err)
	}
	// check if campaign is active, cause then it should be closed before continueing
	if campaign.IsActive() {
		err = c.closeCampaign(
			ctx,
			session,
			id,
			campaign,
			"campaign is not active",
		)
	}
	if err != nil {
		c.Logger.Errorw("failed to close campaign by id before anonymization", "error", err)
		return errs.Wrap(err)
	}
	// assign a anonymized ID to each campaign recipient and make a map between
	// their ID and the anonymized ID, this is a itermidiate step to anonymize the events
	// where campaign receipients have both a anonymized ID and the recipient ID
	campaignRecipients, err := c.CampaignRecipientRepository.GetByCampaignID(
		ctx,
		id,
		&repository.CampaignRecipientOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign recipients by campaign id", "error", err)
		return errs.Wrap(err)
	}
	for _, cr := range campaignRecipients {
		if cr.RecipientID.IsNull() {
			c.Logger.Debug("skipping anonymization of campaign recipient without recipient")
			continue
		}
		// add anonymized ID to each campaign recipient
		anonymizedID := uuid.New()
		cr.AnonymizedID = nullable.NewNullableWithValue(anonymizedID)
		recipientID := cr.RecipientID.MustGet()
		err := c.CampaignRecipientRepository.Anonymize(ctx, &recipientID, &anonymizedID)
		if err != nil {
			c.Logger.Errorw("failed to add anonymized ID to campaign recipient", "error", err)
			return errs.Wrap(err)
		}
		// anonymize events and assign each anonymized ID so the events can still be tracked
		campaignID, err := cr.CampaignID.Get()
		if err != nil {
			c.Logger.Debug("Recipient removed or anonymized, skipping in anonymization")
			continue
		}
		err = c.CampaignRepository.AnonymizeCampaignEvent(
			ctx,
			&campaignID,
			&recipientID,
			&anonymizedID,
		)
		if err != nil {
			c.Logger.Errorw("failed to anonymize campaign event", "error", err)
			return errs.Wrap(err)
		}
	}
	// delete the relation between the campaign and the recipient groups
	err = c.CampaignRepository.RemoveCampaignRecipientGroups(ctx, id)
	if err != nil {
		c.Logger.Errorw("failed to delete campaign recipient groups by campaign id", "error", err)
		return errs.Wrap(err)
	}
	// remove the recipient ID from the campaign recipient so only the anomymized ID is left
	err = c.CampaignRecipientRepository.RemoveRecipientIDByCampaignID(ctx, id)
	if err != nil {
		c.Logger.Errorw("failed to remove recipient ID from campaign recipients", "error", err)
		return errs.Wrap(err)
	}
	// finally add a timestamp to the campaign to indicate when it was anonymized
	err = c.CampaignRepository.AddAnonymizedAt(ctx, id)
	c.AuditLogAuthorized(ae)

	return nil
}

// SendEmailByCampaignRecipientID sends an email to a specific campaign recipient
// Multiple sends to the same recipient are allowed to support retry scenarios and follow-ups.
func (c *Campaign) SendEmailByCampaignRecipientID(
	ctx context.Context,
	session *model.Session,
	campaignRecipientID *uuid.UUID,
) error {
	ae := NewAuditEvent("Campaign.SendEmailByCampaignRecipientID", session)
	ae.Details["campaignRecipientId"] = campaignRecipientID.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}

	// get campaign recipient
	campaignRecipient, err := c.CampaignRecipientRepository.GetByID(
		ctx,
		campaignRecipientID,
		&repository.CampaignRecipientOption{
			WithRecipient: true,
			WithCampaign:  true,
		},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign recipient by id", "error", err)
		return errs.Wrap(err)
	}

	campaign := campaignRecipient.Campaign
	if campaign == nil {
		return errors.New("campaign recipient has no campaign loaded")
	}

	// check if campaign is active
	if !campaign.IsActive() {
		return errors.New("campaign is not active")
	}

	// check if recipient exists (not anonymized)
	if campaignRecipient.Recipient == nil {
		return errors.New("recipient is anonymized or deleted")
	}

	// check if cancelled
	if !campaignRecipient.CancelledAt.IsNull() {
		return errors.New("recipient has been cancelled")
	}

	campaignID := campaign.ID.MustGet()

	// add resend information to audit log
	isResend := !campaignRecipient.SentAt.IsNull()
	ae.Details["isResend"] = isResend
	if isResend {
		ae.Details["previouslySentAt"] = campaignRecipient.SentAt.MustGet().Format(time.RFC3339)
	}

	// send the email using existing logic from sendCampaignMessages
	err = c.sendSingleCampaignMessage(ctx, session, &campaignID, campaignRecipient)
	if err != nil {
		c.Logger.Errorw("failed to send campaign message", "error", err)
		return errs.Wrap(err)
	}

	c.AuditLogAuthorized(ae)
	return nil
}

// sendSingleCampaignMessage sends an email to a single campaign recipient
func (c *Campaign) sendSingleCampaignMessage(
	ctx context.Context,
	session *model.Session,
	campaignID *uuid.UUID,
	campaignRecipient *model.CampaignRecipient,
) error {
	// get campaign template details - similar logic from sendCampaignMessages
	campaign, err := c.CampaignRepository.GetByID(
		ctx,
		campaignID,
		&repository.CampaignOption{},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id", "error", err)
		return errs.Wrap(err)
	}

	templateID, err := campaign.TemplateID.Get()
	if err != nil {
		return errors.New("campaign has no template")
	}

	cTemplate, err := c.CampaignTemplateService.GetByID(
		ctx,
		session,
		&templateID,
		&repository.CampaignTemplateOption{
			WithDomain:             true,
			WithSMTPConfiguration:  true,
			WithIdentifier:         true,
			WithBeforeLandingProxy: true,
			WithLandingProxy:       true,
		},
	)
	if err != nil {
		c.Logger.Errorw("failed to get campaign template by id", "error", err)
		return errs.Wrap(err)
	}

	// check domain
	domain := cTemplate.Domain
	if domain == nil {
		return errors.New("campaign template has no domain")
	}

	// get email details
	emailID, err := cTemplate.EmailID.Get()
	if err != nil {
		return errors.New("campaign template has no email")
	}

	// get campaign's company context for attachment filtering
	var campaignCompanyID *uuid.UUID
	if campaign.CompanyID.IsSpecified() && !campaign.CompanyID.IsNull() {
		companyID := campaign.CompanyID.MustGet()
		campaignCompanyID = &companyID
	}

	email, err := c.MailService.GetByID(ctx, session, &emailID, campaignCompanyID)
	if err != nil {
		c.Logger.Errorw("failed to get email by id", "error", err)
		return errs.Wrap(err)
	}

	// update last attempt timestamp
	campaignRecipientID := campaignRecipient.ID.MustGet()
	campaignRecipient.LastAttemptAt = nullable.NewNullableWithValue(time.Now())
	err = c.CampaignRecipientRepository.UpdateByID(ctx, &campaignRecipientID, campaignRecipient)
	if err != nil {
		c.Logger.Errorw("failed to update last attempted at", "error", err)
		return errs.Wrap(err)
	}

	// prepare template for rendering
	content, err := email.Content.Get()
	if err != nil {
		return errors.New("failed to get email content")
	}

	t := template.New("email")
	t = t.Funcs(TemplateFuncs())
	mailTmpl, err := t.Parse(content.String())
	if err != nil {
		return errs.Wrap(err)
	}

	// check sending method
	isSmtpCampaign := cTemplate.SMTPConfigurationID.IsSpecified() && !cTemplate.SMTPConfigurationID.IsNull()
	isAPISenderCampaign := cTemplate.APISenderID.IsSpecified() && !cTemplate.APISenderID.IsNull()

	if !isSmtpCampaign && !isAPISenderCampaign {
		return errors.New("campaign template has no SMTP configuration or API sender")
	}

	if isAPISenderCampaign {
		// generate custom campaign URL if first page is MITM
		recipientID := campaignRecipient.ID.MustGet()
		customCampaignURL, urlErr := c.GetLandingPageURLByCampaignRecipientID(ctx, session, &recipientID)
		if urlErr != nil {
			c.Logger.Errorw("failed to get campaign url for API sender", "error", urlErr)
			customCampaignURL = ""
		}

		// send via API with custom URL (domain and template stay the same for assets)
		err = c.APISenderService.SendWithCustomURL(
			ctx,
			session,
			cTemplate,
			campaignRecipient,
			domain,
			mailTmpl,
			email,
			customCampaignURL,
		)
	} else {
		// send via SMTP
		err = c.sendSingleEmailSMTP(ctx, session, cTemplate, campaignRecipient, domain, mailTmpl, email)
	}

	// save sending result
	saveErr := c.saveSendingResult(ctx, campaignRecipient, err)
	if saveErr != nil {
		c.Logger.Errorw("failed to save sending result", "error", saveErr)
		return errs.Wrap(saveErr)
	}

	return err
}

// sendSingleEmailSMTP sends an email to a single recipient via SMTP
func (c *Campaign) sendSingleEmailSMTP(
	ctx context.Context,
	session *model.Session,
	cTemplate *model.CampaignTemplate,
	campaignRecipient *model.CampaignRecipient,
	domain *model.Domain,
	mailTmpl *template.Template,
	email *model.Email,
) error {
	// get SMTP configuration
	smtpConfigID, err := cTemplate.SMTPConfigurationID.Get()
	if err != nil {
		return errors.New("failed to get SMTP configuration from template")
	}

	smtpConfig, err := c.SMTPConfigService.GetByID(
		ctx,
		session, // use the actual session passed to the method
		&smtpConfigID,
		&repository.SMTPConfigurationOption{
			WithHeaders: true,
		},
	)
	if err != nil {
		c.Logger.Errorw("smtp configuration did not load", "error", err)
		return errs.Wrap(err)
	}

	smtpPort, err := smtpConfig.Port.Get()
	if err != nil {
		return errs.Wrap(err)
	}

	smtpHost, err := smtpConfig.Host.Get()
	if err != nil {
		return errs.Wrap(err)
	}

	smtpIgnoreCertErrors, err := smtpConfig.IgnoreCertErrors.Get()
	if err != nil {
		return errs.Wrap(err)
	}

	// setup SMTP client options
	emailOptions := []mail.Option{
		mail.WithPort(smtpPort.Int()),
		mail.WithTLSConfig(
			&tls.Config{
				ServerName:         smtpHost.String(),
				InsecureSkipVerify: smtpIgnoreCertErrors,
			},
		),
	}

	// setup authentication if provided
	username, err := smtpConfig.Username.Get()
	if err != nil {
		return errs.Wrap(err)
	}
	password, err := smtpConfig.Password.Get()
	if err != nil {
		return errs.Wrap(err)
	}

	if un := username.String(); len(un) > 0 {
		emailOptions = append(emailOptions, mail.WithUsername(un))
		if pw := password.String(); len(pw) > 0 {
			emailOptions = append(emailOptions, mail.WithPassword(pw))
		}
	}

	// create message
	messageOptions := []mail.MsgOption{
		mail.WithNoDefaultUserAgent(),
	}
	m := mail.NewMsg(messageOptions...)

	// set envelope from
	err = m.EnvelopeFrom(email.MailEnvelopeFrom.MustGet().String())
	if err != nil {
		c.Logger.Errorw("failed to set envelope from", "error", err)
		return errs.Wrap(err)
	}

	// set headers
	err = m.From(email.MailHeaderFrom.MustGet().String())
	if err != nil {
		c.Logger.Errorw("failed to set mail header 'From'", "error", err)
		return errs.Wrap(err)
	}

	recpEmail := campaignRecipient.Recipient.Email.MustGet().String()
	err = m.To(recpEmail)
	if err != nil {
		c.Logger.Errorw("failed to set mail header 'To'", "error", err)
		return errs.Wrap(err)
	}

	// custom headers
	if headers := smtpConfig.Headers; headers != nil {
		for _, header := range headers {
			key := header.Key.MustGet()
			value := header.Value.MustGet()
			m.SetGenHeader(
				mail.Header(key.String()),
				value.String(),
			)
		}
	}

	m.Subject(email.MailHeaderSubject.MustGet().String())

	// setup template variables
	urlIdentifier := cTemplate.URLIdentifier
	if urlIdentifier == nil {
		return errors.New("url identifier must be loaded for the campaign template")
	}

	// get template domain for assets and tracking pixel
	domainName, err := domain.Name.Get()
	if err != nil {
		return errs.Wrap(err)
	}
	urlPath := cTemplate.URLPath.MustGet().String()

	// generate custom campaign URL if first page is MITM
	recipientID := campaignRecipient.ID.MustGet()
	customCampaignURL, err := c.GetLandingPageURLByCampaignRecipientID(ctx, session, &recipientID)
	if err != nil {
		c.Logger.Errorw("failed to get campaign url", "error", err)
		return errs.Wrap(err)
	}

	t := c.TemplateService.CreateMail(
		domainName.String(),
		urlIdentifier.Name.MustGet(),
		urlPath,
		campaignRecipient,
		email,
		nil,
	)

	// override campaign URL if it's different from template domain URL
	templateURL := fmt.Sprintf("https://%s%s?%s=%s", domainName.String(), urlPath, urlIdentifier.Name.MustGet(), recipientID.String())
	if customCampaignURL != templateURL {
		(*t)["URL"] = customCampaignURL
	}

	var bodyBuffer bytes.Buffer
	err = mailTmpl.Execute(&bodyBuffer, t)
	if err != nil {
		c.Logger.Errorw("failed to execute mail template", "error", err)
		return errs.Wrap(err)
	}
	m.SetBodyString("text/html", bodyBuffer.String())

	// handle attachments
	attachments := email.Attachments
	for _, attachment := range attachments {
		p, err := c.MailService.AttachmentService.GetPath(attachment)
		if err != nil {
			return fmt.Errorf("failed to get attachment path: %s", err)
		}
		if !attachment.EmbeddedContent.MustGet() {
			m.AttachFile(p.String())
		} else {
			attachmentContent, err := os.ReadFile(p.String())
			if err != nil {
				return errs.Wrap(err)
			}
			// setup attachment for executing as email template
			attachmentAsEmail := model.Email{
				ID:                email.ID,
				CreatedAt:         email.CreatedAt,
				UpdatedAt:         email.UpdatedAt,
				Name:              email.Name,
				MailEnvelopeFrom:  email.MailEnvelopeFrom,
				MailHeaderFrom:    email.MailHeaderFrom,
				MailHeaderSubject: email.MailHeaderSubject,
				Content:           email.Content,
				AddTrackingPixel:  email.AddTrackingPixel,
				CompanyID:         email.CompanyID,
				Attachments:       email.Attachments,
				Company:           email.Company,
			}
			attachmentAsEmail.Content = nullable.NewNullableWithValue(
				*vo.NewUnsafeOptionalString1MB(string(attachmentContent)),
			)
			// generate custom campaign URL for attachment
			recipientID := campaignRecipient.ID.MustGet()
			customCampaignURL, err := c.GetLandingPageURLByCampaignRecipientID(ctx, session, &recipientID)
			if err != nil {
				c.Logger.Errorw("failed to get campaign url for attachment", "error", err)
				return errs.Wrap(err)
			}

			attachmentStr, err := c.TemplateService.CreateMailBodyWithCustomURL(
				urlIdentifier.Name.MustGet(),
				urlPath,
				domain,
				campaignRecipient,
				&attachmentAsEmail,
				nil,
				customCampaignURL,
			)
			if err != nil {
				return errs.Wrap(fmt.Errorf("failed to setup attachment with embedded content: %s", err))
			}
			m.AttachReadSeeker(
				filepath.Base(p.String()),
				strings.NewReader(attachmentStr),
			)
		}
	}

	// send the email
	var mc *mail.Client

	// try different authentication methods
	if un := username.String(); len(un) > 0 {
		// try CRAM-MD5 first when credentials are provided
		emailOptionsCRAM5 := append(emailOptions, mail.WithSMTPAuth(mail.SMTPAuthCramMD5))
		mc, _ = mail.NewClient(smtpHost.String(), emailOptionsCRAM5...)
		mc.SetLogger(log.NewGoMailLoggerAdapter(c.Logger))
		mc.SetDebugLog(true)
		if build.Flags.Production {
			mc.SetTLSPolicy(mail.TLSMandatory)
		} else {
			mc.SetTLSPolicy(mail.TLSOpportunistic)
		}
		err = mc.DialAndSendWithContext(ctx, m)

		// check if it's an authentication error and try PLAIN auth
		if err != nil && (strings.Contains(err.Error(), "535 ") ||
			strings.Contains(err.Error(), "534 ") ||
			strings.Contains(err.Error(), "538 ") ||
			strings.Contains(err.Error(), "CRAM-MD5") ||
			strings.Contains(err.Error(), "authentication failed")) {
			c.Logger.Debugf("CRAM-MD5 authentication failed, trying PLAIN auth", "error", err)
			emailOptionsBasic := emailOptions
			emailOptionsBasic = append(emailOptions, mail.WithSMTPAuth(mail.SMTPAuthPlain))
			mc, _ = mail.NewClient(smtpHost.String(), emailOptionsBasic...)
			mc.SetLogger(log.NewGoMailLoggerAdapter(c.Logger))
			mc.SetDebugLog(true)
			if build.Flags.Production {
				mc.SetTLSPolicy(mail.TLSMandatory)
			} else {
				mc.SetTLSPolicy(mail.TLSOpportunistic)
			}
			err = mc.DialAndSendWithContext(ctx, m)
		}
	} else {
		// no credentials provided, try without authentication
		mc, _ = mail.NewClient(smtpHost.String(), emailOptions...)
		mc.SetLogger(log.NewGoMailLoggerAdapter(c.Logger))
		mc.SetDebugLog(true)
		if build.Flags.Production {
			mc.SetTLSPolicy(mail.TLSMandatory)
		} else {
			mc.SetTLSPolicy(mail.TLSOpportunistic)
		}
		err = mc.DialAndSendWithContext(ctx, m)

		// if no-auth fails and we get an auth-related error, log it appropriately
		if err != nil && (strings.Contains(err.Error(), "530 ") ||
			strings.Contains(err.Error(), "535 ") ||
			strings.Contains(err.Error(), "authentication required") ||
			strings.Contains(err.Error(), "AUTH")) {
			c.Logger.Warnw("Server requires authentication but no credentials provided", "error", err)
		}
	}

	if err != nil {
		c.Logger.Errorw("failed to send email", "error", err)
		return errs.Wrap(err)
	}

	return nil
}

// SetNotableCampaignEvent checks and update if most notable event for a campaign
func (c *Campaign) setMostNotableCampaignEvent(
	ctx context.Context,
	campaign *model.Campaign,
	eventName string,
) error {
	currentEventID, _ := campaign.NotableEventID.Get()
	notableEventID, _ := cache.EventIDByName[eventName]
	if cache.IsMoreNotableCampaignRecipientEventID(
		&currentEventID,
		notableEventID,
	) {
		campaign.NotableEventID.Set(*notableEventID)
		cid := campaign.ID.MustGet()
		err := c.CampaignRepository.UpdateByID(
			ctx,
			&cid,
			campaign,
		)
		if err != nil {
			c.Logger.Errorw("failed to update notable campaign event", "error", err)
			return errs.Wrap(err)
		}
	}
	return nil
}

// SetNotableCampaignRecipientEvent checks and update if most notable event for campaign recipient
func (c *Campaign) SetNotableCampaignRecipientEvent(
	ctx context.Context,
	campaignRecipient *model.CampaignRecipient,
	eventName string,
) error {
	currentNotableEventID, _ := campaignRecipient.NotableEventID.Get()
	notableEventID, _ := cache.EventIDByName[eventName]
	if cache.IsMoreNotableCampaignRecipientEventID(
		&currentNotableEventID,
		notableEventID,
	) {
		campaignRecipient.NotableEventID.Set(*notableEventID)
		crid := campaignRecipient.ID.MustGet()
		err := c.CampaignRecipientRepository.UpdateByID(
			ctx,
			&crid,
			campaignRecipient,
		)
		if err != nil {
			c.Logger.Errorw("failed to save updating notable campaign recipient event", "error", err)
			return errs.Wrap(err)
		}
	}
	return nil
}

// GenerateCampaignStats generates and stores campaign statistics when a campaign is closed
func (c *Campaign) GenerateCampaignStats(ctx context.Context, session *model.Session, campaignID *uuid.UUID) error {
	c.Logger.Debugw("starting campaign stats generation", "campaignID", campaignID.String())

	// Check if stats already exist for this campaign to prevent duplicates
	existingStats, err := c.CampaignRepository.GetCampaignStats(ctx, campaignID)
	if err == nil && existingStats != nil {
		c.Logger.Debugw("campaign stats already exist, skipping generation", "campaignID", campaignID.String())
		return nil
	}
	// Continue if record not found or table doesn't exist (which is expected for new stats)
	c.Logger.Debugw("no existing stats found, proceeding with generation", "campaignID", campaignID.String(), "checkError", err)

	// Get the campaign without joins to avoid SQL ambiguity
	campaign, err := c.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{})
	if err != nil {
		c.Logger.Errorw("failed to get campaign for stats", "error", err, "campaignID", campaignID.String())
		return errs.Wrap(err)
	}

	campaignName := campaign.Name.MustGet().String()
	c.Logger.Debugf("retrieved campaign for stats", "campaignID", campaignID.String(), "campaignName", campaignName)

	// Get campaign result stats (existing method)
	resultStats, err := c.CampaignRepository.GetResultStats(ctx, campaignID)
	if err != nil {
		c.Logger.Errorw("failed to get result stats", "error", err, "campaignID", campaignID.String())
		return errs.Wrap(err)
	}
	c.Logger.Debugf("retrieved result stats", "campaignID", campaignID.String(), "recipients", resultStats.Recipients)

	// Calculate rates
	openRate := float64(0)
	clickRate := float64(0)
	submissionRate := float64(0)
	reportRate := float64(0)

	if resultStats.Recipients > 0 {
		openRate = (float64(resultStats.TrackingPixelLoaded) / float64(resultStats.Recipients)) * 100
		clickRate = (float64(resultStats.WebsiteLoaded) / float64(resultStats.Recipients)) * 100
		submissionRate = (float64(resultStats.SubmittedData) / float64(resultStats.Recipients)) * 100
		reportRate = (float64(resultStats.Reported) / float64(resultStats.Recipients)) * 100
	}

	// Determine campaign type
	campaignType := "scheduled"
	if campaign.SendStartAt == nil && campaign.SendEndAt == nil {
		campaignType = "self-managed"
	}

	// Get template name with proper session
	templateName := ""
	templateID := campaign.TemplateID.MustGet()
	template, err := c.CampaignTemplateService.GetByID(ctx, session, &templateID, &repository.CampaignTemplateOption{})
	if err == nil && template != nil && !template.Name.IsNull() {
		templateName = template.Name.MustGet().String()
	}

	// Create time pointers
	now := time.Now()

	var companyID *uuid.UUID
	if !campaign.CompanyID.IsNull() {
		id := campaign.CompanyID.MustGet()
		companyID = &id
	}
	// companyID can be nil for global campaigns

	var sendStartAt *time.Time
	if !campaign.SendStartAt.IsNull() {
		t := campaign.SendStartAt.MustGet()
		sendStartAt = &t
	}

	var sendEndAt *time.Time
	if !campaign.SendEndAt.IsNull() {
		t := campaign.SendEndAt.MustGet()
		sendEndAt = &t
	}

	var closedAt *time.Time
	if !campaign.ClosedAt.IsNull() {
		t := campaign.ClosedAt.MustGet()
		closedAt = &t
	}

	// Create campaign stats record
	id := uuid.New()
	stats := &database.CampaignStats{
		ID:                  &id,
		CampaignID:          campaignID,
		CampaignName:        campaignName,
		CompanyID:           companyID,
		CampaignStartDate:   sendStartAt,
		CampaignEndDate:     sendEndAt,
		CampaignClosedAt:    closedAt,
		TotalRecipients:     int(resultStats.Recipients),
		TotalEvents:         0, // Will be calculated from events
		EmailsSent:          int(resultStats.EmailsSent),
		TrackingPixelLoaded: int(resultStats.TrackingPixelLoaded),
		WebsiteVisits:       int(resultStats.WebsiteLoaded),
		DataSubmissions:     int(resultStats.SubmittedData),
		Reported:            int(resultStats.Reported),
		OpenRate:            openRate,
		ClickRate:           clickRate,
		SubmissionRate:      submissionRate,
		ReportRate:          reportRate,

		TemplateName: templateName,
		CampaignType: campaignType,
		CreatedAt:    &now,
		UpdatedAt:    &now,
	}

	// Insert the stats
	c.Logger.Debugf("inserting campaign stats", "campaignID", campaignID.String(), "statsID", stats.ID.String())
	err = c.CampaignRepository.InsertCampaignStats(ctx, stats)
	if err != nil {
		c.Logger.Errorw("failed to insert campaign stats", "error", err, "campaignID", campaignID.String())
		return errs.Wrap(err)
	}

	c.Logger.Debugf("successfully inserted campaign stats", "campaignID", campaignID.String(), "statsID", stats.ID.String())
	return nil
}

// GetCampaignStats retrieves campaign statistics by campaign ID
func (c *Campaign) GetCampaignStats(ctx context.Context, session *model.Session, campaignID *uuid.UUID) (*database.CampaignStats, error) {
	// Check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		return nil, errs.ErrAuthorizationFailed
	}

	return c.CampaignRepository.GetCampaignStats(ctx, campaignID)
}

// GetAllCampaignStats retrieves all campaign statistics
func (c *Campaign) GetAllCampaignStats(ctx context.Context, session *model.Session, companyID *uuid.UUID) (*model.Result[database.CampaignStats], error) {
	result := model.NewEmptyResult[database.CampaignStats]()

	// Check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		return result, errs.ErrAuthorizationFailed
	}

	// Get the data
	stats, err := c.CampaignRepository.GetAllCampaignStats(ctx, companyID)
	if err != nil {
		return result, errs.Wrap(err)
	}

	// Convert to result format with pointers
	rows := make([]*database.CampaignStats, len(stats))
	for i := range stats {
		rows[i] = &stats[i]
	}

	result.Rows = rows
	result.HasNextPage = false

	return result, nil
}

// CreateManualCampaignStats creates campaign statistics manually without requiring a campaign
func (c *Campaign) CreateManualCampaignStats(ctx context.Context, session *model.Session, req *database.CampaignStats) (*database.CampaignStats, error) {
	// Check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		return nil, errs.ErrAuthorizationFailed
	}

	// Set up the stats record
	id := uuid.New()
	now := time.Now()

	// Use provided date for created_at and updated_at, or current time if not provided
	var statsDate time.Time
	if req.CampaignStartDate != nil {
		statsDate = *req.CampaignStartDate
	} else {
		statsDate = now
	}

	// Set required fields
	req.ID = &id
	req.CreatedAt = &statsDate
	req.UpdatedAt = &statsDate
	req.CampaignID = nil // No campaign reference for manual stats

	// Calculate rates
	if req.TotalRecipients > 0 {
		req.OpenRate = float64(req.TrackingPixelLoaded) / float64(req.TotalRecipients) * 100
		req.ClickRate = float64(req.WebsiteVisits) / float64(req.TotalRecipients) * 100
		req.SubmissionRate = float64(req.DataSubmissions) / float64(req.TotalRecipients) * 100
		req.ReportRate = float64(req.Reported) / float64(req.TotalRecipients) * 100
	}

	// Calculate total events
	req.TotalEvents = req.EmailsSent + req.TrackingPixelLoaded + req.WebsiteVisits + req.DataSubmissions + req.Reported

	// Insert the stats
	c.Logger.Debugf("inserting manual campaign stats", "statsID", req.ID.String())
	err = c.CampaignRepository.InsertCampaignStats(ctx, req)
	if err != nil {
		c.Logger.Errorw("failed to insert manual campaign stats", "error", err, "statsID", req.ID.String())
		return nil, errs.Wrap(err)
	}

	c.Logger.Debugf("successfully inserted manual campaign stats", "statsID", req.ID.String())
	return req, nil
}

// GetManualCampaignStats retrieves manual campaign statistics (those without campaignID)
func (c *Campaign) GetManualCampaignStats(ctx context.Context, session *model.Session, companyID *uuid.UUID) (*model.Result[database.CampaignStats], error) {
	result := model.NewEmptyResult[database.CampaignStats]()

	// Check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		return result, errs.ErrAuthorizationFailed
	}

	// Get manual stats (those with null campaignID)
	stats, err := c.CampaignRepository.GetManualCampaignStats(ctx, companyID)
	if err != nil {
		return result, errs.Wrap(err)
	}

	// Convert to result format with pointers
	rows := make([]*database.CampaignStats, len(stats))
	for i := range stats {
		rows[i] = &stats[i]
	}

	result.Rows = rows
	result.HasNextPage = false

	return result, nil
}

// UpdateManualCampaignStats updates manual campaign statistics
func (c *Campaign) UpdateManualCampaignStats(ctx context.Context, session *model.Session, req *database.CampaignStats) (*database.CampaignStats, error) {
	// Check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		return nil, errs.ErrAuthorizationFailed
	}

	// Get existing stats to ensure it's manual (no campaignID)
	existingStats, err := c.CampaignRepository.GetCampaignStatsByID(ctx, req.ID)
	if err != nil {
		return nil, errs.Wrap(err)
	}

	// Ensure this is a manual stat (no campaignID)
	if existingStats.CampaignID != nil {
		return nil, errs.Wrap(errors.New("cannot update system-generated campaign stats"))
	}

	// Set updated timestamp
	now := time.Now()
	req.UpdatedAt = &now
	req.CampaignID = nil // Ensure it remains manual

	// Calculate rates
	if req.TotalRecipients > 0 {
		req.OpenRate = float64(req.TrackingPixelLoaded) / float64(req.TotalRecipients) * 100
		req.ClickRate = float64(req.WebsiteVisits) / float64(req.TotalRecipients) * 100
		req.SubmissionRate = float64(req.DataSubmissions) / float64(req.TotalRecipients) * 100
		req.ReportRate = float64(req.Reported) / float64(req.TotalRecipients) * 100
	}

	// Calculate total events
	req.TotalEvents = req.EmailsSent + req.TrackingPixelLoaded + req.WebsiteVisits + req.DataSubmissions + req.Reported

	// Prepare update data
	updateData := map[string]interface{}{
		"updated_at":            req.UpdatedAt,
		"campaign_name":         req.CampaignName,
		"company_id":            req.CompanyID,
		"campaign_start_date":   req.CampaignStartDate,
		"campaign_end_date":     req.CampaignEndDate,
		"campaign_closed_at":    req.CampaignClosedAt,
		"total_recipients":      req.TotalRecipients,
		"total_events":          req.TotalEvents,
		"emails_sent":           req.EmailsSent,
		"tracking_pixel_loaded": req.TrackingPixelLoaded,
		"website_visits":        req.WebsiteVisits,
		"data_submissions":      req.DataSubmissions,
		"reported":              req.Reported,
		"open_rate":             req.OpenRate,
		"click_rate":            req.ClickRate,
		"submission_rate":       req.SubmissionRate,
		"report_rate":           req.ReportRate,
		"template_name":         req.TemplateName,
		"campaign_type":         req.CampaignType,
	}

	// Update the stats
	err = c.CampaignRepository.UpdateCampaignStats(ctx, req.ID, updateData)
	if err != nil {
		c.Logger.Errorw("failed to update manual campaign stats", "error", err, "statsID", req.ID.String())
		return nil, errs.Wrap(err)
	}

	c.Logger.Debugf("successfully updated manual campaign stats", "statsID", req.ID.String())
	return req, nil
}

// DeleteManualCampaignStats deletes manual campaign statistics by ID
func (c *Campaign) DeleteManualCampaignStats(ctx context.Context, session *model.Session, statsID *uuid.UUID) error {
	// Check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		return errs.ErrAuthorizationFailed
	}

	// Get existing stats to ensure it's manual (no campaignID)
	existingStats, err := c.CampaignRepository.GetCampaignStatsByID(ctx, statsID)
	if err != nil {
		return errs.Wrap(err)
	}

	// Ensure this is a manual stat (no campaignID)
	if existingStats.CampaignID != nil {
		return errs.Wrap(errors.New("cannot delete system-generated campaign stats"))
	}

	// Delete the stats
	err = c.CampaignRepository.DeleteCampaignStatsByID(ctx, statsID)
	if err != nil {
		c.Logger.Errorw("failed to delete manual campaign stats", "error", err, "statsID", statsID.String())
		return errs.Wrap(err)
	}

	c.Logger.Debugf("successfully deleted manual campaign stats", "statsID", statsID.String())
	return nil
}

// ProcessReportedCSV processes a CSV file with reported recipients
func (c *Campaign) ProcessReportedCSV(
	ctx context.Context,
	session *model.Session,
	campaignID *uuid.UUID,
	records [][]string,
) (int, int, error) {
	ae := NewAuditEvent("Campaign.ProcessReportedCSV", session)
	ae.Details["campaignID"] = campaignID.String()

	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		c.LogAuthError(err)
		return 0, 0, errs.Wrap(err)
	}
	if !isAuthorized {
		c.AuditLogNotAuthorized(ae)
		return 0, 0, errs.ErrAuthorizationFailed
	}

	// get campaign to check it exists and get details
	campaign, err := c.CampaignRepository.GetByID(ctx, campaignID, &repository.CampaignOption{})
	if err != nil {
		c.Logger.Errorw("failed to get campaign by id", "error", err)
		return 0, 0, errs.Wrap(err)
	}

	// validate CSV headers
	headers := records[0]
	reportedByIndex := -1
	dateReportedIndex := -1

	c.Logger.Debugw("processing CSV headers", "headers", headers)

	for i, header := range headers {
		switch strings.ToLower(strings.TrimSpace(header)) {
		case "reported by":
			reportedByIndex = i
			c.Logger.Debugw("found reported by column", "index", i)
		case "date reporter (utc+02:00)", "date reported(utc+02:00)", "date reported", "date reporter":
			dateReportedIndex = i
			c.Logger.Debugw("found date column", "index", i, "header", header)
		}
	}

	if reportedByIndex == -1 {
		c.Logger.Errorw("CSV missing required column", "expected", "reported by", "headers", headers)
		return 0, 0, errs.NewValidationError(errors.New("CSV must have 'reported by' column"))
	}
	if dateReportedIndex == -1 {
		c.Logger.Errorw("CSV missing required date column", "expected", "date reported(utc+02:00)", "headers", headers)
		return 0, 0, errs.NewValidationError(errors.New("CSV must have 'date reporter (utc+02:00)' or similar date column"))
	}

	processed := 0
	skipped := 0
	reportedEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_REPORTED]

	// process each row
	for i, record := range records[1:] { // skip header
		if len(record) <= reportedByIndex || len(record) <= dateReportedIndex {
			skipped++
			c.Logger.Debugw("skipping row with insufficient columns", "row", i+2)
			continue
		}

		reportedByEmail := strings.TrimSpace(record[reportedByIndex])
		dateReported := strings.TrimSpace(record[dateReportedIndex])

		if reportedByEmail == "" {
			skipped++
			c.Logger.Debugw("skipping row with empty email", "row", i+2)
			continue
		}
		// parse date - try multiple formats and handle timezone
		var parsedDate time.Time
		dateFormats := []string{
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05-07:00",
			"2006-01-02T15:04:05+02:00",
			"2006-01-02",
			"01/02/2006 15:04:05",
			"01/02/2006",
			"02-01-2006 15:04:05",
			"02-01-2006",
		}

		dateParseError := true
		for _, format := range dateFormats {
			if pd, err := time.Parse(format, dateReported); err == nil {
				// if the parsed date has no timezone info and the header mentions UTC+02:00,
				// assume the time is in UTC+02:00 and convert to UTC
				if pd.Location() == time.UTC && strings.Contains(strings.ToLower(headers[dateReportedIndex]), "utc+02:00") {
					// treat as UTC+02:00 and convert to UTC
					loc, _ := time.LoadLocation("Europe/Berlin") // UTC+2 (or use FixedZone)
					if loc != nil {
						pd = time.Date(pd.Year(), pd.Month(), pd.Day(), pd.Hour(), pd.Minute(), pd.Second(), pd.Nanosecond(), loc).UTC()
					}
				}
				parsedDate = pd
				dateParseError = false
				break
			}
		}

		if dateParseError {
			skipped++
			c.Logger.Debugw("skipping row with invalid date format", "row", i+2, "date", dateReported, "tried_formats", dateFormats)
			continue
		}

		c.Logger.Debugw("processing row", "row", i+2, "email", reportedByEmail, "date", parsedDate)

		// find recipient by email in this campaign
		emailVO, err := vo.NewEmail(reportedByEmail)
		if err != nil {
			skipped++
			c.Logger.Debugw("invalid email format", "email", reportedByEmail, "row", i+2)
			continue
		}

		// Get campaign to check company context
		companyID, _ := campaign.CompanyID.Get()
		var companyPtr *uuid.UUID
		if companyID != uuid.Nil {
			companyPtr = &companyID
		}

		recipient, err := c.RecipientService.GetByEmail(ctx, session, emailVO, companyPtr)
		if err != nil {
			skipped++
			c.Logger.Debugw("recipient not found for email", "email", reportedByEmail, "row", i+2)
			continue
		}

		recipientID := recipient.ID.MustGet()

		// check if recipient is part of this campaign
		campaignRecipient, err := c.CampaignRecipientRepository.GetByCampaignAndRecipientID(
			ctx,
			campaignID,
			&recipientID,
			&repository.CampaignRecipientOption{},
		)
		if err != nil {
			skipped++
			c.Logger.Debugw("recipient not part of campaign", "email", reportedByEmail, "campaignID", campaignID.String(), "row", i+2)
			continue
		}

		// check if already reported (to avoid duplicates)
		existingEvent, err := c.CampaignRepository.GetEventsByCampaignID(
			ctx,
			campaignID,
			&repository.CampaignEventOption{
				QueryArgs: &vo.QueryArgs{
					Limit: 1,
				},
				EventTypeIDs: []string{reportedEventID.String()},
			},
			nil,
		)

		alreadyReported := false
		if err == nil && existingEvent != nil {
			for _, event := range existingEvent.Rows {
				if event.RecipientID != nil && *event.RecipientID == recipientID {
					alreadyReported = true
					break
				}
			}
		}

		if alreadyReported {
			skipped++
			c.Logger.Debugw("recipient already reported", "email", reportedByEmail, "campaignID", campaignID.String())
			continue
		}

		// create campaign event for reported
		eventID := uuid.New()

		var campaignEvent *model.CampaignEvent
		if campaign.IsAnonymous.MustGet() {
			campaignEvent = &model.CampaignEvent{
				ID:          &eventID,
				CampaignID:  campaignID,
				RecipientID: nil,
				IP:          vo.NewEmptyOptionalString64(),
				UserAgent:   vo.NewEmptyOptionalString255(),
				EventID:     reportedEventID,
				Data:        vo.NewEmptyOptionalString1MB(),
			}
		} else {
			campaignEvent = &model.CampaignEvent{
				ID:          &eventID,
				CampaignID:  campaignID,
				RecipientID: &recipientID,
				IP:          vo.NewEmptyOptionalString64(),
				UserAgent:   vo.NewEmptyOptionalString255(),
				EventID:     reportedEventID,
				Data:        vo.NewEmptyOptionalString1MB(),
			}
		}

		// save the event with custom timestamp
		err = c.saveReportedEvent(campaignEvent, parsedDate)
		if err != nil {
			c.Logger.Errorw("failed to save reported event", "error", err, "email", reportedByEmail)
			skipped++
			continue
		}

		// update most notable event for campaign recipient
		err = c.SetNotableCampaignRecipientEvent(
			ctx,
			campaignRecipient,
			data.EVENT_CAMPAIGN_RECIPIENT_REPORTED,
		)
		if err != nil {
			c.Logger.Errorw("failed to update notable event", "error", err)
		}

		processed++
	}

	ae.Details["processed"] = processed
	ae.Details["skipped"] = skipped
	c.AuditLogAuthorized(ae)

	return processed, skipped, nil
}

// saveReportedEvent saves a reported event with custom timestamp
func (c *Campaign) saveReportedEvent(
	campaignEvent *model.CampaignEvent,
	customTime time.Time,
) error {
	row := map[string]any{
		"id":          campaignEvent.ID.String(),
		"event_id":    campaignEvent.EventID.String(),
		"campaign_id": campaignEvent.CampaignID.String(),
		"ip_address":  campaignEvent.IP.String(),
		"user_agent":  campaignEvent.UserAgent.String(),
		"data":        campaignEvent.Data.String(),
		"created_at":  customTime,
		"updated_at":  time.Now(),
	}
	if campaignEvent.RecipientID != nil {
		row["recipient_id"] = campaignEvent.RecipientID.String()
	}

	res := c.CampaignRepository.DB.Model(&database.CampaignEvent{}).Create(row)
	if res.Error != nil {
		return res.Error
	}
	return nil
}
