package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-errors/errors"

	"os"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// Asset is a Asset service
type Asset struct {
	Common
	RootFolder       string
	FileService      *File
	AssetRepository  *repository.Asset
	DomainRepository *repository.Domain
}

// Create creates and stores a new assets
func (a *Asset) Create(
	g *gin.Context,
	session *model.Session,
	assets []*model.Asset,
) ([]*uuid.UUID, error) {
	ids := []*uuid.UUID{}
	ae := NewAuditEvent("Asset.Create", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return ids, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return ids, errs.ErrAuthorizationFailed
	}
	// @TODO for now we allow dublicate names - should we?
	// without no dubs it is easier to reason between assets
	// with dubs it is easier to import a collection of files and etc

	// upload the files
	contextFolder := ""
	// ensure that all assets have the same context
	// and map assets to files
	// @TODO move out of here
	differentContextError := fmt.Errorf(
		"all assets must have the same context '%s'",
		contextFolder,
	)

	files := []*RootFileUpload{}
	for _, asset := range assets {
		domainNameProvided := asset.DomainName.IsSpecified() && !asset.DomainName.IsNull()
		// ensure context is the same across all files
		if !domainNameProvided && (!asset.CompanyID.IsSpecified() || asset.CompanyID.IsNull()) {
			contextFolder = data.ASSET_GLOBAL_FOLDER
		} else {
			// set the context folder
			dn, err := asset.DomainName.Get()
			if err != nil {
				a.Logger.Debugw("failed to get domain name", "error", err)
				return ids, errs.Wrap(err)
			}
			domainName := dn.String()
			if contextFolder == "" {
				contextFolder = domainName
			} else if contextFolder != domainName {
				a.Logger.Error(differentContextError)
				return ids, differentContextError
			}
		}

		// map assets to files
		path := ""
		pp, err := asset.Path.Get()
		if err != nil {
			a.Logger.Debugw("failed to get path", "error", err)
			return ids, errs.Wrap(err)
		}
		if p := pp.String(); len(p) > 0 {
			// ensure the path is safe to use

			// check if the first char is a / if it is, strip it
			p = strings.TrimPrefix(p, "/")
			if strings.Contains(p, "..") || strings.HasPrefix(p, "/") {
				a.Logger.Warnw("insecure path", "path", p)
				return ids, validate.WrapErrorWithField(
					errs.NewValidationError(fmt.Errorf("invalid path: %s", p)),
					"Path",
				)
			}
			path = p
		}
		// build full relative path including filename for DB storage
		fullRelativePath := filepath.Join(path, asset.File.Filename)
		// relative path is used in the DB
		relativePath, err := vo.NewRelativeFilePath(fullRelativePath)
		if err != nil {
			a.Logger.Debugw("failed to make file path", "error", err)
			return ids, validate.WrapErrorWithField(
				errs.NewValidationError(err),
				"Path",
			)
		}
		// TODO a global asset can be attached to a global domain but
		// a company domain can not have a global asset ( asset without company id )
		// a company domain can not have a domain that belongs to another company
		if asset.DomainID.IsSpecified() && !asset.DomainID.IsNull() {
			assetDomainID := asset.DomainID.MustGet()
			domain, err := a.DomainRepository.GetByID(
				g,
				&assetDomainID,
				&repository.DomainOption{},
			)
			if err != nil {
				a.Logger.Debugw("failed to get domain by asset", "error", err)
				return ids, errs.Wrap(err)
			}
			// a company domain can not have a global asset ( asset without company id )
			domainHasCompanyRelation := domain.CompanyID.IsSpecified() && !domain.CompanyID.IsNull()
			assetHasCompanyRelation := asset.CompanyID.IsSpecified() && !asset.CompanyID.IsNull()
			if !assetHasCompanyRelation && domainHasCompanyRelation {
				a.Logger.Debug("company id is required for domain")
				return ids, errs.NewCustomError(errors.New("shared view (no asset company id) can not be attached to a domain with a company id"))
			}
			// company domain can not have a domain company that belongs to another company and is not global
			if domainHasCompanyRelation && assetHasCompanyRelation {
				if domain.CompanyID.MustGet().String() != asset.CompanyID.MustGet().String() {
					a.Logger.Debug("domain company id is not the same as asset company id")
					return ids, errs.NewCustomError(errors.New("domain company id is not the same as asset company id"))
				}
			}
		}

		// this is a bit dirty, but I will do it anyway
		// overwriting the path the client assigned with the context relative path including the file name
		asset.Path = nullable.NewNullableWithValue(*relativePath)
		// ensure base asset directory exists
		if err := os.MkdirAll(a.RootFolder, 0755); err != nil {
			a.Logger.Debugw("failed to create asset root directory", "error", err)
			return ids, fmt.Errorf("failed to create asset root directory: %s", err)
		}

		// create root filesystem for the full context path (controlled paths only)
		fullContextPath := filepath.Join(a.RootFolder, contextFolder)
		contextRoot, err := os.OpenRoot(fullContextPath)
		var isUsingParentRoot bool
		if err != nil {
			// context path doesn't exist - this is OK for uploads, directories will be created
			a.Logger.Debugw("context path doesn't exist yet", "path", fullContextPath, "error", err)
			// for validation purposes, we'll use the parent root and validate the context is safe
			parentRoot, parentErr := os.OpenRoot(a.RootFolder)
			if parentErr != nil {
				a.Logger.Debugw("failed to open root folder", "error", parentErr)
				return ids, fmt.Errorf("failed to open root folder: %s", parentErr)
			}
			defer parentRoot.Close()

			// validate context folder is safe (doesn't need to exist)
			_, statErr := parentRoot.Stat(contextFolder)
			if statErr != nil && !os.IsNotExist(statErr) {
				a.Logger.Debugw("invalid context folder", "error", statErr)
				return ids, fmt.Errorf("invalid context folder: %s", statErr)
			}
			contextRoot = parentRoot
			isUsingParentRoot = true
		} else {
			defer contextRoot.Close()
		}

		// build and validate full user path through OpenRoot
		var fullUserPath string
		if path != "" {
			fullUserPath = filepath.Join(strings.Trim(path, "/"), asset.File.Filename)
		} else {
			fullUserPath = asset.File.Filename
		}

		// validate full path is safe (doesn't need to exist)
		_, err = contextRoot.Stat(fullUserPath)
		if err != nil && !os.IsNotExist(err) {
			a.Logger.Debugw("invalid file path", "path", fullUserPath, "error", err)
			return ids, fmt.Errorf("invalid file path: %s", err)
		}

		// build relative path for secure upload
		var uploadRelativePath string
		if path != "" {
			uploadRelativePath = filepath.Join(strings.Trim(path, "/"), asset.File.Filename)
		} else {
			uploadRelativePath = asset.File.Filename
		}

		// if using parent root, we need to include context folder in path
		var pathToValidate string
		if isUsingParentRoot {
			pathToValidate = filepath.Join(contextFolder, uploadRelativePath)
		} else {
			pathToValidate = uploadRelativePath
		}

		a.Logger.Debugw("secure file path",
			"contextPath", fullContextPath,
			"relativePath", pathToValidate,
		)

		files = append(files, NewRootFileUpload(contextRoot, pathToValidate, &asset.File))
	}
	// upload files to the file system using secure method
	_, err = a.FileService.Upload(
		g,
		files,
	)
	if err != nil {
		a.Logger.Debugw("failed to upload files", "error", err)
		return ids, errs.Wrap(err)
	}
	idsStr := []string{}
	// save uploaded files to the database
	for _, asset := range assets {
		id, err := a.AssetRepository.Insert(
			g,
			asset,
		)
		if err != nil {
			a.Logger.Debugw("failed to save asset", "error", err)
			// TODO remove all previously uploaded files
			// buut maybe not, it would be annoying if there is a multi user system
			// and a user uploads a huge amount of files and one fails and does this
			// repeatedly to burn the server
			return ids, errs.Wrap(err)
		}
		ids = append(ids, id)
		idsStr = append(idsStr, id.String())
	}
	ae.Details["assetIDs"] = idsStr
	a.AuditLogAuthorized(ae)

	return ids, nil
}

