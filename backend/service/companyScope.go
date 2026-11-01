package service

import (
	"context"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"gorm.io/gorm"
)

// CompanyRef is a reference from a record to another record that can be owned
// by a company.
type CompanyRef struct {
	// Table is the referenced table, always a constant from the database package
	Table string
	// Field is the request field named in the validation error
	Field string
	ID    uuid.UUID
}

// companyRefs collects references, skipping fields that are not set
type companyRefs []CompanyRef

func (r *companyRefs) add(table, field string, id nullable.Nullable[uuid.UUID]) {
	if v, err := id.Get(); err == nil {
		*r = append(*r, CompanyRef{Table: table, Field: field, ID: v})
	}
}

func (r *companyRefs) addPtr(table, field string, id *uuid.UUID) {
	if id != nil {
		*r = append(*r, CompanyRef{Table: table, Field: field, ID: *id})
	}
}

// companyIDPtr returns the company of a record, nil when it is global
func companyIDPtr(companyID nullable.Nullable[uuid.UUID]) *uuid.UUID {
	if v, err := companyID.Get(); err == nil {
		return &v
	}
	return nil
}

// ValidateCompanyRefs checks that every referenced record exists and can be
// used by a record in the companyID context, see model.UsableInCompany.
// A nil companyID is the global context. Returns a validation error naming the
// field of the first reference that fails.
func ValidateCompanyRefs(
	ctx context.Context,
	db *gorm.DB,
	companyID *uuid.UUID,
	refs []CompanyRef,
) error {
	idsByTable := map[string][]uuid.UUID{}
	for _, ref := range refs {
		idsByTable[ref.Table] = append(idsByTable[ref.Table], ref.ID)
	}
	ownersByTable := map[string]map[uuid.UUID]*uuid.UUID{}
	for table, ids := range idsByTable {
		owners, err := repository.GetCompanyIDsByIDs(ctx, db, table, ids)
		if err != nil {
			return errs.Wrap(err)
		}
		ownersByTable[table] = owners
	}
	for _, ref := range refs {
		owner, found := ownersByTable[ref.Table][ref.ID]
		if !found {
			return validate.WrapErrorWithField(errors.New("not found"), ref.Field)
		}
		if model.UsableInCompany(owner, companyID) {
			continue
		}
		if companyID == nil {
			return validate.WrapErrorWithField(
				errors.New("belongs to a company and cannot be used globally"),
				ref.Field,
			)
		}
		return validate.WrapErrorWithField(errors.New("belongs to another company"), ref.Field)
	}
	return nil
}

// campaignCompanyRefs lists the references a campaign request sets
func campaignCompanyRefs(c *model.Campaign) []CompanyRef {
	refs := companyRefs{}
	refs.add(database.CAMPAIGN_TEMPLATE_TABLE, "templateID", c.TemplateID)
	if ids, err := c.RecipientGroupIDs.Get(); err == nil {
		for _, id := range ids {
			refs.addPtr(database.RECIPIENT_GROUP_TABLE, "recipientGroupIDs", id)
		}
	}
	if ids, err := c.AllowDenyIDs.Get(); err == nil {
		for _, id := range ids {
			refs.addPtr(database.ALLOW_DENY_TABLE, "allowDenyIDs", id)
		}
	}
	refs.add(database.PAGE_TABLE, "denyPageID", c.DenyPageID)
	refs.add(database.PAGE_TABLE, "evasionPageID", c.EvasionPageID)
	refs.add(database.WEBHOOK_TABLE, "webhookID", c.WebhookID)
	if webhooks, err := c.Webhooks.Get(); err == nil {
		for _, wh := range webhooks {
			if wh != nil {
				refs.add(database.WEBHOOK_TABLE, "webhooks", wh.WebhookID)
			}
		}
	}
	if scripts, err := c.Scripts.Get(); err == nil {
		for _, s := range scripts {
			if s != nil {
				refs.add(database.SCRIPT_TABLE, "scripts", s.ScriptID)
			}
		}
	}
	return refs
}

// campaignTemplateCompanyRefs lists the references a campaign template request sets
func campaignTemplateCompanyRefs(t *model.CampaignTemplate) []CompanyRef {
	refs := companyRefs{}
	refs.add(database.DOMAIN_TABLE, "domainID", t.DomainID)
	refs.add(database.EMAIL_TABLE, "emailID", t.EmailID)
	refs.add(database.SMTP_CONFIGURATION_TABLE, "smtpConfigurationID", t.SMTPConfigurationID)
	refs.add(database.API_SENDER_TABLE, "apiSenderID", t.APISenderID)
	refs.add(database.PAGE_TABLE, "beforeLandingPageID", t.BeforeLandingPageID)
	refs.add(database.PAGE_TABLE, "landingPageID", t.LandingPageID)
	refs.add(database.PAGE_TABLE, "afterLandingPageID", t.AfterLandingPageID)
	refs.add(database.PROXY_TABLE, "beforeLandingProxyID", t.BeforeLandingProxyID)
	refs.add(database.PROXY_TABLE, "landingProxyID", t.LandingProxyID)
	refs.add(database.PROXY_TABLE, "afterLandingProxyID", t.AfterLandingProxyID)
	return refs
}

// newCompanyRefs drops the references a record already holds, so an update
// only checks the links it adds. A link stored before this check existed keeps
// working, and is listed by the company scope report instead.
func newCompanyRefs(incoming, stored []CompanyRef) []CompanyRef {
	type key struct {
		table string
		id    uuid.UUID
	}
	held := map[key]bool{}
	for _, ref := range stored {
		held[key{ref.Table, ref.ID}] = true
	}
	out := []CompanyRef{}
	for _, ref := range incoming {
		if !held[key{ref.Table, ref.ID}] {
			out = append(out, ref)
		}
	}
	return out
}
