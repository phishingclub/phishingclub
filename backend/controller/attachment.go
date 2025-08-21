package controller

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-errors/errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
)

// AttachmentColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var AttachmentColumnsMap = map[string]string{
	"created_at":       repository.TableColumn(database.ATTACHMENT_TABLE, "created_at"),
	"updated_at":       repository.TableColumn(database.ATTACHMENT_TABLE, "updated_at"),
	"name":             repository.TableColumn(database.ATTACHMENT_TABLE, "name"),
	"description":      repository.TableColumn(database.ATTACHMENT_TABLE, "description"),
	"embedded content": repository.TableColumn(database.ATTACHMENT_TABLE, "embeddedContent"),
	"filename":         repository.TableColumn(database.ATTACHMENT_TABLE, "filename"),
}

// Attachment is an static Attachment controller
type Attachment struct {
	Common
	StaticAttachmentPath string
	TemplateService      *service.Template
	AttachmentService    *service.Attachment
	OptionService        *service.Option
	CompanyService       *service.Company
}

// GetContentByID returns the content and mime type of an attachment
func (a *Attachment) GetContentByID(g *gin.Context) {
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	// get the attachment
	ctx := g.Request.Context()
	attachment, err := a.AttachmentService.GetByID(
		ctx,
		session,
		id,
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	p := attachment.Path.MustGet().String()
	// serve the file
	// #nosec
	content, err := os.ReadFile(p)
	if err != nil {
		a.Logger.Errorw("failed to read file",
			"path", p,
			"error", err,
		)
		a.Response.ServerError(g)
		return
	}

	fileExt := filepath.Ext(p)
	mimeType := ""
	switch fileExt {
	case ".html":
		mimeType = "text/html"
	case ".htm":
		mimeType = "text/html"
	case ".xhtml":
		mimeType = "application/xhtml+xml"
	default:
		mimeType = http.DetectContentType(content)
	}
	// get by id is only used for admin viewing of an attachemnt, so all
	// embedded content must contain example data
	if attachment.EmbeddedContent.MustGet() {
		// build email
		domain := &model.Domain{
			Name: nullable.NewNullableWithValue(
				*vo.NewString255Must("example.test"),
			),
		}
		recipient := model.NewRecipientExample()
		campaignRecipient := model.CampaignRecipient{
			ID: nullable.NewNullableWithValue(
				uuid.New(),
			),
			Recipient: recipient,
		}
		email := model.NewEmailExample()
		// hacky
		email.Content = nullable.NewNullableWithValue(
			*vo.NewUnsafeOptionalString1MB(string(content)),
		)
		apiSender := model.NewAPISenderExample()
		b, err := a.TemplateService.CreateMailBody(
			"id",
			"/foo",
			domain,
			&campaignRecipient,
			email,
			apiSender,
		)
		if err != nil {
			a.Logger.Errorw("failed to appy template to attachment",
				"error", err,
			)
			a.Response.ServerError(g)
			return
		}
		content = []byte(b)
	}
	a.Response.OK(g, gin.H{
		"mimeType": mimeType,
		"file":     base64.StdEncoding.EncodeToString(content),
	})
}

// GetAllForContext gets all attachments for a domain
// and has a special case 'shared' to get all global attachments
func (a *Attachment) GetAllForContext(g *gin.Context) {
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// check permissions
	isAuthorized, err := service.IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.Logger.Errorw("failed to check permissions",
			"error", err,
		)
		a.Response.ServerError(g)
		return
	}
	if !isAuthorized {
		// TODO audit log
		_ = handleAuthorizationError(g, a.Response, errs.ErrAuthorizationFailed)
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	// if there is no companyID then it is a global attachment request
	// else the company context name is the attachment scope
	if companyID != nil {
		// get the company id and to check if the user has permission to retrieve it
		_, err := a.CompanyService.GetByID(
			g.Request.Context(),
			session,
			companyID,
		)
		if ok := a.handleErrors(g, err); !ok {
			return
		}
	}
	queryArgs, ok := a.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(AttachmentColumnsMap)
	// get attachments
	a.Logger.Debugw("getting attachments for company ID",
		"companyID", companyID,
	)
	attachments, err := a.AttachmentService.GetAll(
		g,
		session,
		companyID,
		queryArgs,
	)
	// handle responses
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, attachments)
}

