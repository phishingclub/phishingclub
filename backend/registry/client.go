package registry

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxBodyBytes caps a JSON response read so a hostile registry cannot stream an
// unbounded body into memory. Archives use the archive guards instead.
const maxBodyBytes = 16 << 20

// ErrUnauthorized means the registry answered 401 or 403: it refused the read
// token. Callers map it to one message instead of a network failure.
var ErrUnauthorized = errors.New("the Bait Box refused the token")

// ErrUnexpectedAnswer means the registry answered 200 with a body that is not the
// JSON the client expects, for example an HTML page from a proxy.
var ErrUnexpectedAnswer = errors.New("the Bait Box sent an unexpected answer")

// StatusError is a non 200 answer other than 401 or 403.
type StatusError struct {
	What string
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("the Bait Box answered %s with HTTP %d", e.What, e.Code)
}

// Client talks to one baitbox registry. Token is the read token sent on every
// request; the insecure flag allows a self signed TLS cert (dev only, user opt in).
type Client struct {
	BaseURL string
	Token   string
	http    *http.Client
}

// NewClient builds a client for a registry base url
func NewClient(baseURL, token string, insecure bool) *Client {
	// idle connections close on their own after a short while, so a client used
	// for one probe does not keep sockets open
	tr := &http.Transport{
		IdleConnTimeout:     30 * time.Second,
		MaxIdleConnsPerHost: 4,
	}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 user opt in for dev registries
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		http: &http.Client{
			Timeout:   60 * time.Second,
			Transport: tr,
			// a registry never redirects. following one could send the token to
			// another host, so a 3xx is returned as is and fails as a non 200.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// statusError turns a non 200 answer into an error. 401 and 403 become
// ErrUnauthorized. The request path is left out so it never reaches the UI.
func statusError(what string, code int) error {
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return ErrUnauthorized
	}
	return &StatusError{What: what, Code: code}
}

// CloseIdle closes the idle connections the client keeps for reuse. Call it when
// the client is no longer needed.
func (c *Client) CloseIdle() {
	c.http.CloseIdleConnections()
}

func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return c.http.Do(req)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError("a lookup", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return ErrUnexpectedAnswer
	}
	return nil
}

// RegistryInfo is the registry metadata a consumer checks before using it.
// MinPhishingClubVersion is the lowest Phishing Club version the registry
// supports; an older consumer must not connect. Empty means no minimum.
type RegistryInfo struct {
	MinPhishingClubVersion string `json:"minPhishingClubVersion"`
}

