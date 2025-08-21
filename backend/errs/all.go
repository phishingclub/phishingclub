package errs

import (
	goerrors "errors"

	"github.com/go-errors/errors"
)

// errors and messages
var (
	// db
	ErrDBSeedFailure = goerrors.New("failed to seed db")

	// install
	ErrAlreadyInstalled = goerrors.New("already installed")

	// auth and permissions
	ErrAuthenticationFailed = goerrors.New("authentication failed")
	ErrAuthorizationFailed  = goerrors.New("authorization error")

	// mapping
	ErrMappingDBToEntityFailed = goerrors.New("failed to map db to entity")

	// audit
	ErrAuditFailedToSave = goerrors.New("failed to save audit")

	// user
	ErrUserWrongPasword      = goerrors.New("wrong password")
	ErrUserWrongTOTP         = goerrors.New("incorrect code")
	ErrUserWrongRecoveryCode = goerrors.New("incorrect recovery code")

	// session
	ErrSessionCookieNotFound = goerrors.New("session cookie not found")

	// campaign
	ErrCampaignAlreadySetToClose = goerrors.New("campaign already set to closed")
	ErrCampaignAlreadyClosed     = goerrors.New("campaign already closed")
	ErrCampaignAlreadyAnonymized = goerrors.New("campaign already anonymized")

	// validation err
	ErrValidationFailed = goerrors.New("validation error")

	// license
	ErrLicenseMismatchSignature = goerrors.New("signature does not match")
	ErrLicenseExpired           = goerrors.New("expired")
	ErrLicenseEditionMismatch   = goerrors.New("edition does not match subscription")
	ErrLicenseNotValid          = goerrors.New("license is not valid")
	ErrLicenseRequestFailed     = goerrors.New("license request failed")
	ErrLicenseInvalidKey        = goerrors.New("invalid license key")

	// update
	ErrNoUpdateAvailable = goerrors.New("no update available")

	// sso
	ErrSSODisabled = goerrors.New("SSO disabled")
)

// format messages
const (
	MsgPasswordRenewRequired     = "New password required"
	MsgFailedToParseRequest      = "failed to parse request"
	MsgFailedToParseUUID         = "failed to parse uuid"
	MsgfFailedToParseCompanyUUID = "failed to parse company uuid: %s"
	MsgfFailedToMakeName         = "failed to make name: %s"
	MsgfFailedToParseTypeID      = "failed to parse message type uuid: %s"
	MsgfInvalidOffsetOrLimit     = "invalid offset or limit: %s"
)

func Wrap(err error) error {
	if err == nil {
		return nil
	}
	// we only wrap an error once
	if _, ok := err.(*errors.Error); ok {
		return err
	}
	return errors.Wrap(err, 0)
}

// ValidationError is a validation error
type ValidationError struct {
	Err error
}

// NewValidationError creates a new validation error
func NewValidationError(err error) error {
	return ValidationError{
		Err: err,
	}
}

// Error returns the validation error
func (e ValidationError) Error() string {
	return e.Err.Error()
}

// CustomError is a custom error
type CustomError struct {
	Err error
}

// NewCustomError creates a new custom error
// it is used when a custom error message should be
// returned to the consumer
func NewCustomError(err error) error {
	return CustomError{
		Err: err,
	}
}

// Error returns the custom error
func (e CustomError) Error() string {
	return e.Err.Error()
}