// Create uploads an attachment
func (a *Attachment) Create(g *gin.Context) {
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	multipartData, err := g.MultipartForm()
	if err != nil {
		a.Logger.Errorw("failed to get multipart form",
			"error", err,
		)
		a.Response.BadRequest(g)
		return
	}
	if len(multipartData.File["files"]) == 0 {
		a.Logger.Debug("no files to upload")
		a.Response.BadRequestMessage(g, "No files selected")
		return
	}
	companyID := nullable.NewNullNullable[uuid.UUID]()
	companyIDParam := g.PostForm("companyID")
	if len(companyIDParam) > 0 {
		cid, err := uuid.Parse(companyIDParam)
		if err != nil {
			a.Logger.Debugw("failed to parse company id",
				"error", err,
			)
			a.Response.ValidationFailed(g, "companyID", err)
			return
		}
		companyID.Set(cid)
	}
	nameParam, err := vo.NewOptionalString127(g.PostForm("name"))
	if err != nil {
		a.Logger.Debugw("failed to parse name",
			"name", g.PostForm("name"),
			"error", err,
		)
		a.Response.ValidationFailed(g, "name", err)
		return
	}
	name := nullable.NewNullableWithValue(*nameParam)
	descriptionParam, err := vo.NewOptionalString255(g.PostForm("description"))
	if err != nil {
		a.Logger.Debugw("failed to parse description",
			"error", err,
		)
		a.Response.ValidationFailed(g, "description", err)
		return
	}
	description := nullable.NewNullableWithValue(*descriptionParam)
	embeddedContent := nullable.NewNullableWithValue(false)
	embeddedContentString := g.PostForm("embeddedContent")
	if strings.ToLower(embeddedContentString) == "true" {
		embeddedContent.Set(true)
	}
	attachments := []*model.Attachment{}
	for _, file := range multipartData.File["files"] {
		// TODO multi user validate that the company id is the same as the session company id or that the session is a super admin
		// check max file size
		maxFile, err := a.OptionService.GetOption(g, session, data.OptionKeyMaxFileUploadSizeMB)
		if ok := a.handleErrors(g, err); !ok {
			return
		}
		ok, err := utils.CompareFileSizeFromString(file.Size, maxFile.Value.String())
		if err != nil {
			a.Logger.Errorw("failed to compare file size",
				"error", err,
			)
		}
		if !ok {
			a.Logger.Debugw("file too large",
				"filename", file.Filename,
				"size", file.Size,
				"maxSize", maxFile.Value.String(),
			)
			a.Response.ValidationFailed(
				g,
				"File",
				fmt.Errorf("'%s' is too large", utils.ReadableFileName(file.Filename)),
			)
			return
		}
		fileNameParam, err := vo.NewFileName(file.Filename)
		if err != nil {
			a.Logger.Debugw("failed to parse filename",
				"error", err,
			)
			a.Response.ValidationFailed(g, "filename", err)
			return
		}
		fileName := nullable.NewNullableWithValue(*fileNameParam)

		attachment := model.Attachment{
			CompanyID:       companyID,
			Name:            name,
			Description:     description,
			EmbeddedContent: embeddedContent,
			File:            file,
			FileName:        fileName,
		}
		if err := attachment.Validate(); err != nil {
			a.Logger.Debugw("failed to validate attachment",
				"attachmentName", name,
				"error", err,
			)
			a.Response.ValidationFailed(g, "attachment", err)
			return
		}
		attachments = append(attachments, &attachment)
	}
	//  store the files on disk and in database
	createdIDs, err := a.AttachmentService.Create(
		g,
		session,
		attachments,
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, gin.H{
		"ids":            createdIDs,
		"files_uploaded": len(attachments),
	})
}

// GetByID gets an static attachment by id
func (a *Attachment) GetByID(g *gin.Context) {
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	// get the attachment
	ctx := g.Request.Context()
	attachment, err := a.AttachmentService.GetByID(
		ctx,
		session,
		id,
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, attachment)
}

// UpdateByID updates an static attachment by id
func (a *Attachment) UpdateByID(g *gin.Context) {
	// handle session
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	// parse request
	var req model.Attachment
	if ok := a.handleParseRequest(g, &req); !ok {
		return
	}
	// update the attachment
	ctx := g.Request.Context()
	err := a.AttachmentService.UpdateByID(
		ctx,
		session,
		id,
		&req,
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, gin.H{})
}

// RemoveByID removes an static attachment
// if the attachment is a directory, it will be removed recursively
func (a *Attachment) RemoveByID(g *gin.Context) {
	// handle session
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	// remove the attachment
	ctx := g.Request.Context()
	err := a.AttachmentService.DeleteByID(
		ctx,
		session,
		id,
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, gin.H{})
}
