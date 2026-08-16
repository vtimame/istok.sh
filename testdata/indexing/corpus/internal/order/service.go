package order

import (
	"context"
	"errors"

	"example.com/checkout/internal/payment"
)

type Service struct {
	repository Repository
	payments   payment.Gateway
}

func NewService(repository Repository, payments payment.Gateway) *Service {
	return &Service{repository: repository, payments: payments}
}

func (s *Service) CreateOrder(ctx context.Context, id string, amount int64) (*Order, error) {
	existing, err := s.repository.FindByID(ctx, id)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	value := &Order{ID: id, Amount: amount}
	if err := s.payments.Charge(ctx, value.ID, value.Amount); err != nil {
		return nil, err
	}
	if err := s.repository.Save(ctx, value); err != nil {
		return nil, err
	}

	return value, nil
}

func (s *Service) GetOrder(ctx context.Context, id string) (*Order, error) {
	return s.repository.FindByID(ctx, id)
}
