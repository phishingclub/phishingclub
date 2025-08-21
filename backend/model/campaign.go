package model

import (
	"fmt"
	"time"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// Campaign is a phishing Campaign entity
type Campaign struct {
	ID                  nullable.Nullable[uuid.UUID]                 `json:"id"`
	CreatedAt           *time.Time                                   `json:"createdAt"`
	UpdatedAt           *time.Time                                   `json:"updatedAt"`
	CloseAt             nullable.Nullable[time.Time]                 `json:"closeAt"`
	ClosedAt            nullable.Nullable[time.Time]                 `json:"closedAt"`
	AnonymizeAt         nullable.Nullable[time.Time]                 `json:"anonymizeAt"`
	AnonymizedAt        nullable.Nullable[time.Time]                 `json:"anonymizedAt"`
	SortField           nullable.Nullable[vo.CampaignSortField]      `json:"sortField"`
	SortOrder           nullable.Nullable[vo.CampaignSendingOrder]   `json:"sortOrder"`
	SendStartAt         nullable.Nullable[time.Time]                 `json:"sendStartAt"`
	SendEndAt           nullable.Nullable[time.Time]                 `json:"sendEndAt"`
	ConstraintWeekDays  nullable.Nullable[vo.CampaignWeekDays]       `json:"constraintWeekDays"`
	ConstraintStartTime nullable.Nullable[vo.CampaignTimeConstraint] `json:"constraintStartTime"`
	ConstraintEndTime   nullable.Nullable[vo.CampaignTimeConstraint] `json:"constraintEndTime"`

	Name nullable.Nullable[vo.String64] `json:"name"`

	SaveSubmittedData nullable.Nullable[bool]         `json:"saveSubmittedData"`
	IsAnonymous       nullable.Nullable[bool]         `json:"isAnonymous"`
	IsTest            nullable.Nullable[bool]         `json:"isTest"`
	TemplateID        nullable.Nullable[uuid.UUID]    `json:"templateID"`
	Template          *CampaignTemplate               `json:"template"`
	CompanyID         nullable.Nullable[uuid.UUID]    `json:"companyID"`
	Company           *Company                        `json:"company"`
	RecipientGroups   []*RecipientGroup               `json:"recipientGroups"`
	RecipientGroupIDs nullable.Nullable[[]*uuid.UUID] `json:"recipientGroupIDs,omitempty"`
	AllowDeny         []*AllowDeny                    `json:"allowDeny"`
	AllowDenyIDs      nullable.Nullable[[]*uuid.UUID] `json:"allowDenyIDs,omitempty"`
	DenyPageID        nullable.Nullable[uuid.UUID]    `json:"denyPageID,omitempty"`
	DenyPage          *Page                           `json:"denyPage"`
	WebhookID         nullable.Nullable[uuid.UUID]    `json:"webhookID"`

	// must not be set by a user
	NotableEventID   nullable.Nullable[uuid.UUID] `json:"notableEventID"`
	NotableEventName string                       `json:"notableEventName"`
}

// Validate checks if the campaign has a valid state
func (c *Campaign) Validate() error {
	if err := validate.NullableFieldRequired("name", c.Name); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("sortField", c.SortField); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("sortOrder", c.SortOrder); err != nil {
		return err
	}
	// if a start or end is set, then end must be equal or after the start
	if c.SendStartAt.IsSpecified() && !c.SendStartAt.IsNull() || (c.SendEndAt.IsSpecified() && !c.SendEndAt.IsNull()) {
		if err := validate.NullableFieldRequired("sendStartAt", c.SendStartAt); err != nil {
			return err
		}
		if err := validate.NullableFieldRequired("sendEndAt", c.SendEndAt); err != nil {
			return err
		}
		if c.SendEndAt.MustGet().Before(c.SendStartAt.MustGet()) {
			return validate.WrapErrorWithField(errors.New("send end time must be after start time"), "sendEndAt")
		}
	}
	if err := validate.NullableFieldRequired("templateID", c.TemplateID); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("RecipientGroupIDs", c.RecipientGroupIDs); err != nil {
		return err
	}
	if len(c.RecipientGroupIDs.MustGet()) == 0 {
		return validate.WrapErrorWithField(errors.New("must have at least one recipient group"), "RecipientGroupIDs")
	}
	if err := c.ValidateDenyPage(); err != nil {
		return err
	}

	// if ConstraintWeekDays or ConstraintStartTime or ConstraintEndTime is set, then this is a 'scheduled type'
	// this requires that all fields are set and that end time is equal or after start time
	if (c.ConstraintWeekDays.IsSpecified() && !c.ConstraintWeekDays.IsNull()) ||
		(c.ConstraintStartTime.IsSpecified() && !c.ConstraintStartTime.IsNull()) ||
		(c.ConstraintEndTime.IsSpecified() && !c.ConstraintEndTime.IsNull()) {
		// check required fields are set
		if err := c.ValidateSendTimesSet(); err != nil {
			return err
		}
		if err := validate.NullableFieldRequired("ConstraintWeekDays", c.ConstraintWeekDays); err != nil {
			return err
		}
		if c.ConstraintWeekDays.MustGet().Count() == 0 {
			return validate.WrapErrorWithField(errors.New("must have at least one day selected"), "ConstraintWeekDays")
		}
		if err := validate.NullableFieldRequired("ConstraintStartTime", c.ConstraintStartTime); err != nil {
			return err
		}
		if err := validate.NullableFieldRequired("ConstraintEndTime", c.ConstraintEndTime); err != nil {
			return err
		}
		// check that times and days are valid
		constraintStartTime := c.ConstraintStartTime.MustGet()
		constraintEndTime := c.ConstraintEndTime.MustGet()
		if constraintStartTime.IsAfter(constraintEndTime) {
			return validate.WrapErrorWithField(errors.New("constraint end time must be after start time"), "ConstraintEndTime")
		}
		if constraintStartTime.IsEqual(constraintEndTime) {
			return validate.WrapErrorWithField(errors.New("constraint end time must be after start time"), "ConstraintEndTime")
		}
		startAt := c.SendStartAt.MustGet()
		endAt := c.SendEndAt.MustGet()
		//  check that selected weekdays are within the start and end date
		isWithin := c.ConstraintWeekDays.MustGet().IsWithin(&startAt, &endAt)
		if !isWithin {
			return validate.WrapErrorWithField(
				fmt.Errorf(
					"constraint week days must be within the start (%s) and end date (%s)",
					startAt.Format("2006-01-02"),
					endAt.Format("2006-01-02"),
				),
				"ConstraintWeekDays",
			)
		}
	}
	// ensure closeAt and anonymize is correctly set after other dates if set
	if c.CloseAt.IsSpecified() && !c.CloseAt.IsNull() {
		closeAt := c.CloseAt.MustGet()
		if v, err := c.SendEndAt.Get(); err == nil {
			if closeAt.Before(v) {
				return validate.WrapErrorWithField(errors.New("close at must be after end date"), "CloseAt")
			}
		}
	}
	if c.AnonymizeAt.IsSpecified() && !c.AnonymizeAt.IsNull() {
		anonymizeAt := c.AnonymizeAt.MustGet()
		if v, err := c.CloseAt.Get(); err == nil {
			if anonymizeAt.Before(v) {
				return validate.WrapErrorWithField(errors.New("anonymize at must be after close date"), "AnonymizeAt")
			}
		}
		if v, err := c.SendEndAt.Get(); err != nil {
			if anonymizeAt.Before(v) {
				return validate.WrapErrorWithField(errors.New("anonymize at must be after end date"), "AnonymizeAt")
			}
		}
		if v, err := c.SendStartAt.Get(); err == nil {
			if anonymizeAt.Before(v) {
				return validate.WrapErrorWithField(errors.New("anonymize at must be after start date"), "AnonymizeAt")
			}
		}
	}
	// must not be set from api consumers
	if c.NotableEventID.IsSpecified() && !c.NotableEventID.IsNull() {
		c.NotableEventID.SetNull()
	}

	return nil
}

// ValidateSendTimesSet checks that the send start and end times are set
func (c *Campaign) ValidateSendTimesSet() error {
	if err := validate.NullableFieldRequired("sendStartAt", c.SendStartAt); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("sendEndAt", c.SendEndAt); err != nil {
		return err
	}
	return nil
}

// ValidateScheduledType checks times related to a scheduled type campaign
func (c *Campaign) ValidateScheduledTimes() error {
	if err := c.ValidateSendTimesSet(); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("ConstraintWeekDays", c.ConstraintWeekDays); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("ConstraintStartTime", c.ConstraintStartTime); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("ConstraintEndTime", c.ConstraintEndTime); err != nil {
		return err
	}
	return nil
}

// ValidateNoSendTimesSet checks that the send start and end times are not set
func (c *Campaign) ValidateNoSendTimesSet() error {
	if c.SendStartAt.IsSpecified() && !c.SendStartAt.IsNull() {
		return validate.WrapErrorWithField(errors.New("send start time must not be set"), "sendStartAt")
	}
	if c.SendEndAt.IsSpecified() && !c.SendEndAt.IsNull() {
		return validate.WrapErrorWithField(errors.New("send end time must not be set"), "sendEndAt")
	}
	return nil
}

// ValidateDenyPage checks that a deny page is set
func (c *Campaign) ValidateDenyPage() error {
	if c.DenyPageID.IsSpecified() && !c.DenyPageID.IsNull() {
		if c.AllowDenyIDs.IsSpecified() && (c.AllowDenyIDs.IsNull() || len(c.AllowDenyIDs.MustGet()) == 0) {
			return validate.WrapErrorWithField(errors.New("requires a allow deny IDs to be set"), "denyPage")
		}
	}
	return nil
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (c *Campaign) ToDBMap() map[string]any {
	m := map[string]any{}
	if c.Name.IsSpecified() {
		m["name"] = nil
		if v, err := c.Name.Get(); err == nil {
			m["name"] = v.String()
		}
	}
	if c.SortField.IsSpecified() {
		m["sort_field"] = nil
		if v, err := c.SortField.Get(); err == nil {
			m["sort_field"] = v.String()
		}
	}
	if c.SortOrder.IsSpecified() {
		m["sort_order"] = nil
		if v, err := c.SortOrder.Get(); err == nil {
			m["sort_order"] = v.String()
		}
	}
	if c.SendStartAt.IsSpecified() {
		m["send_start_at"] = nil
		if v, err := c.SendStartAt.Get(); err == nil {
			m["send_start_at"] = utils.RFC3339UTC(v)
		}
	}
	if c.ConstraintWeekDays.IsSpecified() {
		m["constraint_week_days"] = nil
		if v, err := c.ConstraintWeekDays.Get(); err == nil {
			m["constraint_week_days"] = v.Int()
		}
	}
	if c.ConstraintStartTime.IsSpecified() {
		m["constraint_start_time"] = nil
		if v, err := c.ConstraintStartTime.Get(); err == nil {
			m["constraint_start_time"] = v.String()
		}
	}
	if c.ConstraintEndTime.IsSpecified() {
		m["constraint_end_time"] = nil
		if v, err := c.ConstraintEndTime.Get(); err == nil {
			m["constraint_end_time"] = v.String()
		}
	}
	if c.SendEndAt.IsSpecified() {
		m["send_end_at"] = nil
		if v, err := c.SendEndAt.Get(); err == nil {
			m["send_end_at"] = utils.RFC3339UTC(v)
		}
	}
	if c.CloseAt.IsSpecified() {
		m["close_at"] = nil
		if v, err := c.CloseAt.Get(); err == nil {
			m["close_at"] = utils.RFC3339UTC(v)
		}
	}
	if c.ClosedAt.IsSpecified() {
		m["closed_at"] = nil
		if v, err := c.ClosedAt.Get(); err == nil {
			m["closed_at"] = utils.RFC3339UTC(v)
		}
	}
	if c.AnonymizeAt.IsSpecified() {
		m["anonymize_at"] = nil
		if v, err := c.AnonymizeAt.Get(); err == nil {
			m["anonymize_at"] = utils.RFC3339UTC(v)
		}
	}
	if c.SaveSubmittedData.IsSpecified() {
		m["save_submitted_data"] = false
		if v, err := c.SaveSubmittedData.Get(); err == nil {
			m["save_submitted_data"] = v
		}
	}
	if c.IsTest.IsSpecified() {
		m["is_test"] = false
		if v, err := c.IsTest.Get(); err == nil {
			m["is_test"] = v
		}
	}
	if c.IsAnonymous.IsSpecified() {
		m["is_anonymous"] = false
		if v, err := c.IsAnonymous.Get(); err == nil {
			m["is_anonymous"] = v
		}
	}
	if c.TemplateID.IsSpecified() {
		m["campaign_template_id"] = nil
		if v, err := c.TemplateID.Get(); err == nil {
			m["campaign_template_id"] = v.String()
		}
	}
	if c.CompanyID.IsSpecified() {
		if c.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = c.CompanyID.MustGet()
		}
	}
	allowDenyIsSet := c.AllowDenyIDs.IsSpecified() && !c.AllowDenyIDs.IsNull() && len(c.AllowDenyIDs.MustGet()) > 0
	if allowDenyIsSet {
		if v, err := c.DenyPageID.Get(); err == nil {
			m["deny_page_id"] = v.String()
		} else {
			m["deny_page_id"] = nil
		}
	} else {
		m["deny_page_id"] = nil
	}
	if c.WebhookID.IsSpecified() {
		m["webhook_id"] = nil
		if v, err := c.WebhookID.Get(); err == nil {
			m["webhook_id"] = v.String()
		}
	}
	if v, err := c.NotableEventID.Get(); err == nil {
		m["notable_event_id"] = v.String()
	}

	return m
}

// Close sets the close at timestamp to now
// dont confuse with method Closed
func (c *Campaign) Close() error {
	if c.ClosedAt.IsSpecified() && !c.ClosedAt.IsNull() {
		return errs.ErrCampaignAlreadyClosed
	}
	if c.CloseAt.IsSpecified() && !c.CloseAt.IsNull() {
		return errs.ErrCampaignAlreadySetToClose
	}
	c.CloseAt.Set(time.Now().UTC())
	return nil
}

// Closed sets the closed at timestamp to now
// dont confuse with method Close
func (c *Campaign) Closed() error {
	if c.ClosedAt.IsSpecified() && !c.ClosedAt.IsNull() {
		return errs.ErrCampaignAlreadyClosed
	}
	c.ClosedAt.Set(time.Now().UTC())
	return nil
}

// Anonymize sets the anonymized at timestamp
func (c *Campaign) Anonymize() error {
	if c.AnonymizedAt.IsSpecified() && !c.AnonymizedAt.IsNull() {
		return errs.ErrCampaignAlreadyAnonymized
	}
	c.AnonymizedAt.Set(time.Now().UTC())
	return nil
}

// IsActive returns true if the campaign is active
func (c *Campaign) IsActive() bool {
	now := time.Now()
	if c.ClosedAt.IsSpecified() && !c.ClosedAt.IsNull() && c.ClosedAt.MustGet().Before(now) {
		return false
	}
	return true
}

// IsSelfManaged returns true if the campaign is self managed
func (c *Campaign) IsSelfManaged() bool {
	return c.SendStartAt.IsSpecified() && c.SendStartAt.IsNull() && c.SendEndAt.IsSpecified() && c.SendEndAt.IsNull()
}
