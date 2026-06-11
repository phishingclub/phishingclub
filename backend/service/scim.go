package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// scim v2 resource types
const (
	scimResourceTypeUser  = "User"
	scimResourceTypeGroup = "Group"
)

// scim v2 schema URNs
const (
	scimSchemaUser            = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimSchemaEnterpriseUser  = "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"
	scimSchemaGroup           = "urn:ietf:params:scim:schemas:core:2.0:Group"
	scimSchemaCustomExtension = "urn:ietf:params:scim:schemas:extension:phishingclub:2.0:User"
	scimSchemaListResponse    = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimSchemaError           = "urn:ietf:params:scim:api:messages:2.0:Error"
	scimSchemaPatchOp         = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
)

// ScimUser is the SCIM v2 User resource representation used for both
// requests from the IdP and responses back to it.
type ScimUser struct {
	// schemas must always be present in responses
	Schemas  []string `json:"schemas"`
	ID       string   `json:"id,omitempty"`
	UserName string   `json:"userName"`
	// name sub-object
	Name *ScimName `json:"name,omitempty"`
	// flat display name (used if name sub-object absent)
	DisplayName string `json:"displayName,omitempty"`
	// core title attribute — Microsoft Entra maps the directory jobTitle here by
	// default (not the enterprise extension), so this is the primary source for Position
	Title string `json:"title,omitempty"`
	// emails list — we treat the first primary (or first) as canonical
	Emails []ScimEmail `json:"emails,omitempty"`
	// phone numbers list
	PhoneNumbers []ScimPhoneNumber `json:"phoneNumbers,omitempty"`
	// enterprise extension fields (department, title/position)
	// division is intentionally omitted — it is not stored
	EnterpriseUser *ScimEnterpriseUser `json:"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User,omitempty"`
	// addresses list — work address maps to city/country
	Addresses []ScimAddress `json:"addresses,omitempty"`
	// active flag — false means the account should be deprovisioned
	Active bool `json:"active"`
	// meta sub-object for responses
	Meta *ScimMeta `json:"meta,omitempty"`
	// externalId from IdP (stored in extra_identifier)
	ExternalID string `json:"externalId,omitempty"`
	// custom extension — misc/notes field
	CustomExtension *ScimCustomExtension `json:"urn:ietf:params:scim:schemas:extension:phishingclub:2.0:User,omitempty"`
	// groups the user is a member of — populated on responses, consumed on writes
	Groups []ScimUserGroup `json:"groups,omitempty"`
}

// ScimUserGroup is an entry in the groups array on a ScimUser resource.
// value is the group ID, display is the group display name.
type ScimUserGroup struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
}

