package service

import (
	"context"
	"errors"
	"sort"
	"text/template"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// languageSendGroup is the rows of one template stamped with one language and
// the email of that language
type languageSendGroup struct {
	code    string
	rows    []*model.CampaignRecipient
	email   *model.Email
	content vo.OptionalString1MB
}

// splitByLanguage groups rows by their language, a row with no stamp under
// the template language. The template language comes first, the others
// follow by code.
func splitByLanguage(t *model.CampaignTemplate, rows []*model.CampaignRecipient) ([]string, map[string][]*model.CampaignRecipient) {
	byCode := map[string][]*model.CampaignRecipient{}
	for _, cr := range rows {
		code := cr.EffectiveLanguage(t)
		byCode[code] = append(byCode[code], cr)
	}
	own := t.TemplateLanguage()
	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		if code != own {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	if _, ok := byCode[own]; ok {
		codes = append([]string{own}, codes...)
	}
	return codes, byCode
}

// languageSendGroups splits the rows of one template by language and loads
// and checks the email of every language before any row is tried: it must
// exist, have content and a body and subject that parse. The language rows of
// the template are read in one query and each email once.
//
// A language that can not be sent closes a normal campaign, the same as a
// broken template email always did, and nothing is sent. Then done is true
// and err is what the sender returns. A program campaign holds only the rows
// of that language, see holdProgramSends, and the other languages are still
// sent; errors of the holds are returned in holdErr.
func (c *Campaign) languageSendGroups(
	ctx context.Context,
	session *model.Session,
	campaign *model.Campaign,
	cTemplate *model.CampaignTemplate,
	campaignRecipients []*model.CampaignRecipient,
	campaignCompanyID *uuid.UUID,
) (groups []*languageSendGroup, holdErr error, done bool, err error) {
	campaignID := campaign.ID.MustGet()
	codes, byCode := splitByLanguage(cTemplate, campaignRecipients)
	own := cTemplate.TemplateLanguage()
	var languageEmails map[string]*model.CampaignTemplateLanguage
	if len(codes) > 1 || (len(codes) == 1 && codes[0] != own) {
		templateID := cTemplate.ID.MustGet()
		rows, err := c.CampaignTemplateService.CampaignTemplateRepository.GetLanguagesByTemplateID(ctx, &templateID, false)
		if err != nil {
			c.Logger.Errorw("failed to get the languages of the template", "error", err)
			return nil, nil, true, errs.Wrap(err)
		}
		languageEmails = make(map[string]*model.CampaignTemplateLanguage, len(rows))
		for _, row := range rows {
			languageEmails[row.Language] = row
		}
	}
	holdErrs := []error{}
	// broken handles a language that can not be sent. It reports whether the
	// sender must stop, with the error it returns. quiet keeps the old result
	// of a template without an email, a close with no error.
	broken := func(code string, holdReason string, closeReason string, cause error, quiet bool) (bool, error) {
		if code != own {
			name := data.LanguageName(code)
			holdReason = name + ": " + holdReason
			// an anonymous campaign's close reason does not tell which
			// language was due
			if !campaignAnonymous(campaign) {
				closeReason = name + ": " + closeReason
			}
		}
		if held, err := c.holdProgramSends(ctx, campaign, byCode[code], holdReason); held {
			if err != nil {
				holdErrs = append(holdErrs, err)
			}
			return false, nil
		}
		c.Logger.Warnw("campaign email can not be sent, closing campaign",
			"campaignID", campaignID.String(),
			"reason", closeReason,
		)
		closeErr := c.closeCampaign(ctx, session, &campaignID, campaign, closeReason)
		if quiet {
			if closeErr != nil {
				return true, errs.Wrap(errors.Join(cause, closeErr))
			}
			return true, nil
		}
		return true, errs.Wrap(errors.Join(cause, closeErr))
	}
	for _, code := range codes {
		var emailID uuid.UUID
		if code == own {
			v, err := cTemplate.EmailID.Get()
			if err != nil {
				if stop, err := broken(code, "the template has no email", "Campaign does not have a email relation", err, true); stop {
					return nil, nil, true, err
				}
				continue
			}
			emailID = v
		} else {
			row := languageEmails[code]
			v, err := func() (uuid.UUID, error) {
				if row == nil || !row.IsUsable {
					return uuid.Nil, errors.New("the language is not usable")
				}
				return row.EmailID.Get()
			}()
			if err != nil {
				if stop, err := broken(code, "the language of the template is not usable", "the language of the template is not usable", err, false); stop {
					return nil, nil, true, err
				}
				continue
			}
			emailID = v
		}
		email, err := c.MailService.GetByID(ctx, session, &emailID, campaignCompanyID)
		if err != nil {
			// a failed read may pass, a program keeps its campaign and the
			// sends are tried again on the next run. Only an email that is gone
			// holds the sends.
			if campaign.IsProgramManaged() && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil, true, errs.Wrap(err)
			}
			if stop, err := broken(code, "the email of the template was deleted", "failed get email", err, false); stop {
				return nil, nil, true, err
			}
			continue
		}
		content, err := email.Content.Get()
		if err != nil {
			if stop, err := broken(code, "the email has no content", "failed get email content", err, false); stop {
				return nil, nil, true, err
			}
			continue
		}
		// validate the template is parseable before entering the per recipient
		// loop. the actual per recipient execution uses
		// TemplateFuncsWithDeviceCode so that {{MicrosoftDeviceCode}} and
		// {{MicrosoftDeviceCodeURL}} resolve correctly.
		funcs := c.TemplateService.TemplateFuncsWithCompany(ctx, campaignCompanyID)
		if _, err := template.New("email").Funcs(funcs).Parse(content.String()); err != nil {
			if stop, err := broken(code, "the email does not parse", "failed to parse the template", err, false); stop {
				return nil, nil, true, err
			}
			continue
		}
		// the subject is the same for every recipient, so a subject that does
		// not parse is handled like a body that does not parse, before any row
		// is tried
		subject, err := email.MailHeaderSubject.Get()
		if err == nil {
			_, err = template.New("subject").Funcs(funcs).Parse(subject.String())
		}
		if err != nil {
			if stop, err := broken(code, "the email subject does not parse", "failed to parse the subject", err, false); stop {
				return nil, nil, true, err
			}
			continue
		}
		groups = append(groups, &languageSendGroup{
			code:    code,
			rows:    byCode[code],
			email:   email,
			content: content,
		})
	}
	return groups, errors.Join(holdErrs...), false, nil
}
