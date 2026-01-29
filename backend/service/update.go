package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-errors/errors"
	"golang.org/x/mod/semver"

	"github.com/phishingclub/phishingclub/build"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/version"
)

type Update struct {
	Common
	OptionService *Option
	updateMutex   sync.Mutex
}

// GitHubRelease represents a GitHub release response
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// UpdateDetails represents update information
type UpdateDetails struct {
	LatestVersion string `json:"latestVersion"`
	DownloadURL   string `json:"downloadUrl"`
	Message       string `json:"message"`
}

// CheckForUpdate returns if an update is ready and if installation
// supports the update to be performed from the application
func (u *Update) CheckForUpdate(
	ctx context.Context,
	session *model.Session,
) (bool, bool, error) {
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		u.LogAuthError(err)
		return false, false, errs.Wrap(err)
	}
	if !isAuthorized {
		// skip audit logging on this endpoint
		return false, false, errors.New("unauthorized")
	}

	// Check GitHub for latest release
	updateAvailable, err := u.checkGitHubForUpdate()
	if err != nil {
		return false, false, errs.Wrap(err)
	}

	// Check if using systemd (for update capability)
	usingSystemd, err := u.OptionService.GetOption(ctx, session, data.OptionKeyUsingSystemd)
	if err != nil {
		return false, false, errs.Wrap(err)
	}

	return updateAvailable, usingSystemd.Value.String() == data.OptionValueUsingSystemdYes, nil
}

// CheckForUpdateCached returns if an update is ready based on cached data
// and if installation supports the update to be performed from the application
func (u *Update) CheckForUpdateCached(
	ctx context.Context,
	session *model.Session,
) (bool, bool, error) {
	// check permissions
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		u.LogAuthError(err)
		return false, false, errs.Wrap(err)
	}
	if !isAuthorized {
		// skip audit logging on this endpoint
		return false, false, errors.New("unauthorized")
	}

	// Get cached update availability
	updateAvailable := cache.IsUpdateAvailable()

	// Check if using systemd (for update capability)
	usingSystemd, err := u.OptionService.GetOption(ctx, session, data.OptionKeyUsingSystemd)
	if err != nil {
		return false, false, errs.Wrap(err)
	}

	return updateAvailable, usingSystemd.Value.String() == data.OptionValueUsingSystemdYes, nil
}

// checkGitHubForUpdate checks GitHub releases API for newer version
func (u *Update) checkGitHubForUpdate() (bool, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	if !build.Flags.Production {
		customTransport := &http.Transport{
			TLSClientConfig: &tls.Config{
				// #nosec
				InsecureSkipVerify: true,
			},
		}
		client.Transport = customTransport
	}

	req, err := http.NewRequest(
		http.MethodGet,
		"https://api.github.com/repos/phishingclub/phishingclub/releases/latest",
		nil,
	)
	if err != nil {
		return false, errs.Wrap(err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "PhishingClub-Client")

	resp, err := client.Do(req)
	if err != nil {
		return false, errs.Wrap(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, errors.New("unexpected response from GitHub API")
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return false, errs.Wrap(err)
	}

	currentVersion := version.GetSemver()
	latestVersion := release.TagName

	// Compare versions
	isNewer, err := u.CheckUpdateVersion(currentVersion, latestVersion)
	if err != nil {
		u.Logger.Errorw("version comparison failed", "error", err)
		return false, nil // Don't fail on version comparison error
	}

	// Cache the update availability
	cache.SetUpdateAvailable(isNewer)

	u.Logger.Debugw("update check completed",
		"current", currentVersion,
		"latest", latestVersion,
		"updateAvailable", isNewer)

	return isNewer, nil
}

func (u *Update) GetUpdateDetails(
	ctx context.Context,
	session *model.Session,
) (*UpdateDetails, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		u.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		// skip audit logging on this endpoint
		return nil, errors.New("unauthorized")
	}

	// Get latest release info from GitHub
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	if !build.Flags.Production {
		customTransport := &http.Transport{
			TLSClientConfig: &tls.Config{
				// #nosec
				InsecureSkipVerify: true,
			},
		}
		client.Transport = customTransport
	}

	req, err := http.NewRequest(
		http.MethodGet,
		"https://api.github.com/repos/phishingclub/phishingclub/releases/latest",
		nil,
	)
	if err != nil {
		return nil, errs.Wrap(err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "PhishingClub-Client")

	resp, err := client.Do(req)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("unexpected response from GitHub API")
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, errs.Wrap(err)
	}

	currentVersion := version.GetSemver()
	isNewer, _ := u.CheckUpdateVersion(currentVersion, release.TagName)

	if !isNewer {
		return nil, errs.ErrNoUpdateAvailable
	}

	// detect current system architecture
	arch := runtime.GOARCH
	expectedFilename := fmt.Sprintf("_linux_%s.tar.gz", arch)

	// find the binary asset matching current architecture
	var downloadURL string
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, expectedFilename) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return nil, fmt.Errorf("no downloadable binary found for architecture %s in latest release", arch)
	}

	return &UpdateDetails{
		LatestVersion: release.TagName,
		DownloadURL:   downloadURL,
		Message:       "Update available from GitHub releases",
	}, nil
}

