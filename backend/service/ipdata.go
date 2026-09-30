package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/build"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/ipdata"
	"github.com/phishingclub/phishingclub/model"
)

const (
	// ipdataBaseURL is the fixed location of the latest data packages. It takes
	// no user input, so it adds no request forgery surface.
	ipdataBaseURL = "https://github.com/phishingclub/ipdata/releases/latest/download"

	// ipdataMaxDownload caps a package download. The packages are a few MB, so
	// this only stops a runaway response.
	ipdataMaxDownload = 64 << 20

	// ipdataMaxFile caps a single extracted file.
	ipdataMaxFile = 128 << 20
)

// IPData manages the downloadable country and ASN data packages.
type IPData struct {
	Common
	Store *ipdata.Store
	// mu serializes install and remove so two operators cannot race on the
	// package directory
	mu sync.Mutex
}

// manifestPackage mirrors one package entry in the ipdata manifest.
type manifestPackage struct {
	File         string `json:"file"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	ContentHash  string `json:"content_hash"`
	Entries      int    `json:"entries"`
	IPv4Prefixes int    `json:"ipv4_prefixes"`
	IPv6Prefixes int    `json:"ipv6_prefixes"`
}

// manifest mirrors the ipdata manifest.json.
type manifest struct {
	Format   int                        `json:"format"`
	Version  string                     `json:"version"`
	Created  string                     `json:"created"`
	Packages map[string]manifestPackage `json:"packages"`
}

// PackageStatus is the state of one package for the settings screen.
type PackageStatus struct {
	Kind            string       `json:"kind"`
	Installed       bool         `json:"installed"`
	Info            *ipdata.Info `json:"info,omitempty"`
	UpdateAvailable bool         `json:"updateAvailable"`
	LatestVersion   string       `json:"latestVersion"`
	LatestCreated   string       `json:"latestCreated"`
}

func validKind(kind string) bool {
	return kind == ipdata.KindGeoIP || kind == ipdata.KindASN
}

func (s *IPData) httpClient(timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}
	if !build.Flags.Production {
		client.Transport = &http.Transport{
			// #nosec
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	return client
}

// fetchManifest downloads and parses the current manifest.
func (s *IPData) fetchManifest() (*manifest, error) {
	req, err := http.NewRequest(http.MethodGet, ipdataBaseURL+"/manifest.json", nil)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	req.Header.Set("User-Agent", "PhishingClub-Client")
	resp, err := s.httpClient(20 * time.Second).Do(req)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("unexpected response fetching ipdata manifest")
	}
	var m manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return nil, errs.Wrap(err)
	}
	return &m, nil
}

// Status returns the state of both packages, including whether a newer version
// is available. The remote check is best effort and never fails the call.
func (s *IPData) Status(
	ctx context.Context,
	session *model.Session,
) ([]PackageStatus, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		return nil, errors.New("unauthorized")
	}

	installed := s.Store.Status()
	var remote *manifest
	if m, err := s.fetchManifest(); err == nil {
		remote = m
	} else {
		s.Logger.Debugw("could not fetch ipdata manifest", "error", err)
	}

	out := make([]PackageStatus, 0, 2)
	for _, kind := range []string{ipdata.KindGeoIP, ipdata.KindASN} {
		ps := PackageStatus{Kind: kind}
		if info, ok := installed[kind]; ok {
			infoCopy := info
			ps.Installed = info.Downloaded
			ps.Info = &infoCopy
		}
		if remote != nil {
			if rp, ok := remote.Packages[kind]; ok {
				ps.LatestVersion = remote.Version
				ps.LatestCreated = remote.Created
				current := ""
				if ps.Info != nil {
					current = ps.Info.ContentHash
				}
				ps.UpdateAvailable = current != rp.ContentHash
			}
		}
		out = append(out, ps)
	}
	return out, nil
}

// Download fetches, verifies and installs the package of the given kind, then
// reloads it into the running store without a restart.
func (s *IPData) Download(
	ctx context.Context,
	session *model.Session,
	kind string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ae := NewAuditEvent("IPData.Download", session)
	ae.Details["kind"] = kind

	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errors.New("unauthorized")
	}
	if !validKind(kind) {
		return errs.NewValidationError(fmt.Errorf("unknown package kind: %s", kind))
	}

	m, err := s.fetchManifest()
	if err != nil {
		return errs.NewOperationalError(
			"Could not reach the data server. The data packages may not be published yet, or this server has no outbound internet access.",
			err,
		)
	}
	pkg, ok := m.Packages[kind]
	if !ok {
		return errs.NewOperationalError(
			fmt.Sprintf("The %s package is not available for download yet.", kind),
			fmt.Errorf("manifest has no package %q", kind),
		)
	}

	// download into the ipdata directory so the final rename stays on one
	// filesystem
	root := s.Store.DataRoot()
	if err := os.MkdirAll(root, 0o750); err != nil {
		return errs.Wrap(err)
	}
	tmpArchive, err := os.CreateTemp(root, ".dl-*.tar.gz")
	if err != nil {
		return errs.Wrap(err)
	}
	tmpArchivePath := tmpArchive.Name()
	defer os.Remove(tmpArchivePath)

	// build the URL from the fixed kind, not from a manifest field, so no part
	// of the path is influenced by the fetched manifest
	url := ipdataBaseURL + "/" + kind + ".tar.gz"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		tmpArchive.Close()
		return errs.Wrap(err)
	}
	req.Header.Set("User-Agent", "PhishingClub-Client")
	resp, err := s.httpClient(3 * time.Minute).Do(req)
	if err != nil {
		tmpArchive.Close()
		return errs.NewOperationalError("Could not reach the data server to download the package.", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmpArchive.Close()
		return errs.NewOperationalError(
			"The data server did not return the package.",
			fmt.Errorf("http %d downloading %s", resp.StatusCode, pkg.File),
		)
	}

	// hash while copying, cap the size
	hasher := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmpArchive, hasher), io.LimitReader(resp.Body, ipdataMaxDownload+1))
	tmpArchive.Close()
	if err != nil {
		return errs.NewOperationalError("The package download was interrupted.", err)
	}
	if n > ipdataMaxDownload {
		return errs.NewOperationalError("The package is larger than the allowed size.", nil)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != pkg.SHA256 {
		return errs.NewOperationalError(
			"The downloaded package failed its integrity check.",
			fmt.Errorf("sha256 mismatch for %s: want %s got %s", kind, pkg.SHA256, got),
		)
	}

	// extract into a temp directory, then validate before moving into place
	tmpDir, err := os.MkdirTemp(root, ".extract-*")
	if err != nil {
		return errs.Wrap(err)
	}
	defer os.RemoveAll(tmpDir)
	if err := extractPackage(tmpArchivePath, tmpDir); err != nil {
		return errs.NewOperationalError("The downloaded package could not be read.", err)
	}
	if err := ipdata.Validate(tmpDir, kind); err != nil {
		return errs.NewOperationalError("The downloaded package is not valid.", err)
	}

	// swap into the final location
	finalDir := s.Store.PackageDir(kind)
	oldDir := finalDir + ".old"
	_ = os.RemoveAll(oldDir)
	if _, err := os.Stat(finalDir); err == nil {
		if err := os.Rename(finalDir, oldDir); err != nil {
			return errs.Wrap(err)
		}
	}
	if err := os.Rename(tmpDir, finalDir); err != nil {
		// try to restore the previous package
		_ = os.Rename(oldDir, finalDir)
		return errs.Wrap(err)
	}
	_ = os.RemoveAll(oldDir)

	s.Store.Reload(kind)
	ae.Details["version"] = pkg.ContentHash
	s.AuditLogAuthorized(ae)
	return nil
}

// Remove deletes the installed package of the given kind and reloads the
// store. Removing geoip falls back to the embedded data, removing asn turns
// ASN lookups off.
func (s *IPData) Remove(
	ctx context.Context,
	session *model.Session,
	kind string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ae := NewAuditEvent("IPData.Remove", session)
	ae.Details["kind"] = kind

	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		s.AuditLogNotAuthorized(ae)
		return errors.New("unauthorized")
	}
	if !validKind(kind) {
		return errs.NewValidationError(fmt.Errorf("unknown package kind: %s", kind))
	}

	if err := os.RemoveAll(s.Store.PackageDir(kind)); err != nil {
		return errs.Wrap(err)
	}
	s.Store.Reload(kind)
	s.AuditLogAuthorized(ae)
	return nil
}

// extractPackage unpacks package.json and entries.json from a gzip tar into
// dir. It reads only those two files by base name and rejects anything else.
func extractPackage(archivePath, dir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	allowed := map[string]bool{"package.json": true, "entries.json": true}
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.Base(hdr.Name)
		if !allowed[name] {
			continue
		}
		out, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, ipdataMaxFile)); err != nil {
			out.Close()
			return err
		}
		out.Close()
		seen[name] = true
	}
	if !seen["package.json"] || !seen["entries.json"] {
		return errors.New("package archive is missing package.json or entries.json")
	}
	return nil
}
