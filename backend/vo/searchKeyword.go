package vo

import (
	"fmt"

	"github.com/phishingclub/phishingclub/validate"
)

// SearchKeyword is a search keyword
type SearchKeyword struct {
	inner string
}

// NewSearchKeyword creates a new search keyword
func NewSearchKeyword(keyword string) (*SearchKeyword, error) {
	err := validate.ErrorIfStringNotbetweenOrEqualTo(keyword, 3, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid search keyword: %w", err)
	}
	return &SearchKeyword{
		inner: keyword,
	}, nil
}

// String returns the string representation of the keyword
func (n *SearchKeyword) String() string {
	return n.inner
}
