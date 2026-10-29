package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/registry"
	"github.com/phishingclub/phishingclub/safepath"
	"github.com/phishingclub/phishingclub/vo"
)

// createAttachment stores a package attachment in the global attachment folder.
// The stored file name is what recipients see and what an inline cid: reference
// names, so it is kept as declared. The bytes are never written over an existing
// file: when a file on disk or another global attachment row already uses the
// name, the install fails with a clear error. Each attachment this creates
// therefore owns its file, so uninstall deletes only its own bytes.
func (s *ContentRegistry) createAttachment(
	ctx context.Context,
	session *model.Session,
	name, fileName, description string,
	embedded, calendar bool,
	body []byte,
) (*uuid.UUID, error) {
	m := &model.Attachment{}
	if n, e := vo.NewOptionalString127(name); e == nil {
		m.Name = nn(*n)
	}
	if description != "" {
		if d, e := vo.NewOptionalString255(description); e == nil {
			m.Description = nn(*d)
		}
	}
	fn, err := vo.NewFileName(strings.Trim(fileName, "/"))
	if err != nil {
		return nil, errs.NewValidationError(err)
	}
	m.FileName = nn(*fn)
	m.EmbeddedContent = nn(embedded)
	m.SendAsCalendar = nn(calendar)
	full := filepath.Join(s.Attachment.RootFolder, data.ASSET_GLOBAL_FOLDER)
	if mkerr := os.MkdirAll(full, 0o755); mkerr != nil {
		return nil, errs.Wrap(mkerr)
	}
	root, oerr := os.OpenRoot(full)
	if oerr != nil {
		return nil, errs.Wrap(oerr)
	}
	defer root.Close()
	used, err := s.attachmentFileNameTaken(ctx, root, fn.String())
	if err != nil {
		return nil, err
	}
	if used {
		return nil, attachmentConflictErr(name, fn.String())
	}
	// Path is a read side value computed from the context folder and file name; the
	// attachments table has no path column, so it is not set here.
	id, err := s.Attachment.AttachmentRepository.Insert(ctx, m)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if uerr := s.Attachment.FileService.UploadFile(root, fn.String(), bytes.NewBuffer(body), false); uerr != nil {
		// the file was not written, so remove only the row.
		_ = s.Attachment.AttachmentRepository.DeleteByID(ctx, id)
		var ve errs.ValidationError
		if errors.As(uerr, &ve) {
			return nil, attachmentConflictErr(name, fn.String())
		}
		return nil, errs.Wrap(uerr)
	}
	return id, nil
}

// attachmentFileNameTaken reports whether a file in the global attachment folder
// or a global attachment row already uses the file name.
func (s *ContentRegistry) attachmentFileNameTaken(ctx context.Context, root *os.Root, fileName string) (bool, error) {
	if _, err := root.Stat(fileName); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, errs.Wrap(err)
	}
	n, err := s.Attachment.AttachmentRepository.CountGlobalByFileName(ctx, fileName)
	if err != nil {
		return false, errs.Wrap(err)
	}
	return n > 0, nil
}

func attachmentConflictErr(name, fileName string) error {
	return errs.NewValidationError(fmt.Errorf(
		"attachment %q: the file name %q is already used by an existing attachment. "+
			"Rename or delete that attachment, then try again",
		name, fileName,
	))
}

// checkAssetRel refuses a package asset path unless it is strict and the asset
// path value object keeps it unchanged, so the same string is used for the
// lookup, the database row, the file on disk and the provenance row.
func checkAssetRel(rel string) error {
	if err := safepath.Check(rel); err != nil {
		return errs.NewValidationError(fmt.Errorf("invalid asset path: %w", err))
	}
	p, err := vo.NewRelativeFilePath(rel)
	if err != nil || p.String() != rel {
		return errs.NewValidationError(fmt.Errorf("invalid asset path %q", rel))
	}
	return nil
}

