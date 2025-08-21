package app

import (
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/cli"
	"github.com/phishingclub/phishingclub/password"
)

// Utilities is a collection of utils
type Utilities struct {
	CLIOutputter        cli.Outputter
	PasswordHasher      *password.Argon2Hasher
	JSONResponseHandler api.JSONResponseHandler
}

// NewUtils creates a collection of utils
func NewUtils() *Utilities {
	return &Utilities{
		CLIOutputter:        cli.NewCLIOutputter(),
		PasswordHasher:      password.NewHasherWithDefaultValues(),
		JSONResponseHandler: api.NewJSONResponseHandler(),
	}
}