// GetAll gets all assets
func (a *Asset) GetAll(
	ctx context.Context,
	session *model.Session,
	domainID *uuid.UUID,
	companyID *uuid.UUID,
	queryArgs *vo.QueryArgs,
) (*model.Result[model.Asset], error) {
	result := model.NewEmptyResult[model.Asset]()
	ae := NewAuditEvent("Asset.GetAll", session)
	if domainID != nil {
		ae.Details["domainID"] = domainID.String()
	}
	if companyID != nil {
		ae.Details["companyID"] = companyID.String()
	}
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
	// if there is no companyID or domainID then the scope is 'shared'
	if companyID == nil && domainID == nil {
		result, err = a.AssetRepository.GetAllByGlobalContext(
			ctx,
			queryArgs,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			a.Logger.Errorw("failed to get global asset", "error", err)
			return nil, errs.Wrap(err)
		}
	} else {
		if domainID == nil {
			a.Logger.Errorw("domain id required", "error", errors.New("domainID is nil"))
			return nil, fmt.Errorf("domain id is required")
		}
		result, err = a.AssetRepository.GetAllByDomainAndContext(
			ctx,
			domainID,
			companyID,
			queryArgs,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			a.Logger.Errorw("failed to get domain assets", "error", err)
			return nil, errs.Wrap(err)
		}
	}
	// no audit log for read
	return result, nil
}

