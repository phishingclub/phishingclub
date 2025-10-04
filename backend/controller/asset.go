package controller

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
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

// AssetOrderByMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var AssetsColumnsMap = map[string]string{
	"created_at":  repository.TableColumn(database.ASSET_TABLE, "created_at"),
	"updated_at":  repository.TableColumn(database.ASSET_TABLE, "updated_at"),
	"name":        repository.TableColumn(database.ASSET_TABLE, "name"),
	"description": repository.TableColumn(database.ASSET_TABLE, "description"),
	"path":        repository.TableColumn(database.ASSET_TABLE, "path"),
}

// Asset is an static Asset controller
type Asset struct {
	Common
	StaticAssetPath string
	DomainService   *service.Domain
	OptionService   *service.Option
	AssetService    *service.Asset
}

// GetContentByID get the content and mime type of an asset
func (a *Asset) GetContentByID(g *gin.Context) {
	// handle session
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// check permissions
	isAuthorized, err := service.IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		_ = handleServerError(g, a.Response, err)
		return
	}
	if !isAuthorized {
		a.Response.Unauthorized(g)
		return
	}
	// get domain
	domain, err := vo.NewString255(g.Param("domain"))
	if err != nil {
		a.Logger.Errorw("invalid domain",
			"domain", domain,
		)
		a.Response.ValidationFailed(g, "Domain", err)
		return
	}
	// if the target is the global folder, use the global folder
	if domain.String() == data.ASSET_GLOBAL_FOLDER {
		// TODO this shold require special permissions or be prefixed with a special path
		// such as the company name or something that is prefixed
		_ = data.ASSET_GLOBAL_FOLDER
	}
	// create root filesystem for secure asset access
	root, err := os.OpenRoot(a.StaticAssetPath)
	if err != nil {
		a.Logger.Debugw("failed to open static asset path root",
			"path", a.StaticAssetPath,
			"error", err,
		)
		a.Response.ServerError(g)
		return
	}
	defer root.Close()

	// validate domain directory access
	domainRoot, err := root.OpenRoot(domain.String())
	if err != nil {
		a.Logger.Debugw("insecure domain path",
			"domain", domain.String(),
			"error", err,
		)
		a.Response.BadRequest(g)
		return
	}
	defer domainRoot.Close()

	// get the file path
	pathDecoded, err := url.QueryUnescape(g.Param("path"))
	if err != nil {
		a.Logger.Debugw("failed to decode path",
			"error", err,
		)
		a.Response.BadRequest(g)
		return
	}

	// clean path and remove leading slash for os.OpenRoot compatibility
	cleanPath := strings.TrimPrefix(filepath.Clean(pathDecoded), "/")

	// validate file path within domain directory
	_, err = domainRoot.Stat(cleanPath)
	if err != nil {
		a.Logger.Debugw("insecure file path",
			"path", cleanPath,
			"error", err,
		)
		a.Response.BadRequest(g)
		return
	}

	// build safe file path (validated by OpenRoot)
	filePath := filepath.Join(a.StaticAssetPath, domain.String(), cleanPath)
	// check if the file exists
	a.Logger.Debugw("checking if asset exists",
		"path", filePath,
	)
	_, err = os.Stat(filePath)
	if errors.Is(err, fs.ErrNotExist) {
		a.Logger.Debugw("asset not found",
			"path", filePath,
		)
		a.Response.NotFound(g)
		return
	}
	if err != nil {
		a.Logger.Errorw("failed to get asset path info",
			"path", filePath,
			"error", err,
		)
		a.Response.ServerError(g)
		return
	}
	// serve the file
	// #nosec
	content, err := os.ReadFile(filePath)
	if err != nil {
		a.Logger.Errorw("failed to read asset",
			"path", filePath,
			"error", err,
		)
		a.Response.ServerError(g)
		return
	}

	fileExt := filepath.Ext(filePath)
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
	encodedContent := base64.StdEncoding.EncodeToString(content)

	a.Response.OK(g, gin.H{
		"mimeType": mimeType,
		"file":     encodedContent,
	})
}

// GetAllForContext gets all static assets for a domain
// and has a special case 'shared' to get all global assets
func (a *Asset) GetAllForContext(g *gin.Context) {
	// handle session
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// check permissions
	isAuthorized, err := service.IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		_ = handleServerError(g, a.Response, err)
		return
	}
	if !isAuthorized {
		a.Response.Unauthorized(g)
		return
	}
	// parse request
	var domainID *uuid.UUID
	companyID := companyIDFromRequestQuery(g)
	domainParam := g.Param("domain")
	queryArgs, ok := a.handleQueryArgs(g)
	if !ok {
		return
	}
	// set default sort by
	queryArgs.RemapOrderBy(AssetsColumnsMap)
	queryArgs.DefaultSortByUpdatedAt()
	a.Logger.Debugw("getting assets for domain",
		"domain", domainParam,
		"companyID", companyID,
	)
	// if there is no domain then it is a global asset request
	// else the domain name is the asset scope
	if len(domainParam) > 0 {
		domainName, err := vo.NewString255(domainParam)
		if err != nil {
			a.Logger.Errorw("invalid domain",
				"domain", domainName,
			)
			a.Response.ValidationFailed(g, "Domain", err)
			return
		}
		// get the domains id and also check if the user has permission to retrieve it
		domain, err := a.DomainService.GetByName(
			g.Request.Context(),
			session,
			domainName,
			&repository.DomainOption{},
		)
		if ok := a.handleErrors(g, err); !ok {
			return
		}
		did := domain.ID.MustGet()
		domainID = &did
	}
	// get assets
	a.Logger.Debugw("getting assets for domain by ID",
		"domainID", domainID,
	)
	assets, err := a.AssetService.GetAll(
		g,
		session,
		domainID,
		companyID,
		queryArgs,
	)
	// handle responses
	a.handleErrors(g, err)
	a.Response.OK(g, assets)
}

