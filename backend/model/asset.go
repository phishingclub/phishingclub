package model

import (
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"golang.org/x/net/http/httpguts"
)

// Asset is a file Asset entity
type Asset struct {
	ID          nullable.Nullable[uuid.UUID]            `json:"id"`
	CreatedAt   *time.Time                              `json:"createdAt"`
	UpdatedAt   *time.Time                              `json:"updatedAt"`
	CompanyID   nullable.Nullable[uuid.UUID]            `json:"companyID"`
	DomainName  nullable.Nullable[vo.String255]         `json:"domainName"`
	DomainID    nullable.Nullable[uuid.UUID]            `json:"domainID"`
	Name        nullable.Nullable[vo.OptionalString127] `json:"name"`
	Description nullable.Nullable[vo.OptionalString255] `json:"description"`
	Path        nullable.Nullable[vo.RelativeFilePath]  `json:"path"`
	// ContentType is the media type sent when the asset is served, empty means
	// the type is found from the file extension
	ContentType nullable.Nullable[vo.OptionalString255] `json:"contentType"`
	// DetectedContentType is the type found from the file extension, read only
	DetectedContentType string         `json:"detectedContentType"`
	Headers             []*AssetHeader `json:"headers"`
	// AllowDeny are the allow deny lists that decide who can get the file, read
	// only, they are set with their own endpoint
	AllowDeny []*AllowDeny `json:"allowDeny"`
	// AllowDenyCompanyID is the company whose lists can be used on the asset
	// together with the global lists. Null means only global lists, read only
	AllowDenyCompanyID nullable.Nullable[uuid.UUID] `json:"allowDenyCompanyID"`
	// HasAllowDeny tells if the asset has allow deny lists, set in listings,
	// read only
	HasAllowDeny bool                 `json:"hasAllowDeny"`
	File         multipart.FileHeader `json:"-"`
}

// Validate checks if the Asset has a valid state
func (a *Asset) Validate() error {
	if err := validate.NullableFieldRequired("name", a.Name); err != nil {
		return err
	}
	if ct, err := a.ContentType.Get(); err == nil {
		if err := ValidateAssetContentType(ct.String()); err != nil {
			return validate.WrapErrorWithField(err, "contentType")
		}
	}
	return nil
}

// ValidateAssetContentType checks that a custom content type is empty or a
// media type that can be parsed
func ValidateAssetContentType(contentType string) error {
	if contentType == "" {
		return nil
	}
	if !httpguts.ValidHeaderFieldValue(contentType) {
		return errs.NewValidationError(errors.New("content type contains invalid characters"))
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return errs.NewValidationError(fmt.Errorf("invalid content type: %w", err))
	}
	// a media type is a type and a subtype, such as text/html
	if major, minor, ok := strings.Cut(mediaType, "/"); !ok || major == "" || minor == "" {
		return errs.NewValidationError(errors.New("content type must be a type and subtype, such as text/html"))
	}
	return nil
}

// blockedAssetHeaders are response headers an asset can not set, because they
// control how the response itself is sent and would break it. Content-Type is
// set with the content type field instead.
var blockedAssetHeaders = map[string]bool{
	"Content-Length":    true,
	"Transfer-Encoding": true,
	"Connection":        true,
	"Keep-Alive":        true,
	"Upgrade":           true,
	"Te":                true,
	"Trailer":           true,
	"Proxy-Connection":  true,
	"Content-Type":      true,
}

// AssetHeader is a response header sent when a specific asset is served
type AssetHeader struct {
	ID        uuid.UUID                        `json:"id"`
	CreatedAt *time.Time                       `json:"createdAt"`
	UpdatedAt *time.Time                       `json:"updatedAt"`
	AssetID   nullable.Nullable[uuid.UUID]     `json:"assetID"`
	Key       nullable.Nullable[vo.String127]  `json:"key"`
	Value     nullable.Nullable[vo.String2048] `json:"value"`
}

// Validate checks that the header can be sent as a response header
func (h *AssetHeader) Validate() error {
	if err := validate.NullableFieldRequired("assetID", h.AssetID); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("key", h.Key); err != nil {
		return err
	}
	if err := validate.NullableFieldRequired("value", h.Value); err != nil {
		return err
	}
	if err := ValidateAssetHeader(h.Key.MustGet().String(), h.Value.MustGet().String()); err != nil {
		return err
	}
	return nil
}

