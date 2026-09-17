package data

const (
	// BrandingSlotHeaderLogo is the logo shown in the app header
	BrandingSlotHeaderLogo = "header-logo"
	// BrandingSlotLoginLogo is the logo shown on the login screen
	BrandingSlotLoginLogo = "login-logo"
	// BrandingSlotLoginSideImage is the image shown beside the login form
	BrandingSlotLoginSideImage = "login-side-image"

	// BrandingModeDefault means the built in asset is used
	BrandingModeDefault = "default"
	// BrandingModeCustom means an uploaded image is used
	BrandingModeCustom = "custom"

	// OptionKeyBrandingLoginSideImageRemoved marks the login side image as hidden
	OptionKeyBrandingLoginSideImageRemoved = "branding_login_side_image_removed"

	// OptionKeyBrandingDisplay holds the per slot display settings as a JSON map
	OptionKeyBrandingDisplay = "branding_display"

	// BrandingMaxUploadBytes is the largest accepted branding image
	BrandingMaxUploadBytes = 5 * 1024 * 1024
	// BrandingMaxImageDimension bounds width and height to stop decompression bombs
	BrandingMaxImageDimension = 4096

	// display fit modes, map to CSS object-fit
	BrandingFitContain = "contain"
	BrandingFitCover   = "cover"
	BrandingFitFill    = "fill"

	// display background behind the image
	BrandingBackgroundNone  = "none"
	BrandingBackgroundLight = "light"
	BrandingBackgroundDark  = "dark"

	// display scale is a percentage bound so a value can not break the layout
	BrandingScaleMin     = 25
	BrandingScaleMax     = 200
	BrandingScaleDefault = 100
)

// BrandingFits are the accepted fit modes
var BrandingFits = map[string]bool{
	BrandingFitContain: true,
	BrandingFitCover:   true,
	BrandingFitFill:    true,
}

// BrandingBackgrounds are the accepted background values
var BrandingBackgrounds = map[string]bool{
	BrandingBackgroundNone:  true,
	BrandingBackgroundLight: true,
	BrandingBackgroundDark:  true,
}

// BrandingPositionsX are the accepted horizontal positions
var BrandingPositionsX = map[string]bool{"left": true, "center": true, "right": true}

// BrandingPositionsY are the accepted vertical positions
var BrandingPositionsY = map[string]bool{"top": true, "center": true, "bottom": true}

// BrandingSlotFilename maps a branding slot to its on disk PNG filename. Only
// slots present here are accepted, so a request can not name an arbitrary path.
var BrandingSlotFilename = map[string]string{
	BrandingSlotHeaderLogo:     "header-logo.png",
	BrandingSlotLoginLogo:      "login-logo.png",
	BrandingSlotLoginSideImage: "login-side-image.png",
}