// Info returns the registry's metadata, used to check version compatibility at
// connect time. A missing field reads as the zero value, treated as no minimum.
func (c *Client) Info(ctx context.Context) (*RegistryInfo, error) {
	var out RegistryInfo
	if err := c.getJSON(ctx, "/api/v1/info", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PackageSummary is one entry in the registry index
type PackageSummary struct {
	Namespace     string   `json:"namespace"`
	Name          string   `json:"name"`
	Title         string   `json:"title"`
	Type          string   `json:"type"`
	Description   string   `json:"description"`
	Visibility    string   `json:"visibility"`
	DownloadCount int64    `json:"downloadCount"`
	Version       string   `json:"version"`
	PreviewImages []string `json:"previewImages"`
	Tags          []string `json:"tags"`
}

// IndexResult is the registry package list plus the color the registry assigns to
// each tag name that appears on a visible package. Tag names ride along in the
// package manifest already; the colors are the registry operator's own choice.
type IndexResult struct {
	Packages  []PackageSummary
	TagColors map[string]string
}

// Index lists the packages the client can see, with the colors for their tags
func (c *Client) Index(ctx context.Context) (*IndexResult, error) {
	var out struct {
		Packages  []PackageSummary  `json:"packages"`
		TagColors map[string]string `json:"tagColors"`
	}
	if err := c.getJSON(ctx, "/api/v1/index", &out); err != nil {
		return nil, err
	}
	return &IndexResult{Packages: out.Packages, TagColors: out.TagColors}, nil
}

// VersionResponse is the version metadata the consumer reads: the version string
// and the hashes it checks the downloaded archive against.
type VersionResponse struct {
	Version     string `json:"version"`
	ContentHash string `json:"contentHash"`
	ArchiveSHA  string `json:"archiveSha"`
}

// GetVersion returns the metadata for the package's current version. The living
// library model always installs and updates to the latest version.
func (c *Client) GetVersion(ctx context.Context, namespace, name string) (*VersionResponse, error) {
	return c.getVersionAt(ctx, namespace, name, "latest")
}

// getVersionAt returns the metadata for one version of a package, or for the
// current one when version is "latest".
func (c *Client) getVersionAt(ctx context.Context, namespace, name, version string) (*VersionResponse, error) {
	var out VersionResponse
	path := "/api/v1/packages/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) + "/" + url.PathEscape(version)
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	if !ValidVersion(out.Version) {
		return nil, fmt.Errorf("the Bait Box returned an invalid version")
	}
	return &out, nil
}

// maxPreviewImageBytes caps a proxied preview image so a hostile registry cannot
// stream an unbounded body into memory.
const maxPreviewImageBytes = 8 << 20

// previewImageTypes are the raster types a proxied preview may have. SVG and
// every other type are refused, since the image is served from the admin origin
// and an active document there could run script.
var previewImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// CheckPreviewImage validates a preview image by its declared Content-Type and by
// sniffing its bytes. Both must be an allowed raster type. It returns the sniffed
// type, which is what the consumer serves.
func CheckPreviewImage(declared string, b []byte) (string, error) {
	mt, _, err := mime.ParseMediaType(declared)
	if err != nil || !previewImageTypes[strings.ToLower(mt)] {
		return "", fmt.Errorf("the preview is not a png, jpeg, gif or webp image")
	}
	sniffed := http.DetectContentType(b)
	if !previewImageTypes[sniffed] {
		return "", fmt.Errorf("the preview is not a png, jpeg, gif or webp image")
	}
	return sniffed, nil
}

// FetchPreviewImage fetches one preview image from the registry so the consumer
// can serve it same origin (the browser CSP blocks loading it cross origin from
// the registry). absURL must be under this registry's base url, so the proxy
// cannot be pointed at an arbitrary host. Returns the image bytes and media type.
func (c *Client) FetchPreviewImage(ctx context.Context, absURL string) ([]byte, string, error) {
	path := absURL
	if strings.HasPrefix(absURL, "http://") || strings.HasPrefix(absURL, "https://") {
		// an absolute url must be on this registry's own origin (same scheme and
		// host), so the proxy cannot be pointed at another host. a prefix test is not
		// enough: "https://reg.example.com.evil.com" has the base url as a prefix.
		u, err := url.Parse(absURL)
		if err != nil {
			return nil, "", fmt.Errorf("the preview URL is invalid")
		}
		base, berr := url.Parse(c.BaseURL)
		if berr != nil {
			return nil, "", fmt.Errorf("the Bait Box URL is invalid")
		}
		if u.Scheme != base.Scheme || u.Host != base.Host {
			return nil, "", fmt.Errorf("the preview URL is not on this Bait Box")
		}
		path = u.EscapedPath()
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	resp, err := c.get(ctx, path)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", statusError("a preview", resp.StatusCode)
	}
	declared := resp.Header.Get("Content-Type")
	if mt, _, perr := mime.ParseMediaType(declared); perr != nil || !previewImageTypes[strings.ToLower(mt)] {
		return nil, "", fmt.Errorf("the preview is not a png, jpeg, gif or webp image")
	}
	if resp.ContentLength > maxPreviewImageBytes {
		return nil, "", fmt.Errorf("the preview image is too large")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPreviewImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > maxPreviewImageBytes {
		return nil, "", fmt.Errorf("the preview image is too large")
	}
	ct, err := CheckPreviewImage(declared, b)
	if err != nil {
		return nil, "", err
	}
	return b, ct, nil
}

// download streams a version archive into memory under the archive size guard.
func (c *Client) download(ctx context.Context, namespace, name, version string) ([]byte, error) {
	path := "/api/v1/packages/" + url.PathEscape(namespace) + "/" + url.PathEscape(name) +
		"/" + url.PathEscape(version) + "/download"
	resp, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusError("a download", resp.StatusCode)
	}
	if resp.ContentLength > maxArchiveBytes {
		return nil, fmt.Errorf("the package archive is too large")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxArchiveBytes {
		return nil, fmt.Errorf("the package archive is too large")
	}
	return b, nil
}

// FetchVerified resolves the current version, downloads its archive, and verifies
// it against the archive sha256 and the manifest content and per file hashes. The
// returned package payload is safe to install.
func (c *Client) FetchVerified(
	ctx context.Context, namespace, name string,
) (*VerifiedPackage, *VersionResponse, error) {
	return c.fetchVerifiedAt(ctx, namespace, name, "latest")
}

// FetchVerifiedVersion downloads and verifies one exact version of a package, under
// the same checks as FetchVerified. It is used to read what an installed version
// declared.
func (c *Client) FetchVerifiedVersion(ctx context.Context, namespace, name, version string) (*VerifiedPackage, error) {
	if !ValidVersion(version) {
		return nil, fmt.Errorf("invalid version")
	}
	pkg, _, err := c.fetchVerifiedAt(ctx, namespace, name, version)
	return pkg, err
}

func (c *Client) fetchVerifiedAt(
	ctx context.Context, namespace, name, version string,
) (*VerifiedPackage, *VersionResponse, error) {
	vr, err := c.getVersionAt(ctx, namespace, name, version)
	if err != nil {
		return nil, nil, err
	}
	if version != "latest" && vr.Version != version {
		return nil, nil, fmt.Errorf("the Bait Box returned a different version than requested")
	}
	archive, err := c.download(ctx, namespace, name, vr.Version)
	if err != nil {
		return nil, nil, err
	}
	pkg, err := VerifyAndExtract(archive, vr.ArchiveSHA)
	if err != nil {
		return nil, nil, err
	}
	// the manifest inside the archive must match the id and version we asked for,
	// so a registry cannot swap a different package into the response.
	if pkg.Manifest.Namespace() != namespace ||
		pkg.Manifest.PackageName() != name ||
		pkg.Manifest.Version != vr.Version {
		return nil, nil, fmt.Errorf("archive manifest does not match requested package")
	}
	// the version endpoint content hash must equal the archive manifest hash, so the
	// two can never diverge (the update available check compares against this field).
	if normHex(vr.ContentHash) != normHex(pkg.Manifest.ContentHash) {
		return nil, nil, fmt.Errorf("version content hash does not match archive manifest")
	}
	return pkg, vr, nil
}