// installAsset stores one bundled asset as a global asset (no company and no
// domain). When a global asset already sits at the path it is reused only if
// another installed package owns it and the file holds the same bytes, so the
// asset is shared and ref counted. An asset the user added, or one with other
// bytes, is never adopted and fails with a conflict error. Returns the asset id
// and whether this call created it.
func (s *ContentRegistry) installAsset(
	ctx context.Context, session *model.Session, ipID *uuid.UUID, rel string, content []byte,
) (*uuid.UUID, bool, error) {
	if err := checkAssetRel(rel); err != nil {
		return nil, false, err
	}
	existing, err := s.Asset.AssetRepository.GetGlobalByPath(ctx, rel)
	if err != nil {
		return nil, false, errs.Wrap(err)
	}
	if existing != nil {
		if !existing.ID.IsSpecified() || existing.ID.IsNull() {
			return nil, false, errs.Wrap(fmt.Errorf("asset at %q has no id", rel))
		}
		id := existing.ID.MustGet()
		shared, cerr := s.assetOwnedByOtherInstall(ctx, &id, ipID)
		if cerr != nil {
			return nil, false, errs.Wrap(cerr)
		}
		if !shared {
			return nil, false, assetConflictErr([]string{rel})
		}
		if err := s.checkSharedAssetBytes(ctx, &id, ipID, rel, content); err != nil {
			return nil, false, err
		}
		return &id, false, nil
	}
	asset := &model.Asset{}
	if name, err := vo.NewOptionalString127(filepath.Base(rel)); err == nil {
		asset.Name = nullable.NewNullableWithValue(*name)
	}
	// the path is required: without it the row cannot be found or deleted later, so a
	// bad path fails the install instead of inserting a row and orphaning the file.
	path, err := vo.NewRelativeFilePath(rel)
	if err != nil {
		return nil, false, errs.NewValidationError(fmt.Errorf("invalid asset path %q: %w", rel, err))
	}
	asset.Path = nullable.NewNullableWithValue(*path)
	id, err := s.Asset.AssetRepository.Insert(ctx, asset)
	if err != nil {
		return nil, false, errs.Wrap(err)
	}
	// never write over a file already on disk; it belongs to someone else.
	if err := s.writeAssetFile(rel, content, false); err != nil {
		_ = s.Asset.AssetRepository.DeleteByID(ctx, id)
		return nil, false, errs.NewValidationError(fmt.Errorf("asset %q: %w", rel, err))
	}
	return id, true, nil
}

// assetOwnedByOtherInstall reports whether an installed package other than ipID
// has a provenance row for the asset. ipID may be nil before the install row
// exists.
func (s *ContentRegistry) assetOwnedByOtherInstall(ctx context.Context, assetID, ipID *uuid.UUID) (bool, error) {
	exclude := ipID
	if exclude == nil {
		none := uuid.Nil
		exclude = &none
	}
	n, err := s.RegistryRepository.CountInstalledResourcesByResource(ctx, assetID, exclude)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// checkSharedAssetBytes refuses to share the global asset at rel unless its file
// holds exactly content. Another package wrote that file, so a package that ships
// other bytes must not take it over, and must not record a hash for bytes it did
// not write. A missing file counts as other bytes.
func (s *ContentRegistry) checkSharedAssetBytes(
	ctx context.Context, assetID, ipID *uuid.UUID, rel string, content []byte,
) error {
	onDisk, err := s.readAssetFile(rel)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errs.Wrap(err)
	}
	if err == nil && bytes.Equal(onDisk, content) {
		return nil
	}
	owner, err := s.assetOwnerLabel(ctx, assetID, ipID)
	if err != nil {
		return errs.Wrap(err)
	}
	return errs.NewValidationError(fmt.Errorf(
		"asset path conflict: %q is already installed by %s with different content. "+
			"Uninstall that package, then try again",
		rel, owner,
	))
}

// sharedAssetChangeErr refuses an update that ships new bytes for an asset this
// install shares with another installed package.
func (s *ContentRegistry) sharedAssetChangeErr(ctx context.Context, assetID, ipID *uuid.UUID, rel string) error {
	owner, err := s.assetOwnerLabel(ctx, assetID, ipID)
	if err != nil {
		return errs.Wrap(err)
	}
	return errs.NewValidationError(fmt.Errorf(
		"asset path conflict: %q is shared with %s and this version ships different content. "+
			"Uninstall that package, then try again",
		rel, owner,
	))
}

