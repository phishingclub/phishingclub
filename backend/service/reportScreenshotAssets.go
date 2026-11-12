package service

import (
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"mime"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/remotebrowser"
	"github.com/phishingclub/phishingclub/repository"
)

// reportAssetMaxSize is the largest asset file a report capture loads.
const reportAssetMaxSize = 10 << 20

// reportAssets answers the requests a report capture makes to Phishing Club
// domains, so asset images, styles and fonts show in the screenshot without
// any request reaching the live server.
type reportAssets struct {
	// rootFolder is the asset folder, holding one folder per domain name and
	// the shared folder for global assets
	rootFolder string
	// local holds every Phishing Club domain name, lower case. Requests to
	// these hosts are always answered here.
	local map[string]bool
	// served maps the lower case names of the domains whose files may be used
	// to their folder name. Other local domains get 404.
	served map[string]string
}

// newReportAssets builds the lookup from every domain. Files are served for the
// global domains and the domains of companyID, the same domains a campaign of
// that company can use.
func newReportAssets(rootFolder string, domains []repository.DomainNameCompany, companyID *uuid.UUID) *reportAssets {
	r := &reportAssets{
		rootFolder: rootFolder,
		local:      map[string]bool{},
		served:     map[string]string{},
	}
	for _, d := range domains {
		name := strings.ToLower(d.Name)
		r.local[name] = true
		if d.CompanyID == nil || (companyID != nil && *d.CompanyID == *companyID) {
			r.served[name] = d.Name
		}
	}
	return r
}

// isLocalHost implements remotebrowser.CaptureOptions.LocalHost.
func (r *reportAssets) isLocalHost(host string) bool {
	return r.local[strings.TrimSuffix(strings.ToLower(host), ".")]
}

// lookup implements remotebrowser.CaptureOptions.Local.
func (r *reportAssets) lookup(u *url.URL) (*remotebrowser.LocalFile, bool) {
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, false
	}
	// a trailing dot names the same host
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if !r.local[host] {
		return nil, false
	}
	folder, ok := r.served[host]
	if !ok {
		return nil, true
	}
	// the same cleaning the public server does, any ".." is resolved against
	// the root so the path can not leave it
	clean := strings.TrimPrefix(path.Clean("/"+u.Path), "/")
	if clean == "" {
		return nil, true
	}
	root, err := os.OpenRoot(r.rootFolder)
	if err != nil {
		return nil, true
	}
	defer root.Close()
	// a domain file wins over a shared file at the same path, as on the
	// public server
	for _, rel := range []string{path.Join(folder, clean), path.Join(data.ASSET_GLOBAL_FOLDER, clean)} {
		if body, ok := readReportAsset(root, rel); ok {
			return &remotebrowser.LocalFile{Body: body, ContentType: contentTypeFor(clean)}, true
		}
	}
	return nil, true
}

// readReportAsset reads a regular file through root, up to reportAssetMaxSize.
func readReportAsset(root *os.Root, rel string) ([]byte, bool) {
	f, err := root.Open(rel)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > reportAssetMaxSize {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(f, reportAssetMaxSize))
	if err != nil {
		return nil, false
	}
	return body, true
}

func contentTypeFor(name string) string {
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

var (
	baseTagPattern = regexp.MustCompile(`(?i)<base[\s>/]`)
	headTagPattern = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
)

// withBaseURL makes relative links in a page resolve against base, as they do
// when the page is served from its campaign URL. A page with its own base tag
// is left as it is.
func withBaseURL(content string, base string) string {
	if baseTagPattern.MatchString(content) {
		return content
	}
	tag := `<base href="` + html.EscapeString(base) + `">`
	if loc := headTagPattern.FindStringIndex(content); loc != nil {
		return content[:loc[1]] + tag + content[loc[1]:]
	}
	return tag + content
}

// withInlineImages replaces each cid: reference to an inline attachment with
// the attachment itself as a data URI, as a mail client shows it.
func withInlineImages(content string, images map[string][]byte) string {
	// longest name first, so a name that starts with another name is
	// replaced whole
	names := make([]string, 0, len(images))
	for name := range images {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for _, name := range names {
		body := images[name]
		uri := "data:" + contentTypeFor(name) + ";base64," + base64.StdEncoding.EncodeToString(body)
		content = strings.ReplaceAll(content, "cid:"+name, uri)
	}
	return content
}

// readFileLimited reads a regular file of at most max bytes.
func readFileLimited(name string, max int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, fmt.Errorf("not a regular file of at most %d bytes", max)
	}
	return io.ReadAll(io.LimitReader(f, max))
}
