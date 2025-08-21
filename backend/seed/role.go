package seed

import (
	"context"

	"github.com/go-errors/errors"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

// SeedRoles seeds roles
func SeedRoles(roleRepository *repository.Role) error {
	roles := []struct {
		Name string
	}{
		{
			Name: data.RoleSuperAdministrator,
		},
		{
			Name: data.RoleCompanyUser,
		},
	}
	for _, role := range roles {
		id := uuid.New()
		createRole := model.Role{
			ID:   id,
			Name: role.Name,
		}
		r, err := roleRepository.GetByName(context.Background(), role.Name)
		// if error is not found, create event type
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
		if r != nil {
			continue
		}
		_, err = roleRepository.Insert(context.TODO(), &createRole)
		if err != nil {
			return errors.Errorf("%w: %s", errs.ErrDBSeedFailure, err)
		}
	}
	return nil
}
