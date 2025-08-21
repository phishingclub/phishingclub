package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
)

// Role is the Role repository
type Role struct {
	DB *gorm.DB
}

// GetByName gets a role by name
func (r *Role) GetByName(
	ctx context.Context,
	name string,
) (*model.Role, error) {
	var dbRole database.Role
	result := r.DB.Where("name = ?", name).First(&dbRole)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToRole(&dbRole), nil
}

// GetByID gets a role by id
func (r *Role) GetByID(
	ctx context.Context,
	id *uuid.UUID,
) (*model.Role, error) {
	var dbRole database.Role
	result := r.DB.
		Where("id = ?", id.String()).
		First(&dbRole)

	if result.Error != nil {
		return nil, result.Error
	}
	return ToRole(&dbRole), nil
}

// insert saves a new role
// Insert saves a new user role
func (r *Role) Insert(
	ctx context.Context,
	role *model.Role,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := role.ToDBMap()
	row["id"] = id

	res := r.DB.
		Model(&database.Role{}).
		Create(row)

	if res.Error != nil {
		return nil, res.Error
	}
	return &id, nil
}

func ToRole(row *database.Role) *model.Role {
	return &model.Role{
		ID:   *row.ID,
		Name: row.Name,
	}
}
