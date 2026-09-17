package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// Branding is a service for install wide UI branding. It stores uploaded PNG
// images on disk and tracks whether the login side image has been hidden.
type Branding struct {
	Common
	RootFolder       string
	OptionRepository *repository.Option
	FileService      *File
}

// BrandingDisplay is how an image is fitted within its area on screen.
type BrandingDisplay struct {
	Fit        string `json:"fit"`
	Scale      int    `json:"scale"`
	Background string `json:"background"`
	PositionX  string `json:"positionX"`
	PositionY  string `json:"positionY"`
}

// BrandingState is the branding state returned to the frontend. Each slot mode
// is 'default' or 'custom'. The login side image can additionally be hidden,
// which is independent of whether a custom image is stored. Display holds the
// per slot fit settings keyed by slot.
type BrandingState struct {
	HeaderLogo           string                     `json:"headerLogo"`
	LoginLogo            string                     `json:"loginLogo"`
	LoginSideImage       string                     `json:"loginSideImage"`
	LoginSideImageHidden bool                       `json:"loginSideImageHidden"`
	Display              map[string]BrandingDisplay `json:"display"`
}

// GetState returns the branding state. There is no authorization check as the
// login screen reads this before a user is authenticated.
func (b *Branding) GetState(ctx context.Context) (*BrandingState, error) {
	hidden, err := b.isSideImageRemoved(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	display, err := b.getDisplayMap(ctx)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &BrandingState{
		HeaderLogo:           b.slotMode(data.BrandingSlotHeaderLogo),
		LoginLogo:            b.slotMode(data.BrandingSlotLoginLogo),
		LoginSideImage:       b.slotMode(data.BrandingSlotLoginSideImage),
		LoginSideImageHidden: hidden,
		Display:              display,
	}, nil
}

// defaultDisplay is the fit settings for a slot when none are stored. Logos
// show whole (contain); the side image fills its area (cover).
func defaultDisplay(slot string) BrandingDisplay {
	fit := data.BrandingFitContain
	if slot == data.BrandingSlotLoginSideImage {
		fit = data.BrandingFitCover
	}
	// the login logo sits top left like the original; other slots center
	posX, posY := "center", "center"
	if slot == data.BrandingSlotLoginLogo {
		posX, posY = "left", "top"
	}
	return BrandingDisplay{
		Fit:        fit,
		Scale:      data.BrandingScaleDefault,
		Background: data.BrandingBackgroundNone,
		PositionX:  posX,
		PositionY:  posY,
	}
}

// getDisplayMap returns the stored display settings for every slot, filling in
// defaults for any slot or field that is unset.
func (b *Branding) getDisplayMap(ctx context.Context) (map[string]BrandingDisplay, error) {
	stored := map[string]BrandingDisplay{}
	opt, err := b.OptionRepository.GetByKey(ctx, data.OptionKeyBrandingDisplay)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(err)
	}
	if err == nil && opt.Value.String() != "" {
		if uErr := json.Unmarshal([]byte(opt.Value.String()), &stored); uErr != nil {
			b.Logger.Errorw("failed to parse branding display settings", "error", uErr)
			stored = map[string]BrandingDisplay{}
		}
	}
	out := map[string]BrandingDisplay{}
	for slot := range data.BrandingSlotFilename {
		out[slot] = withDisplayDefaults(slot, stored[slot])
	}
	return out, nil
}

// withDisplayDefaults fills any empty or out of range field with its default.
func withDisplayDefaults(slot string, d BrandingDisplay) BrandingDisplay {
	def := defaultDisplay(slot)
	if !data.BrandingFits[d.Fit] {
		d.Fit = def.Fit
	}
	if !data.BrandingBackgrounds[d.Background] {
		d.Background = def.Background
	}
	if !data.BrandingPositionsX[d.PositionX] {
		d.PositionX = def.PositionX
	}
	if !data.BrandingPositionsY[d.PositionY] {
		d.PositionY = def.PositionY
	}
	if d.Scale < data.BrandingScaleMin || d.Scale > data.BrandingScaleMax {
		d.Scale = def.Scale
	}
	return d
}

