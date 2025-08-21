package vo

import (
	"strconv"

	"github.com/go-errors/errors"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/validate"
)

const MAX_PER_PAGE = 1000

// Pagination is contains an offset and limit
type Pagination struct {
	offset int
	limit  int
}

// NewPagination creates a new pagination
func NewPagination(
	offset int,
	limit int,
) (*Pagination, error) {
	// the min offset is 0 and the max is undefined
	if err := validate.ErrorIfIntEqualOrLessThan(offset, -1); err != nil {
		return nil, errs.Wrap(err)
	}
	// the min limit is 1 and the max is 50
	if err := validate.ErrorIfIntEqualOrLessThan(limit, 0); err != nil {
		return nil, errs.Wrap(err)
	}
	if err := validate.ErrorIfIntLargerThan(limit, MAX_PER_PAGE); err != nil {
		return nil, errs.Wrap(err)
	}
	return &Pagination{
		offset: offset,
		limit:  limit,
	}, nil
}

// NewPaginationFromRequest creates a new pagination from a gin request
func NewPaginationFromRequest(gin *gin.Context) (*Pagination, error) {
	o := gin.DefaultQuery("offset", "0")
	l := gin.DefaultQuery("limit", "25")
	offset, err := strconv.Atoi(o)
	if err != nil {
		_ = err
		return nil, errs.NewValidationError(errors.New("failed to parse offset"))
	}
	limit, err := strconv.Atoi(l)
	if err != nil {
		_ = err
		return nil, errs.NewValidationError(errors.New("failed to parse limit"))
	}

	return NewPagination(offset, limit)
}

// Offset returns the offset
func (p *Pagination) Offset() int {
	return p.offset
}

// Limit returns the limit
func (p *Pagination) Limit() int {
	return p.limit
}
