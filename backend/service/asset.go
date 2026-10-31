package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

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

// textEditableExtensions are the file extensions whose content can be loaded
// into the asset editor and saved back as UTF-8 text.
var textEditableExtensions = map[string]bool{
	".html":  true,
	".htm":   true,
	".xhtml": true,
	".txt":   true,
	".css":   true,
	".js":    true,
	".mjs":   true,
	".json":  true,
	".xml":   true,
	".svg":   true,
	".md":    true,
	".csv":   true,
	".yml":   true,
	".yaml":  true,
}

// isTextEditablePath reports whether a path points at a file that can be
// edited as text, based on its extension.
func isTextEditablePath(p string) bool {
	return textEditableExtensions[strings.ToLower(filepath.Ext(p))]
}

// Asset is a Asset service
type Asset struct {
	Common
	RootFolder          string
	FileService         *File
	AssetRepository     *repository.Asset
	DomainRepository    *repository.Domain
	CompanyRepository   *repository.Company
	AllowDenyRepository *repository.AllowDeny
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
		companyProvided := asset.CompanyID.IsSpecified() && !asset.CompanyID.IsNull()
		// ensure context is the same across all files
		if domainNameProvided {
			// domain context
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
		} else if companyProvided {
			// company folder lives under the shared directory keyed by the
			// company assets slug, so it resolves on any domain via the
			// existing shared asset fallback
			companyID := asset.CompanyID.MustGet()
			company, err := a.CompanyRepository.GetByID(g, &companyID)
			if err != nil {
				a.Logger.Debugw("failed to get company for asset", "error", err)
				return ids, errs.Wrap(err)
			}
			slug, err := company.AssetsKey.Get()
			if err != nil || slug.String() == "" {
				a.Logger.Errorw("company has no assets key", "companyID", companyID.String())
				return ids, errs.NewCustomError(errors.New("company has no asset folder"))
			}
			companyFolder := filepath.Join(data.ASSET_GLOBAL_FOLDER, slug.String())
			if contextFolder == "" {
				contextFolder = companyFolder
			} else if contextFolder != companyFolder {
				a.Logger.Error(differentContextError)
				return ids, differentContextError
			}
		} else {
			contextFolder = data.ASSET_GLOBAL_FOLDER
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
	// scope selection:
	// - no company, no domain -> global shared assets
	// - company, no domain     -> company folder assets (stored under shared/<slug>)
	// - domain                 -> domain assets (incl. shared assets for that domain)
	switch {
	case companyID == nil && domainID == nil:
		result, err = a.AssetRepository.GetAllByGlobalContext(
			ctx,
			queryArgs,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			a.Logger.Errorw("failed to get global asset", "error", err)
			return nil, errs.Wrap(err)
		}
	case domainID == nil:
		result, err = a.AssetRepository.GetAllByCompanyContext(
			ctx,
			companyID,
			queryArgs,
		)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			a.Logger.Errorw("failed to get company assets", "error", err)
			return nil, errs.Wrap(err)
		}
	default:
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
	headers, err := a.AssetRepository.GetHeadersByAssetID(ctx, id)
	if err != nil {
		a.Logger.Errorw("failed to get asset headers", "id", id.String(), "error", err)
		return nil, errs.Wrap(err)
	}
	asset.Headers = headers
	lists, err := a.AssetRepository.GetAllowDenyByAssetID(ctx, id)
	if err != nil {
		a.Logger.Errorw("failed to get asset allow deny lists", "id", id.String(), "error", err)
		return nil, errs.Wrap(err)
	}
	asset.AllowDeny = lists
	companyID, err := a.allowDenyCompanyID(ctx, asset)
	if err != nil {
		a.Logger.Errorw("failed to get asset company context", "id", id.String(), "error", err)
		return nil, errs.Wrap(err)
	}
	asset.AllowDenyCompanyID = nullable.NewNullNullable[uuid.UUID]()
	if companyID != nil {
		asset.AllowDenyCompanyID.Set(*companyID)
	}
	// no audit on read
	return asset, nil
}

// allowDenyCompanyID returns the company whose allow deny lists the asset can
// use together with the global lists. An asset with a company uses that
// company. An asset without a company on a domain that belongs to a company
// uses the company of the domain. Otherwise it is nil and only global lists
// can be used.
func (a *Asset) allowDenyCompanyID(
	ctx context.Context,
	asset *model.Asset,
) (*uuid.UUID, error) {
	if companyID, err := asset.CompanyID.Get(); err == nil {
		return &companyID, nil
	}
	domainID, err := asset.DomainID.Get()
	if err != nil {
		return nil, nil
	}
	domain, err := a.DomainRepository.GetByID(ctx, &domainID, &repository.DomainOption{})
	if err != nil {
		return nil, err
	}
	if companyID, err := domain.CompanyID.Get(); err == nil {
		return &companyID, nil
	}
	return nil, nil
}

// SetAllowDenyByID replaces the allow deny lists of an asset. The lists must
// all be allow lists or all deny lists. An asset in a company context can use
// the lists of that company and global lists, any other asset can only use
// global lists. An empty list removes all lists from the asset.
func (a *Asset) SetAllowDenyByID(
	ctx context.Context,
	session *model.Session,
	assetID *uuid.UUID,
	allowDenyIDs []uuid.UUID,
) error {
	ae := NewAuditEvent("Asset.SetAllowDeny", session)
	ae.Details["id"] = assetID.String()
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
	asset, err := a.AssetRepository.GetByID(ctx, assetID)
	if err != nil {
		a.Logger.Debugw("asset not found", "id", assetID.String(), "error", err)
		return err
	}
	companyID, err := a.allowDenyCompanyID(ctx, asset)
	if err != nil {
		a.Logger.Errorw("failed to get asset company context", "id", assetID.String(), "error", err)
		return errs.Wrap(err)
	}
	// load each list once, in the given order
	ids := []*uuid.UUID{}
	idsStr := []string{}
	lists := []*model.AllowDeny{}
	seen := map[uuid.UUID]bool{}
	for _, id := range allowDenyIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		listID := id
		list, err := a.AllowDenyRepository.GetByID(ctx, &listID, &repository.AllowDenyOption{})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return validate.WrapErrorWithField(errors.New("filter not found"), "allowDenyIDs")
		}
		if err != nil {
			a.Logger.Errorw("failed to get allow deny list", "id", listID.String(), "error", err)
			return errs.Wrap(err)
		}
		if !model.AllowDenyUsableInCompany(list, companyID) {
			a.Logger.Debugw("allow deny list not usable for asset",
				"assetID", assetID.String(),
				"allowDenyID", listID.String(),
			)
			return validate.WrapErrorWithField(
				errors.New("filter belongs to another company"),
				"allowDenyIDs",
			)
		}
		lists = append(lists, list)
		ids = append(ids, &listID)
		idsStr = append(idsStr, listID.String())
	}
	if err := model.ValidateAllowDenySameType(lists); err != nil {
		return err
	}
	if err := a.AssetRepository.ReplaceAllowDeny(ctx, assetID, ids); err != nil {
		a.Logger.Errorw("failed to set asset allow deny lists", "id", assetID.String(), "error", err)
		return errs.Wrap(err)
	}
	ae.Details["allowDenyIDs"] = idsStr
	a.AuditLogAuthorized(ae)
	return nil
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
	contentType nullable.Nullable[vo.OptionalString255],
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
	// the content type is kept when the request does not include it
	if contentType.IsSpecified() {
		if contentType.IsNull() {
			current.ContentType = nullable.NewNullableWithValue(*vo.NewEmptyOptionalString255())
		} else {
			current.ContentType = contentType
		}
		if ct, err := current.ContentType.Get(); err == nil {
			ae.Details["contentType"] = ct.String()
		}
	}
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