// Create uploads an static asset
func (a *Asset) Create(g *gin.Context) {
	// handle session
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// this is a form data request, so we must handle all fields manually as is it not parsed from the struct
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
	contextParam := g.PostForm("domain")
	// if no domain is set, use the global folder
	var domain *model.Domain
	// if a domain is supplied we look for its assets
	if len(contextParam) > 0 {
		// check that the domain exists
		name, err := vo.NewString255(contextParam)
		if err != nil {
			a.Logger.Errorw("invalid domain name",
				"error", err,
			)
			a.Response.ValidationFailed(g, "Domain", err)
			return
		}
		d, err := a.DomainService.GetByName(
			g,
			session,
			name,
			&repository.DomainOption{},
		)
		if ok := a.handleErrors(g, err); !ok {
			return
		}
		domain = d
		a.Logger.Debugw("uploading assets to domain",
			"domain", contextParam,
		)
	} else {
		a.Logger.Debug("uploading shared assets")
	}
	// map files to assets
	assets := []*model.Asset{}
	for _, file := range multipartData.File["files"] {
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
				fmt.Errorf("file '%s' is too large", utils.ReadableFileName(file.Filename)),
			)
			return
		}
		// TODO multi user validate that the company id is the same as the session company id or that the session is a super admin
		// TODO can the creation of the ID be moved to the repo
		var domainID string
		if domain != nil {
			did := domain.ID.MustGet()
			domainID = did.String()
		}
		name, err := vo.NewOptionalString127(g.Request.PostFormValue("name"))
		if err != nil {
			a.Logger.Debugw("failed to parse name",
				"error", err,
			)
			a.Response.ValidationFailed(g, "Name", err)
			return
		}
		description, err := vo.NewOptionalString255(g.Request.PostFormValue("description"))
		if err != nil {
			a.Logger.Debugw("failed to parse description",
				"error", err,
			)
			a.Response.ValidationFailed(g, "Description", err)
			return
		}
		path, err := vo.NewRelativeFilePath(g.Request.PostFormValue("path"))
		if err != nil {
			a.Logger.Debugw("failed to parse path",
				"error", err,
			)
			a.Response.ValidationFailed(g, "Path", err)
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
				a.Response.ValidationFailed(g, "CompanyID", err)
			}
			companyID.Set(cid)
		} else {
			companyID.SetNull()
		}

		assetName := nullable.NewNullableWithValue(*name)
		assetDescription := nullable.NewNullableWithValue(*description)
		assetPath := nullable.NewNullableWithValue(*path)
		assetDomainID := nullable.NewNullNullable[uuid.UUID]()
		if len(domainID) > 0 {
			did, err := uuid.Parse(domainID)
			if err != nil {
				a.Logger.Debugw("failed to parse domain id",
					"error", err,
				)
				a.Response.ValidationFailed(g, "DomainID", err)
				return
			}
			assetDomainID.Set(did)
			// if the asset belongs to a domain it must not be 'global' context
			if !companyID.IsSpecified() {
				a.Logger.Debugw(
					"cant add a shared asset to a company owned domain",
					"domainID", domainID,
					"domainOwnerCompanyID", companyID,
				)
				a.Response.ValidationFailed(
					g,
					"domainID",
					errors.New("cant add a shared asset to a company owned domain"),
				)
				return
			}
		}
		asset := model.Asset{
			Name:        assetName,
			Description: assetDescription,
			Path:        assetPath,
			File:        *file,
			DomainID:    assetDomainID,
			CompanyID:   companyID,
		}
		if domain != nil {
			asset.DomainName = domain.Name
		}
		assets = append(assets, &asset)
	}
	//  store the files on disk and in database
	ids, err := a.AssetService.Create(g, session, assets)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, gin.H{
		"ids":            ids,
		"files_uploaded": len(assets),
	})
}

// GetByID gets an static asset by id
func (a *Asset) GetByID(g *gin.Context) {
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
	// get the asset
	ctx := g.Request.Context()
	asset, err := a.AssetService.GetByID(ctx, session, id)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, asset)
}

// UpdateByID updates an static asset by id
func (a *Asset) UpdateByID(g *gin.Context) {
	// handle session
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.Asset
	if ok := a.handleParseRequest(g, &req); !ok {
		return
	}
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	// update the asset
	ctx := g.Request.Context()
	err := a.AssetService.UpdateByID(
		ctx,
		session,
		id,
		req.Name,
		req.Description,
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, gin.H{})
}

// RemoveByID removes an static asset
// if the asset is a directory, it will be removed recursively
func (a *Asset) RemoveByID(g *gin.Context) {
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
	// remove the asset
	ctx := g.Request.Context()
	err := a.AssetService.DeleteByID(
		ctx,
		session,
		id,
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, gin.H{})
}