// SetDisplay stores the display settings for a slot.
func (b *Branding) SetDisplay(
	ctx context.Context,
	session *model.Session,
	slot string,
	display BrandingDisplay,
) error {
	ae := NewAuditEvent("Branding.SetDisplay", session)
	ae.Details["slot"] = slot
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		b.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		b.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if _, ok := data.BrandingSlotFilename[slot]; !ok {
		return errs.NewValidationError(fmt.Errorf("unknown branding slot"))
	}
	// clamp and default any invalid field so a bad value can not break rendering
	display = withDisplayDefaults(slot, display)

	stored := map[string]BrandingDisplay{}
	opt, getErr := b.OptionRepository.GetByKey(ctx, data.OptionKeyBrandingDisplay)
	if getErr != nil && !errors.Is(getErr, gorm.ErrRecordNotFound) {
		return errs.Wrap(getErr)
	}
	if getErr == nil && opt.Value.String() != "" {
		if uErr := json.Unmarshal([]byte(opt.Value.String()), &stored); uErr != nil {
			// corrupt value, start fresh rather than fail; log so it is visible
			b.Logger.Errorw("failed to parse branding display settings", "error", uErr)
			stored = map[string]BrandingDisplay{}
		}
	}
	stored[slot] = display
	blob, err := json.Marshal(stored)
	if err != nil {
		return errs.Wrap(err)
	}
	if err := b.upsertOption(ctx, data.OptionKeyBrandingDisplay, string(blob)); err != nil {
		return errs.Wrap(err)
	}
	b.AuditLogAuthorized(ae)
	return nil
}

// GetImage returns the uploaded PNG bytes for a slot. The found flag is false
// when no custom image is stored, so the caller falls back to the built in
// default. There is no authorization check as branding images are shown on the
// pre login screen.
func (b *Branding) GetImage(slot string) ([]byte, bool, error) {
	filename, ok := data.BrandingSlotFilename[slot]
	if !ok {
		return nil, false, nil
	}
	content, err := os.ReadFile(filepath.Join(b.RootFolder, filename))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		b.Logger.Errorw("failed to read branding image", "slot", slot, "error", err)
		return nil, false, errs.Wrap(err)
	}
	return content, true, nil
}

// SetImage validates and stores an uploaded PNG for a slot.
func (b *Branding) SetImage(
	ctx context.Context,
	session *model.Session,
	slot string,
	content []byte,
) error {
	ae := NewAuditEvent("Branding.SetImage", session)
	ae.Details["slot"] = slot
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		b.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		b.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	filename, ok := data.BrandingSlotFilename[slot]
	if !ok {
		return errs.NewValidationError(fmt.Errorf("unknown branding slot"))
	}
	if err := validatePNG(content); err != nil {
		return err
	}
	// write through an os.Root sandbox using the shared file service, the same
	// safe write path the asset and attachment uploads use
	if err := os.MkdirAll(b.RootFolder, 0755); err != nil {
		b.Logger.Errorw("failed to create branding folder", "error", err)
		return errs.Wrap(err)
	}
	root, err := os.OpenRoot(b.RootFolder)
	if err != nil {
		b.Logger.Errorw("failed to open branding folder", "error", err)
		return errs.Wrap(err)
	}
	defer root.Close()
	if err := b.FileService.UploadFile(root, filename, bytes.NewBuffer(content), true); err != nil {
		b.Logger.Errorw("failed to write branding image", "slot", slot, "error", err)
		return errs.Wrap(err)
	}
	// uploading a side image clears any previous hidden state
	if slot == data.BrandingSlotLoginSideImage {
		if err := b.setSideImageRemoved(ctx, false); err != nil {
			return errs.Wrap(err)
		}
	}
	b.AuditLogAuthorized(ae)
	return nil
}

// Reset removes any uploaded image for a slot, returning it to the built in
// default. For the login side image it also clears the hidden state.
func (b *Branding) Reset(
	ctx context.Context,
	session *model.Session,
	slot string,
) error {
	ae := NewAuditEvent("Branding.Reset", session)
	ae.Details["slot"] = slot
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		b.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		b.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	filename, ok := data.BrandingSlotFilename[slot]
	if !ok {
		return errs.NewValidationError(fmt.Errorf("unknown branding slot"))
	}
	if err := os.Remove(filepath.Join(b.RootFolder, filename)); err != nil && !os.IsNotExist(err) {
		b.Logger.Errorw("failed to remove branding image", "slot", slot, "error", err)
		return errs.Wrap(err)
	}
	if slot == data.BrandingSlotLoginSideImage {
		if err := b.setSideImageRemoved(ctx, false); err != nil {
			return errs.Wrap(err)
		}
	}
	b.AuditLogAuthorized(ae)
	return nil
}