// AddHeader adds a response header to an asset
func (a *Asset) AddHeader(
	ctx context.Context,
	session *model.Session,
	assetID *uuid.UUID,
	header *model.AssetHeader,
) (*uuid.UUID, error) {
	ae := NewAuditEvent("Asset.AddHeader", session)
	ae.Details["id"] = assetID.String()
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
	// ensure the asset exists
	if _, err := a.AssetRepository.GetByID(ctx, assetID); err != nil {
		a.Logger.Debugw("asset not found", "id", assetID.String(), "error", err)
		return nil, errs.Wrap(err)
	}
	header.AssetID = nullable.NewNullableWithValue(*assetID)
	if err := header.Validate(); err != nil {
		a.Logger.Debugw("failed to validate asset header", "error", err)
		return nil, err
	}
	headerID, err := a.AssetRepository.AddHeader(ctx, header)
	if err != nil {
		a.Logger.Errorw("failed to add asset header", "error", err)
		return nil, errs.Wrap(err)
	}
	ae.Details["headerId"] = headerID.String()
	ae.Details["key"] = header.Key.MustGet().String()
	a.AuditLogAuthorized(ae)
	return headerID, nil
}

// RemoveHeader removes a response header from an asset
func (a *Asset) RemoveHeader(
	ctx context.Context,
	session *model.Session,
	assetID *uuid.UUID,
	headerID *uuid.UUID,
) error {
	ae := NewAuditEvent("Asset.RemoveHeader", session)
	ae.Details["id"] = assetID.String()
	ae.Details["headerId"] = headerID.String()
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
	// ensure the asset exists
	if _, err := a.AssetRepository.GetByID(ctx, assetID); err != nil {
		a.Logger.Debugw("asset not found", "id", assetID.String(), "error", err)
		return err
	}
	// only a header that belongs to this asset is removed
	if err := a.AssetRepository.RemoveHeader(ctx, assetID, headerID); err != nil {
		a.Logger.Debugw("failed to remove asset header", "error", err)
		return err
	}
	a.AuditLogAuthorized(ae)
	return nil
}