// RunUpdate runs a software update
func (u *Update) RunUpdate(
	ctx context.Context,
	session *model.Session,
) error {
	// Prevent concurrent updates
	u.updateMutex.Lock()
	defer u.updateMutex.Unlock()

	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		u.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		// skip audit logging on this endpoint
		return errors.New("unauthorized")
	}

	// Get update details
	details, err := u.GetUpdateDetails(ctx, session)
	if err != nil {
		u.Logger.Errorw("failed to get update details", "error", err)
		return errs.Wrap(err)
	}

	// Download release from GitHub
	req, err := http.NewRequest(
		http.MethodGet,
		details.DownloadURL,
		nil,
	)
	if err != nil {
		u.Logger.Errorw("failed to create download request", "error", err)
		return errs.Wrap(err)
	}

	req.Header.Set("User-Agent", "PhishingClub-Client")

	client := &http.Client{
		Timeout: 3 * time.Minute, // extended timeout for downloads
	}

	if !build.Flags.Production {
		customTransport := &http.Transport{
			TLSClientConfig: &tls.Config{
				// #nosec
				InsecureSkipVerify: true,
			},
		}
		client.Transport = customTransport
	}

	resp, err := client.Do(req)
	if err != nil {
		u.Logger.Errorw("failed to download update", "error", err)
		return errs.Wrap(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		u.Logger.Errorw("unexpected response from downloading update", "statusCode", resp.StatusCode)
		return errors.New("unexpected response from GitHub")
	}

	currentVersion := version.GetSemver()
	// Check that the version is greater than the current
	// to protect against downgrade attacks
	isNewer, err := u.CheckUpdateVersion(currentVersion, details.LatestVersion)
	if err != nil {
		u.Logger.Errorf("version comparison failed", "error", err)
		// You might still want to proceed with the update
	} else if !isNewer {
		u.Logger.Infow("update is not newer than current version",
			"current", currentVersion,
			"latest", details.LatestVersion)
		return errors.New("update version is not newer than current")
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		u.Logger.Errorw("failed to get current executable path", "error", err)
		return errs.Wrap(err)
	}
	execDir := filepath.Dir(execPath)

	// Get original permissions
	fileInfo, err := os.Stat(execPath)
	if err != nil {
		u.Logger.Errorw("failed to get original file permissions", "error", err)
		return errs.Wrap(err)
	}
	originalMode := fileInfo.Mode()

	// Create temp directory in same filesystem as executable
	tmpDir, err := os.MkdirTemp(execDir, ".update-*")
	if err != nil {
		return errs.Wrap(err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract and verify the update package
	binaryPath, err := u.extractAndVerifyPackage(resp.Body, tmpDir)
	if err != nil {
		u.Logger.Errorw("failed to verify update package", "error", err)
		return errs.Wrap(err)
	}

	// if not production, we stop the upgrade process here
	if !build.Flags.Production {
		u.Logger.Infow("update verification successful (development mode - not installing)")
		return nil
	}

	// Create backup with atomic rename (same filesystem)
	backupPath := execPath + ".bak"
	if err := os.Rename(execPath, backupPath); err != nil {
		return errs.Wrap(err)
	}

	// Move new binary with atomic rename (same filesystem)
	if err := os.Rename(binaryPath, execPath); err != nil {
		// Restore from backup on failure
		os.Rename(backupPath, execPath)
		return errs.Wrap(err)
	}

	// After rename, set the same permissions as the original executable
	if err := os.Chmod(execPath, originalMode); err != nil {
		u.Logger.Errorw("failed to set original permissions", "error", err)
		return errs.Wrap(err)
	}

	u.Logger.Infow("update completed successfully", "version", details.LatestVersion)

	// Schedule shutdown after a brief delay to allow HTTP response to be sent
	go func() {
		time.Sleep(1 * time.Second)
		u.Logger.Infow("initiating shutdown after update")
		pid := os.Getpid()
		if process, err := os.FindProcess(pid); err == nil {
			process.Signal(syscall.SIGTERM)
		}
	}()

	return nil
}

// extractAndVerifyPackage extracts binary and signature from tar.gz and verifies the signature
func (u *Update) extractAndVerifyPackage(packageData io.Reader, tmpDir string) (string, error) {
	// Read the entire package
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, packageData); err != nil {
		return "", errs.Wrap(err)
	}

	// Create gzip reader
	gzipReader, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return "", errs.Wrap(err)
	}
	defer gzipReader.Close()

	// Create tar reader
	tarReader := tar.NewReader(gzipReader)

	var binaryPath, sigPath string

	// Extract files from archive
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errs.Wrap(err)
		}

		// Skip directories
		if header.Typeflag != tar.TypeReg {
			continue
		}

		// Get the base filename
		fileName := filepath.Base(header.Name)
		outputPath := filepath.Join(tmpDir, fileName)

		// Create output file
		outFile, err := os.Create(outputPath)
		if err != nil {
			return "", errs.Wrap(err)
		}

		// Copy file content
		if _, err := io.Copy(outFile, tarReader); err != nil {
			outFile.Close()
			return "", errs.Wrap(err)
		}
		outFile.Close()

		// Save paths based on file extension
		if filepath.Ext(fileName) == ".sig" {
			sigPath = outputPath
		} else if fileName == "phishingclub" {
			binaryPath = outputPath
		}
	}

	// Ensure we have both binary and signature
	if binaryPath == "" || sigPath == "" {
		return "", errors.New("update package is incomplete: missing binary or signature")
	}

	// Verify the signature
	if err := u.verifySignature(binaryPath, sigPath); err != nil {
		return "", errs.Wrap(err)
	}

	return binaryPath, nil
}

