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

	OptionKeyDisplayMode           = "display_mode"
	OptionValueDisplayModeWhitebox = "whitebox"
	OptionValueDisplayModeBlackbox = "blackbox"

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
