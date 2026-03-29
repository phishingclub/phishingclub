package model

import (
	"encoding/json"
	"slices"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// AutoPruneOption holds the auto prune orphaned recipients setting for both
// the global scope and all per company overrides. It is stored as a
// JSON value under the key data.OptionKeyAutoPruneOrphanedRecipients.
//
// example stored value:
//
//	{"enabled":false,"companies":["uuid1","uuid2"]}
type AutoPruneOption struct {
	// Enabled is the global (shared / no-company) auto prune flag.
	Enabled bool `json:"enabled"`
	// Companies is the list of company UUIDs that have opted in to auto pruning.
	// a company not present in this list will not be pruned.
	Companies []string `json:"companies,omitempty"`
}

// NewAutoPruneOptionDefault returns a disabled AutoPruneOption with no company entries.
func NewAutoPruneOptionDefault() *AutoPruneOption {
	return &AutoPruneOption{
		Enabled:   false,
		Companies: []string{},
	}
}

// NewAutoPruneOptionFromJSON deserialises an AutoPruneOption from JSON bytes.
func NewAutoPruneOptionFromJSON(jsonData []byte) (*AutoPruneOption, error) {
	opt := &AutoPruneOption{}
	if err := json.Unmarshal(jsonData, opt); err != nil {
		return nil, validate.WrapErrorWithField(
			errs.NewValidationError(errors.New("invalid format")),
			"AutoPruneOption",
		)
	}
	if opt.Companies == nil {
		opt.Companies = []string{}
	}
	return opt, nil
}

// NewAutoPruneOptionFromOption deserialises an AutoPruneOption from a generic Option model.
func NewAutoPruneOptionFromOption(option *Option) (*AutoPruneOption, error) {
	if option == nil {
		return nil, errors.New("option cannot be nil")
	}
	return NewAutoPruneOptionFromJSON([]byte(option.Value.String()))
}

// IsCompanyEnabled returns true if the given company has explicitly opted in to auto pruning.
func (a *AutoPruneOption) IsCompanyEnabled(companyID *uuid.UUID) bool {
	return slices.Contains(a.Companies, companyID.String())
}

// SetCompanyEnabled adds or removes the given company from the opted in list.
func (a *AutoPruneOption) SetCompanyEnabled(companyID *uuid.UUID, enabled bool) {
	id := companyID.String()
	if enabled {
		if !slices.Contains(a.Companies, id) {
			a.Companies = append(a.Companies, id)
		}
		return
	}
	a.Companies = slices.DeleteFunc(a.Companies, func(s string) bool {
		return s == id
	})
}

// ToJSON serialises the AutoPruneOption to JSON bytes.
func (a *AutoPruneOption) ToJSON() ([]byte, error) {
	return json.Marshal(a)
}

// ToOption converts the AutoPruneOption into a generic Option ready to be persisted
// under data.OptionKeyAutoPruneOrphanedRecipients.
func (a *AutoPruneOption) ToOption() (*Option, error) {
	j, err := a.ToJSON()
	if err != nil {
		return nil, errs.Wrap(err)
	}
	str, err := vo.NewOptionalString1MB(string(j))
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &Option{
		Key:   *vo.NewString127Must(data.OptionKeyAutoPruneOrphanedRecipients),
		Value: *str,
	}, nil
}