// assetOwnerLabel names an installed package other than ipID that owns the
// asset, as package and Bait Box, for an error message.
func (s *ContentRegistry) assetOwnerLabel(ctx context.Context, assetID, ipID *uuid.UUID) (string, error) {
	rows, err := s.RegistryRepository.GetInstalledResourcesByResource(ctx, "asset", assetID)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if ipID != nil && *r.InstalledPackageID == *ipID {
			continue
		}
		ip, gerr := s.RegistryRepository.GetInstalledPackageByID(ctx, r.InstalledPackageID)
		if gerr != nil {
			continue
		}
		label := fmt.Sprintf("package %q", ip.PackageID)
		if reg, rerr := s.RegistryRepository.GetRegistryByID(ctx, ip.RegistryID); rerr == nil && reg != nil {
			label += fmt.Sprintf(" from Bait Box %q", reg.Name)
		}
		return label, nil
	}
	return "another installed package", nil
}

// assetConflictErr reports global assets the user added at paths a package
// wants to install to. The package never takes over such a file.
func assetConflictErr(paths []string) error {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = fmt.Sprintf("%q", p)
	}
	return errs.NewValidationError(fmt.Errorf(
		"asset path conflict: %s already exists as a global asset not managed by an installed "+
			"package. Rename or delete the existing asset, then try again",
		strings.Join(quoted, ", "),
	))
}

