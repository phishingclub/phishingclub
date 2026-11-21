package model

import (
	"time"

	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/validate"
)

// IsAnonymized reports whether the people of the program are replaced with
// pseudonyms in every result
func (p *Program) IsAnonymized() bool {
	return p.AnonymizedAt != nil
}

// AnonymizeBegun reports whether Anonymize All began, also when it did not
// finish yet. The views then name no person.
func (p *Program) AnonymizeBegun() bool {
	return p.AnonymizedAt != nil || p.AnonymizingAt != nil
}

// IsDataAnonymized reports whether the submitted data, IP addresses, user
// agents and browser metadata of the program were removed
func (p *Program) IsDataAnonymized() bool {
	return p.DataAnonymizedAt != nil
}

// AnonymizeAtTime returns the anonymize date, nil when none is set
func (p *Program) AnonymizeAtTime() *time.Time {
	if v, err := p.AnonymizeAt.Get(); err == nil {
		return &v
	}
	return nil
}

// DataAnonymizeAtTime returns the data anonymize date, nil when none is set
func (p *Program) DataAnonymizeAtTime() *time.Time {
	if v, err := p.DataAnonymizeAt.Get(); err == nil {
		return &v
	}
	return nil
}

// ValidateAnonymizeDates checks the anonymize dates of the program a create
// or update leaves, with the start and end it will have. Only a closed program
// is anonymized, so the anonymize date needs an end date, when the program
// closes on its own, and must be on or after it. An anonymous program is
// anonymized when it closes and takes no anonymize date. The data anonymize
// date must be on or after the start and the end when they are set, as for a
// campaign.
func (p *Program) ValidateAnonymizeDates(anonymous bool, start, end *time.Time) error {
	if at := p.AnonymizeAtTime(); at != nil {
		if anonymous {
			return validate.WrapErrorWithField(errors.New("An anonymous program is anonymized when it closes"), "anonymizeAt")
		}
		if end == nil {
			return validate.WrapErrorWithField(errors.New("Set an end date to anonymize the program on a date"), "anonymizeAt")
		}
		if at.Before(*end) {
			return validate.WrapErrorWithField(errors.New("Anonymize all data must be after the end"), "anonymizeAt")
		}
		if start != nil && at.Before(*start) {
			return validate.WrapErrorWithField(errors.New("Anonymize all data must be after the start"), "anonymizeAt")
		}
	}
	if at := p.DataAnonymizeAtTime(); at != nil {
		if end != nil && at.Before(*end) {
			return validate.WrapErrorWithField(errors.New("Anonymize data must be after the end"), "dataAnonymizeAt")
		}
		if start != nil && at.Before(*start) {
			return validate.WrapErrorWithField(errors.New("Anonymize data must be after the start"), "dataAnonymizeAt")
		}
	}
	return nil
}

// anonymizeToDBMap adds the anonymize dates to a column map.
func (p *Program) anonymizeToDBMap(m map[string]any) {
	if p.AnonymizeAt.IsSpecified() {
		m["anonymize_at"] = nil
		if v, err := p.AnonymizeAt.Get(); err == nil {
			m["anonymize_at"] = v.UTC()
		}
	}
	if p.DataAnonymizeAt.IsSpecified() {
		m["data_anonymize_at"] = nil
		if v, err := p.DataAnonymizeAt.Get(); err == nil {
			m["data_anonymize_at"] = v.UTC()
		}
	}
}
