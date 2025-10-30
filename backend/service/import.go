package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

const (
	MaxIndividualFileSize = 100 * 1024 * 1024 // 100MB per file
	MaxTotalExtractedSize = 500 * 1024 * 1024 // 500MB total extracted
	MaxFileCount          = 20000             // Maximum files in zip
	MaxCompressionRatio   = 100               // Maximum compression ratio (100:1)
)

// validateCompressionRatio checks if the compression ratio is within safe limits
func validateCompressionRatio(compressedSize, uncompressedSize int64) error {
	if compressedSize == 0 {
		return fmt.Errorf("invalid compressed size")
	}
	ratio := float64(uncompressedSize) / float64(compressedSize)
	if ratio > MaxCompressionRatio {
		return fmt.Errorf("compression ratio too high: %.2f (max: %d)", ratio, MaxCompressionRatio)
	}
	return nil
}

// Import controller handles import from assets, emails, landing pages and etc.
type Import struct {
	Common
	File            *File
	Asset           *Asset
	Page            *Page
	Email           *Email
	EmailRepository *repository.Email
	PageRepository  *repository.Page
}

// DataYAML represents the structure of data.yaml for templates
type EmailMeta struct {
	Name         string `yaml:"name"`
	File         string `yaml:"file"`
	EnvelopeFrom string `yaml:"envelope from"`
	From         string `yaml:"from"`
	Subject      string `yaml:"subject"`
}

type DataYAML struct {
	Name  string `yaml:"name"`
	Pages []struct {
		Name string `yaml:"name"`
		File string `yaml:"file"`
	} `yaml:"pages"`
	Emails []EmailMeta `yaml:"emails"`
}

// Import takes a file header and import from a zip file
type ImportSummary struct {
	AssetsCreated     int           `json:"assets_created"`
	AssetsCreatedList []string      `json:"assets_created_list"`
	AssetsSkipped     int           `json:"assets_skipped"`
	AssetsSkippedList []string      `json:"assets_skipped_list"`
	AssetsErrors      int           `json:"assets_errors"`
	AssetsErrorsList  []ImportError `json:"assets_errors_list"`

	PagesCreated     int           `json:"pages_created"`
	PagesCreatedList []string      `json:"pages_created_list"`
	PagesUpdated     int           `json:"pages_updated"`
	PagesUpdatedList []string      `json:"pages_updated_list"`
	PagesSkipped     int           `json:"pages_skipped"`
	PagesSkippedList []string      `json:"pages_skipped_list"`
	PagesErrors      int           `json:"pages_errors"`
	PagesErrorsList  []ImportError `json:"pages_errors_list"`

	EmailsCreated     int           `json:"emails_created"`
	EmailsCreatedList []string      `json:"emails_created_list"`
	EmailsUpdated     int           `json:"emails_updated"`
	EmailsUpdatedList []string      `json:"emails_updated_list"`
	EmailsSkipped     int           `json:"emails_skipped"`
	EmailsSkippedList []string      `json:"emails_skipped_list"`
	EmailsErrors      int           `json:"emails_errors"`
	EmailsErrorsList  []ImportError `json:"emails_errors_list"`

	Errors []ImportError `json:"errors"`
}

func NewImportSummary() *ImportSummary {
	return &ImportSummary{
		AssetsCreatedList: []string{},
		AssetsSkippedList: []string{},
		AssetsErrorsList:  []ImportError{},

		PagesCreatedList: []string{},
		PagesUpdatedList: []string{},
		PagesSkippedList: []string{},
		PagesErrorsList:  []ImportError{},

		EmailsCreatedList: []string{},
		EmailsUpdatedList: []string{},
		EmailsSkippedList: []string{},
		EmailsErrorsList:  []ImportError{},

		Errors: []ImportError{},
	}
}

type ImportError struct {
	Type    string `json:"type"` // "asset", "page", "email", "data.yaml"
	Name    string `json:"name"` // file or template name
	Message string `json:"message"`
}

