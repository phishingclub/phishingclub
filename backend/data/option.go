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
)