// ScimName holds the structured name sub-object
type ScimName struct {
	Formatted  string `json:"formatted,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

// ScimEmail is a single email entry in the emails array
type ScimEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// ScimPhoneNumber is a single phone number entry
type ScimPhoneNumber struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// ScimAddress is a single address entry in the addresses array
type ScimAddress struct {
	Type      string `json:"type,omitempty"`
	Locality  string `json:"locality,omitempty"` // maps to city
	Country   string `json:"country,omitempty"`  // maps to country (ISO 3166-1)
	Primary   bool   `json:"primary,omitempty"`
	Formatted string `json:"formatted,omitempty"`
}

// ScimEnterpriseUser holds enterprise extension fields
type ScimEnterpriseUser struct {
	Department string `json:"department,omitempty"`
	Title      string `json:"title,omitempty"`
}

// ScimCustomExtension holds fields that have no standard SCIM home
type ScimCustomExtension struct {
	// Misc maps to recipient.misc — free-form notes
	Misc string `json:"misc,omitempty"`
}

// ScimMeta is the meta sub-object returned in responses
type ScimMeta struct {
	ResourceType string `json:"resourceType"`
	Location     string `json:"location,omitempty"`
}

// ScimListResponse is the SCIM v2 ListResponse envelope
type ScimListResponse struct {
	Schemas      []string   `json:"schemas"`
	TotalResults int        `json:"totalResults"`
	StartIndex   int        `json:"startIndex"`
	ItemsPerPage int        `json:"itemsPerPage"`
	Resources    []ScimUser `json:"Resources"`
}

// ScimError is the SCIM v2 error response body
type ScimError struct {
	Schemas  []string `json:"schemas"`
	Status   int      `json:"status"`
	Detail   string   `json:"detail,omitempty"`
	ScimType string   `json:"scimType,omitempty"`
}

// ScimPatchOp is the body of a PATCH request (RFC 7644 §3.5.2)
type ScimPatchOp struct {
	Schemas    []string          `json:"schemas"`
	Operations []ScimPatchOpItem `json:"Operations"`
}

// ScimPatchOpItem is a single operation within a PatchOp
type ScimPatchOpItem struct {
	Op    string `json:"op"`
	Path  string `json:"path,omitempty"`
	Value any    `json:"value,omitempty"`
}

// ScimServiceProviderConfig is the ServiceProviderConfig response (RFC 7643 §5)
type ScimServiceProviderConfig struct {
	Schemas               []string             `json:"schemas"`
	DocumentationURI      string               `json:"documentationUri,omitempty"`
	Patch                 ScimSupportedFeature `json:"patch"`
	Bulk                  ScimBulkFeature      `json:"bulk"`
	Filter                ScimFilterFeature    `json:"filter"`
	ChangePassword        ScimSupportedFeature `json:"changePassword"`
	Sort                  ScimSupportedFeature `json:"sort"`
	ETag                  ScimSupportedFeature `json:"etag"`
	AuthenticationSchemes []ScimAuthScheme     `json:"authenticationSchemes"`
	Meta                  *ScimMeta            `json:"meta,omitempty"`
}

// ScimSupportedFeature indicates whether a feature is supported
type ScimSupportedFeature struct {
	Supported bool `json:"supported"`
}

// ScimBulkFeature describes bulk support
type ScimBulkFeature struct {
	Supported      bool `json:"supported"`
	MaxOperations  int  `json:"maxOperations"`
	MaxPayloadSize int  `json:"maxPayloadSize"`
}

// ScimFilterFeature describes filter support
type ScimFilterFeature struct {
	Supported  bool `json:"supported"`
	MaxResults int  `json:"maxResults"`
}

// ScimAuthScheme describes a supported authentication scheme
type ScimAuthScheme struct {
	Type             string `json:"type"`
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	SpecURI          string `json:"specUri,omitempty"`
	DocumentationURI string `json:"documentationUri,omitempty"`
	Primary          bool   `json:"primary,omitempty"`
}

// ScimResourceType is an entry in /ResourceTypes
type ScimResourceType struct {
	Schemas          []string              `json:"schemas"`
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	Endpoint         string                `json:"endpoint"`
	Description      string                `json:"description,omitempty"`
	Schema           string                `json:"schema"`
	SchemaExtensions []ScimSchemaExtension `json:"schemaExtensions"`
	Meta             *ScimMeta             `json:"meta,omitempty"`
}

// ScimSchemaExtension is a schema extension reference within a ResourceType
type ScimSchemaExtension struct {
	Schema   string `json:"schema"`
	Required bool   `json:"required"`
}

// ScimSchemaAttribute describes a single attribute within a Schema document
type ScimSchemaAttribute struct {
	Name            string                `json:"name"`
	Type            string                `json:"type"`
	MultiValued     bool                  `json:"multiValued"`
	Description     string                `json:"description,omitempty"`
	Required        bool                  `json:"required"`
	CaseExact       bool                  `json:"caseExact"`
	Mutability      string                `json:"mutability"`
	Returned        string                `json:"returned"`
	Uniqueness      string                `json:"uniqueness"`
	SubAttributes   []ScimSchemaAttribute `json:"subAttributes,omitempty"`
	ReferenceTypes  []string              `json:"referenceTypes,omitempty"`
	CanonicalValues []string              `json:"canonicalValues,omitempty"`
}

// ScimSchema is a single Schema document returned by /Schemas
type ScimSchema struct {
	Schemas     []string              `json:"schemas"`
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Attributes  []ScimSchemaAttribute `json:"attributes"`
	Meta        *ScimMeta             `json:"meta,omitempty"`
}

// ScimGroup is the SCIM v2 Group resource representation.
// the IdP is the source of truth — groups are created, updated and deleted
// directly via the /Groups endpoints.
type ScimGroup struct {
	Schemas     []string          `json:"schemas"`
	ID          string            `json:"id,omitempty"`
	DisplayName string            `json:"displayName"`
	Members     []ScimGroupMember `json:"members,omitempty"`
	Meta        *ScimMeta         `json:"meta,omitempty"`
}

// ScimGroupMember is a member reference within a ScimGroup
type ScimGroupMember struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
}

// ScimConfigResult is returned by VerifyAndLoadConfig to the controller layer
type ScimConfigResult struct {
	Config    *model.CompanyScimConfig
	CompanyID *uuid.UUID
}

// ScimGroupPatchOp is the body of a PATCH /Groups/:id request
type ScimGroupPatchOp struct {
	Schemas    []string               `json:"schemas"`
	Operations []ScimGroupPatchOpItem `json:"Operations"`
}

// ScimGroupPatchOpItem is a single operation within a PATCH /Groups request.
// op is "add", "remove", or "replace". path is optional.
type ScimGroupPatchOpItem struct {
	Op    string `json:"op"`
	Path  string `json:"path,omitempty"`
	Value any    `json:"value,omitempty"`
}

// GetSchemaByID returns a single schema document by its URN.
// returns the schema and true if found, nil and false otherwise.
func (s *Scim) GetSchemaByID(baseURL string, id string) (*ScimSchema, bool) {
	for _, schema := range s.Schemas(baseURL) {
		if schema.ID == id {
			return &schema, true
		}
	}
	return nil, false
}

// GetResourceTypeByID returns a single resource type by its ID (e.g. "User" or "Group").
// returns the resource type and true if found, nil and false otherwise.
func (s *Scim) GetResourceTypeByID(baseURL string, id string) (*ScimResourceType, bool) {
	for _, rt := range s.ResourceTypes(baseURL) {
		if rt.ID == id {
			return &rt, true
		}
	}
	return nil, false
}

// Scim is the service that handles SCIM v2 protocol operations.
// it is called by the SCIM HTTP handler (controller/scim.go) after
// bearer-token authentication has already been verified.
type Scim struct {
	Common
	CompanyScimConfigRepository *repository.CompanyScimConfig
	CompanyScimConfigService    *CompanyScimConfig
	RecipientRepository         *repository.Recipient
	RecipientGroupRepository    *repository.RecipientGroup
	RecipientService            *Recipient
	OptionService               *Option
	CampaignRepository          *repository.Campaign
	CampaignRecipientRepository *repository.CampaignRecipient
}

// ServiceProviderConfig returns the static service provider configuration
// document describing what this SCIM implementation supports.
func (s *Scim) ServiceProviderConfig(baseURL string) *ScimServiceProviderConfig {
	return &ScimServiceProviderConfig{
		Schemas:        []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		Patch:          ScimSupportedFeature{Supported: true},
		Bulk:           ScimBulkFeature{Supported: false, MaxOperations: 0, MaxPayloadSize: 0},
		Filter:         ScimFilterFeature{Supported: true, MaxResults: 200},
		ChangePassword: ScimSupportedFeature{Supported: false},
		Sort:           ScimSupportedFeature{Supported: false},
		ETag:           ScimSupportedFeature{Supported: false},
		AuthenticationSchemes: []ScimAuthScheme{
			{
				Type:        "oauthbearertoken",
				Name:        "OAuth Bearer Token",
				Description: "authentication using a bearer token issued by this application",
				Primary:     true,
			},
		},
		Meta: &ScimMeta{
			ResourceType: "ServiceProviderConfig",
			Location:     baseURL + "/ServiceProviderConfig",
		},
	}
}

// ResourceTypes returns the list of supported resource types
func (s *Scim) ResourceTypes(baseURL string) []ScimResourceType {
	return []ScimResourceType{
		{
			Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
			ID:          "User",
			Name:        "User",
			Endpoint:    "/Users",
			Description: "user accounts",
			Schema:      scimSchemaUser,
			SchemaExtensions: []ScimSchemaExtension{
				{Schema: scimSchemaEnterpriseUser, Required: false},
				{Schema: scimSchemaCustomExtension, Required: false},
			},
			Meta: &ScimMeta{
				ResourceType: "ResourceType",
				Location:     baseURL + "/ResourceTypes/User",
			},
		},
		{
			Schemas:          []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
			ID:               "Group",
			Name:             "Group",
			Endpoint:         "/Groups",
			Description:      "recipient groups",
			Schema:           scimSchemaGroup,
			SchemaExtensions: []ScimSchemaExtension{},
			Meta: &ScimMeta{
				ResourceType: "ResourceType",
				Location:     baseURL + "/ResourceTypes/Group",
			},
		},
	}
}

// Schemas returns the hardcoded schema documents for all supported resource types.
// these are static — no database required.
func (s *Scim) Schemas(baseURL string) []ScimSchema {
	return []ScimSchema{
		{
			Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
			ID:          scimSchemaUser,
			Name:        "User",
			Description: "user account",
			Attributes: []ScimSchemaAttribute{
				{
					Name: "userName", Type: "string", MultiValued: false,
					Description: "unique identifier for the user",
					Required:    true, CaseExact: true,
					Mutability: "readWrite", Returned: "default", Uniqueness: "server",
				},
				{
					Name: "name", Type: "complex", MultiValued: false,
					Description: "the components of the user's name",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
					SubAttributes: []ScimSchemaAttribute{
						{Name: "givenName", Type: "string", MultiValued: false, Description: "first name", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
						{Name: "familyName", Type: "string", MultiValued: false, Description: "last name", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
						{Name: "formatted", Type: "string", MultiValued: false, Description: "full name", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
					},
				},
				{
					Name: "displayName", Type: "string", MultiValued: false,
					Description: "display name of the user",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
				},
				{
					Name: "emails", Type: "complex", MultiValued: true,
					Description: "email addresses for the user",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
					SubAttributes: []ScimSchemaAttribute{
						{Name: "value", Type: "string", MultiValued: false, Description: "email address", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
						{Name: "type", Type: "string", MultiValued: false, Description: "type of email address", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none", CanonicalValues: []string{"work", "home", "other"}},
						{Name: "primary", Type: "boolean", MultiValued: false, Description: "primary email indicator", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
					},
				},
				{
					Name: "phoneNumbers", Type: "complex", MultiValued: true,
					Description: "phone numbers for the user",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
					SubAttributes: []ScimSchemaAttribute{
						{Name: "value", Type: "string", MultiValued: false, Description: "phone number", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
						{Name: "type", Type: "string", MultiValued: false, Description: "type of phone number", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none", CanonicalValues: []string{"work", "home", "mobile", "other"}},
						{Name: "primary", Type: "boolean", MultiValued: false, Description: "primary phone indicator", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
					},
				},
				{
					Name: "addresses", Type: "complex", MultiValued: true,
					Description: "addresses for the user — work address maps to city and country fields",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
					SubAttributes: []ScimSchemaAttribute{
						{Name: "type", Type: "string", MultiValued: false, Description: "type of address", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none", CanonicalValues: []string{"work", "home", "other"}},
						{Name: "locality", Type: "string", MultiValued: false, Description: "city", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
						{Name: "country", Type: "string", MultiValued: false, Description: "country (ISO 3166-1 alpha-2 or full name)", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
						{Name: "primary", Type: "boolean", MultiValued: false, Description: "primary address indicator", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
						{Name: "formatted", Type: "string", MultiValued: false, Description: "full mailing address formatted for display", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
					},
				},
				{
					Name: "active", Type: "boolean", MultiValued: false,
					Description: "administrative status of the user — false removes them from all groups",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
				},
				{
					Name: "externalId", Type: "string", MultiValued: false,
					Description: "identifier from the provisioning client (stored as extraIdentifier — unique per company)",
					Required:    false, CaseExact: true,
					Mutability: "readWrite", Returned: "default", Uniqueness: "server",
				},
				{
					Name: "groups", Type: "complex", MultiValued: true,
					Description: "groups the user belongs to",
					Required:    false, CaseExact: false,
					Mutability: "readOnly", Returned: "request", Uniqueness: "none",
					SubAttributes: []ScimSchemaAttribute{
						{Name: "value", Type: "string", MultiValued: false, Description: "group ID", Required: false, CaseExact: false, Mutability: "readOnly", Returned: "default", Uniqueness: "none"},
						{Name: "display", Type: "string", MultiValued: false, Description: "display name of the group", Required: false, CaseExact: false, Mutability: "readOnly", Returned: "default", Uniqueness: "none"},
						{Name: "$ref", Type: "reference", MultiValued: false, Description: "URI of the group", Required: false, CaseExact: false, Mutability: "readOnly", Returned: "default", Uniqueness: "none"},
					},
				},
			},
			Meta: &ScimMeta{
				ResourceType: "Schema",
				Location:     baseURL + "/Schemas/" + scimSchemaUser,
			},
		},
		{
			Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
			ID:          scimSchemaEnterpriseUser,
			Name:        "EnterpriseUser",
			Description: "enterprise user extension attributes",
			Attributes: []ScimSchemaAttribute{
				{
					Name: "department", Type: "string", MultiValued: false,
					Description: "department the user belongs to (stored as department)",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
				},
				{
					Name: "title", Type: "string", MultiValued: false,
					Description: "job title / position (stored as position)",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
				},
				{
					Name: "manager", Type: "complex", MultiValued: false,
					Description: "the user's manager — not stored, accepted and silently ignored",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
					SubAttributes: []ScimSchemaAttribute{
						{Name: "value", Type: "string", MultiValued: false, Description: "manager user ID", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
						{Name: "$ref", Type: "reference", MultiValued: false, Description: "URI of the manager", Required: false, CaseExact: false, Mutability: "readWrite", Returned: "default", Uniqueness: "none"},
						{Name: "displayName", Type: "string", MultiValued: false, Description: "display name of the manager", Required: false, CaseExact: false, Mutability: "readOnly", Returned: "default", Uniqueness: "none"},
					},
				},
			},
			Meta: &ScimMeta{
				ResourceType: "Schema",
				Location:     baseURL + "/Schemas/" + scimSchemaEnterpriseUser,
			},
		},
		{
			Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
			ID:          scimSchemaCustomExtension,
			Name:        "PhishingClubUser",
			Description: "phishingclub-specific user extension attributes",
			Attributes: []ScimSchemaAttribute{
				{
					Name: "misc", Type: "string", MultiValued: false,
					Description: "free-form notes field (stored as misc)",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
				},
			},
			Meta: &ScimMeta{
				ResourceType: "Schema",
				Location:     baseURL + "/Schemas/" + scimSchemaCustomExtension,
			},
		},
		{
			Schemas:     []string{"urn:ietf:params:scim:schemas:core:2.0:Schema"},
			ID:          scimSchemaGroup,
			Name:        "Group",
			Description: "recipient group",
			Attributes: []ScimSchemaAttribute{
				{
					Name: "displayName", Type: "string", MultiValued: false,
					Description: "name of the group (unique per company)",
					Required:    true, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "server",
				},
				{
					Name: "members", Type: "complex", MultiValued: true,
					Description: "members of the group",
					Required:    false, CaseExact: false,
					Mutability: "readWrite", Returned: "default", Uniqueness: "none",
					SubAttributes: []ScimSchemaAttribute{
						{Name: "value", Type: "string", MultiValued: false, Description: "recipient ID", Required: false, CaseExact: false, Mutability: "immutable", Returned: "default", Uniqueness: "none"},
						{Name: "display", Type: "string", MultiValued: false, Description: "display name of the member", Required: false, CaseExact: false, Mutability: "readOnly", Returned: "default", Uniqueness: "none"},
						{Name: "$ref", Type: "reference", MultiValued: false, Description: "URI of the member user resource", Required: false, CaseExact: false, Mutability: "readOnly", Returned: "default", Uniqueness: "none"},
					},
				},
			},
			Meta: &ScimMeta{
				ResourceType: "Schema",
				Location:     baseURL + "/Schemas/" + scimSchemaGroup,
			},
		},
	}
}

// ListGroupsRaw returns all recipient groups for this company wrapped in a
// spec-compliant ListResponse. the IdP owns group creation so all company
// groups are visible. supports startIndex and count query parameters.
func (s *Scim) ListGroupsRaw(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	baseURL string,
	startIndex int,
	count int,
	filter string,
	excludedAttributes string,
) (any, error) {
	groups, err := s.RecipientGroupRepository.GetAllByCompanyID(ctx, companyID, &repository.RecipientGroupOption{
		WithRecipients: true,
	})
	if err != nil {
		s.Logger.Errorw("scim list groups: failed to list groups", "error", err)
		return nil, errs.Wrap(err)
	}

	excludeMembers := scimExcludesAttribute(excludedAttributes, "members")

	all := make([]ScimGroup, 0, len(groups))
	for _, g := range groups {
		if filter != "" && !scimGroupFilterMatches(filter, g) {
			continue
		}
		sg := recipientGroupToScimGroup(g, baseURL)
		if excludeMembers {
			sg.Members = nil
		}
		all = append(all, sg)
	}

	total := len(all)

	// apply startIndex (1-based per rfc 7644 §3.4.2)
	if startIndex < 1 {
		startIndex = 1
	}
	offset := startIndex - 1
	if offset > total {
		offset = total
	}
	all = all[offset:]

	// apply count — 0 returns zero resources (RFC 7644 §3.4.2.4); a negative or
	// absent value means no limit
	if count == 0 {
		all = []ScimGroup{}
	} else if count > 0 && count < len(all) {
		all = all[:count]
	}

	return map[string]any{
		"schemas":      []string{scimSchemaListResponse},
		"totalResults": total,
		"startIndex":   startIndex,
		"itemsPerPage": len(all),
		"Resources":    all,
	}, nil
}

// GetGroup returns a single group by ID as a SCIM Group resource.
func (s *Scim) GetGroup(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	groupID *uuid.UUID,
	baseURL string,
) (*ScimGroup, error) {
	group, err := s.RecipientGroupRepository.GetByID(ctx, groupID, &repository.RecipientGroupOption{
		WithRecipients: true,
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.Wrap(gorm.ErrRecordNotFound)
		}
		s.Logger.Errorw("scim get group: failed to get group", "error", err)
		return nil, errs.Wrap(err)
	}
	// ensure the group belongs to this company
	gCompanyID, err := group.CompanyID.Get()
	if err != nil || gCompanyID != *companyID {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	g := recipientGroupToScimGroup(group, baseURL)
	return &g, nil
}

// CreateGroup provisions a new recipient group from a SCIM Group resource.
// the IdP is the source of truth — it chooses the display name and membership.
func (s *Scim) CreateGroup(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	req *ScimGroup,
	baseURL string,
) (*ScimGroup, error) {
	if strings.TrimSpace(req.DisplayName) == "" {
		return nil, errs.NewSyntaxError(fmt.Errorf("displayName is required"))
	}

	nameVO, err := vo.NewString127(req.DisplayName)
	if err != nil {
		return nil, errs.NewSyntaxError(fmt.Errorf("displayName too long"))
	}

	rg := &model.RecipientGroup{
		Name:      nullable.NewNullableWithValue(*nameVO),
		CompanyID: nullable.NewNullableWithValue(*companyID),
	}
	groupID, err := s.RecipientGroupRepository.Insert(ctx, rg)
	if err != nil {
		if isScimUniqueConflict(err) {
			return nil, errs.NewConflictError(fmt.Errorf("a group named %q already exists", req.DisplayName))
		}
		s.Logger.Errorw("scim create group: failed to insert group", "error", err)
		return nil, errs.Wrap(err)
	}

	// add any members supplied in the create request
	if len(req.Members) > 0 {
		if err := s.applyGroupMembers(ctx, companyID, groupID, req.Members); err != nil {
			return nil, err
		}
	}

	created, err := s.RecipientGroupRepository.GetByID(ctx, groupID, &repository.RecipientGroupOption{
		WithRecipients: true,
	})
	if err != nil {
		s.Logger.Errorw("scim create group: failed to reload group", "error", err)
		return nil, errs.Wrap(err)
	}
	s.auditScim("Scim.CreateGroup", config, map[string]any{"groupID": groupID.String()})
	g := recipientGroupToScimGroup(created, baseURL)
	return &g, nil
}

// ReplaceGroup performs a full replacement (PUT) of an existing group.
// the display name is updated and membership is replaced wholesale.
func (s *Scim) ReplaceGroup(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	groupID *uuid.UUID,
	req *ScimGroup,
	baseURL string,
) (*ScimGroup, error) {
	existing, err := s.RecipientGroupRepository.GetByID(ctx, groupID, &repository.RecipientGroupOption{
		WithRecipients: true,
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.Wrap(gorm.ErrRecordNotFound)
		}
		return nil, errs.Wrap(err)
	}
	// ensure the group belongs to this company
	gCompanyID, compErr := existing.CompanyID.Get()
	if compErr != nil || gCompanyID != *companyID {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}

	if strings.TrimSpace(req.DisplayName) != "" {
		nameVO, err := vo.NewString127(req.DisplayName)
		if err != nil {
			return nil, errs.NewValidationError(fmt.Errorf("displayName too long"))
		}
		existing.Name = nullable.NewNullableWithValue(*nameVO)
		if err := s.RecipientGroupRepository.UpdateByID(ctx, groupID, existing); err != nil {
			s.Logger.Errorw("scim replace group: failed to update group name", "error", err)
			return nil, errs.Wrap(err)
		}
	}

	// replace membership: remove everyone then add the supplied list
	if err := s.replaceGroupMembers(ctx, companyID, groupID, existing.Recipients, req.Members); err != nil {
		return nil, err
	}

	updated, err := s.RecipientGroupRepository.GetByID(ctx, groupID, &repository.RecipientGroupOption{
		WithRecipients: true,
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	s.auditScim("Scim.ReplaceGroup", config, map[string]any{"groupID": groupID.String()})
	g := recipientGroupToScimGroup(updated, baseURL)
	return &g, nil
}

// PatchGroup applies a SCIM PatchOp to an existing group.
// supported operations: replace displayName, add/remove members.
func (s *Scim) PatchGroup(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	groupID *uuid.UUID,
	patch *ScimGroupPatchOp,
	baseURL string,
) (*ScimGroup, error) {
	existing, err := s.RecipientGroupRepository.GetByID(ctx, groupID, &repository.RecipientGroupOption{
		WithRecipients: true,
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.Wrap(gorm.ErrRecordNotFound)
		}
		return nil, errs.Wrap(err)
	}
	// ensure the group belongs to this company
	gCompanyID, compErr := existing.CompanyID.Get()
	if compErr != nil || gCompanyID != *companyID {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}

	for _, op := range patch.Operations {
		switch strings.ToLower(op.Op) {
		case "replace":
			if err := s.applyGroupPatchReplace(ctx, companyID, existing, groupID, op); err != nil {
				return nil, err
			}
		case "add":
			members := groupMembersFromPatchValue(op.Value)
			if err := s.applyGroupMembers(ctx, companyID, groupID, members); err != nil {
				return nil, err
			}
		case "remove":
			// path can be "members" (value array) or "members[value eq \"<id>\"]" (filter form)
			members := groupMembersFromPatchPath(op.Path, op.Value)
			if len(members) > 0 {
				if err := s.removeGroupMembers(ctx, companyID, groupID, members); err != nil {
					return nil, err
				}
			}
		}
	}

	updated, err := s.RecipientGroupRepository.GetByID(ctx, groupID, &repository.RecipientGroupOption{
		WithRecipients: true,
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	s.auditScim("Scim.PatchGroup", config, map[string]any{"groupID": groupID.String()})
	g := recipientGroupToScimGroup(updated, baseURL)
	return &g, nil
}

// DeleteGroup removes a recipient group provisioned via SCIM.
// members are removed from the group but not deleted from the system.
func (s *Scim) DeleteGroup(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	groupID *uuid.UUID,
) error {
	existing, err := s.RecipientGroupRepository.GetByID(ctx, groupID, &repository.RecipientGroupOption{})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.Wrap(gorm.ErrRecordNotFound)
		}
		return errs.Wrap(err)
	}
	// ensure the group belongs to this company
	gCompanyID, compErr := existing.CompanyID.Get()
	if compErr != nil || gCompanyID != *companyID {
		return errs.Wrap(gorm.ErrRecordNotFound)
	}
	// unlink the group from any campaigns before deleting it so foreign key
	// constraints on campaign_recipient_groups do not block the delete
	if err := s.CampaignRepository.RemoveCampaignRecipientGroupByGroupID(ctx, groupID); err != nil {
		s.Logger.Errorw("scim delete group: failed to remove group from campaigns", "error", err)
		return errs.Wrap(err)
	}
	if err := s.RecipientGroupRepository.DeleteByID(ctx, groupID); err != nil {
		s.Logger.Errorw("scim delete group: failed to delete group", "error", err)
		return errs.Wrap(err)
	}
	s.auditScim("Scim.DeleteGroup", config, map[string]any{"groupID": groupID.String()})
	return nil
}

// recipientGroupToScimGroup maps a model.RecipientGroup to a ScimGroup
func recipientGroupToScimGroup(group *model.RecipientGroup, baseURL string) ScimGroup {
	id := ""
	if gid, err := group.ID.Get(); err == nil {
		id = gid.String()
	}
	name := ""
	if n, err := group.Name.Get(); err == nil {
		name = n.String()
	}

	members := make([]ScimGroupMember, 0, len(group.Recipients))
	for _, r := range group.Recipients {
		rid, err := r.ID.Get()
		if err != nil {
			continue
		}
		display := ""
		if fn, err := r.FirstName.Get(); err == nil {
			display = fn.String()
		}
		if ln, err := r.LastName.Get(); err == nil {
			if display != "" {
				display += " "
			}
			display += ln.String()
		}
		if display == "" {
			if e, err := r.Email.Get(); err == nil {
				display = e.String()
			}
		}
		ref := ""
		if baseURL != "" {
			ref = baseURL + "/Users/" + rid.String()
		}
		members = append(members, ScimGroupMember{
			Value:   rid.String(),
			Display: display,
			Ref:     ref,
		})
	}

	g := ScimGroup{
		Schemas:     []string{scimSchemaGroup},
		ID:          id,
		DisplayName: name,
		Members:     members,
	}
	if baseURL != "" && id != "" {
		g.Meta = &ScimMeta{
			ResourceType: scimResourceTypeGroup,
			Location:     baseURL + "/Groups/" + id,
		}
	}
	return g
}

// ListUsers returns a SCIM ListResponse of all recipients belonging to this
// company. all provisioned users are visible regardless of group membership.
// supports filter, startIndex, count, sortBy and sortOrder query parameters.
func (s *Scim) ListUsers(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	baseURL string,
	filter string,
	startIndex int,
	count int,
	sortBy string,
	sortOrder string,
) (*ScimListResponse, error) {
	recipientResult, err := s.RecipientRepository.GetAllByCompanyID(ctx, companyID, &repository.RecipientOption{})
	if err != nil {
		s.Logger.Errorw("scim list users: failed to list recipients", "error", err)
		return nil, errs.Wrap(err)
	}

	// load group memberships once so each user can report its groups
	groupsByRecipient := map[uuid.UUID][]ScimUserGroup{}
	if groupList, gErr := s.RecipientGroupRepository.GetAllByCompanyID(ctx, companyID, &repository.RecipientGroupOption{WithRecipients: true}); gErr != nil {
		s.Logger.Warnw("scim list users: failed to load group memberships", "error", gErr)
	} else {
		groupsByRecipient = buildGroupsByRecipient(groupList)
	}

	// build the full filtered list first so totalResults is accurate
	all := make([]ScimUser, 0, len(recipientResult.Rows))
	for _, r := range recipientResult.Rows {
		u := recipientToScimUser(r, baseURL)
		if rid, idErr := r.ID.Get(); idErr == nil {
			u.Groups = groupsByRecipient[rid]
		}
		if filter != "" && !scimFilterMatchesUser(filter, u) {
			continue
		}
		all = append(all, u)
	}

	// sort if requested
	if sortBy != "" {
		scimSortUsers(all, sortBy, sortOrder)
	}

	total := len(all)

	// apply startIndex (1-based per rfc 7644 §3.4.2)
	if startIndex < 1 {
		startIndex = 1
	}
	offset := startIndex - 1
	if offset > total {
		offset = total
	}
	all = all[offset:]

	// apply count — 0 returns zero resources (RFC 7644 §3.4.2.4); a negative or
	// absent value means no limit
	if count == 0 {
		all = []ScimUser{}
	} else if count > 0 && count < len(all) {
		all = all[:count]
	}

	return &ScimListResponse{
		Schemas:      []string{scimSchemaListResponse},
		TotalResults: total,
		StartIndex:   startIndex,
		ItemsPerPage: len(all),
		Resources:    all,
	}, nil
}

// GetUser returns a single SCIM User resource by recipient ID.
func (s *Scim) GetUser(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	recipientID *uuid.UUID,
	baseURL string,
) (*ScimUser, error) {
	recipient, err := s.RecipientRepository.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.Wrap(gorm.ErrRecordNotFound)
		}
		s.Logger.Errorw("scim get user: failed to get recipient", "error", err, "recipientID", recipientID.String())
		return nil, errs.Wrap(err)
	}
	// ensure the recipient belongs to this company
	rCompanyID, compErr := recipient.CompanyID.Get()
	if compErr != nil || rCompanyID != *companyID {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	u := recipientToScimUser(recipient, baseURL)
	u.Groups = s.groupsForRecipient(ctx, companyID, recipientID, baseURL)
	return &u, nil
}

// CreateUser provisions a new recipient from a SCIM User resource.
// duplicate userName within the same company returns a 409 ConflictError.
// group membership from the groups array is applied after the recipient is persisted.
func (s *Scim) CreateUser(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	scimUser *ScimUser,
	baseURL string,
) (*ScimUser, error) {
	email, err := canonicalEmail(scimUser)
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	emailVO, err := vo.NewEmail(email)
	if err != nil {
		return nil, errs.NewValidationError(fmt.Errorf("invalid email %q: %w", email, err))
	}

	// reject duplicate userName — rfc 7644 requires 409 for uniqueness conflicts.
	// the lookup is case-insensitive so John@X.com and john@x.com collide.
	existingByEmail, err := s.RecipientRepository.GetByEmailLowerAndCompanyID(ctx, emailVO, companyID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.Logger.Errorw("scim create user: lookup by email failed", "error", err, "email", email)
		return nil, errs.Wrap(err)
	}
	if existingByEmail != nil {
		// if the existing recipient was SCIM soft-deleted, the IdP is re-provisioning
		// the same person — revive and update it instead of returning a conflict
		if existingByEmail.ScimSoftDeletedAt != nil {
			existingID := existingByEmail.ID.MustGet()
			if err := s.RecipientRepository.ClearScimSoftDeleted(ctx, &existingID); err != nil {
				return nil, errs.Wrap(err)
			}
			existingByEmail.ScimSoftDeletedAt = nil
			if err := s.applyScimUserToRecipient(ctx, existingByEmail, scimUser); err != nil {
				return nil, err
			}
			if err := s.syncUserGroupMembership(ctx, companyID, &existingID, scimUser.Groups); err != nil {
				s.Logger.Warnw("scim create user (revive): failed to sync group membership", "error", err)
			}
			revived, err := s.RecipientRepository.GetByID(ctx, &existingID, &repository.RecipientOption{})
			if err != nil {
				return nil, errs.Wrap(err)
			}
			s.auditScim("Scim.CreateUser", config, map[string]any{"recipientID": existingID.String(), "revived": true})
			u := recipientToScimUser(revived, baseURL)
			return &u, nil
		}
		return nil, errs.NewConflictError(fmt.Errorf("a user with userName %q already exists", scimUserNameFrom(scimUser)))
	}

	// create new recipient
	var recipientID *uuid.UUID
	r := scimUserToRecipient(scimUser, companyID)
	id, err := s.RecipientRepository.Insert(ctx, r)
	if err != nil {
		if isScimUniqueConflict(err) {
			return nil, errs.NewConflictError(fmt.Errorf("a user with userName %q already exists", scimUserNameFrom(scimUser)))
		}
		s.Logger.Errorw("scim create user: failed to insert recipient", "error", err)
		return nil, errs.Wrap(err)
	}
	recipientID = id

	// add to any groups specified in the request
	if err := s.syncUserGroupMembership(ctx, companyID, recipientID, scimUser.Groups); err != nil {
		s.Logger.Warnw("scim create user: failed to sync group membership", "error", err)
	}

	created, err := s.RecipientRepository.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		s.Logger.Errorw("scim create user: failed to reload recipient", "error", err)
		return nil, errs.Wrap(err)
	}
	// note: active=false on create is not separately representable — a recipient
	// either exists (active) or is deprovisioned (deleted). the resource is still
	// created so the IdP receives a retrievable 201 response.
	s.auditScim("Scim.CreateUser", config, map[string]any{"recipientID": recipientID.String()})
	u := recipientToScimUser(created, baseURL)
	return &u, nil
}

// deprovisionedUserResponse builds the SCIM body returned after a user has been
// deprovisioned via active=false. Microsoft Entra sends a soft-delete (PATCH
// active=false) and expects a 200 response with the resource showing
// active=false. Returning 404 makes Entra log the disable as a failure and retry
// it every sync cycle, so the recipient is hard-deleted but a success body with
// active=false is returned.
func deprovisionedUserResponse(existing *model.Recipient, baseURL string) *ScimUser {
	u := recipientToScimUser(existing, baseURL)
	u.Active = false
	return &u
}

// ReplaceUser performs a full replacement (PUT) of an existing recipient from
// a SCIM User resource. group membership is replaced if a groups array is present.
func (s *Scim) ReplaceUser(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	recipientID *uuid.UUID,
	scimUser *ScimUser,
	baseURL string,
) (*ScimUser, error) {
	existing, err := s.RecipientRepository.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.Wrap(gorm.ErrRecordNotFound)
		}
		return nil, errs.Wrap(err)
	}
	// ensure the recipient belongs to this company
	rCompanyID, compErr := existing.CompanyID.Get()
	if compErr != nil || rCompanyID != *companyID {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}
	// a PUT with active=false is a deprovision request — hard-delete the recipient
	// but return 200 with active=false so the IdP records the disable as a success
	if !scimUser.Active {
		if err := s.deprovisionRecipient(ctx, recipientID); err != nil {
			return nil, errs.Wrap(err)
		}
		s.auditScim("Scim.DeprovisionUser", config, map[string]any{"recipientID": recipientID.String(), "via": "replace"})
		return deprovisionedUserResponse(existing, baseURL), nil
	}
	// active=true: revive a previously soft-deleted recipient
	if existing.ScimSoftDeletedAt != nil {
		if err := s.RecipientRepository.ClearScimSoftDeleted(ctx, recipientID); err != nil {
			return nil, errs.Wrap(err)
		}
		existing.ScimSoftDeletedAt = nil
	}
	if err := s.applyScimUserToRecipient(ctx, existing, scimUser); err != nil {
		return nil, err
	}
	if len(scimUser.Groups) > 0 {
		if err := s.syncUserGroupMembership(ctx, companyID, recipientID, scimUser.Groups); err != nil {
			s.Logger.Warnw("scim replace user: failed to sync group membership", "error", err)
		}
	}
	updated, err := s.RecipientRepository.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	s.auditScim("Scim.ReplaceUser", config, map[string]any{"recipientID": recipientID.String()})
	u := recipientToScimUser(updated, baseURL)
	return &u, nil
}

// PatchUser applies a SCIM PatchOp to an existing recipient.
// supported operations: replace on top-level attributes and the active flag.
// active=false removes the recipient from all scim-managed groups (safe deprovision).
func (s *Scim) PatchUser(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	recipientID *uuid.UUID,
	patch *ScimPatchOp,
	baseURL string,
) (*ScimUser, error) {
	existing, err := s.RecipientRepository.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.Wrap(gorm.ErrRecordNotFound)
		}
		return nil, errs.Wrap(err)
	}
	// ensure the recipient belongs to this company
	rCompanyID, compErr := existing.CompanyID.Get()
	if compErr != nil || rCompanyID != *companyID {
		return nil, errs.Wrap(gorm.ErrRecordNotFound)
	}

	for _, op := range patch.Operations {
		switch strings.ToLower(op.Op) {
		case "replace", "add":
			deactivated, err := s.applyPatchOperation(ctx, existing, config, recipientID, op)
			if err != nil {
				return nil, err
			}
			// active=false triggers a hard-delete; return 200 with active=false so
			// the IdP records the soft-delete as a success instead of retrying
			if deactivated {
				s.auditScim("Scim.DeprovisionUser", config, map[string]any{"recipientID": recipientID.String(), "via": "patch"})
				return deprovisionedUserResponse(existing, baseURL), nil
			}
		case "remove":
			// remove op on "active" means deactivate — hard-delete the recipient
			if strings.EqualFold(op.Path, "active") {
				if err := s.deprovisionRecipient(ctx, recipientID); err != nil {
					return nil, errs.Wrap(err)
				}
				s.auditScim("Scim.DeprovisionUser", config, map[string]any{"recipientID": recipientID.String(), "via": "patch"})
				return deprovisionedUserResponse(existing, baseURL), nil
			}
		}
	}

	updated, err := s.RecipientRepository.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	s.auditScim("Scim.PatchUser", config, map[string]any{"recipientID": recipientID.String()})
	u := recipientToScimUser(updated, baseURL)
	return &u, nil
}

// DeprovisionUser hard-deletes a recipient provisioned via SCIM.
// a subsequent GET returns 404, satisfying the validator expectation that
// a deleted user is no longer accessible.
func (s *Scim) DeprovisionUser(
	ctx context.Context,
	companyID *uuid.UUID,
	config *model.CompanyScimConfig,
	recipientID *uuid.UUID,
) error {
	existing, err := s.RecipientRepository.GetByID(ctx, recipientID, &repository.RecipientOption{})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.Wrap(gorm.ErrRecordNotFound)
		}
		return errs.Wrap(err)
	}
	// ensure the recipient belongs to this company
	rCompanyID, compErr := existing.CompanyID.Get()
	if compErr != nil || rCompanyID != *companyID {
		return errs.Wrap(gorm.ErrRecordNotFound)
	}
	if err := s.deprovisionRecipient(ctx, recipientID); err != nil {
		return errs.Wrap(err)
	}
	s.auditScim("Scim.DeprovisionUser", config, map[string]any{"recipientID": recipientID.String(), "via": "delete"})
	return nil
}

// VerifyAndLoadConfig authenticates the bearer token against the stored hash
// for the given company and returns the active SCIM config.
// returns (config, authed, error). authed is false when the token is wrong.
func (s *Scim) VerifyAndLoadConfig(
	ctx context.Context,
	companyID *uuid.UUID,
	plainToken string,
) (*model.CompanyScimConfig, bool, error) {
	ok, config, err := s.CompanyScimConfigService.VerifyToken(ctx, companyID, plainToken)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	if config == nil || !config.Enabled {
		return config, false, nil
	}
	return config, true, nil
}

// UpdateLastSync stamps the last sync time for the config
func (s *Scim) UpdateLastSync(ctx context.Context, config *model.CompanyScimConfig) {
	id, err := config.ID.Get()
	if err != nil {
		return
	}
	if err := s.CompanyScimConfigRepository.UpdateLastSyncAt(ctx, &id); err != nil {
		s.Logger.Warnw("scim: failed to update last_sync_at", "error", err, "configID", id.String())
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// deprovisionRecipient removes a recipient from all groups and hard-deletes it.
// shared by DELETE, PUT active=false and PATCH active=false.
func (s *Scim) deprovisionRecipient(ctx context.Context, recipientID *uuid.UUID) error {
	// mark the recipient as SCIM soft-deleted; the anonymizing delete runs only
	// after the retention grace period (scheduled job or on-demand prune)
	if err := s.RecipientRepository.MarkScimSoftDeleted(ctx, recipientID, time.Now()); err != nil {
		return err
	}
	// cancel pending sends in active campaigns so no email reaches the disabled
	// recipient; the rows are kept (cancelled, not deleted) so stats and any
	// already-sent tracking links stay consistent
	return s.CampaignRecipientRepository.CancelInActiveCampaigns(ctx, recipientID)
}

// reviveIfSoftDeleted clears the soft-delete mark when the IdP re-activates a
// recipient. It is a no-op when the recipient is not soft-deleted.
func (s *Scim) reviveIfSoftDeleted(ctx context.Context, existing *model.Recipient, recipientID *uuid.UUID) error {
	if existing.ScimSoftDeletedAt == nil {
		return nil
	}
	if err := s.RecipientRepository.ClearScimSoftDeleted(ctx, recipientID); err != nil {
		return err
	}
	existing.ScimSoftDeletedAt = nil
	return nil
}

// PruneSoftDeleted runs the anonymizing delete for SCIM-disabled recipients whose
// retention window has elapsed. A nil companyID prunes across all companies.
// No authorization check — callers (the scheduled job and the authorized wrapper)
// are responsible for that. Returns the number pruned.
func (s *Scim) PruneSoftDeleted(ctx context.Context, companyID *uuid.UUID) (int, error) {
	days, err := s.OptionService.GetScimSoftDeleteRetentionDaysInternal(ctx)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	recipients, err := s.RecipientRepository.GetScimSoftDeletedBefore(ctx, companyID, cutoff)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	pruned := 0
	for _, r := range recipients {
		id, idErr := r.ID.Get()
		if idErr != nil {
			continue
		}
		if delErr := s.RecipientService.deleteRecipientByID(ctx, &id); delErr != nil {
			s.Logger.Errorw("scim prune: failed to delete soft-deleted recipient", "error", delErr, "recipientID", id.String())
			continue
		}
		pruned++
	}
	return pruned, nil
}

// PruneSoftDeletedAuthorized is the admin (session-authenticated) entry point for
// the on-demand prune of a company's disabled recipients past the threshold.
func (s *Scim) PruneSoftDeletedAuthorized(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (int, error) {
	ae := NewAuditEvent("Scim.PruneSoftDeletedAuthorized", session)
	if companyID != nil {
		ae.Details["companyID"] = companyID.String()
	}
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return 0, errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return 0, errs.ErrAuthorizationFailed
	}
	pruned, err := s.PruneSoftDeleted(ctx, companyID)
	if err != nil {
		return 0, err
	}
	ae.Details["pruned"] = pruned
	s.AuditLogAuthorized(ae)
	return pruned, nil
}

// auditScim emits an audit event for an externally driven SCIM mutation.
// SCIM has no admin session, so the actor is identified by the company and the
// token prefix instead of a user id.
func (s *Scim) auditScim(name string, config *model.CompanyScimConfig, details map[string]any) {
	ae := NewAuditEvent(name, nil)
	ae.Details["actor"] = "scim"
	if config != nil {
		if cid, err := config.CompanyID.Get(); err == nil {
			ae.Details["companyID"] = cid.String()
		}
		if tp, err := config.TokenPrefix.Get(); err == nil {
			ae.Details["scimTokenPrefix"] = tp
		}
	}
	for k, v := range details {
		ae.Details[k] = v
	}
	s.AuditLogAuthorized(ae)
}

// groupsForRecipient returns the ScimUserGroup list for a recipient by scanning
// all company groups for membership.
func (s *Scim) groupsForRecipient(
	ctx context.Context,
	companyID *uuid.UUID,
	recipientID *uuid.UUID,
	baseURL string,
) []ScimUserGroup {
	groups, err := s.RecipientGroupRepository.GetAllByCompanyID(ctx, companyID, &repository.RecipientGroupOption{
		WithRecipients: true,
	})
	if err != nil {
		s.Logger.Warnw("scim: failed to load groups for recipient membership", "error", err)
		return nil
	}
	var result []ScimUserGroup
	for _, g := range groups {
		for _, r := range g.Recipients {
			rid, ridErr := r.ID.Get()
			if ridErr != nil {
				continue
			}
			if rid == *recipientID {
				gid, gidErr := g.ID.Get()
				if gidErr != nil {
					continue
				}
				name := ""
				if n, err := g.Name.Get(); err == nil {
					name = n.String()
				}
				ref := ""
				if baseURL != "" {
					ref = baseURL + "/Groups/" + gid.String()
				}
				result = append(result, ScimUserGroup{
					Value:   gid.String(),
					Display: name,
					Ref:     ref,
				})
				break
			}
		}
	}
	return result
}

// syncUserGroupMembership adds the recipient to all groups referenced in the
// groups array. groups that do not belong to this company are silently skipped.
func (s *Scim) syncUserGroupMembership(
	ctx context.Context,
	companyID *uuid.UUID,
	recipientID *uuid.UUID,
	groups []ScimUserGroup,
) error {
	for _, g := range groups {
		if g.Value == "" {
			continue
		}
		gid, err := uuid.Parse(g.Value)
		if err != nil {
			continue
		}
		// verify the group belongs to this company before adding
		group, err := s.RecipientGroupRepository.GetByID(ctx, &gid, &repository.RecipientGroupOption{})
		if err != nil {
			s.Logger.Warnw("scim sync group membership: group not found, skipping", "groupID", gid.String())
			continue
		}
		gCompanyID, compErr := group.CompanyID.Get()
		if compErr != nil || gCompanyID != *companyID {
			s.Logger.Warnw("scim sync group membership: group does not belong to company, skipping", "groupID", gid.String())
			continue
		}
		if err := s.RecipientGroupRepository.AddRecipients(ctx, &gid, []*uuid.UUID{recipientID}); err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

// applyGroupMembers adds a list of SCIM member entries to a group.
// members referencing recipients that do not belong to companyID are skipped.
func (s *Scim) applyGroupMembers(
	ctx context.Context,
	companyID *uuid.UUID,
	groupID *uuid.UUID,
	members []ScimGroupMember,
) error {
	for _, m := range members {
		if m.Value == "" {
			continue
		}
		rid, err := uuid.Parse(m.Value)
		if err != nil {
			continue
		}
		// verify the recipient belongs to the same company as the SCIM token
		recipient, err := s.RecipientRepository.GetByID(ctx, &rid, &repository.RecipientOption{})
		if err != nil {
			s.Logger.Warnw("scim apply group members: recipient not found, skipping", "recipientID", rid.String())
			continue
		}
		rCompanyID, compErr := recipient.CompanyID.Get()
		if compErr != nil || rCompanyID != *companyID {
			s.Logger.Warnw("scim apply group members: recipient does not belong to company, skipping", "recipientID", rid.String())
			continue
		}
		if err := s.RecipientGroupRepository.AddRecipients(ctx, groupID, []*uuid.UUID{&rid}); err != nil {
			s.Logger.Errorw("scim apply group members: failed to add recipient", "error", err, "recipientID", rid.String())
			return errs.Wrap(err)
		}
	}
	return nil
}

// removeGroupMembers removes a list of SCIM member entries from a group.
// recipients that do not belong to companyID are skipped.
func (s *Scim) removeGroupMembers(
	ctx context.Context,
	companyID *uuid.UUID,
	groupID *uuid.UUID,
	members []ScimGroupMember,
) error {
	ids := make([]*uuid.UUID, 0, len(members))
	for _, m := range members {
		if m.Value == "" {
			continue
		}
		rid, err := uuid.Parse(m.Value)
		if err != nil {
			continue
		}
		// verify the recipient belongs to the same company as the SCIM token
		recipient, err := s.RecipientRepository.GetByID(ctx, &rid, &repository.RecipientOption{})
		if err != nil {
			s.Logger.Warnw("scim remove group members: recipient not found, skipping", "recipientID", rid.String())
			continue
		}
		rCompanyID, compErr := recipient.CompanyID.Get()
		if compErr != nil || rCompanyID != *companyID {
			s.Logger.Warnw("scim remove group members: recipient does not belong to company, skipping", "recipientID", rid.String())
			continue
		}
		idCopy := rid
		ids = append(ids, &idCopy)
	}
	if len(ids) == 0 {
		return nil
	}
	return s.RecipientGroupRepository.RemoveRecipients(ctx, groupID, ids)
}

// replaceGroupMembers removes all existing members and adds the new set.
func (s *Scim) replaceGroupMembers(
	ctx context.Context,
	companyID *uuid.UUID,
	groupID *uuid.UUID,
	existing []*model.Recipient,
	incoming []ScimGroupMember,
) error {
	// remove current members
	currentIDs := make([]*uuid.UUID, 0, len(existing))
	for _, r := range existing {
		rid, err := r.ID.Get()
		if err != nil {
			continue
		}
		idCopy := rid
		currentIDs = append(currentIDs, &idCopy)
	}
	if len(currentIDs) > 0 {
		// existing members were already verified when they were added; remove directly
		if err := s.RecipientGroupRepository.RemoveRecipients(ctx, groupID, currentIDs); err != nil {
			return errs.Wrap(err)
		}
	}
	return s.applyGroupMembers(ctx, companyID, groupID, incoming)
}

// applyGroupPatchReplace handles a replace operation on a group patch.
func (s *Scim) applyGroupPatchReplace(
	ctx context.Context,
	companyID *uuid.UUID,
	existing *model.RecipientGroup,
	groupID *uuid.UUID,
	op ScimGroupPatchOpItem,
) error {
	path := strings.ToLower(op.Path)
	switch path {
	case "displayname":
		name := stringFromPatchValue(op.Value)
		if name == "" {
			return nil
		}
		nameVO, err := vo.NewString127(name)
		if err != nil {
			return errs.NewValidationError(fmt.Errorf("displayName too long"))
		}
		existing.Name = nullable.NewNullableWithValue(*nameVO)
		if err := s.RecipientGroupRepository.UpdateByID(ctx, groupID, existing); err != nil {
			return errs.Wrap(err)
		}
	case "members":
		members := groupMembersFromPatchValue(op.Value)
		if err := s.replaceGroupMembers(ctx, companyID, groupID, existing.Recipients, members); err != nil {
			return err
		}
	case "":
		// no path — value is a map of attributes
		if m, ok := op.Value.(map[string]any); ok {
			if dn, ok := m["displayName"].(string); ok && dn != "" {
				nameVO, err := vo.NewString127(dn)
				if err != nil {
					return errs.NewValidationError(fmt.Errorf("displayName too long"))
				}
				existing.Name = nullable.NewNullableWithValue(*nameVO)
				if err := s.RecipientGroupRepository.UpdateByID(ctx, groupID, existing); err != nil {
					return errs.Wrap(err)
				}
			}
		}
	}
	return nil
}

// groupMembersFromPatchValue coerces a PatchOp value to []ScimGroupMember.
// the IdP may send either a slice of objects or a single object.
func groupMembersFromPatchValue(v any) []ScimGroupMember {
	if v == nil {
		return nil
	}
	// slice of maps
	if items, ok := v.([]any); ok {
		result := make([]ScimGroupMember, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				val, _ := m["value"].(string)
				display, _ := m["display"].(string)
				result = append(result, ScimGroupMember{Value: val, Display: display})
			}
		}
		return result
	}
	// single map
	if m, ok := v.(map[string]any); ok {
		val, _ := m["value"].(string)
		display, _ := m["display"].(string)
		return []ScimGroupMember{{Value: val, Display: display}}
	}
	return nil
}

// groupMembersFromPatchPath handles both the plain value array form and the
// filter path form "members[value eq \"<id>\"]" used by some IdPs for remove ops.
func groupMembersFromPatchPath(path string, v any) []ScimGroupMember {
	// filter path form: members[value eq "<uuid>"]
	lower := strings.ToLower(strings.TrimSpace(path))
	const filterPrefix = "members[value eq \""
	if strings.HasPrefix(lower, filterPrefix) {
		inner := path[len(filterPrefix):]
		inner = strings.TrimSuffix(inner, "\"]")
		inner = strings.TrimSuffix(inner, "\"]")
		if inner != "" {
			return []ScimGroupMember{{Value: inner}}
		}
	}
	// plain "members" path — fall back to parsing the value array
	return groupMembersFromPatchValue(v)
}

// buildGroupsByRecipient builds a map from recipient UUID to the list of
// ScimUserGroup entries the recipient belongs to.
func buildGroupsByRecipient(groups []*model.RecipientGroup) map[uuid.UUID][]ScimUserGroup {
	result := make(map[uuid.UUID][]ScimUserGroup)
	for _, g := range groups {
		gid, err := g.ID.Get()
		if err != nil {
			continue
		}
		name := ""
		if n, err := g.Name.Get(); err == nil {
			name = n.String()
		}
		for _, r := range g.Recipients {
			rid, err := r.ID.Get()
			if err != nil {
				continue
			}
			result[rid] = append(result[rid], ScimUserGroup{
				Value:   gid.String(),
				Display: name,
			})
		}
	}
	return result
}

// applyScimUserToRecipient writes the mutable SCIM User fields onto an existing
// recipient model and persists the changes via the repository.
func (s *Scim) applyScimUserToRecipient(
	ctx context.Context,
	existing *model.Recipient,
	scimUser *ScimUser,
) error {
	// PUT is a full replace (RFC 7644 §3.5.1): attributes absent from the
	// request are cleared. email is the one exception — it is required, so an
	// absent or invalid email leaves the existing address untouched.
	existing.ScimUserName.Set(*vo.NewOptionalString127Must(truncate(scimUserNameFrom(scimUser), 127)))
	// email — stored lowercased for case-insensitive matching
	if email, err := canonicalEmailLower(scimUser); err == nil && email != "" {
		if ev, err := vo.NewEmail(email); err == nil {
			existing.Email.Set(*ev)
		}
	}
	// first and last name
	existing.FirstName.Set(*vo.NewOptionalString127Must(truncate(firstNameFrom(scimUser), 127)))
	existing.LastName.Set(*vo.NewOptionalString127Must(truncate(lastNameFrom(scimUser), 127)))
	// phone
	existing.Phone.Set(*vo.NewOptionalString127Must(truncate(primaryPhoneFrom(scimUser), 127)))
	// department from the enterprise extension; job title from the core title
	// attribute (where Entra puts it by default) with the enterprise title as fallback
	department := ""
	if scimUser.EnterpriseUser != nil {
		department = scimUser.EnterpriseUser.Department
	}
	existing.Department.Set(*vo.NewOptionalString127Must(truncate(department, 127)))
	existing.Position.Set(*vo.NewOptionalString127Must(truncate(jobTitleFrom(scimUser), 127)))
	// addresses — city and country from primary/work address
	city, country := primaryAddressFrom(scimUser)
	existing.City.Set(*vo.NewOptionalString127Must(truncate(city, 127)))
	existing.Country.Set(*vo.NewOptionalString127Must(truncate(country, 127)))
	// externalId -> extra_identifier
	existing.ExtraIdentifier.Set(*vo.NewOptionalString127Must(truncate(scimUser.ExternalID, 127)))
	// misc from custom extension
	misc := ""
	if scimUser.CustomExtension != nil {
		misc = scimUser.CustomExtension.Misc
	}
	existing.Misc.Set(*vo.NewOptionalString127Must(truncate(misc, 127)))

	id := existing.ID.MustGet()
	if err := s.RecipientRepository.UpdateByID(ctx, &id, existing); err != nil {
		s.Logger.Errorw("scim apply user: failed to update recipient", "error", err)
		return errs.Wrap(err)
	}
	return nil
}

// primaryAddressFrom extracts city and country from the addresses array.
// prefers primary=true, then type="work", then the first entry.
func primaryAddressFrom(u *ScimUser) (city, country string) {
	var best *ScimAddress
	for i := range u.Addresses {
		a := &u.Addresses[i]
		if a.Primary {
			best = a
			break
		}
	}
	if best == nil {
		for i := range u.Addresses {
			a := &u.Addresses[i]
			if strings.EqualFold(a.Type, "work") {
				best = a
				break
			}
		}
	}
	if best == nil && len(u.Addresses) > 0 {
		best = &u.Addresses[0]
	}
	if best == nil {
		return "", ""
	}
	return best.Locality, best.Country
}

// applyPatchOperation handles a single replace/add PatchOp operation on a recipient.
// returns (deactivated bool, error) — deactivated is true when active=false triggers
// a hard-delete so the caller can short-circuit without trying to reload the recipient.
func (s *Scim) applyPatchOperation(
	ctx context.Context,
	existing *model.Recipient,
	config *model.CompanyScimConfig,
	recipientID *uuid.UUID,
	op ScimPatchOpItem,
) (bool, error) {
	path := strings.ToLower(op.Path)

	// handle active flag — false means deprovision the recipient
	if path == "active" {
		active := boolFromPatchValue(op.Value)
		if !active {
			return true, s.deprovisionRecipient(ctx, recipientID)
		}
		// active=true revives a soft-deleted recipient; otherwise a no-op
		return false, s.reviveIfSoftDeleted(ctx, existing, recipientID)
	}

	// for no path, value is expected to be a map of attribute → value
	if op.Path == "" {
		if m, ok := op.Value.(map[string]any); ok {
			// check for active=false inside the map before applying other fields
			if rawActive, ok := m["active"]; ok && !boolFromPatchValue(rawActive) {
				return true, s.deprovisionRecipient(ctx, recipientID)
			}
			// active=true in the map revives a soft-deleted recipient
			if rawActive, ok := m["active"]; ok && boolFromPatchValue(rawActive) {
				if err := s.reviveIfSoftDeleted(ctx, existing, recipientID); err != nil {
					return false, err
				}
			}
			if err := s.applyAttributeMap(ctx, existing, config, recipientID, m); err != nil {
				return false, err
			}
		}
		id := existing.ID.MustGet()
		if err := s.RecipientRepository.UpdateByID(ctx, &id, existing); err != nil {
			return false, errs.Wrap(err)
		}
		return false, nil
	}

	// single attribute path — only apply values that map to our data model
	strVal := stringFromPatchValue(op.Value)
	switch path {
	case "username":
		existing.ScimUserName.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
	case "emails[type eq \"work\"].value", "emails":
		if ev, err := vo.NewEmail(strings.ToLower(strings.TrimSpace(strVal))); err == nil {
			existing.Email.Set(*ev)
		}
	case "name.givenname":
		existing.FirstName.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
	case "name.familyname":
		existing.LastName.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
	case "name.formatted":
		// split formatted into first/last only when individual names are not already set
		parts := strings.SplitN(strVal, " ", 2)
		existingFirst := ""
		if v, err := existing.FirstName.Get(); err == nil {
			existingFirst = v.String()
		}
		existingLast := ""
		if v, err := existing.LastName.Get(); err == nil {
			existingLast = v.String()
		}
		if existingFirst == "" && len(parts) >= 1 && parts[0] != "" {
			existing.FirstName.Set(*vo.NewOptionalString127Must(truncate(parts[0], 127)))
		}
		if existingLast == "" && len(parts) == 2 && parts[1] != "" {
			existing.LastName.Set(*vo.NewOptionalString127Must(truncate(parts[1], 127)))
		}
	// home/other typed emails and phones are not stored — silently ignore
	case "phonenumbers[type eq \"work\"].value", "phonenumbers":
		existing.Phone.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
	case "urn:ietf:params:scim:schemas:extension:enterprise:2.0:user:department":
		existing.Department.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
	case "title", "urn:ietf:params:scim:schemas:extension:enterprise:2.0:user:title":
		existing.Position.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
	case "addresses[type eq \"work\"].locality", "addresses.locality":
		existing.City.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
	case "addresses[type eq \"work\"].country", "addresses.country":
		existing.Country.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
	// home/other typed addresses are not stored — silently ignore
	case "externalid":
		existing.ExtraIdentifier.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
	case "urn:ietf:params:scim:schemas:extension:phishingclub:2.0:user:misc":
		existing.Misc.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
	}

	id := existing.ID.MustGet()
	if err := s.RecipientRepository.UpdateByID(ctx, &id, existing); err != nil {
		return false, errs.Wrap(err)
	}
	return false, nil
}

// applyAttributeMap applies a flat attribute map from a no-path PatchOp.
// only attributes that map to our data model are persisted; others are silently ignored.
func (s *Scim) applyAttributeMap(
	_ context.Context,
	existing *model.Recipient,
	config *model.CompanyScimConfig,
	recipientID *uuid.UUID,
	m map[string]any,
) error {
	// collect name sub-attributes first so we can merge them correctly
	givenName := ""
	familyName := ""
	formattedName := ""

	for k, v := range m {
		strVal := fmt.Sprintf("%v", v)
		switch strings.ToLower(k) {
		case "username":
			// update the stored scim userName so it round-trips exactly
			existing.ScimUserName.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
		case "active":
			// active flag handled at the PatchUser call site after this map is applied
		case "externalid":
			existing.ExtraIdentifier.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
		case "displayname":
			// displayname is not a stored field; use it as a fallback for name only
			// if the IdP also sends name.givenName / name.familyName those take priority
			_ = strVal
		case "name.givenname":
			givenName = strVal
		case "name.familyname":
			familyName = strVal
		case "name.formatted":
			formattedName = strVal
		case "urn:ietf:params:scim:schemas:extension:enterprise:2.0:user:department":
			existing.Department.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
		case "title", "urn:ietf:params:scim:schemas:extension:enterprise:2.0:user:title":
			existing.Position.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
		case "urn:ietf:params:scim:schemas:extension:phishingclub:2.0:user:misc":
			existing.Misc.Set(*vo.NewOptionalString127Must(truncate(strVal, 127)))
		}
	}

	// apply name fields — explicit sub-attributes take priority over formatted
	if givenName != "" {
		existing.FirstName.Set(*vo.NewOptionalString127Must(truncate(givenName, 127)))
	}
	if familyName != "" {
		existing.LastName.Set(*vo.NewOptionalString127Must(truncate(familyName, 127)))
	}
	// use formatted only as a fallback when explicit names were not provided
	if givenName == "" && familyName == "" && formattedName != "" {
		parts := strings.SplitN(formattedName, " ", 2)
		if len(parts) >= 1 && parts[0] != "" {
			existing.FirstName.Set(*vo.NewOptionalString127Must(truncate(parts[0], 127)))
		}
		if len(parts) == 2 && parts[1] != "" {
			existing.LastName.Set(*vo.NewOptionalString127Must(truncate(parts[1], 127)))
		}
	}

	return nil
}

// ── SCIM <-> model conversion helpers ─────────────────────────────────────────

// recipientToScimUser maps a model.Recipient to a ScimUser
func recipientToScimUser(r *model.Recipient, baseURL string) ScimUser {
	id := ""
	if rid, err := r.ID.Get(); err == nil {
		id = rid.String()
	}

	emailStr := ""
	if e, err := r.Email.Get(); err == nil {
		emailStr = e.String()
	}

	// prefer the stored scim userName; fall back to the email address
	userNameStr := emailStr
	if v, err := r.ScimUserName.Get(); err == nil && v.String() != "" {
		userNameStr = v.String()
	}

	firstName := ""
	if v, err := r.FirstName.Get(); err == nil {
		firstName = v.String()
	}
	lastName := ""
	if v, err := r.LastName.Get(); err == nil {
		lastName = v.String()
	}

	var name *ScimName
	if firstName != "" || lastName != "" {
		name = &ScimName{
			GivenName:  firstName,
			FamilyName: lastName,
			Formatted:  strings.TrimSpace(firstName + " " + lastName),
		}
	}

	var emails []ScimEmail
	if emailStr != "" {
		emails = []ScimEmail{{Value: emailStr, Type: "work", Primary: true}}
	}

	var phones []ScimPhoneNumber
	if v, err := r.Phone.Get(); err == nil && v.String() != "" {
		phones = []ScimPhoneNumber{{Value: v.String(), Type: "work", Primary: true}}
	}

	var enterprise *ScimEnterpriseUser
	dept := ""
	if v, err := r.Department.Get(); err == nil {
		dept = v.String()
	}
	pos := ""
	if v, err := r.Position.Get(); err == nil {
		pos = v.String()
	}
	if dept != "" || pos != "" {
		enterprise = &ScimEnterpriseUser{
			Department: dept,
			Title:      pos,
		}
	}

	// addresses — map city + country to a single work address entry
	var addresses []ScimAddress
	city := ""
	if v, err := r.City.Get(); err == nil {
		city = v.String()
	}
	country := ""
	if v, err := r.Country.Get(); err == nil {
		country = v.String()
	}
	if city != "" || country != "" {
		addresses = []ScimAddress{{
			Type:     "work",
			Locality: city,
			Country:  country,
			Primary:  true,
		}}
	}

	externalID := ""
	if v, err := r.ExtraIdentifier.Get(); err == nil {
		externalID = v.String()
	}

	// custom extension — misc
	var custom *ScimCustomExtension
	if v, err := r.Misc.Get(); err == nil && v.String() != "" {
		custom = &ScimCustomExtension{Misc: v.String()}
	}

	schemas := []string{scimSchemaUser}
	if enterprise != nil {
		schemas = append(schemas, scimSchemaEnterpriseUser)
	}
	if custom != nil {
		schemas = append(schemas, scimSchemaCustomExtension)
	}

	u := ScimUser{
		Schemas:         schemas,
		ID:              id,
		UserName:        userNameStr,
		Name:            name,
		Title:           pos,
		Emails:          emails,
		PhoneNumbers:    phones,
		EnterpriseUser:  enterprise,
		Addresses:       addresses,
		Active:          r.ScimSoftDeletedAt == nil,
		ExternalID:      externalID,
		CustomExtension: custom,
	}
	if baseURL != "" && id != "" {
		u.Meta = &ScimMeta{
			ResourceType: scimResourceTypeUser,
			Location:     baseURL + "/Users/" + id,
		}
	}
	return u
}

// jobTitleFrom returns the user's job title. Microsoft Entra maps the directory
// jobTitle to the core SCIM "title" attribute by default, so that is preferred;
// the enterprise extension title is used as a fallback for IdPs that send it there.
func jobTitleFrom(u *ScimUser) string {
	if u.Title != "" {
		return u.Title
	}
	if u.EnterpriseUser != nil {
		return u.EnterpriseUser.Title
	}
	return ""
}

// scimUserToRecipient creates a new model.Recipient from a ScimUser
func scimUserToRecipient(scimUser *ScimUser, companyID *uuid.UUID) *model.Recipient {
	r := &model.Recipient{}

	// store the original userName so it round-trips exactly
	r.ScimUserName = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(truncate(scimUserNameFrom(scimUser), 127)))

	// email is stored lowercased so dedup and the unique index are case
	// insensitive; the original userName case is preserved in scim_user_name
	emailStr, _ := canonicalEmailLower(scimUser)
	if emailStr != "" {
		if ev, err := vo.NewEmail(emailStr); err == nil {
			r.Email = nullable.NewNullableWithValue(*ev)
		}
	}
	r.FirstName = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(truncate(firstNameFrom(scimUser), 127)))
	r.LastName = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(truncate(lastNameFrom(scimUser), 127)))

	if phone := primaryPhoneFrom(scimUser); phone != "" {
		r.Phone = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(truncate(phone, 127)))
	} else {
		r.Phone = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(""))
	}

	department := ""
	if scimUser.EnterpriseUser != nil {
		department = scimUser.EnterpriseUser.Department
	}
	r.Department = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(truncate(department, 127)))
	r.Position = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(truncate(jobTitleFrom(scimUser), 127)))

	// addresses — prefer work, fall back to first entry
	city, country := primaryAddressFrom(scimUser)
	r.City = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(truncate(city, 127)))
	r.Country = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(truncate(country, 127)))

	if scimUser.ExternalID != "" {
		r.ExtraIdentifier = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(truncate(scimUser.ExternalID, 127)))
	} else {
		r.ExtraIdentifier = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(""))
	}

	// custom extension — misc
	if scimUser.CustomExtension != nil && scimUser.CustomExtension.Misc != "" {
		r.Misc = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(truncate(scimUser.CustomExtension.Misc, 127)))
	} else {
		r.Misc = nullable.NewNullableWithValue(*vo.NewOptionalString127Must(""))
	}

	if companyID != nil {
		r.CompanyID = nullable.NewNullableWithValue(*companyID)
	}
	return r
}

// scimUserNameFrom returns the raw userName value to persist as-is.
func scimUserNameFrom(u *ScimUser) string {
	return strings.TrimSpace(u.UserName)
}

// isScimUniqueConflict returns true when the error indicates a unique constraint
// violation, which means a resource with that identifier already exists.
func isScimUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}

// canonicalEmail extracts the canonical email address from a ScimUser,
// preserving the original case sent by the IdP so userName round-trips exactly.
// preference: first primary email, then first email, then userName.
func canonicalEmail(u *ScimUser) (string, error) {
	for _, e := range u.Emails {
		if e.Primary && e.Value != "" {
			return strings.TrimSpace(e.Value), nil
		}
	}
	for _, e := range u.Emails {
		if e.Value != "" {
			return strings.TrimSpace(e.Value), nil
		}
	}
	if u.UserName != "" {
		return strings.TrimSpace(u.UserName), nil
	}
	return "", fmt.Errorf("scim user has no email or userName")
}

// canonicalEmailLower returns the lowercased canonical email, used only for
// case-insensitive dedup lookups against existing recipients.
func canonicalEmailLower(u *ScimUser) (string, error) {
	v, err := canonicalEmail(u)
	if err != nil {
		return "", err
	}
	return strings.ToLower(v), nil
}

// firstNameFrom extracts the given name from a ScimUser
func firstNameFrom(u *ScimUser) string {
	if u.Name != nil && u.Name.GivenName != "" {
		return u.Name.GivenName
	}
	if u.DisplayName != "" {
		parts := strings.SplitN(u.DisplayName, " ", 2)
		if len(parts) >= 1 {
			return parts[0]
		}
	}
	return ""
}

// lastNameFrom extracts the family name from a ScimUser
func lastNameFrom(u *ScimUser) string {
	if u.Name != nil && u.Name.FamilyName != "" {
		return u.Name.FamilyName
	}
	if u.DisplayName != "" {
		parts := strings.SplitN(u.DisplayName, " ", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	return ""
}

// primaryPhoneFrom extracts the primary (or first) phone number from a ScimUser
func primaryPhoneFrom(u *ScimUser) string {
	for _, p := range u.PhoneNumbers {
		if p.Primary && p.Value != "" {
			return p.Value
		}
	}
	for _, p := range u.PhoneNumbers {
		if p.Value != "" {
			return p.Value
		}
	}
	return ""
}

// truncate truncates a string to max bytes without splitting a UTF-8 rune
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// walk back to a valid rune boundary
	for i := max; i > 0; i-- {
		if s[i]&0xC0 != 0x80 {
			return s[:i]
		}
	}
	return ""
}

// scimFilterMatchesUser performs a very simple filter evaluation.
// only "userName eq <value>" and "externalId eq <value>" are handled.
// unrecognised filters always pass (return true) to be permissive.
func scimFilterMatchesUser(filter string, u ScimUser) bool {
	lower := strings.ToLower(strings.TrimSpace(filter))
	// e.g. userName eq "user@example.com"
	for _, attr := range []string{"username", "externalid"} {
		prefix := attr + " eq "
		if strings.HasPrefix(lower, prefix) {
			want := strings.Trim(lower[len(prefix):], `"' `)
			switch attr {
			case "username":
				return strings.EqualFold(u.UserName, want)
			case "externalid":
				return strings.EqualFold(u.ExternalID, want)
			}
		}
	}
	return true
}

