package service

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
)

// Browse lists the packages a registry offers, with the colors for their tags.
func (s *ContentRegistry) Browse(
	ctx context.Context, session *model.Session, registryID *uuid.UUID,
) (*registry.IndexResult, error) {
	if _, err := s.authorize(session, "Registry.Browse"); err != nil {
		return nil, err
	}
	reg, err := s.RegistryRepository.GetRegistryByID(ctx, registryID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	client := s.clientFor(reg)
	defer client.CloseIdle()
	// probe first under the short timeout, so a registry that is down fails fast
	// with a clear reason instead of waiting on the full client timeout.
	info, perr := s.testConnection(ctx, client)
	if perr != nil {
		return nil, connectError(reg.URL, perr)
	}
	if err := s.checkRegistryCompatible(info); err != nil {
		return nil, err
	}
	res, err := client.Index(ctx)
	if err != nil {
		return nil, registryError("could not list the Bait Box packages", err)
	}
	return res, nil
}

// PreviewImage proxies a registry preview image so the browser can load it same
// origin (its CSP blocks the registry origin). absURL must be a preview url from
// the registry index, constrained to this registry's base. Returns bytes + type.
func (s *ContentRegistry) PreviewImage(
	ctx context.Context, session *model.Session, registryID *uuid.UUID, absURL string,
) ([]byte, string, error) {
	if _, err := s.authorize(session, "Registry.Browse"); err != nil {
		return nil, "", err
	}
	reg, err := s.RegistryRepository.GetRegistryByID(ctx, registryID)
	if err != nil {
		return nil, "", errs.Wrap(err)
	}
	client := s.clientFor(reg)
	defer client.CloseIdle()
	b, ct, err := client.FetchPreviewImage(ctx, absURL)
	if err != nil {
		return nil, "", registryError("could not load preview", err)
	}
	return b, ct, nil
}

// InspectFile is one file in a package, decoded for display: text content for a
// text file, a data uri for a small image, or just the size for anything else.
type InspectFile struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Size    int    `json:"size"`
	Text    string `json:"text,omitempty"`
	DataURI string `json:"dataUri,omitempty"`
}

// InspectAttachment is one email attachment declared in a package data.yaml,
// surfaced so the consumer can see an email's attachments as attachments rather
// than as loose files. File is the attachment's path within the archive so the
// view can match it to the file's thumbnail and size.
type InspectAttachment struct {
	Name        string `json:"name"`
	File        string `json:"file"`
	Description string `json:"description,omitempty"`
	Embedded    bool   `json:"embedded"`
	Inline      bool   `json:"inline"`
	Calendar    bool   `json:"calendar"`
	EmailName   string `json:"emailName,omitempty"`
}

// InspectVariable is a declared variable with the key and scope an install of the
// package reads its value under, as MissingVariable has them.
type InspectVariable struct {
	registry.Variable
	Scope string `json:"scope,omitempty"`
	Key   string `json:"key"`
}

// InspectResult is the full verified contents of a package, shown before
// installing.
type InspectResult struct {
	Namespace    string                `json:"namespace"`
	Name         string                `json:"name"`
	Title        string                `json:"title"`
	Type         string                `json:"type"`
	Version      string                `json:"version"`
	Description  string                `json:"description"`
	Variables    []InspectVariable     `json:"variables"`
	Dependencies []registry.Dependency `json:"dependencies"`
	// BrokenDependencies lists the referenced package ids that no longer resolve on
	// the registry (for example a referenced package was renamed or deleted). A
	// bundle with any of these cannot be installed, so the view disables install and
	// says why instead of letting the install fail with a raw error.
	BrokenDependencies []string `json:"brokenDependencies,omitempty"`
	// RefusedDependencies lists the referenced package ids the registry refused to
	// read with this token. They may exist, so they are kept apart from
	// BrokenDependencies, and RefusedMessage carries the token refused message.
	RefusedDependencies []string            `json:"refusedDependencies,omitempty"`
	RefusedMessage      string              `json:"refusedMessage,omitempty"`
	Attachments         []InspectAttachment `json:"attachments"`
	Files               []InspectFile       `json:"files"`
}

// errReferencesUnchecked is the inspect error when a referenced package lookup
// was skipped without an error of its own.
var errReferencesUnchecked = errors.New("the referenced packages could not be checked")

// maxInlineImageBytes caps the image a preview inlines as a data uri, so a huge
// bundled image does not bloat the inspect response.
const maxInlineImageBytes = 512 * 1024

