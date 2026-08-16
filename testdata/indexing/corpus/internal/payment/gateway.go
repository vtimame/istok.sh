package payment

import "context"

type Gateway interface {
	Charge(ctx context.Context, orderID string, amount int64) error
}