// scimGroupFilterMatches evaluates a SCIM filter expression against a group.
// only "displayName eq <value>" is supported; unrecognised filters pass.
func scimGroupFilterMatches(filter string, g *model.RecipientGroup) bool {
	lower := strings.ToLower(strings.TrimSpace(filter))
	const prefix = "displayname eq "
	if strings.HasPrefix(lower, prefix) {
		want := strings.Trim(lower[len(prefix):], `"' `)
		name := ""
		if n, err := g.Name.Get(); err == nil {
			name = strings.ToLower(n.String())
		}
		return name == want
	}
	return true
}

// scimExcludesAttribute returns true when the excludedAttributes query param
// contains the named attribute (case-insensitive, comma-separated list).
func scimExcludesAttribute(excludedAttributes, attr string) bool {
	if excludedAttributes == "" {
		return false
	}
	attrLower := strings.ToLower(attr)
	for _, part := range strings.Split(excludedAttributes, ",") {
		if strings.ToLower(strings.TrimSpace(part)) == attrLower {
			return true
		}
	}
	return false
}

// boolFromPatchValue coerces a PatchOp value to bool
func boolFromPatchValue(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(val, "true")
	case float64:
		return val != 0
	}
	return false
}

// stringFromPatchValue coerces a PatchOp value to string
func stringFromPatchValue(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// scimSortUsers sorts a slice of ScimUser in-place by the given attribute.
// only "username", "id", "name.familyname", and "name.givenname" are handled;
// unrecognised attributes are ignored. sortOrder defaults to ascending.
func scimSortUsers(users []ScimUser, sortBy string, sortOrder string) {
	descending := strings.EqualFold(sortOrder, "descending")
	key := strings.ToLower(sortBy)

	// insertion sort — swap when the left element is out of order relative to right
	for i := 1; i < len(users); i++ {
		for j := i; j > 0; j-- {
			a := strings.ToLower(scimUserSortKey(users[j-1], key))
			b := strings.ToLower(scimUserSortKey(users[j], key))
			// for ascending: swap when a > b (left is larger than right)
			// for descending: swap when a < b (left is smaller than right)
			outOfOrder := a > b
			if descending {
				outOfOrder = a < b
			}
			if outOfOrder {
				users[j-1], users[j] = users[j], users[j-1]
			} else {
				break
			}
		}
	}
}

// scimUserSortKey returns the string value of the requested sort attribute.
func scimUserSortKey(u ScimUser, key string) string {
	switch key {
	case "username":
		return u.UserName
	case "id":
		return u.ID
	case "name.familyname":
		if u.Name != nil {
			return u.Name.FamilyName
		}
	case "name.givenname":
		if u.Name != nil {
			return u.Name.GivenName
		}
	case "displayname":
		return u.DisplayName
	}
	return ""
}
