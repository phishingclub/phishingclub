package sso

import (
	"fmt"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/confidential"
	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
)

func NewEntreIDClient(sso *model.SSOOption) (*confidential.Client, error) {
	if !sso.Enabled {
		return nil, errs.Wrap(errs.ErrSSODisabled)
	}
	clientID := sso.ClientID.String()
	tenantID := sso.TenantID.String()
	clientSecret := sso.ClientSecret.String()
	// Create credential from client secret
	cred, err := confidential.NewCredFromSecret(clientSecret)
	if err != nil {
		return nil, errs.Wrap(errors.Errorf("failed setup ENTRE ID credentials: %s", err))
	}
	url := fmt.Sprintf("https://login.microsoftonline.com/%s", tenantID)

	// Create the client
	client, err := confidential.New(
		url,
		clientID,
		cred,
	)
	if err != nil {
		return nil, errs.Wrap(errors.Errorf("failed setup ENTRE ID client: %w", err))
	}
	return &client, nil
}
