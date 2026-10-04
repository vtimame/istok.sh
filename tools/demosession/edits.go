package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The change the agent makes: CreateOrder refuses amounts that would charge
// nothing, and a test proves no charge happens.

const serviceFile = "internal/orders/service.go"

const testFile = "internal/orders/service_test.go"

type replacement struct {
	before string
	after  string
}

var serviceEdits = []replacement{
	{
		before: `// ErrNotFound is returned when an order does not exist.
var ErrNotFound = errors.New("order not found")
`,
		after: `// ErrNotFound is returned when an order does not exist.
var ErrNotFound = errors.New("order not found")

// ErrInvalidAmount is returned for orders that would charge nothing.
var ErrInvalidAmount = errors.New("order amount must be positive")
`,
	},
	{
		before: `func (s *Service) CreateOrder(ctx context.Context, order Order) (Order, error) {
	existing, err := s.orders.FindByID(ctx, order.ID)
`,
		after: `func (s *Service) CreateOrder(ctx context.Context, order Order) (Order, error) {
	if order.AmountMinor <= 0 {
		return Order{}, ErrInvalidAmount
	}

	existing, err := s.orders.FindByID(ctx, order.ID)
`,
	},
}

const serviceTest = `package orders

import (
	"context"
	"errors"
	"testing"
)

type memoryOrders map[string]Order

func (m memoryOrders) FindByID(_ context.Context, id string) (Order, error) {
	order, found := m[id]
	if !found {
		return Order{}, ErrNotFound
	}
	return order, nil
}

func (m memoryOrders) Save(_ context.Context, order Order) error {
	m[order.ID] = order
	return nil
}

type countingGateway struct{ charges int }

func (g *countingGateway) Charge(context.Context, Order) error {
	g.charges++
	return nil
}

func TestCreateOrderRejectsNonPositiveAmount(t *testing.T) {
	for _, amount := range []int64{0, -500} {
		gateway := &countingGateway{}
		service := NewService(memoryOrders{}, gateway)

		_, err := service.CreateOrder(context.Background(), Order{ID: "order-1", AmountMinor: amount, Currency: "EUR"})
		if !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("amount %d: err = %v, want ErrInvalidAmount", amount, err)
		}
		if gateway.charges != 0 {
			t.Fatalf("amount %d: charged %d times, want 0", amount, gateway.charges)
		}
	}
}
`

// readLineCount returns how many lines the agent sees when it reads a file.
func readLineCount(repository, name string) (int, error) {
	content, err := os.ReadFile(filepath.Join(repository, name))
	if err != nil {
		return 0, err
	}

	return strings.Count(string(content), "\n"), nil
}

// applyServiceEdits changes the service and returns the lines it added.
func applyServiceEdits(repository string) ([]string, error) {
	path := filepath.Join(repository, serviceFile)

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	updated := string(content)
	var added []string
	for _, edit := range serviceEdits {
		if !strings.Contains(updated, edit.before) {
			return nil, fmt.Errorf("%s does not contain the expected code; was the demo repository changed?", serviceFile)
		}
		updated = strings.Replace(updated, edit.before, edit.after, 1)
		added = append(added, addedLines(edit.before, edit.after)...)
	}

	return added, os.WriteFile(path, []byte(updated), 0o644)
}

func writeServiceTest(repository string) (int, error) {
	path := filepath.Join(repository, testFile)
	if err := os.WriteFile(path, []byte(serviceTest), 0o644); err != nil {
		return 0, err
	}

	return strings.Count(serviceTest, "\n"), nil
}

// addedLines lists the lines of after that are not in before; each edit only
// inserts lines, so this is the edit's diff.
func addedLines(before, after string) []string {
	existing := map[string]int{}
	for _, text := range strings.Split(before, "\n") {
		existing[text]++
	}

	var added []string
	for _, text := range strings.Split(after, "\n") {
		if existing[text] > 0 {
			existing[text]--
			continue
		}
		added = append(added, text)
	}

	return added
}
