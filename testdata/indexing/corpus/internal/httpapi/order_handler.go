package httpapi

import (
	"context"
	"errors"
	"net/http"

	"example.com/checkout/internal/order"
)

type OrderReader interface {
	GetOrder(ctx context.Context, id string) (*order.Order, error)
}

type OrderHandler struct {
	orders OrderReader
}

func (h *OrderHandler) GetOrder(response http.ResponseWriter, request *http.Request) {
	value, err := h.orders.GetOrder(request.Context(), request.PathValue("id"))
	if errors.Is(err, order.ErrNotFound) {
		http.Error(response, "order not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(response, "internal error", http.StatusInternalServerError)
		return
	}

	response.WriteHeader(http.StatusOK)
	_, _ = response.Write([]byte(value.ID))
}