// GetByID gets an asset by id
func (a *Asset) GetByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (*model.Asset, error) {
	ae := NewAuditEvent("Asset.GetById", session)
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
	// get the asset
	asset, err := a.AssetRepository.GetByID(
		ctx,
		id,
	)
	if err != nil {
		a.Logger.Debugw("asset not found",
			"id", id.String(),
			"error", err,
		)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return asset, nil
}

// GetByID gets an asset by path
func (a *Asset) GetByPath(
	ctx context.Context,
	session *model.Session,
	path string,
) (*model.Asset, error) {
	ae := NewAuditEvent("Asset.GetByPath", session)
	ae.Details["path"] = path
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
	// get the asset
	asset, err := a.AssetRepository.GetByPath(ctx, path)
	if err != nil {
		a.Logger.Debugw("asset not found by path",
			"path", path,
			"error", err,
		)
		return nil, errs.Wrap(err)
	}
	// no audit on read
	return asset, nil
}

// UpdateByID updates an asset by id
func (a *Asset) UpdateByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	name nullable.Nullable[vo.OptionalString127],
	description nullable.Nullable[vo.OptionalString255],
) error {
	ae := NewAuditEvent("Asset.UpdateById", session)
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
	// get the current
	current, err := a.AssetRepository.GetByID(
		ctx,
		id,
	)
	if err != nil {
		a.Logger.Debugw("asset not found", "error", err)
		return err
	}
	// update the asset
	current.Name = name
	current.Description = description
	// validate
	if err := current.Validate(); err != nil {
		a.Logger.Debugw("failed to validate asset", "error", err)
		return err
	}
	// save the change
	err = a.AssetRepository.UpdateByID(
		ctx,
		id,
		current,
	)
	if err != nil {
		a.Logger.Errorw("failed to update asset", "error", err)
		return err
	}
	a.AuditLogAuthorized(ae)
	return nil
}

// DeleteByID deletes an asset by id
func (a *Asset) DeleteByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) error {
	ae := NewAuditEvent("Asset.DeleteById", session)
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
	// get the asset
	asset, err := a.AssetRepository.GetByID(
		ctx,
		id,
	)
	if err != nil {
		a.Logger.Debugw("asset not found",
			"id", id.String(),
			"error", err,
		)
		return err
	}
	// delete the file
	domainContext := data.ASSET_GLOBAL_FOLDER
	if domainName, err := asset.DomainName.Get(); err == nil {
		domainContext = domainName.String()
	}
	p, err := asset.Path.Get()
	if err != nil {
		a.Logger.Debugw("failed to get path", "error", err)
		return err
	}

	// create root filesystem for secure deletion
	root, err := os.OpenRoot(a.RootFolder)
	if err != nil {
		a.Logger.Debugw("failed to open root folder", "error", err)
		return err
	}
	defer root.Close()

	// validate domain context access
	domainRoot, err := root.OpenRoot(domainContext)
	if err != nil {
		a.Logger.Debugw("failed to open domain context", "error", err)
		return err
	}
	defer domainRoot.Close()

	// validate file exists within domain context
	_, err = domainRoot.Stat(p.String())
	if err != nil {
		a.Logger.Debugw("file not found in domain context", "error", err)
		return err
	}

	// build safe file path (validated by OpenRoot)
	filePath := filepath.Join(a.RootFolder, domainContext, p.String())

	err = a.FileService.Delete(
		filePath,
	)
	if err != nil {
		a.Logger.Debugw("failed to delete file",
			"path", filePath,
			"error", err,
		)
		return err
	}
	err = a.FileService.RemoveEmptyFolderRecursively(
		filepath.Join(a.RootFolder, domainContext),
		filepath.Dir(filePath),
	)
	if err != nil {
		a.Logger.Debugw("failed to remove empty folders",
			"path", filePath,
			"error", err,
		)
		return err
	}
	// delete the asset from the database
	err = a.AssetRepository.DeleteByID(
		ctx,
		id,
	)
	if err != nil {
		a.Logger.Errorw("failed to delete asset from database but the file is deleted",
			"path", filePath,
			"error", err,
		)
		return err
	}
	ae.Details["path"] = filePath
	a.AuditLogAuthorized(ae)
	return nil
}