// assetPathConflicts lists the asset paths across pkgs where a global asset
// exists that no installed package owns. Install checks this before it creates
// anything so the user sees every conflicting path at once.
func (s *ContentRegistry) assetPathConflicts(ctx context.Context, pkgs []*registry.VerifiedPackage) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, pkg := range pkgs {
		for p := range pkg.Files {
			if !strings.HasPrefix(p, "assets/") {
				continue
			}
			rel := strings.TrimPrefix(p, "assets/")
			if rel == "" || seen[rel] {
				continue
			}
			if perr := checkAssetRel(rel); perr != nil {
				return nil, perr
			}
			seen[rel] = true
			existing, err := s.Asset.AssetRepository.GetGlobalByPath(ctx, rel)
			if err != nil {
				return nil, err
			}
			if existing == nil || !existing.ID.IsSpecified() || existing.ID.IsNull() {
				continue
			}
			id := existing.ID.MustGet()
			shared, err := s.assetOwnedByOtherInstall(ctx, &id, nil)
			if err != nil {
				return nil, err
			}
			if !shared {
				out = append(out, rel)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// writeAssetFile writes the asset bytes into the shared asset directory, confined
// by os.Root so the relative path cannot escape. With overwrite false an existing
// file fails the write.
func (s *ContentRegistry) writeAssetFile(rel string, content []byte, overwrite bool) error {
	if err := os.MkdirAll(s.Asset.RootFolder, 0o755); err != nil {
		return err
	}
	fullContextPath := filepath.Join(s.Asset.RootFolder, "shared")
	if err := os.MkdirAll(fullContextPath, 0o755); err != nil {
		return err
	}
	contextRoot, err := os.OpenRoot(fullContextPath)
	if err != nil {
		return err
	}
	defer contextRoot.Close()
	return s.File.UploadFile(contextRoot, strings.Trim(rel, "/"), bytes.NewBuffer(content), overwrite)
}

// attachmentDecl is one attachment a package declares whose file is present.
// name is the declared name, the key update uses to link a tracked attachment.
type attachmentDecl struct {
	name     string
	fileName string
}

// installAttachmentDecls lists every attachment the packages declare, nested
// under an email or standalone, whose file the package carries.
func installAttachmentDecls(pkgs []*registry.VerifiedPackage) []attachmentDecl {
	var out []attachmentDecl
	for _, pkg := range pkgs {
		docs, err := contentDocs(pkg)
		if err != nil {
			continue
		}
		for _, doc := range docs {
			add := func(at installAttachmentYAML) {
				if _, ok := pkg.Files[doc.prefix+at.File]; ok {
					out = append(out, attachmentDecl{name: strings.TrimSpace(at.Name), fileName: at.File})
				}
			}
			for _, e := range doc.dy.Emails {
				for _, at := range e.Attachments {
					add(at)
				}
			}
			for _, at := range doc.dy.Attachments {
				add(at)
			}
		}
	}
	return out
}

// attachmentNameConflicts lists the file names among decls that an existing
// global attachment or file already uses, or that two of the decls would both
// create. A decl whose name is in tracked is linked, not created, and so is
// skipped. With linkSameName set, a later decl with a name seen before links the
// attachment the first one creates, as update does; install creates one
// attachment per decl. A file name that is not valid is left to the create,
// which reports it.
func (s *ContentRegistry) attachmentNameConflicts(
	ctx context.Context, decls []attachmentDecl, tracked map[string]bool, linkSameName bool,
) ([]string, error) {
	if s.Attachment == nil || len(decls) == 0 {
		return nil, nil
	}
	var root *os.Root
	full := filepath.Join(s.Attachment.RootFolder, data.ASSET_GLOBAL_FOLDER)
	if r, err := os.OpenRoot(full); err == nil {
		root = r
		defer root.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errs.Wrap(err)
	}
	seenName := map[string]bool{}
	seenFile := map[string]bool{}
	conflict := map[string]bool{}
	for _, d := range decls {
		if tracked[d.name] {
			continue
		}
		if linkSameName && seenName[d.name] {
			continue
		}
		seenName[d.name] = true
		fn, err := vo.NewFileName(strings.Trim(d.fileName, "/"))
		if err != nil {
			continue
		}
		f := fn.String()
		if seenFile[f] {
			conflict[f] = true
			continue
		}
		seenFile[f] = true
		var used bool
		if root != nil {
			used, err = s.attachmentFileNameTaken(ctx, root, f)
			if err != nil {
				return nil, err
			}
		} else {
			n, cerr := s.Attachment.AttachmentRepository.CountGlobalByFileName(ctx, f)
			if cerr != nil {
				return nil, errs.Wrap(cerr)
			}
			used = n > 0
		}
		if used {
			conflict[f] = true
		}
	}
	out := make([]string, 0, len(conflict))
	for f := range conflict {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}

func attachmentNamesConflictErr(fileNames []string) error {
	quoted := make([]string, len(fileNames))
	for i, f := range fileNames {
		quoted[i] = fmt.Sprintf("%q", f)
	}
	return errs.NewValidationError(fmt.Errorf(
		"attachment file name conflict: %s already used by an existing attachment or by another "+
			"attachment in this package. Rename or delete the existing attachment, then try again",
		strings.Join(quoted, ", "),
	))
}

// attachEmailAttachments gives an email the update created the attachments its
// declaration nests under it, with each link's inline flag. An attachment this
// install already tracks under the same declared name is linked rather than
// created again, so the fresh copy of an email (the "both" resolution) shares the
// file with the local copy. Each created attachment and provenance row pushes its
// reverse onto undo.
func (s *ContentRegistry) attachEmailAttachments(
	ctx context.Context,
	session *model.Session,
	ipID *uuid.UUID,
	pkg *registry.VerifiedPackage,
	prefix string,
	emailID *uuid.UUID,
	atts []installAttachmentYAML,
	vars map[string]string,
	boundVars string,
	tracked map[string]*database.InstalledResource,
	result *InstallResult,
	undo *updateUndo,
) error {
	for _, at := range atts {
		body, ok := pkg.Files[prefix+at.File]
		if !ok {
			result.Skipped = append(result.Skipped, "attachment "+at.Name+": file missing")
			continue
		}
		if r, isTracked := tracked[strings.TrimSpace(at.Name)]; isTracked {
			if err := s.EmailRepository.AddAttachment(ctx, emailID, r.ResourceID, at.Inline); err != nil {
				return errs.Wrap(err)
			}
			continue
		}
		atID, err := s.createAttachment(
			ctx, session, substitute(at.Name, vars), at.File, substitute(at.Description, vars),
			at.Embedded, at.Calendar, body,
		)
		if err != nil {
			return err
		}
		aid := atID
		undo.pushErr(func() error { return s.deleteResourceEntity(ctx, session, "attachment", aid) })
		err = s.recordResourceTracked(
			ctx, ipID, "attachment", at.Name, atID, contentHash(string(body)), boundVars, undo, nil,
		)
		if err != nil {
			return errs.Wrap(err)
		}
		if err := s.EmailRepository.AddAttachment(ctx, emailID, atID, at.Inline); err != nil {
			return errs.Wrap(err)
		}
		tracked[strings.TrimSpace(at.Name)] = &database.InstalledResource{
			ResourceType: "attachment", ResourceID: atID, SourceName: at.Name,
		}
		result.Others = append(result.Others, "attachment "+at.Name)
	}
	return nil
}

// readAssetFile reads the bytes of a shared asset, confined by os.Root the same
// way writeAssetFile writes them.
func (s *ContentRegistry) readAssetFile(rel string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Join(s.Asset.RootFolder, "shared"))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(strings.Trim(rel, "/"))
}
