package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-errors/errors"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// Attachment is a Attachment service
type Attachment struct {
	Common
	RootFolder           string
	FileService          *File
	AttachmentRepository *repository.Attachment
	EmailRepository      *repository.Email
}

// Create creates and stores a new attachments
func (a *Attachment) Create(
	g *gin.Context,
	session *model.Session,
	attachments []*model.Attachment,
) ([]*uuid.UUID, error) {
	ae := NewAuditEvent("Attachment.Create", session)
	createdIDs := []*uuid.UUID{}
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return createdIDs, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return createdIDs, errs.ErrAuthorizationFailed
	}
	// @TODO for now we allow dublicate names - should we?
	// without no dubs it is easier to reason between attachments
	// with dubs it is easier to import a collection of files and etc

	// upload the files
	contextFolder := ""
	// ensure that all attachments have the same context
	// and map attachments to files
	// @TODO move out of here
	differentContextError := fmt.Errorf(
		"all attachments must have the same context '%s'",
		contextFolder,
	)
	files := []*FileUpload{}
	filePaths := []string{}
	for _, attachment := range attachments {
		// ensure context is the same across all files
		if attachment.CompanyID.IsSpecified() && attachment.CompanyID.IsNull() {
			if contextFolder == "" {
				contextFolder = data.ASSET_GLOBAL_FOLDER
			} else if contextFolder != data.ASSET_GLOBAL_FOLDER {
				a.Logger.Error(differentContextError)
				return createdIDs, differentContextError
			}
		} else {
			companyID, err := attachment.CompanyID.Get()
			if err != nil {
				a.Logger.Debugw("failed to get company id", "error", err)
				return createdIDs, errs.Wrap(err)
			}
			if contextFolder == "" {
				contextFolder = companyID.String()
			} else if contextFolder != companyID.String() {
				a.Logger.Error(differentContextError)
				return createdIDs, differentContextError
			}
		}
		// map attachments to files
		attachmentFilename := filepath.Clean(attachment.File.Filename)
		// relative path is used in the DB
		relativePath, err := vo.NewRelativeFilePath(attachmentFilename)
		if err != nil {
			a.Logger.Debugw("failed to make file path",
				"path", attachmentFilename,
				err,
			)
			return createdIDs, errs.Wrap(err)
		}
		// full path is used in the file system
		pathWithRootAndDomainContext, err := securejoin.SecureJoin(
			a.RootFolder,
			contextFolder,
		)
		if err != nil {
			a.Logger.Infow("insecure path", "error", err)
			return createdIDs, errs.Wrap(err)
		}
		pathWithRootAndDomainContext, err = securejoin.SecureJoin(
			pathWithRootAndDomainContext,
			attachmentFilename,
		)
		if err != nil {
			a.Logger.Infow("insecure path", "error", err)
			return createdIDs, errs.Wrap(err)
		}
		a.Logger.Debugw("file path: %s",
			"relative", relativePath.String(),
			"relativeWithRootFilePath", pathWithRootAndDomainContext,
		)
		filePaths = append(filePaths, pathWithRootAndDomainContext)
		files = append(files, NewFileUpload(pathWithRootAndDomainContext, attachment.File))
	}
	// upload files to the file system
	_, err = a.FileService.Upload(
		g,
		files,
	)
	if err != nil {
		a.Logger.Debugw("failed to upload files", "error", err)
		return createdIDs, errs.Wrap(err)
	}
	// save uploaded files to the database
	for _, attachment := range attachments {
		_, err := a.AttachmentRepository.Insert(
			g,
			attachment,
		)
		if err != nil {
			a.Logger.Debugw("failed to save attachment", "error", err)
			// TODO remove all previously uploaded files
			// buut maybe not, it would be annoying if there is a multi user system
			// and a user uploads a huge amount of files and one fails and does this
			// repeatedly to burn the server
			return createdIDs, errs.Wrap(err)
		}
	}
	strIds := []string{}
	for _, id := range createdIDs {
		strIds = append(strIds, id.String())

	}
	ae.Details["paths"] = filePaths
	ae.Details["ids"] = strIds
	a.AuditLogAuthorized(ae)

	return createdIDs, nil
}

// GetAll gets all attachments
func (a *Attachment) GetAll(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	query *vo.QueryArgs,
) (*model.Result[model.Attachment], error) {
	ae := NewAuditEvent("Attachment.GetAll", session)
	result := model.NewEmptyResult[model.Attachment]()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return result, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return result, errs.ErrAuthorizationFailed
	}
	// if there is no companyID then the scope is 'shared'
	if companyID == nil {
		result, err = a.AttachmentRepository.GetAllByGlobalContext(
			ctx,
			query,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			a.Logger.Errorw("failed to get global attachments", "error", err)
			return nil, errs.Wrap(err)
		}
	} else {
		result, err = a.AttachmentRepository.GetAllByContext(
			ctx,
			companyID,
			query,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			a.Logger.Errorw("failed to get company attachments", "error", err)
			return nil, errs.Wrap(err)
		}
	}
	for _, attachment := range result.Rows {
		path, err := a.GetPath(attachment)
		if err != nil {
			a.Logger.Debugw("failed to get path", "error", err)
			return nil, errs.Wrap(err)
		}
		attachment.Path = nullable.NewNullableWithValue(*path)
	}
	// no audit on read
	return result, nil
}