// SetSideImageHidden shows or hides the login side image. Hiding centers the
// login form. Any uploaded side image is kept so showing it again restores the
// custom image; use Reset to remove the uploaded image entirely.
func (b *Branding) SetSideImageHidden(
	ctx context.Context,
	session *model.Session,
	hidden bool,
) error {
	ae := NewAuditEvent("Branding.SetSideImageHidden", session)
	ae.Details["hidden"] = hidden
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		b.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		b.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	if err := b.setSideImageRemoved(ctx, hidden); err != nil {
		return errs.Wrap(err)
	}
	b.AuditLogAuthorized(ae)
	return nil
}

// slotMode resolves whether a slot has a custom uploaded image.
func (b *Branding) slotMode(slot string) string {
	if b.hasCustomImage(slot) {
		return data.BrandingModeCustom
	}
	return data.BrandingModeDefault
}

// hasCustomImage reports whether an uploaded image exists for the slot.
func (b *Branding) hasCustomImage(slot string) bool {
	filename, ok := data.BrandingSlotFilename[slot]
	if !ok {
		return false
	}
	_, err := os.Stat(filepath.Join(b.RootFolder, filename))
	return err == nil
}

// isSideImageRemoved reads the hidden flag for the login side image.
func (b *Branding) isSideImageRemoved(ctx context.Context) (bool, error) {
	opt, err := b.OptionRepository.GetByKey(ctx, data.OptionKeyBrandingLoginSideImageRemoved)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, errs.Wrap(err)
	}
	return opt.Value.String() == "true", nil
}

// setSideImageRemoved upserts the hidden flag for the login side image.
func (b *Branding) setSideImageRemoved(ctx context.Context, removed bool) error {
	value := "false"
	if removed {
		value = "true"
	}
	return b.upsertOption(ctx, data.OptionKeyBrandingLoginSideImageRemoved, value)
}

// upsertOption inserts or updates a single option row by key.
func (b *Branding) upsertOption(ctx context.Context, key string, value string) error {
	valueVO, err := vo.NewOptionalString1MB(value)
	if err != nil {
		return errs.NewValidationError(err)
	}
	opt := &model.Option{
		Key:   *vo.NewString127Must(key),
		Value: *valueVO,
	}
	_, getErr := b.OptionRepository.GetByKey(ctx, key)
	if getErr != nil {
		if !errors.Is(getErr, gorm.ErrRecordNotFound) {
			return errs.Wrap(getErr)
		}
		if _, insertErr := b.OptionRepository.Insert(ctx, opt); insertErr != nil {
			return errs.Wrap(insertErr)
		}
		return nil
	}
	if updateErr := b.OptionRepository.UpdateByKey(ctx, opt); updateErr != nil {
		return errs.Wrap(updateErr)
	}
	return nil
}

// validatePNG rejects anything that is not a small, sane PNG. This is the only
// upload type gate in the branding path, so it is strict: it checks the sniffed
// content type, decodes the image to prove it is a real PNG and not a polyglot,
// and bounds the dimensions to stop decompression bombs.
func validatePNG(content []byte) error {
	if len(content) == 0 {
		return errs.NewValidationError(fmt.Errorf("file is empty"))
	}
	if len(content) > data.BrandingMaxUploadBytes {
		return errs.NewValidationError(fmt.Errorf("file is too large"))
	}
	sniffLen := len(content)
	if sniffLen > 512 {
		sniffLen = 512
	}
	if http.DetectContentType(content[:sniffLen]) != "image/png" {
		return errs.NewValidationError(fmt.Errorf("file is not a png"))
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return errs.NewValidationError(fmt.Errorf("file is not a valid png"))
	}
	if cfg.Width <= 0 || cfg.Height <= 0 ||
		cfg.Width > data.BrandingMaxImageDimension ||
		cfg.Height > data.BrandingMaxImageDimension {
		return errs.NewValidationError(fmt.Errorf("image dimensions are out of bounds"))
	}
	if _, err := png.Decode(bytes.NewReader(content)); err != nil {
		return errs.NewValidationError(fmt.Errorf("file is not a valid png"))
	}
	return nil
}