func (im *Import) Import(
	g *gin.Context,
	session *model.Session,
	fileHeader *multipart.FileHeader,
	forCompany bool,
	companyID *uuid.UUID,
) (*ImportSummary, error) {
	ae := NewAuditEvent("Import.Import", session)
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		im.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		im.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}

	// check size limits
	if fileHeader.Size > MaxIndividualFileSize {
		return nil, fmt.Errorf("file too large: %d bytes (max: %d)", fileHeader.Size, MaxIndividualFileSize)
	}

	// handle file
	zipFile, err := fileHeader.Open()
	if err != nil {
		return nil, err
	}
	defer zipFile.Close()

	// Read all bytes from zipFile to allow random access
	zipBytes, err := io.ReadAll(zipFile)
	if err != nil {
		return nil, err
	}

	return im.ImportFromBytes(g, session, zipBytes, forCompany, companyID)
}

// ImportFromBytes imports templates from raw zip bytes
func (im *Import) ImportFromBytes(
	g *gin.Context,
	session *model.Session,
	zipBytes []byte,
	forCompany bool,
	companyID *uuid.UUID,
) (*ImportSummary, error) {
	// check size limits
	if int64(len(zipBytes)) > MaxIndividualFileSize {
		return nil, fmt.Errorf("file too large: %d bytes (max: %d)", len(zipBytes), MaxIndividualFileSize)
	}

	readerAt := bytes.NewReader(zipBytes)
	r, err := zip.NewReader(readerAt, int64(len(zipBytes)))
	if err != nil {
		return nil, errs.Wrap(err)
	}

	// validate zip file structure and prevent zip bombs
	var totalUncompressedSize int64
	fileCount := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}

		fileCount++
		if fileCount > MaxFileCount {
			return nil, fmt.Errorf("zip contains too many files: %d (max: %d)", fileCount, MaxFileCount)
		}

		if f.UncompressedSize64 > MaxIndividualFileSize {
			return nil, fmt.Errorf("file %s is too large: %d bytes (max: %d)", f.Name, f.UncompressedSize64, MaxIndividualFileSize)
		}

		totalUncompressedSize += int64(f.UncompressedSize64)
		if totalUncompressedSize > MaxTotalExtractedSize {
			return nil, fmt.Errorf("total extracted size too large: %d bytes (max: %d)", totalUncompressedSize, MaxTotalExtractedSize)
		}

		if err := validateCompressionRatio(int64(f.CompressedSize64), int64(f.UncompressedSize64)); err != nil {
			return nil, fmt.Errorf("file %s: %v", f.Name, err)
		}
	}

	summary := &ImportSummary{}
	// 1. Collect all asset files from root-level "assets/"
	var assetFiles []*zip.File
	for _, f := range r.File {
		if !f.FileInfo().IsDir() && strings.HasPrefix(f.Name, "assets/") {
			assetFiles = append(assetFiles, f)
		}
	}
	for _, assetFile := range assetFiles {
		// relative path inside assets/
		relPath := strings.TrimPrefix(assetFile.Name, "assets/")
		// check if exists
		createdNew, err := im.createAssetFromZipFile(g, session, assetFile, relPath)
		if err != nil {
			summary.AssetsErrors++
			summary.AssetsErrorsList = append(summary.AssetsErrorsList, ImportError{
				Type:    "asset",
				Name:    assetFile.Name,
				Message: err.Error(),
			})
		} else if createdNew {
			summary.AssetsCreated++
			summary.AssetsCreatedList = append(summary.AssetsCreatedList, relPath)
		} else {
			summary.AssetsSkipped++
			summary.AssetsSkippedList = append(summary.AssetsSkippedList, relPath)
		}
	}

	// 2. Find all folders containing a data.yaml and process them as template folders
	// map: folder path -> *zip.File for data.yaml
	templateFolders := make(map[string]*zip.File)
	for _, f := range r.File {
		if !f.FileInfo().IsDir() && strings.HasSuffix(f.Name, "data.yaml") {
			dir := filepath.Dir(f.Name)
			templateFolders[dir] = f
			im.Logger.Debugw("Found template folder", "folder", dir, "dataYamlPath", f.Name)
		}
	}

	// 3. Find all standalone template files without data.yaml
	standaloneTemplates := make(map[string][]*zip.File) // folder -> list of HTML files
	processedFolders := make(map[string]bool)

	// mark folders with data.yaml as processed
	for folder := range templateFolders {
		processedFolders[folder] = true
	}

	// find standalone HTML files in folders without data.yaml
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}

		fileName := filepath.Base(f.Name)
		folder := filepath.Dir(f.Name)

		// skip if this folder already has data.yaml or is assets folder
		if processedFolders[folder] || strings.HasPrefix(f.Name, "assets/") {
			continue
		}

		// look for common template file patterns
		if strings.HasSuffix(fileName, ".html") &&
			(fileName == "landing.html" || fileName == "index.html" ||
				fileName == "email.html" || fileName == "landingpage.html") {
			standaloneTemplates[folder] = append(standaloneTemplates[folder], f)
			im.Logger.Debugw("Found standalone template file", "folder", folder, "file", fileName)
		}
	}

	im.Logger.Debugw("Template discovery complete", "foldersWithDataYaml", len(templateFolders), "standaloneTemplateFolders", len(standaloneTemplates))

	// 4. For each template folder, parse data.yaml and process pages/emails
	zipRelPath := func(folder, name string) string {
		cleanName := filepath.Clean(filepath.ToSlash(name))
		cleanFolder := filepath.Clean(filepath.ToSlash(folder))
		if cleanFolder == "." {
			return cleanName
		}
		if !strings.HasPrefix(cleanName, cleanFolder+"/") {
			return cleanName
		}
		return strings.TrimPrefix(cleanName, cleanFolder+"/")
	}

	im.Logger.Debugw("Starting template folder processing", "totalTemplateFolders", len(templateFolders), "totalZipFiles", len(r.File))

	// Pre-build file indices for efficient lookup
	buildFileIndex := func(folder string) map[string]*zip.File {
		fileIndex := make(map[string]*zip.File)
		folderPath := filepath.ToSlash(folder)

		for _, f := range r.File {
			if f.FileInfo().IsDir() {
				continue
			}
			zipPath := filepath.ToSlash(f.Name)

			// Check if file is in this template folder
			if folderPath != "." && !strings.HasPrefix(zipPath, folderPath+"/") {
				continue
			}

			relPath := zipRelPath(folder, f.Name)
			cleanRelPath := filepath.Clean(filepath.ToSlash(relPath))

			// Index by cleaned relative path
			fileIndex[cleanRelPath] = f
			// Also index by case-insensitive version for robustness
			fileIndex[strings.ToLower(cleanRelPath)] = f
		}
		return fileIndex
	}

	for folder, dataYamlFile := range templateFolders {
		im.Logger.Debugw("Processing template folder", "folder", folder, "dataYamlFile", dataYamlFile.Name)

		// read data.yaml
		rc, err := dataYamlFile.Open()
		if err != nil {
			summary.Errors = append(summary.Errors, ImportError{
				Type:    "data.yaml",
				Name:    dataYamlFile.Name,
				Message: fmt.Sprintf("failed to open data.yaml in %s: %v", folder, err),
			})
			continue
		}
		dataYamlContent, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			summary.Errors = append(summary.Errors, ImportError{
				Type:    "data.yaml",
				Name:    dataYamlFile.Name,
				Message: fmt.Sprintf("failed to read data.yaml in %s: %v", folder, err),
			})
			continue
		}
		var dataYaml DataYAML
		if err := yaml.Unmarshal(dataYamlContent, &dataYaml); err != nil {
			summary.Errors = append(summary.Errors, ImportError{
				Type:    "data.yaml",
				Name:    dataYamlFile.Name,
				Message: fmt.Sprintf("failed to parse data.yaml in %s: %v", folder, err),
			})
			continue
		}

		im.Logger.Debugw("Parsed data.yaml", "folder", folder, "name", dataYaml.Name, "pageCount", len(dataYaml.Pages), "emailCount", len(dataYaml.Emails))

		// build file index for this template folder
		fileIndex := buildFileIndex(folder)
		im.Logger.Debugw("Built file index for folder", "folder", folder, "fileCount", len(fileIndex))

		// build sets of page and email file relative paths (relative to the template folder)
		pageFiles := make(map[string]struct{})
		for _, page := range dataYaml.Pages {
			cleanPageFile := filepath.Clean(filepath.ToSlash(page.File))
			im.Logger.Debugw("Page file from yaml", "original", page.File, "cleaned", cleanPageFile)
			pageFiles[cleanPageFile] = struct{}{}
		}
		emailFiles := make(map[string]struct{})
		for _, email := range dataYaml.Emails {
			cleanEmailFile := filepath.Clean(filepath.ToSlash(email.File))
			im.Logger.Debugw("Email file from yaml", "original", email.File, "cleaned", cleanEmailFile)
			emailFiles[cleanEmailFile] = struct{}{}
		}

		// for each file in the zip, check if it's under this template folder (including subfolders)
		for _, f := range r.File {
			if f.FileInfo().IsDir() {
				continue
			}
			zipPath := filepath.ToSlash(f.Name)
			folderPath := filepath.ToSlash(folder)
			if folderPath != "." && !strings.HasPrefix(zipPath, folderPath+"/") {
				continue
			}
			relPath := zipRelPath(folder, f.Name)
			im.Logger.Debugw("Found file in template folder", "name", f.Name, "relPath", relPath)
			// skip data.yaml, page files, and email files by relative path
			if relPath == "data.yaml" {
				im.Logger.Debugw("Skipping data.yaml")
				continue
			}
			if _, ok := pageFiles[relPath]; ok {
				im.Logger.Debugw("Skipping page file", "relPath", relPath)
				continue
			}
			if _, ok := emailFiles[relPath]; ok {
				im.Logger.Debugw("Skipping emailfile", "relPath", relPath)
				continue
			}
			im.Logger.Debugw("Processing as asset", "relPath", relPath)
			// upload as asset, using relPath as the asset path
			created, err := im.createAssetFromZipFile(g, session, f, relPath)
			if err != nil {
				summary.AssetsErrors++
				summary.AssetsErrorsList = append(summary.AssetsErrorsList, ImportError{
					Type:    "asset",
					Name:    f.Name,
					Message: err.Error(),
				})
			} else if created {
				summary.AssetsCreated++
				summary.AssetsCreatedList = append(summary.AssetsCreatedList, relPath)
			} else {
				summary.AssetsSkipped++
				summary.AssetsSkippedList = append(summary.AssetsSkippedList, relPath)
			}
		}

		// PAGE IMPORT
		for _, page := range dataYaml.Pages {
			cleanPageFile := filepath.Clean(filepath.ToSlash(page.File))
			im.Logger.Debugw("Looking for page file", "pageName", page.Name, "pageFile", page.File, "cleanedFile", cleanPageFile, "folder", folder)

			// use file index for efficient lookup
			pageFile, pageFileFound := fileIndex[cleanPageFile]
			if !pageFileFound {
				pageFile, pageFileFound = fileIndex[strings.ToLower(cleanPageFile)]
				if pageFileFound {
					im.Logger.Debugw("Found page file with case-insensitive match", "pageName", page.Name, "file", pageFile.Name)
				}
			}

			if !pageFileFound {
				im.Logger.Warnw("Page file not found", "pageName", page.Name, "expectedFile", cleanPageFile, "folder", folder, "availableFiles", len(fileIndex))
				summary.PagesErrors++
				summary.PagesErrorsList = append(summary.PagesErrorsList, ImportError{
					Type:    "page",
					Name:    page.Name,
					Message: fmt.Sprintf("page file %s not found in folder %s", page.File, folder),
				})
				continue
			}

			// read HTML content
			rc, err := pageFile.Open()
			if err != nil {
				summary.PagesErrors++
				summary.PagesErrorsList = append(summary.PagesErrorsList, ImportError{
					Type:    "page",
					Name:    page.Name,
					Message: fmt.Sprintf("failed to open page file: %v", err),
				})
				continue
			}
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				summary.PagesErrors++
				summary.PagesErrorsList = append(summary.PagesErrorsList, ImportError{
					Type:    "page",
					Name:    page.Name,
					Message: fmt.Sprintf("failed to read page file: %v", err),
				})
				continue
			}

			// create new page
			newPage := &model.Page{}
			name, err := vo.NewString64(page.Name)
			if err != nil {
				summary.PagesErrors++
				summary.PagesErrorsList = append(summary.PagesErrorsList, ImportError{
					Type:    "page",
					Name:    page.Name,
					Message: fmt.Sprintf("failed to create page name: %v", err),
				})
				continue
			}
			newPage.Name = nullable.NewNullableWithValue(*name)
			if pageContent, err := vo.NewOptionalString1MB(string(content)); err == nil {
				newPage.Content = nullable.NewNullableWithValue(*pageContent)
			}
			if forCompany && companyID != nil {
				newPage.CompanyID = nullable.NewNullableWithValue(*companyID)
			}

			// check if page already exists
			existing, err := im.PageRepository.GetByNameAndCompanyID(g.Request.Context(), name, companyID, &repository.PageOption{})
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				summary.PagesErrors++
				summary.PagesErrorsList = append(summary.PagesErrorsList, ImportError{
					Type:    "page",
					Name:    page.Name,
					Message: fmt.Sprintf("failed check for existing page: %v", err),
				})
				continue
			}
			// determin if this is a update operation
			isUpdate := existing != nil
			if isUpdate {
				if companyID == nil && existing.CompanyID.IsNull() {
					// this is a update operation as both have no company ID
				} else if companyID != nil && existing.CompanyID.IsNull() {
					// if we are creating with a company id and the existing has none
					// then this is actually a create
					isUpdate = false
				} else if companyID == nil && existing.CompanyID.IsSpecified() && !existing.CompanyID.IsNull() {
					// if we have no company id but the existing asset has then this is  a create operation
					isUpdate = false
				} else if companyID != nil && existing.CompanyID.IsSpecified() && !existing.CompanyID.IsNull() {
					if companyID.String() != existing.CompanyID.MustGet().String() {
						summary.PagesErrors++
						summary.PagesErrorsList = append(summary.PagesErrorsList, ImportError{
							Type:    "page",
							Name:    page.Name,
							Message: fmt.Sprintf("page '%s' belongs to another company", page.Name),
						})
						continue
					}
				}
			}
			if isUpdate {
				// update
				existingID := existing.ID.MustGet()
				err = im.Page.UpdateByID(g.Request.Context(), session, &existingID, newPage)
				if err != nil {
					summary.PagesErrors++
					summary.PagesErrorsList = append(summary.PagesErrorsList, ImportError{
						Type:    "page",
						Name:    page.Name,
						Message: fmt.Sprintf("failed to update page: %v", err),
					})
				} else {
					summary.PagesUpdated++
					summary.PagesUpdatedList = append(summary.PagesUpdatedList, page.Name)
				}
			} else {
				// create
				_, err = im.Page.Create(g.Request.Context(), session, newPage)
				if err != nil {
					summary.PagesErrors++
					summary.PagesErrorsList = append(summary.PagesErrorsList, ImportError{
						Type:    "page",
						Name:    page.Name,
						Message: fmt.Sprintf("failed to create page: %v", err),
					})
				} else {
					summary.PagesCreated++
					summary.PagesCreatedList = append(summary.PagesCreatedList, page.Name)
				}
			}
		}

		// --- EMAIL IMPORT ---
		for _, email := range dataYaml.Emails {
			cleanEmailFile := filepath.Clean(filepath.ToSlash(email.File))
			im.Logger.Debugw("Looking for email file", "emailName", email.Name, "emailFile", email.File, "cleanedFile", cleanEmailFile, "folder", folder)

			// Use file index for efficient lookup
			emailFile, emailFileFound := fileIndex[cleanEmailFile]
			if !emailFileFound {
				// Try case-insensitive lookup
				emailFile, emailFileFound = fileIndex[strings.ToLower(cleanEmailFile)]
				if emailFileFound {
					im.Logger.Debugw("Found email file with case-insensitive match", "emailName", email.Name, "file", emailFile.Name)
				}
			}

			if !emailFileFound {
				im.Logger.Warnw("Email file not found", "emailName", email.Name, "expectedFile", cleanEmailFile, "folder", folder, "availableFiles", len(fileIndex))
				summary.EmailsErrors++
				summary.EmailsErrorsList = append(summary.EmailsErrorsList, ImportError{
					Type:    "email",
					Name:    email.Name,
					Message: fmt.Sprintf("email file %s not found in folder %s", email.File, folder),
				})
				continue
			}

			// Read HTML content
			rc, err := emailFile.Open()
			if err != nil {
				summary.EmailsErrors++
				summary.EmailsErrorsList = append(summary.EmailsErrorsList, ImportError{
					Type:    "email",
					Name:    email.Name,
					Message: fmt.Sprintf("failed to open email file: %v", err),
				})
				continue
			}
			content, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				summary.EmailsErrors++
				summary.EmailsErrorsList = append(summary.EmailsErrorsList, ImportError{
					Type:    "email",
					Name:    email.Name,
					Message: fmt.Sprintf("failed to read email file: %v", err),
				})
				continue
			}

			// Create new email for this context
			newEmail := &model.Email{}
			if companyID != nil {
				newEmail.CompanyID = nullable.NewNullableWithValue(*companyID)
			}
			if emailContent, err := vo.NewOptionalString1MB(string(content)); err == nil {
				newEmail.Content = nullable.NewNullableWithValue(*emailContent)
			}
			name, err := vo.NewString64(email.Name)
			if err == nil {
				newEmail.Name = nullable.NewNullableWithValue(*name)
			}
			if from, err := vo.NewEmail(email.From); err == nil {
				newEmail.MailHeaderFrom = nullable.NewNullableWithValue(*from)
			}
			if envelopeFrom, err := vo.NewMailEnvelopeFrom(email.EnvelopeFrom); err == nil {
				newEmail.MailEnvelopeFrom = nullable.NewNullableWithValue(*envelopeFrom)
			}
			if subject, err := vo.NewOptionalString255(email.Subject); err == nil {
				newEmail.MailHeaderSubject = nullable.NewNullableWithValue(*subject)
			}
			newEmail.AddTrackingPixel = nullable.NewNullableWithValue(true)
			if forCompany && companyID != nil {
				newEmail.CompanyID = nullable.NewNullableWithValue(*companyID)
			}
			// check if email already exists
			existing, err := im.EmailRepository.GetByNameAndCompanyID(g, name, companyID, &repository.EmailOption{})
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				summary.EmailsErrors++
				summary.EmailsErrorsList = append(summary.EmailsErrorsList, ImportError{
					Type:    "email",
					Name:    email.Name,
					Message: fmt.Sprintf("failed check for existing email: %v", err),
				})
				continue
			}
			// determin if this is a update operation
			isUpdate := existing != nil
			if isUpdate {
				if companyID == nil && existing.CompanyID.IsNull() {
					// this is a update operation as both have no company ID
				} else if companyID != nil && existing.CompanyID.IsNull() {
					// if we are creating with a company id and the existing has none
					// then this is actually a create
					isUpdate = false
				} else if companyID == nil && existing.CompanyID.IsSpecified() && !existing.CompanyID.IsNull() {
					// if we have no company id but the existing email has then this is a create operation
					isUpdate = false
				} else if companyID != nil && existing.CompanyID.IsSpecified() && !existing.CompanyID.IsNull() {
					if companyID.String() != existing.CompanyID.MustGet().String() {
						summary.EmailsErrors++
						summary.EmailsErrorsList = append(summary.EmailsErrorsList, ImportError{
							Type:    "email",
							Name:    email.Name,
							Message: fmt.Sprintf("email '%s' belongs to another company", email.Name),
						})
						continue
					}
				}
			}
			if isUpdate {
				// update
				existingID := existing.ID.MustGet()
				err = im.Email.UpdateByID(g, session, &existingID, newEmail)
				if err != nil {
					summary.EmailsErrors++
					summary.EmailsErrorsList = append(summary.EmailsErrorsList, ImportError{
						Type:    "email",
						Name:    email.Name,
						Message: fmt.Sprintf("failed to update email: %v", err),
					})
				} else {
					summary.EmailsUpdated++
					summary.EmailsUpdatedList = append(summary.EmailsUpdatedList, email.Name)
				}
			} else {
				// create
				_, err = im.Email.Create(g.Request.Context(), session, newEmail)
				if err != nil {
					summary.EmailsErrors++
					summary.EmailsErrorsList = append(summary.EmailsErrorsList, ImportError{
						Type:    "email",
						Name:    email.Name,
						Message: fmt.Sprintf("failed to create email: %v", err),
					})
				} else {
					summary.EmailsCreated++
					summary.EmailsCreatedList = append(summary.EmailsCreatedList, email.Name)
				}
			}
		}
	}

	return summary, nil
}