// ValidateAssetHeader checks a header name and value. The name must be a valid
// HTTP header name and not one of the blocked headers. The value must not hold
// CR, LF, NUL or other control characters except tab.
func ValidateAssetHeader(key string, value string) error {
	if !httpguts.ValidHeaderFieldName(key) {
		return validate.WrapErrorWithField(
			errs.NewValidationError(errors.New("invalid header name")),
			"key",
		)
	}
	if blockedAssetHeaders[http.CanonicalHeaderKey(key)] {
		msg := fmt.Sprintf("the %s header can not be set on an asset", http.CanonicalHeaderKey(key))
		if strings.EqualFold(key, "Content-Type") {
			msg = "use the content type field to set Content-Type"
		}
		return validate.WrapErrorWithField(
			errs.NewValidationError(errors.New(msg)),
			"key",
		)
	}
	if strings.ContainsAny(value, "\r\n\x00") || !httpguts.ValidHeaderFieldValue(value) {
		return validate.WrapErrorWithField(
			errs.NewValidationError(errors.New("header value contains invalid characters")),
			"value",
		)
	}
	return nil
}

// ToDBMap converts the fields that can be stored to a map
func (h *AssetHeader) ToDBMap() map[string]any {
	m := map[string]any{}
	if h.AssetID.IsSpecified() {
		if assetID, err := h.AssetID.Get(); err == nil {
			m["asset_id"] = assetID.String()
		}
	}
	if h.Key.IsSpecified() {
		m["key"] = nil
		if key, err := h.Key.Get(); err == nil {
			m["key"] = key.String()
		}
	}
	if h.Value.IsSpecified() {
		m["value"] = nil
		if value, err := h.Value.Get(); err == nil {
			m["value"] = value.String()
		}
	}
	return m
}

// ToDBMap converts the fields that can be stored or updated to a map
// if the value is nullable and not set, it is not included
// if the value is nullable and set, it is included, if it is null, it is set to nil
func (a *Asset) ToDBMap() map[string]any {
	m := map[string]any{}
	if a.CompanyID.IsSpecified() {
		if a.CompanyID.IsNull() {
			m["company_id"] = nil
		} else {
			m["company_id"] = a.CompanyID.MustGet()
		}
	}
	if a.DomainName.IsSpecified() {
		m["domain_name"] = nil
		if domainName, err := a.DomainName.Get(); err == nil {
			m["domain_name"] = domainName.String()
		}
	}
	// TODO is a global asset attached to a domain? if not then this should be possible to set to null like company ID
	if a.DomainID.IsSpecified() {
		m["domain_id"] = nil
		if domainID, err := a.DomainID.Get(); err == nil {
			m["domain_id"] = domainID.String()
		}
	}
	if a.Name.IsSpecified() {
		m["name"] = nil
		if name, err := a.Name.Get(); err == nil {
			m["name"] = name.String()
		}
	}
	if a.Description.IsSpecified() {
		m["description"] = nil
		if description, err := a.Description.Get(); err == nil {
			m["description"] = description.String()
		}
	}
	if a.Path.IsSpecified() {
		m["path"] = nil
		if path, err := a.Path.Get(); err == nil {
			m["path"] = path.String()
		}
	}
	if a.ContentType.IsSpecified() {
		m["content_type"] = ""
		if contentType, err := a.ContentType.Get(); err == nil {
			m["content_type"] = contentType.String()
		}
	}
	return m
}

// AssetServeSettings are the settings used when an asset file is served
type AssetServeSettings struct {
	AssetID     uuid.UUID
	ContentType string
	Headers     []AssetServeHeader
	// FilteredAssetIDs are the ids of every asset row stored for the file that
	// has allow deny lists. It holds more than one id when several rows point
	// at the same file, the visitor must pass the lists of each of them.
	FilteredAssetIDs []uuid.UUID
}

// AssetServeHeader is one response header sent with an asset
type AssetServeHeader struct {
	Key   string
	Value string
}