// GetByID gets an attachment by id
func (a *Attachment) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*model.Attachment, error) {
	ae := NewAuditEvent("Attachment.GetById", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	// get the attachment
	attachment, err := a.AttachmentRepository.GetByID(
		ctx,
		id,
	)
	if err != nil {
		a.Logger.Debugw("attachment not found", "error", err)
		return nil, errs.Wrap(err)
	}
	// path
	path, err := a.GetPath(attachment)
	if err != nil {
		a.Logger.Debugw("failed to get path", "error", err)
		return nil, errs.Wrap(err)
	}
	attachment.Path = nullable.NewNullableWithValue(*path)
	// no audit log on read
	return attachment, nil
}

// UpdateByID updates an attachment by id
func (a *Attachment) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	attachment *model.Attachment,
) error {
	ae := NewAuditEvent("Attachment.UpdateById", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get the attachment
	current, err := a.AttachmentRepository.GetByID(
		ctx,
		id,
	)
	if err != nil {
		a.Logger.Debugw("attachment not found", "error", err)
		return err
	}
	// update the attachment
	if attachment.Name.IsSpecified() {
		current.Name = attachment.Name
	}
	if attachment.Description.IsSpecified() {
		current.Description = attachment.Description
	}
	if attachment.EmbeddedContent.IsSpecified() {
		current.EmbeddedContent = attachment.EmbeddedContent
	}
	// validate
	if err := attachment.Validate(); err != nil {
		a.Logger.Debugw("failed to validate attachment", "error", err)
		return err
	}
	// save the change
	err = a.AttachmentRepository.UpdateByID(
		ctx,
		id,
		current,
	)
	if err != nil {
		a.Logger.Errorw("failed to update attachment", "error", err)
		return err
	}
	a.AuditLogAuthorized(ae)
	return nil
}

// DeleteByID deletes an attachment by id
func (a *Attachment) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Attachment.DeleteById", session)
	ae.Details["id"] = id.String()
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	// get the attachment
	attachment, err := a.AttachmentRepository.GetByID(
		ctx,
		id,
	)
	if err != nil {
		a.Logger.Debugw("attachment not found", "error", err)
		return err
	}
	// delete any references to the attachments in emails
	err = a.EmailRepository.RemoveAttachmentsByAttachmentID(
		ctx,
		id,
	)
	if err != nil {
		a.Logger.Debugw("failed to delete attachment references", "error", err)
		return err
	}
	// delete the file
	companyContext := data.ASSET_GLOBAL_FOLDER
	if attachment.CompanyID.IsSpecified() && !attachment.CompanyID.IsNull() {
		companyContext = attachment.CompanyID.MustGet().String()
	}
	domainRoot, err := securejoin.SecureJoin(a.RootFolder, companyContext)
	if err != nil {
		a.Logger.Debugw("insecure path", "error", err)
		return err
	}
	attachmentFileName := attachment.FileName.MustGet()
	filename, err := securejoin.SecureJoin(domainRoot, attachmentFileName.String())
	if err != nil {

		a.Logger.Debugw("insecure path", "error", err)
		return err
	}
	err = a.FileService.Delete(
		filename,
	)
	if err != nil {
		a.Logger.Debugw("failed to delete attachment file", "error", err)
		return err
	}
	// delete the attachment from the database
	err = a.AttachmentRepository.DeleteByID(
		ctx,
		id,
	)
	if err != nil {
		a.Logger.Errorw("failed to delete attachment from database but the file is deleted",
			"error", err,
		)
		return err
	}
	a.AuditLogAuthorized(ae)
	return nil
}

func (a *Attachment) GetPath(attachment *model.Attachment) (*vo.RelativeFilePath, error) {
	// path
	contextFolder := ""
	if !attachment.CompanyID.IsSpecified() || attachment.CompanyID.IsNull() {
		contextFolder = data.ASSET_GLOBAL_FOLDER
	} else {
		companyID := attachment.CompanyID.MustGet().String()
		contextFolder = companyID
	}
	// map attachments to files
	attachmentFilename := filepath.Clean(attachment.FileName.MustGet().String())
	// relative path is used in the DB
	/*
		relativePath, err := vo.NewRelativeFilePath(attachmentFilename)
		if err != nil {
			a.Logger.Debugw("failed to make file path", err)
			return nil,errs.Wrap(err)
		}
	*/
	// full path is used in the file system
	pathWithRootAndDomainContext, err := securejoin.SecureJoin(a.RootFolder, contextFolder)
	if err != nil {
		a.Logger.Infow("insecure path", "error", err)
		return nil, errs.Wrap(err)
	}
	pathWithRootAndDomainContext, err = securejoin.SecureJoin(
		pathWithRootAndDomainContext,
		attachmentFilename,
	)
	if err != nil {
		a.Logger.Infow("insecure path", "error", err)
		return nil, errs.Wrap(err)
	}
	path, err := vo.NewRelativeFilePath(pathWithRootAndDomainContext)
	if err != nil {
		a.Logger.Debugw("failed to make file path", "error", err)
		return nil, errs.Wrap(err)
	}
	return path, nil
}