// createAssetFromZipFile creates an Asset from a zip file entry and saves it directly
// returns true if a new asset was created
func (im *Import) createAssetFromZipFile(
	g *gin.Context,
	session *model.Session,
	f *zip.File,
	relativePath string,
) (bool, error) {
	// check if asset already exists by path
	existing, err := im.Asset.GetByPath(g.Request.Context(), session, relativePath)

	if err == nil && existing != nil {
		// asset already exists - check if it has a company ID (if so, skip it)
		if existing.CompanyID.IsSpecified() && !existing.CompanyID.IsNull() {
			// asset belongs to a company - skip it to maintain global-only policy
			return false, nil
		}
		// asset already exists and is global
		return false, nil
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}

	// open the file from zip
	rc, err := f.Open()
	if err != nil {
		return false, err
	}
	defer rc.Close()

	// read file content
	content, err := io.ReadAll(rc)
	if err != nil {
		return false, err
	}

	// use the provided relativePath for the asset path
	fullRelativePath := relativePath

	// create asset model
	asset := &model.Asset{}

	// set the name from filename
	filename := filepath.Base(f.Name)
	if name, err := vo.NewOptionalString127(filename); err == nil {
		asset.Name = nullable.NewNullableWithValue(*name)
	}

	// set the relative path
	if path, err := vo.NewRelativeFilePath(fullRelativePath); err == nil {
		asset.Path = nullable.NewNullableWithValue(*path)
	}

	// Assets are always global/shared - never set company ID

	// save asset to database first
	id, err := im.Asset.AssetRepository.Insert(g, asset)
	if err != nil {
		return false, err
	}

	// build the file system path - assets are always stored in shared folder
	contextFolder := "shared"

	// ensure base asset directory exists
	if err := os.MkdirAll(im.Asset.RootFolder, 0755); err != nil {
		return false, err
	}

	// ensure shared directory exists
	fullContextPath := filepath.Join(im.Asset.RootFolder, contextFolder)
	if err := os.MkdirAll(fullContextPath, 0755); err != nil {
		return false, err
	}

	// create root filesystem for the full context path (controlled paths only)
	contextRoot, err := os.OpenRoot(fullContextPath)
	if err != nil {
		return false, err
	}
	defer contextRoot.Close()

	// validate file path within context
	_, err = contextRoot.Stat(fullRelativePath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	// Upload the file content directly using secure method
	contentBuffer := bytes.NewBuffer(content)
	err = im.File.UploadFile(contextRoot, strings.Trim(fullRelativePath, "/"), contentBuffer, true)
	if err != nil {
		// Clean up the database entry if file upload fails
		im.Asset.AssetRepository.DeleteByID(g, id)
		return false, err
	}

	return true, nil
}
