package controller

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// RecipientGroupColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var RecipientGroupColumnsMap = map[string]string{
	"created_at": repository.TableColumn(database.RECIPIENT_GROUP_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.RECIPIENT_GROUP_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.RECIPIENT_GROUP_TABLE, "name"),
}

// AddRecipientRequest is a request to add recipients to a recipient group
type AddRecipientRequest struct {
	RecipientIDs []string `json:"recipientIDs"`
}

// RemoveRecipientRequest is a request to remove recipients from a recipient group
type RemoveRecipientRequest struct {
	RecipientIDs []string `json:"recipientIDs"`
}

// RecipientGroup is a recipient group controller
type RecipientGroup struct {
	Common
	RecipientGroupService *service.RecipientGroup
}

// Create creates a new recipient group
func (r *RecipientGroup) Create(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.RecipientGroup
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	// save recipient group
	recipientGroupID, err := r.RecipientGroupService.Create(
		g.Request.Context(),
		session,
		&req,
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(
		g,
		&gin.H{
			"id": recipientGroupID.String(),
		},
	)
}

// GetAll returns all recipient groups using pagination
func (r *RecipientGroup) GetAll(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := r.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByName()
	queryArgs.RemapOrderBy(RecipientGroupColumnsMap)
	companyContextID := companyIDFromRequestQuery(g)

	// get recipient groups
	recipientGroups, err := r.RecipientGroupService.GetAll(
		g,
		session,
		companyContextID,
		&repository.RecipientGroupOption{
			QueryArgs:          queryArgs,
			WithCompany:        true,
			WithRecipientCount: true,
		},
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, recipientGroups)
}

// GetByID gets a recipient group by id
func (r *RecipientGroup) GetByID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	recipientGroup, err := r.RecipientGroupService.GetByID(
		g.Request.Context(),
		session,
		id,
		&repository.RecipientGroupOption{
			WithCompany: true,
		},
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, recipientGroup)
}

// GetRecipientsByGroupID gets recipients by recipient group id
func (r *RecipientGroup) GetRecipientsByGroupID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	queryArgs, ok := r.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortBy("email")
	// remap query args
	queryArgs.RemapOrderBy(recipientColumnByMap)
	if !ok {
		return
	}
	// get recipients
	ctx := g.Request.Context()
	recipients, err := r.RecipientGroupService.GetRecipientsByGroupID(
		ctx,
		session,
		id,
		&repository.RecipientOption{
			QueryArgs:   queryArgs,
			WithCompany: true,
		},
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}

	r.Response.OK(g, recipients)
}

// UpdateByID updates a recipient group by id
// updates only the name and company relations
func (r *RecipientGroup) UpdateByID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	// parse request
	var req model.RecipientGroup
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	// check if recipient group exists already exists
	err := r.RecipientGroupService.UpdateByID(
		g.Request.Context(),
		session,
		id,
		&req,
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, &gin.H{})
}

// Import imports recipients to a recipient group
func (r *RecipientGroup) Import(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	groupID, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	var req struct {
		Recipients                 []*model.Recipient      `json:"recipients"`
		CompanyID                  *uuid.UUID              `json:"companyID"`
		IgnoreOverwriteEmptyFields nullable.Nullable[bool] `json:"ignoreOverwriteEmptyFields"`
	}
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	// IgnoreOverwriteEmptyFields default value is true
	if !req.IgnoreOverwriteEmptyFields.IsSpecified() || req.IgnoreOverwriteEmptyFields.IsNull() {
		req.IgnoreOverwriteEmptyFields = nullable.NewNullableWithValue(true)
	}

	err := r.RecipientGroupService.Import(
		g,
		session,
		req.Recipients,
		req.IgnoreOverwriteEmptyFields.MustGet(),
		groupID,
		req.CompanyID,
	)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, &gin.H{})
}

// AddRecipients adds recipients to a recipient group
func (r *RecipientGroup) AddRecipients(g *gin.Context) {
	// handle session
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse group ID
	groupID, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	// parse request
	var req AddRecipientRequest
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	// parse recipient ids
	recipientIDs := []*uuid.UUID{}
	for _, id := range req.RecipientIDs {
		rid, err := uuid.Parse(id)
		if err != nil {
			r.Logger.Debugw("failed to add recipients to recipient group",
				"error", fmt.Errorf("failed to parse recipient id: %w", err),
			)
			r.Response.BadRequestMessage(g, "invalid recipient id")
			return
		}
		recipientIDs = append(recipientIDs, &rid)
	}
	// add recipients
	err := r.RecipientGroupService.AddRecipients(
		g.Request.Context(),
		session,
		groupID,
		recipientIDs,
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, &gin.H{})
}

// RemoveRecipients removes a recipient from a recipient group
func (r *RecipientGroup) RemoveRecipients(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	// parse request
	var req RemoveRecipientRequest
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	// parse recipient ids
	recipientIDs := []*uuid.UUID{}
	for _, id := range req.RecipientIDs {
		rid, err := uuid.Parse(id)
		if err != nil {
			r.Logger.Debugw("failed to remove recipients from recipient group",
				"error", fmt.Errorf("failed to parse recipient id: %w", err),
			)
			r.Response.BadRequestMessage(g, "invalid recipient id")
			return
		}
		recipientIDs = append(recipientIDs, &rid)
	}
	// remove recipients
	err := r.RecipientGroupService.RemoveRecipients(
		g.Request.Context(),
		session,
		id,
		recipientIDs,
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, &gin.H{})
}

// DeleteByID deletes a recipient group by id
// deleting a group also deletes all recipients in that group
func (r *RecipientGroup) DeleteByID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete recipient group
	err := r.RecipientGroupService.DeleteByID(
		g.Request.Context(),
		session,
		id,
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(
		g,
		&gin.H{},
	)
}
