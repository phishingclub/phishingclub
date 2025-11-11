package model

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/vo"
)

const (
	// headerJA4 is the internal header key for ja4 fingerprint
	headerJA4 = "X-JA4"

	// contextKeyJA4 is the gin context key for ja4 fingerprint
	contextKeyJA4 = "ja4_fingerprint"
)

// CampaignEventMetadata contains fingerprinting and browser metadata
type CampaignEventMetadata struct {
	JA4Fingerprint string `json:"ja4_fingerprint,omitempty"`
	Platform       string `json:"platform,omitempty"`
	AcceptLanguage string `json:"accept_language,omitempty"`
}

// ExtractCampaignEventMetadata extracts ja4 fingerprint, sec-ch-ua-platform, and accept-language from request
// returns an OptionalString1MB containing the json representation of the metadata
// only extracts if campaign.SaveBrowserMetadata is enabled
func ExtractCampaignEventMetadata(ctx *gin.Context, campaign *Campaign) *vo.OptionalString1MB {
	// check if metadata collection is enabled for this campaign
	if saveBrowserMetadata, err := campaign.SaveBrowserMetadata.Get(); err != nil || !saveBrowserMetadata {
		return vo.NewEmptyOptionalString1MB()
	}
	// extract ja4 from context or header
	ja4 := ""
	if fingerprint, exists := ctx.Get(contextKeyJA4); exists {
		if fp, ok := fingerprint.(string); ok {
			ja4 = fp
		}
	}
	if ja4 == "" {
		ja4 = ctx.Request.Header.Get(headerJA4)
	}

	metadata := CampaignEventMetadata{
		JA4Fingerprint: ja4,
		Platform:       ctx.Request.Header.Get("Sec-CH-UA-Platform"),
		AcceptLanguage: ctx.Request.Header.Get("Accept-Language"),
	}

	// only create json if at least one field is populated
	if metadata.JA4Fingerprint == "" && metadata.Platform == "" && metadata.AcceptLanguage == "" {
		return vo.NewEmptyOptionalString1MB()
	}

	jsonData, err := json.Marshal(metadata)
	if err != nil {
		// if marshaling fails, return empty
		return vo.NewEmptyOptionalString1MB()
	}

	result, err := vo.NewOptionalString1MB(string(jsonData))
	if err != nil {
		// if data is too large, return empty
		return vo.NewEmptyOptionalString1MB()
	}

	return result
}

// ExtractCampaignEventMetadataFromHTTPRequest extracts ja4 fingerprint, sec-ch-ua-platform, and accept-language from http.Request
// returns an OptionalString1MB containing the json representation of the metadata
// only extracts if campaign.SaveBrowserMetadata is enabled
func ExtractCampaignEventMetadataFromHTTPRequest(req *http.Request, campaign *Campaign) *vo.OptionalString1MB {
	// check if metadata collection is enabled for this campaign
	if saveBrowserMetadata, err := campaign.SaveBrowserMetadata.Get(); err != nil || !saveBrowserMetadata {
		return vo.NewEmptyOptionalString1MB()
	}
	metadata := CampaignEventMetadata{
		JA4Fingerprint: req.Header.Get(headerJA4),
		Platform:       req.Header.Get("Sec-CH-UA-Platform"),
		AcceptLanguage: req.Header.Get("Accept-Language"),
	}

	// only create json if at least one field is populated
	if metadata.JA4Fingerprint == "" && metadata.Platform == "" && metadata.AcceptLanguage == "" {
		return vo.NewEmptyOptionalString1MB()
	}

	jsonData, err := json.Marshal(metadata)
	if err != nil {
		// if marshaling fails, return empty
		return vo.NewEmptyOptionalString1MB()
	}

	result, err := vo.NewOptionalString1MB(string(jsonData))
	if err != nil {
		// if data is too large, return empty
		return vo.NewEmptyOptionalString1MB()
	}

	return result
}
