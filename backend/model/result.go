package model

type Result[T any] struct {
	Rows        []*T `json:"rows"`
	HasNextPage bool `json:"hasNextPage"`
}

func NewResult[T any](rows []*T) *Result[T] {
	return &Result[T]{
		Rows:        rows,
		HasNextPage: false,
	}
}

func NewEmptyResult[T any]() *Result[T] {
	t := []*T{}
	return &Result[T]{
		Rows:        t,
		HasNextPage: false,
	}
}
