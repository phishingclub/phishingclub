package repository

import (
	"context"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// Option is a repository for Option
type Option struct {
	DB *gorm.DB
}

// GetByKey gets an option by key
func (o *Option) GetByKey(
	ctx context.Context,
	key string,
) (*model.Option, error) {
	var option database.Option
	result := o.DB.Where("key = ?", key).First(&option)
	if result.Error != nil {
		return nil, errors.Wrap(result.Error, 0)
	}
	return ToOption(&option)
}

// updateByKey updates an option by key
func (o *Option) updateByKey(
	ctx context.Context,
	d *gorm.DB,
	option *model.Option,
) error {
	result := d.
		Model(&database.Option{}).
		Where("key = ?", option.Key.String()).
		Update("value", option.Value.String())

	if result.Error != nil {
		return errors.Wrap(result.Error, 0)
	}
	return nil
}

// UpdateByKey updates an option by key
func (o *Option) UpdateByKey(
	ctx context.Context,
	option *model.Option,
) error {
	return o.updateByKey(
		ctx,
		o.DB,
		option,
	)
}

// UpdateByKeyWithTransaction updates an option by key with transaction
func (o *Option) UpdateByKeyWithTransaction(
	ctx context.Context,
	tx *gorm.DB,
	option *model.Option,
) error {
	return o.updateByKey(ctx, tx, option)
}

// insert creates an option from an option without id
func (o *Option) insert(
	ctx context.Context,
	d *gorm.DB,
	opt *model.Option,
) (*uuid.UUID, error) {
	id := uuid.New()
	res := d.Create(database.Option{
		ID:    &id,
		Key:   opt.Key.String(),
		Value: opt.Value.String(),
	})
	if res.Error != nil {
		return nil, errors.Wrap(res.Error, 0)
	}
	return &id, nil
}

// Insert creates an option
func (o *Option) Insert(
	ctx context.Context,
	opt *model.Option,
) (*uuid.UUID, error) {
	return o.insert(ctx, o.DB, opt)
}

// InsertWithTransaction creates an option using an transaction
func (o *Option) InsertWithTransaction(
	ctx context.Context,
	tx *gorm.DB,
	opt *model.Option,
) (*uuid.UUID, error) {
	return o.insert(ctx, tx, opt)
}

func ToOption(dbModel *database.Option) (*model.Option, error) {
	id := nullable.NewNullableWithValue(*dbModel.ID)
	key, err := vo.NewString64(dbModel.Key)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	value, err := vo.NewOptionalString1MB(dbModel.Value)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &model.Option{
		ID:    id,
		Key:   *key,
		Value: *value,
	}, nil
}