// verifySignature verifies binary using Ed25519
func (u *Update) verifySignature(binaryPath, sigPath string) error {
	// Load binary data
	binaryData, err := os.ReadFile(binaryPath)
	if err != nil {
		return errs.Wrap(err)
	}

	// Load signature
	signature, err := os.ReadFile(sigPath)
	if err != nil {
		return errs.Wrap(err)
	}

	// Verify using Ed25519
	if !ed25519.Verify(embedded.SigningKey1, binaryData, signature) {
		u.Logger.Infow("failed to verify update - trying backup")
		if !ed25519.Verify(embedded.SigningKey2, binaryData, signature) {
			return errors.New("signature verification failed")
		}
	}

	return nil
}

func (u *Update) CheckUpdateVersion(currentVersion, latestVersion string) (bool, error) {
	if !build.Flags.Production {
		// ignroe version in development
		return false, nil
	}
	// The semver package expects versions to be prefixed with 'v'
	current := ensureVPrefix(currentVersion)
	latest := ensureVPrefix(latestVersion)

	// Validate versions
	if !semver.IsValid(current) {
		u.Logger.Errorw("invalid current version format", "version", currentVersion)
		return false, errs.Wrap(errors.New("invalid current version format"))
	}

	if !semver.IsValid(latest) {
		u.Logger.Errorw("invalid latest version format", "version", latestVersion)
		return false, errs.Wrap(errors.New("invalid latest version format"))
	}

	// Compare - returns > 0 if latest is greater
	return semver.Compare(latest, current) > 0, nil
}

// ensureVPrefix ensures the version string has a 'v' prefix
func ensureVPrefix(version string) string {
	if !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}