// assetContextFolder returns the folder an asset is stored in, relative to the
// asset root. A domain asset lives under the domain folder. A company asset
// with no domain lives under the shared folder in a subfolder named by the
// company assets key. Everything else lives directly in the shared folder.
// This mirrors the folder resolution in Create.
func (a *Asset) assetContextFolder(
	ctx context.Context,
	asset *model.Asset,
) (string, error) {
	if domainName, err := asset.DomainName.Get(); err == nil {
		return domainName.String(), nil
	}
	if asset.CompanyID.IsSpecified() && !asset.CompanyID.IsNull() {
		companyID := asset.CompanyID.MustGet()
		company, err := a.CompanyRepository.GetByID(ctx, &companyID)
		if err != nil {
			a.Logger.Debugw("failed to get company for asset", "error", err)
			return "", err
		}
		slug, err := company.AssetsKey.Get()
		if err != nil || slug.String() == "" {
			a.Logger.Errorw("company has no assets key", "companyID", companyID.String())
			return "", errs.NewCustomError(errors.New("company has no asset folder"))
		}
		return filepath.Join(data.ASSET_GLOBAL_FOLDER, slug.String()), nil
	}
	return data.ASSET_GLOBAL_FOLDER, nil
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
	filePath, err := a.deleteAssetFileAndRow(ctx, asset)
	if err != nil {
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
		if _, err := a.deleteAssetFileAndRow(ctx, asset); err != nil {
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
	assets, err := a.AssetRepository.GetAllByDomainID(
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
		if _, err := a.deleteAssetFileAndRow(ctx, asset); err != nil {
			return err
		}
	}
	a.AuditLogAuthorized(ae)
	return nil
}

// deleteAssetFileAndRow deletes the file of an asset and then its row. An
// asset whose stored path can not be loaded has no file to act on, so only
// the row is deleted. It returns the full file path, or an empty string when
// no file was deleted.
func (a *Asset) deleteAssetFileAndRow(
	ctx context.Context,
	asset *model.Asset,
) (string, error) {
	assetID, err := asset.ID.Get()
	if err != nil {
		a.Logger.Debugw("failed to get asset id", "error", err)
		return "", err
	}
	filePath := ""
	p, err := asset.Path.Get()
	relPath := ""
	if err == nil {
		relPath = path.Clean(p.String())
	}
	if relPath == "" || relPath == "." || relPath == "/" {
		a.Logger.Warnw("asset has no usable path, deleting only the row",
			"id", assetID.String(),
		)
	} else {
		domainContext, err := a.assetContextFolder(ctx, asset)
		if err != nil {
			return "", err
		}
		root, contextRoot, err := a.openContextRootFor(domainContext)
		if err != nil {
			return "", err
		}
		defer root.Close()
		defer contextRoot.Close()

		filePath = filepath.Join(a.RootFolder, domainContext, relPath)
		if err := a.FileService.Delete(contextRoot, relPath); err != nil {
			a.Logger.Debugw("failed to delete file",
				"path", filePath,
				"error", err,
			)
			return "", err
		}
		if err := a.FileService.RemoveEmptyFolderRecursively(
			contextRoot,
			path.Dir(relPath),
		); err != nil {
			a.Logger.Debugw("failed to remove empty folders",
				"path", filePath,
				"error", err,
			)
			return "", err
		}
	}
	if err := a.AssetRepository.DeleteByID(ctx, &assetID); err != nil {
		a.Logger.Errorw("failed to delete asset from database",
			"id", assetID.String(),
			"path", filePath,
			"error", err,
		)
		return "", err
	}
	return filePath, nil
}

// openContextRoot opens a root confined to the folder the asset is stored in.
// The caller is responsible for closing both returned roots.
func (a *Asset) openContextRoot(
	ctx context.Context,
	asset *model.Asset,
) (*os.Root, *os.Root, error) {
	domainContext, err := a.assetContextFolder(ctx, asset)
	if err != nil {
		return nil, nil, err
	}
	return a.openContextRootFor(domainContext)
}

// openContextRootFor opens a root confined to the given context folder.
// The caller is responsible for closing both returned roots.
func (a *Asset) openContextRootFor(
	domainContext string,
) (*os.Root, *os.Root, error) {
	root, err := os.OpenRoot(a.RootFolder)
	if err != nil {
		a.Logger.Debugw("failed to open root folder", "error", err)
		return nil, nil, err
	}
	contextRoot, err := root.OpenRoot(domainContext)
	if err != nil {
		root.Close()
		a.Logger.Debugw("failed to open context", "error", err)
		return nil, nil, err
	}
	return root, contextRoot, nil
}

// GetContentByID returns an asset's file content and whether it can be edited
// as text. Content is read through a root confined to the asset folder.
func (a *Asset) GetContentByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	maxBytes int64,
) ([]byte, bool, error) {
	ae := NewAuditEvent("Asset.GetContentById", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return nil, false, errs.Wrap(err)
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return nil, false, errs.ErrAuthorizationFailed
	}
	asset, err := a.AssetRepository.GetByID(ctx, id)
	if err != nil {
		a.Logger.Debugw("asset not found", "id", id.String(), "error", err)
		return nil, false, errs.Wrap(err)
	}
	p, err := asset.Path.Get()
	if err != nil {
		a.Logger.Debugw("failed to get path", "error", err)
		return nil, false, err
	}
	root, contextRoot, err := a.openContextRoot(ctx, asset)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	defer contextRoot.Close()
	f, err := contextRoot.Open(p.String())
	if err != nil {
		a.Logger.Debugw("failed to open asset file", "error", err)
		return nil, false, err
	}
	defer f.Close()
	// refuse to load a file larger than the limit so a huge asset is not read into
	// memory (and base64 inflated) just to answer an edit request.
	if maxBytes > 0 {
		if info, serr := f.Stat(); serr == nil && info.Size() > maxBytes {
			return nil, false, errs.NewValidationError(fmt.Errorf("file is too large to edit"))
		}
	}
	// bound the read as a safeguard even if the Stat above was skipped or lied
	var reader io.Reader = f
	if maxBytes > 0 {
		reader = io.LimitReader(f, maxBytes)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		a.Logger.Errorw("failed to read asset file", "error", err)
		return nil, false, err
	}
	editable := isTextEditablePath(p.String()) && utf8.Valid(content)
	// no audit on read
	return content, editable, nil
}

// SaveContentByID overwrites an existing text asset's file content. Only UTF-8
// text files with an editable extension can be saved this way.
func (a *Asset) SaveContentByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	content []byte,
) error {
	ae := NewAuditEvent("Asset.SaveContentById", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	asset, err := a.AssetRepository.GetByID(ctx, id)
	if err != nil {
		a.Logger.Debugw("asset not found", "id", id.String(), "error", err)
		return err
	}
	p, err := asset.Path.Get()
	if err != nil {
		a.Logger.Debugw("failed to get path", "error", err)
		return err
	}
	// only editable text files can be written through the editor
	if !isTextEditablePath(p.String()) {
		return errs.NewValidationError(fmt.Errorf("file is not an editable text file"))
	}
	if !utf8.Valid(content) {
		return errs.NewValidationError(fmt.Errorf("content is not valid UTF-8 text"))
	}
	root, contextRoot, err := a.openContextRoot(ctx, asset)
	if err != nil {
		return err
	}
	defer root.Close()
	defer contextRoot.Close()
	// the file must already exist, editing never creates a new asset
	if _, err := contextRoot.Stat(p.String()); err != nil {
		a.Logger.Debugw("asset file not found", "path", p.String(), "error", err)
		return err
	}
	if err := a.FileService.UploadFile(contextRoot, p.String(), bytes.NewBuffer(content), true); err != nil {
		a.Logger.Errorw("failed to write asset content", "error", err)
		return err
	}
	if err := a.AssetRepository.UpdateByID(ctx, id, &model.Asset{}); err != nil {
		a.Logger.Errorw("failed to update asset timestamp", "error", err)
		return err
	}
	ae.Details["path"] = p.String()
	a.AuditLogAuthorized(ae)
	return nil
}

// MoveByID renames or moves an asset to a new path within its current context
// folder. It does not change the asset owner (domain or company).
func (a *Asset) MoveByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	newPath nullable.Nullable[vo.RelativeFilePath],
) error {
	ae := NewAuditEvent("Asset.MoveById", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	asset, err := a.AssetRepository.GetByID(ctx, id)
	if err != nil {
		a.Logger.Debugw("asset not found", "id", id.String(), "error", err)
		return err
	}
	oldPath, err := asset.Path.Get()
	if err != nil {
		a.Logger.Debugw("failed to get path", "error", err)
		return err
	}
	np, err := newPath.Get()
	if err != nil {
		return validate.WrapErrorWithField(errs.NewValidationError(err), "Path")
	}
	// ensure the destination path is safe to use
	cleaned := strings.TrimPrefix(np.String(), "/")
	if cleaned == "" || strings.Contains(cleaned, "..") || strings.HasPrefix(cleaned, "/") {
		a.Logger.Warnw("insecure path", "path", cleaned)
		return validate.WrapErrorWithField(
			errs.NewValidationError(fmt.Errorf("invalid path: %s", cleaned)),
			"Path",
		)
	}
	dst, err := vo.NewRelativeFilePath(cleaned)
	if err != nil {
		return validate.WrapErrorWithField(errs.NewValidationError(err), "Path")
	}
	if dst.String() == oldPath.String() {
		a.AuditLogAuthorized(ae)
		return nil
	}
	root, contextRoot, err := a.openContextRoot(ctx, asset)
	if err != nil {
		return err
	}
	defer root.Close()
	defer contextRoot.Close()
	if _, err := contextRoot.Stat(oldPath.String()); err != nil {
		a.Logger.Debugw("source file not found", "path", oldPath.String(), "error", err)
		return err
	}
	// create destination directories through the root
	dstDir := filepath.Dir(dst.String())
	if dstDir != "." {
		if err := contextRoot.MkdirAll(dstDir, 0755); err != nil {
			a.Logger.Errorw("failed to create destination directory", "error", err)
			return err
		}
	}
	// move with no overwrite: Link creates the destination only when it does not
	// already exist, failing with EEXIST otherwise, so a move can never replace an
	// existing file, even if one appears between a check and the move. the source
	// is then unlinked. Rename is avoided because it silently overwrites the
	// destination. both paths stay confined to the asset context root.
	if err := contextRoot.Link(oldPath.String(), dst.String()); err != nil {
		if os.IsExist(err) {
			return errs.NewValidationError(fmt.Errorf("file already exists: %s", dst.String()))
		}
		a.Logger.Errorw("failed to move asset file", "error", err)
		return err
	}
	if err := contextRoot.Remove(oldPath.String()); err != nil {
		// nothing visible changed yet; drop the new link so no duplicate is left.
		_ = contextRoot.Remove(dst.String())
		a.Logger.Errorw("failed to remove old asset file after move", "error", err)
		return err
	}
	// update the path in the database. if this fails, move the file back so the
	// filesystem and the database never disagree about where the asset lives.
	update := &model.Asset{Path: nullable.NewNullableWithValue(*dst)}
	if err := a.AssetRepository.UpdateByID(ctx, id, update); err != nil {
		a.Logger.Errorw("failed to update asset path, rolling back the file move", "error", err)
		if rbErr := contextRoot.Link(dst.String(), oldPath.String()); rbErr != nil {
			a.Logger.Errorw("failed to restore asset file after a failed move, manual fix needed",
				"from", dst.String(), "to", oldPath.String(), "error", rbErr)
		} else {
			_ = contextRoot.Remove(dst.String())
		}
		return err
	}
	// remove directories left empty by the move. this is cosmetic and runs only
	// after the database agrees with the filesystem, so a failure here does not
	// fail the move or leave the two out of sync.
	if err := a.FileService.RemoveEmptyFolderRecursively(
		contextRoot,
		path.Dir(oldPath.String()),
	); err != nil {
		a.Logger.Debugw("failed to remove empty folders after move", "error", err)
	}
	ae.Details["from"] = oldPath.String()
	ae.Details["to"] = dst.String()
	a.AuditLogAuthorized(ae)
	return nil
}

// ReplaceFileByID overwrites an existing asset's file content with an uploaded
// file, keeping the asset's path and filename.
func (a *Asset) ReplaceFileByID(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
	file *multipart.FileHeader,
) error {
	ae := NewAuditEvent("Asset.ReplaceFileById", session)
	ae.Details["id"] = id.String()
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		a.LogAuthError(err)
		return err
	}
	if !isAuthorized {
		a.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	asset, err := a.AssetRepository.GetByID(ctx, id)
	if err != nil {
		a.Logger.Debugw("asset not found", "id", id.String(), "error", err)
		return err
	}
	p, err := asset.Path.Get()
	if err != nil {
		a.Logger.Debugw("failed to get path", "error", err)
		return err
	}
	root, contextRoot, err := a.openContextRoot(ctx, asset)
	if err != nil {
		return err
	}
	defer root.Close()
	defer contextRoot.Close()
	// the file must already exist, replacing never creates a new asset
	if _, err := contextRoot.Stat(p.String()); err != nil {
		a.Logger.Debugw("asset file not found", "path", p.String(), "error", err)
		return err
	}
	src, err := file.Open()
	if err != nil {
		a.Logger.Errorw("failed to open uploaded file", "error", err)
		return err
	}
	defer src.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, src); err != nil {
		a.Logger.Errorw("failed to read uploaded file", "error", err)
		return err
	}
	if err := a.FileService.UploadFile(contextRoot, p.String(), &buf, true); err != nil {
		a.Logger.Errorw("failed to write asset content", "error", err)
		return err
	}
	if err := a.AssetRepository.UpdateByID(ctx, id, &model.Asset{}); err != nil {
		a.Logger.Errorw("failed to update asset timestamp", "error", err)
		return err
	}
	ae.Details["path"] = p.String()
	a.AuditLogAuthorized(ae)
	return nil
}
