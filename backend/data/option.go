package data

const (
	OptionKeyIsInstalled      = "is_installed"
	OptionValueIsInstalled    = "true"
	OptionValueIsNotInstalled = "false"
	// KeyIsInstalled is the key for the is_installed option
	OptionKeyInstanceID = "instance_id"

	OptionKeyLogLevel   = "log_level"
	OptionKeyDBLogLevel = "db_log_level"

	OptionKeyUsingSystemd      = "systemd_install"
	OptionValueUsingSystemdYes = "true"
	OptionValueUsingSystemdNo  = "false"

	OptionKeyDevelopmentSeeded = "development_seeded"
	OptionValueSeeded          = "true"

	OptionKeyMaxFileUploadSizeMB             = "max_file_upload_size_mb"
	OptionValueKeyMaxFileUploadSizeMBDefault = "100"

	OptionKeyRepeatOffenderMonths = "repeat_offender_months"

	OptionKeyAdminSSOLogin = "sso_login"

	OptionKeyProxyCookieName = "proxy_cookie_name"

	// OptionKeyRemoteBrowserWSPath is the seeded random path segment used for the
	// victim-facing remote browser WebSocket endpoint. Randomised at first startup
	// so the endpoint is not fingerprinted by path alone.
	OptionKeyRemoteBrowserWSPath = "remote_browser_ws_path"

	OptionKeyDisplayMode           = "display_mode"
	OptionValueDisplayModeWhitebox = "whitebox"
	OptionValueDisplayModeBlackbox = "blackbox"

	OptionKeyAutoPruneOrphanedRecipients = "auto_prune_orphaned_recipients"

	OptionKeyReportPDFEnabled = "report_pdf_enabled"

	// OptionKeyScimDomain is the single global domain on which SCIM provisioning
	// endpoints are served by the phishing server. Empty disables SCIM serving.
	OptionKeyScimDomain = "scim_domain"

	OptionKeyObfuscationTemplate = "obfuscation_template"
	// OptionValueObfuscationTemplateDefault is the default HTML template for obfuscation
	// the template receives {{.Script}} variable containing the obfuscated javascript
	OptionValueObfuscationTemplateDefault = `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body>
<script>{{.Script}}</script>
</body>
</html>`
)