// Inspect fetches and verifies a package at the registry current version and
// returns its full contents for display. It runs the same verification as an
// install, so a corrupt package is rejected rather than shown.
func (s *ContentRegistry) Inspect(
	ctx context.Context, session *model.Session, registryID *uuid.UUID, namespace, name string,
) (*InspectResult, error) {
	if _, err := s.authorize(session, "Registry.Browse"); err != nil {
		return nil, err
	}
	reg, err := s.RegistryRepository.GetRegistryByID(ctx, registryID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	client := s.clientFor(reg)
	defer client.CloseIdle()
	pkg, vr, err := client.FetchVerified(ctx, namespace, name)
	if err != nil {
		return nil, registryError("could not inspect package", err)
	}
	m := pkg.Manifest
	out := &InspectResult{
		Namespace:    namespace,
		Name:         name,
		Title:        m.Title,
		Type:         m.Type,
		Version:      vr.Version,
		Description:  m.Description,
		Variables:    make([]InspectVariable, 0, len(m.Variables)),
		Dependencies: m.Dependencies,
		Files:        make([]InspectFile, 0, len(pkg.Files)),
	}
	share := installedShare(nil, m)
	for _, v := range m.Variables {
		scope, key := varScopeKey(m.ID, v.Name, share)
		out.Variables = append(out.Variables, InspectVariable{Variable: v, Scope: scope, Key: key})
	}
	// a bundle references other packages by address. flag any that no longer resolve
	// (renamed or deleted at the registry) so the view can refuse the install with a
	// reason rather than failing when the install tries to fetch them. GetVersion is
	// the lightweight resolve the install uses, so this matches what install sees.
	// each lookup runs under a short timeout, a few at a time, so a slow registry
	// cannot hold the inspect request open for long.
	depIDs := make([]string, len(m.Dependencies))
	for i, d := range m.Dependencies {
		depIDs[i] = d.ID
	}
	resolved := s.resolveVersions(ctx, client, depIDs)
	// only a 404 marks a reference as gone. Any other failure, or a lookup skipped
	// after the Bait Box stopped answering, fails the inspect instead.
	var checkErr error
	unchecked := false
	for _, d := range m.Dependencies {
		lk := resolved[d.ID]
		var se *registry.StatusError
		switch {
		case lk.vr != nil:
		case errors.Is(lk.err, registry.ErrUnauthorized):
			out.RefusedDependencies = append(out.RefusedDependencies, d.ID)
			out.RefusedMessage = tokenRefusedMessage
		case errors.As(lk.err, &se) && se.Code == http.StatusNotFound:
			out.BrokenDependencies = append(out.BrokenDependencies, d.ID)
		default:
			unchecked = true
			if checkErr == nil && lk.err != nil {
				checkErr = lk.err
			}
		}
	}
	if unchecked {
		if checkErr == nil {
			checkErr = errReferencesUnchecked
		}
		return nil, registryError("could not inspect package", checkErr)
	}
	paths := make([]string, 0, len(pkg.Files))
	for p := range pkg.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		b := pkg.Files[p]
		f := InspectFile{Path: p, Size: len(b), Kind: inspectKind(p)}
		switch f.Kind {
		case "image":
			if len(b) <= maxInlineImageBytes {
				f.DataURI = "data:" + mimeForExt(p) + ";base64," + base64.StdEncoding.EncodeToString(b)
			}
		case "binary":
			// size only
		default:
			f.Text = string(b)
		}
		out.Files = append(out.Files, f)
	}
	// surface email attachments from each content folder's data.yaml so the view
	// shows them as attachments. the file path resolves relative to its folder so
	// it matches the file entry carrying the thumbnail and size. a parse failure
	// is ignored here: the file list still shows everything.
	if docs, derr := contentDocs(pkg); derr == nil {
		for _, d := range docs {
			add := func(a installAttachmentYAML, emailName string) {
				if a.File == "" {
					return
				}
				out.Attachments = append(out.Attachments, InspectAttachment{
					Name:        a.Name,
					File:        d.prefix + a.File,
					Description: a.Description,
					Embedded:    a.Embedded,
					Inline:      a.Inline,
					Calendar:    a.Calendar,
					EmailName:   emailName,
				})
			}
			for _, e := range d.dy.Emails {
				for _, a := range e.Attachments {
					add(a, e.Name)
				}
			}
			for _, a := range d.dy.Attachments {
				add(a, "")
			}
		}
	}
	return out, nil
}

// inspectKind classifies a file for display: text shown as code, image shown as a
// thumbnail, everything else as an opaque binary.
func inspectKind(p string) string {
	ext := strings.ToLower(path.Ext(p))
	switch ext {
	case ".html", ".htm", ".js", ".css", ".yaml", ".yml", ".json", ".txt", ".svg", ".md":
		return "text"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".bmp":
		return "image"
	default:
		return "binary"
	}
}

// mimeForExt returns a content type for a bundled image by extension, for the
// data uri the inspect view inlines.
func mimeForExt(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	case ".bmp":
		return "image/bmp"
	default:
		return "application/octet-stream"
	}
}
