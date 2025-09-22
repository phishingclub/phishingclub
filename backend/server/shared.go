package server

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// GetCampaignRecipientFromURLParams extracts campaign recipient information from URL parameters
// by checking all identifiers against query parameters and finding the first matching campaign recipient.
// returns the campaign recipient object, parameter name, and any error encountered.
func GetCampaignRecipientFromURLParams(
	ctx context.Context,
	req *http.Request,
	identifierRepo *repository.Identifier,
	campaignRecipientRepo *repository.CampaignRecipient,
) (*model.CampaignRecipient, string, error) {
	// get all identifiers
	identifiers, err := identifierRepo.GetAll(ctx, &repository.IdentifierOption{})
	if err != nil {
		return nil, "", err
	}

	query := req.URL.Query()
	var matchingParams []struct {
		name string
		id   *uuid.UUID
	}

	// collect all query parameters that match identifier names and can be parsed as UUIDs
	for _, identifier := range identifiers.Rows {
		if name := identifier.Name.MustGet(); query.Has(name) {
			if id, err := uuid.Parse(query.Get(name)); err == nil {
				matchingParams = append(matchingParams, struct {
					name string
					id   *uuid.UUID
				}{name: name, id: &id})
			}
		}
	}

	if len(matchingParams) == 0 {
		return nil, "", nil
	}

	// check each matching parameter to find a valid campaign recipient
	for _, param := range matchingParams {
		campaignRecipient, err := campaignRecipientRepo.GetByCampaignRecipientID(ctx, param.id)
		if err == nil && campaignRecipient != nil {
			return campaignRecipient, param.name, nil
		}
	}

	return nil, "", nil
}