// DeleteAllByCompanyID deletes all assets by company ID
func (a *Asset) DeleteAllByCompanyID(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) error {
	ae := NewAuditEvent("Asset.DeleteAllByCompanyID", session)
	if companyID != nil {
		ae.Details["companyID"] = companyID
	}
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
	// get assets
	assets, err := a.AssetRepository.GetAllByCompanyID(
		ctx,
		companyID,
	)
	if err != nil {
		a.Logger.Debugw("asset not found", "error", err)
		return err
	}
	for _, asset := range assets {
		// delete the file
		domainContext := data.ASSET_GLOBAL_FOLDER
		if domainName, err := asset.DomainName.Get(); err == nil {
			domainContext = domainName.String()
		}
		p, err := asset.Path.Get()
		if err != nil {
			a.Logger.Debugw("failed to get path", "error", err)
			return err
		}
		// create root filesystem for secure deletion
		root, err := os.OpenRoot(a.RootFolder)
		if err != nil {
			a.Logger.Debugw("failed to open root folder", "error", err)
			return err
		}
		defer root.Close()

		// validate domain context access
		domainContextRoot, err := root.OpenRoot(domainContext)
		if err != nil {
			a.Logger.Debugw("failed to open domain context", "error", err)
			return err
		}
		defer domainContextRoot.Close()

		// validate file exists within domain context
		_, err = domainContextRoot.Stat(p.String())
		if err != nil {
			a.Logger.Debugw("file not found in domain context", "error", err)
			return err
		}

		// build safe file path (validated by OpenRoot)
		filePath := filepath.Join(a.RootFolder, domainContext, p.String())
		err = a.FileService.Delete(
			filePath,
		)
		if err != nil {
			a.Logger.Debugw("failed to delete file",
				"path", filePath,
				"error", err,
			)
			return err
		}
		err = a.FileService.RemoveEmptyFolderRecursively(
			filepath.Join(a.RootFolder, domainContext),
			filepath.Dir(filePath),
		)
		if err != nil {
			a.Logger.Debugw("failed to remove empty folders",
				"path", filePath,
				"error", err,
			)
			return err
		}
		// delete the asset from the database
		err = a.AssetRepository.DeleteByID(
			ctx,
			companyID,
		)
		if err != nil {
			a.Logger.Errorw("failed to delete asset from database but the file is deleted",
				"path", filePath,
				"error", err,
			)
			return err
		}
	}
	a.AuditLogAuthorized(ae)
	return nil
}

// DeleteAllByDomainID deletes all assets by domain ID
func (a *Asset) DeleteAllByDomainID(
	ctx context.Context,
	session *model.Session,
	domainID *uuid.UUID,
) error {
	ae := NewAuditEvent("Asset.DeleteAllByDomainID", session)
	if domainID != nil {
		ae.Details["domainId"] = domainID.String()
	}
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
	// get assets
	assets, err := a.AssetRepository.GetAllByCompanyID(
		ctx,
		domainID,
	)
	if err != nil {
		a.Logger.Debugw("assets not found by domain ID",
			"domainID", domainID.String(),
			"error", err,
		)
		return err
	}
	// delete
	for _, asset := range assets {

		// delete the file
		domainContext := data.ASSET_GLOBAL_FOLDER
		if domainName, err := asset.DomainName.Get(); err == nil {
			domainContext = domainName.String()
		}
		p, err := asset.Path.Get()
		if err != nil {
			a.Logger.Debugw("failed to get path",
				"error", err,
			)
			return err
		}

		// create root filesystem for secure deletion
		root, err := os.OpenRoot(a.RootFolder)
		if err != nil {
			a.Logger.Debugw("failed to open root folder", "error", err)
			return err
		}
		defer root.Close()

		// validate domain context access
		domainContextRoot, err := root.OpenRoot(domainContext)
		if err != nil {
			a.Logger.Debugw("failed to open domain context", "error", err)
			return err
		}
		defer domainContextRoot.Close()

		// validate file exists within domain context
		_, err = domainContextRoot.Stat(p.String())
		if err != nil {
			a.Logger.Debugw("file not found in domain context", "error", err)
			return err
		}

		// build safe file path (validated by OpenRoot)
		filePath := filepath.Join(a.RootFolder, domainContext, p.String())
		err = a.FileService.Delete(
			filePath,
		)
		if err != nil {
			a.Logger.Debugw("failed to delete file",
				"path", filePath,
				"error", err,
			)
			return err
		}
		err = a.FileService.RemoveEmptyFolderRecursively(
			filepath.Join(a.RootFolder, domainContext),
			filepath.Dir(filePath),
		)
		if err != nil {
			a.Logger.Debugw("failed to remove empty folders",
				"path", filePath,
				"error", err,
			)
			return err
		}
		// delete the asset from the database
		err = a.AssetRepository.DeleteByID(
			ctx,
			domainID,
		)
		if err != nil {
			a.Logger.Errorw("failed to delete asset from database but the file is deleted",
				"path", filePath,
				"error", err,
			)
			return err
		}
	}
	a.AuditLogAuthorized(ae)
	return nil
}
