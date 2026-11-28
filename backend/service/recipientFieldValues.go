package service

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
)

// MaxFieldValueGroups is the most groups one field values request reads
const MaxFieldValueGroups = 100

// GetFieldValues returns the values of a recipient field over the members of
// the groups, normalized the way language routes match them and merged by
// key, the most common first, so an operator can map them to languages. Every
// group must be global or belong to the company in view. Only a refused
// session is audited, the read changes nothing.
func (r *RecipientGroup) GetFieldValues(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	field string,
	groupIDs []*uuid.UUID,
) (*model.RecipientFieldValues, error) {
	ae := NewAuditEvent("RecipientGroup.GetFieldValues", session)
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		r.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		r.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	if field == "" || !slices.Contains(model.LanguageRouteFields, field) {
		return nil, validate.WrapErrorWithField(errors.New("unknown recipient field"), "field")
	}
	if len(groupIDs) == 0 {
		return nil, validate.WrapErrorWithField(errors.New("no groups provided"), "groupIDs")
	}
	if len(groupIDs) > MaxFieldValueGroups {
		return nil, validate.WrapErrorWithField(
			fmt.Errorf("at most %d groups can be read at once", MaxFieldValueGroups),
			"groupIDs",
		)
	}
	refs := companyRefs{}
	for _, id := range groupIDs {
		refs.addPtr(database.RECIPIENT_GROUP_TABLE, "groupIDs", id)
	}
	if err := ValidateCompanyRefs(ctx, r.RecipientGroupRepository.DB, companyID, refs); err != nil {
		return nil, err
	}
	counts, err := r.RecipientGroupRepository.GetFieldValueCounts(ctx, field, groupIDs)
	if err != nil {
		r.Logger.Errorw("failed to get recipient field values", "error", err)
		return nil, errs.Wrap(err)
	}
	return mergeFieldValues(field, counts), nil
}

// mergeFieldValues merges raw values that are the same after
// model.NormalizeLanguageKey. The database can not do this, its lower case
// is ASCII only and it has no Unicode normal forms. When the raw values were
// cut, each value that was not read counts as one more value in Total, and
// the members of values that are not listed are counted in UnlistedCount.
func mergeFieldValues(field string, counts *repository.FieldValueCounts) *model.RecipientFieldValues {
	out := &model.RecipientFieldValues{
		Field:     field,
		Values:    []*model.RecipientFieldValue{},
		Truncated: counts.More,
	}
	byKey := map[string]*model.RecipientFieldValue{}
	for _, row := range counts.Rows {
		key := model.NormalizeLanguageKey(row.Value)
		if key == "" {
			out.EmptyCount += row.Count
			continue
		}
		v, ok := byKey[key]
		if !ok {
			v = &model.RecipientFieldValue{Key: key, Values: []string{}}
			byKey[key] = v
			out.Values = append(out.Values, v)
		}
		v.Count += row.Count
		if !slices.Contains(v.Values, row.Value) {
			v.Values = append(v.Values, row.Value)
		}
	}
	sort.SliceStable(out.Values, func(i, j int) bool {
		if out.Values[i].Count != out.Values[j].Count {
			return out.Values[i].Count > out.Values[j].Count
		}
		return out.Values[i].Key < out.Values[j].Key
	})
	for _, v := range out.Values {
		sort.Strings(v.Values)
	}
	out.Total = len(out.Values)
	if counts.More {
		out.Total += max(counts.Distinct-len(counts.Rows), 1)
	}
	if len(out.Values) > model.MaxRecipientFieldValues {
		out.Values = out.Values[:model.MaxRecipientFieldValues]
		out.Truncated = true
	}
	listed := 0
	for _, v := range out.Values {
		listed += v.Count
	}
	out.UnlistedCount = max(counts.Members-out.EmptyCount-listed, 0)
	return out
}
