package billing

import (
	"fmt"
	ext "github.com/example/ext"
)

type BillingContext struct{}

type Billing interface {
	Pay(amount int)
}

type Processor struct {
	repo *Repository
}

type Repository struct{}

func NewProcessor(repo *Repository) *Processor {
	return &Processor{repo: repo}
}

func (p *Processor) Pay(ctx *BillingContext, amount int) {
	fmt.Sprintf("%d", amount)
	ext.Run(ctx, amount)
}
