package vo

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/errs"
)

type QueryArgs struct {
	Offset  int
	Limit   int
	OrderBy string
	Desc    bool // z to a. 9 to 0
	Search  string
}

func QueryFromRequest(gin *gin.Context) (*QueryArgs, error) {
	pagination, err := NewPaginationFromRequest(gin)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	sortBy := gin.DefaultQuery("sortBy", "")
	descParam := gin.DefaultQuery("sortOrder", "asc")
	var desc bool
	if descParam == "desc" {
		desc = true
	} else if descParam == "asc" {
		desc = false
	}
	search := gin.DefaultQuery("search", "")
	return &QueryArgs{
		Offset:  pagination.Offset(),
		Limit:   pagination.Limit(),
		OrderBy: sortBy,
		Desc:    desc,
		Search:  search,
	}, nil
}

func (q *QueryArgs) DefaultSortByName() {
	if q.OrderBy == "" {
		q.OrderBy = "name"
	}
}

func (q *QueryArgs) DefaultSortByCreatedAt() {
	if q.OrderBy == "" {
		q.OrderBy = "created_at"
	}
}

func (q *QueryArgs) DefaultSortByUpdatedAt() {
	if q.OrderBy == "" {
		q.OrderBy = "updated_at"
	}
}

func (q *QueryArgs) DefaultSortBy(column string) {
	if q.OrderBy == "" {
		q.OrderBy = column
	}
}

// RemapOrderBy remaps the order by column using the provided mapping.
// if the column is not found in the mapping, it is cleared to prevent SQL injection.
func (q *QueryArgs) RemapOrderBy(m map[string]string) {
	if q.OrderBy == "" {
		return
	}
	if v, ok := m[q.OrderBy]; ok {
		q.OrderBy = v
	} else {
		q.OrderBy = ""
	}
}
