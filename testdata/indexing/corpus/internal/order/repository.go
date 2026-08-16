package order

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("order not found")

type Order struct {
	ID     string
	Amount int64
}

type Repository interface {
	FindByID(context.Context, string) (*Order, error)
	Save(context.Context, *Order) error
}
