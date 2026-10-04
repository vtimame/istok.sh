package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// sampleRepos holds small but real source trees, so the index, retrieval and
// "What the agent saw" show genuine code in screenshots.
var sampleRepos = map[string]map[string]string{
	"acme-api": {
		"go.mod": "module example.com/acme-api\n\ngo 1.26\n",
		"README.md": `# acme-api

Order and payment API for the Acme store.
`,
		"internal/orders/service.go": `package orders

import (
	"context"
	"errors"
)

// ErrNotFound is returned when an order does not exist.
var ErrNotFound = errors.New("order not found")

type Order struct {
	ID          string
	CustomerID  string
	AmountMinor int64
	Currency    string
	Status      string
}

type Repository interface {
	FindByID(ctx context.Context, id string) (Order, error)
	Save(ctx context.Context, order Order) error
}

type PaymentGateway interface {
	Charge(ctx context.Context, order Order) error
}

type Service struct {
	orders   Repository
	payments PaymentGateway
}

func NewService(orders Repository, payments PaymentGateway) *Service {
	return &Service{orders: orders, payments: payments}
}

// CreateOrder is idempotent: a retried request with the same ID returns the
// stored order instead of charging the customer twice.
func (s *Service) CreateOrder(ctx context.Context, order Order) (Order, error) {
	existing, err := s.orders.FindByID(ctx, order.ID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Order{}, err
	}

	if err := s.payments.Charge(ctx, order); err != nil {
		return Order{}, err
	}

	order.Status = "paid"
	return order, s.orders.Save(ctx, order)
}

func (s *Service) GetOrder(ctx context.Context, id string) (Order, error) {
	return s.orders.FindByID(ctx, id)
}
`,
		"internal/payments/gateway.go": `package payments

import (
	"context"
	"errors"
	"time"

	"example.com/acme-api/internal/orders"
)

// ErrTemporary marks failures that are safe to retry.
var ErrTemporary = errors.New("temporary payment provider failure")

type Provider interface {
	Charge(ctx context.Context, idempotencyKey string, amountMinor int64, currency string) error
}

type Gateway struct {
	provider Provider
	attempts int
	backoff  time.Duration
}

func NewGateway(provider Provider) *Gateway {
	return &Gateway{provider: provider, attempts: 3, backoff: 200 * time.Millisecond}
}

// Charge retries temporary failures with exponential backoff. The order ID is
// the idempotency key, so a retry never charges twice.
func (g *Gateway) Charge(ctx context.Context, order orders.Order) error {
	delay := g.backoff
	var err error
	for attempt := 0; attempt < g.attempts; attempt++ {
		err = g.provider.Charge(ctx, order.ID, order.AmountMinor, order.Currency)
		if err == nil || !errors.Is(err, ErrTemporary) {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}

	return err
}
`,
		"internal/httpapi/order_handler.go": `package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"example.com/acme-api/internal/orders"
)

type OrderHandler struct {
	orders *orders.Service
}

func NewOrderHandler(service *orders.Service) *OrderHandler {
	return &OrderHandler{orders: service}
}

// GetOrder answers 404 for unknown orders and 500 for everything else.
func (h *OrderHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	order, err := h.orders.GetOrder(r.Context(), r.PathValue("id"))
	if errors.Is(err, orders.ErrNotFound) {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}
`,
	},
	"storefront-web": {
		"package.json": `{
  "name": "storefront-web",
  "private": true,
  "type": "module",
  "scripts": { "test": "vitest run", "lint": "eslint ." }
}
`,
		"src/cart/cart-store.ts": `export type CartItem = {
  productId: string
  quantity: number
  priceMinor: number
}

const STORAGE_KEY = "storefront.cart"

// The cart survives reloads and new sessions through localStorage. Storage can
// be unavailable (private mode), so every access is guarded.
export function loadCart(): CartItem[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as CartItem[]) : []
  } catch {
    return []
  }
}

export function saveCart(items: CartItem[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(items))
  } catch {
    // Keep the in-memory cart when storage is not available.
  }
}

export function cartTotalMinor(items: CartItem[]): number {
  return items.reduce((total, item) => total + item.priceMinor * item.quantity, 0)
}
`,
		"src/checkout/validate-address.ts": `export type Address = {
  name: string
  line1: string
  city: string
  postalCode: string
  country: string
}

export type FieldErrors = Partial<Record<keyof Address, string>>

const POSTAL_CODES: Record<string, RegExp> = {
  US: /^\d{5}(-\d{4})?$/,
  DE: /^\d{5}$/,
  GB: /^[A-Z]{1,2}\d[A-Z\d]? ?\d[A-Z]{2}$/i,
}

// validateAddress returns one message per invalid field, so the checkout form
// can show errors inline next to each input.
export function validateAddress(address: Address): FieldErrors {
  const errors: FieldErrors = {}

  if (!address.name.trim()) errors.name = "Enter the recipient's name"
  if (!address.line1.trim()) errors.line1 = "Enter a street address"
  if (!address.city.trim()) errors.city = "Enter a city"

  const pattern = POSTAL_CODES[address.country]
  if (pattern && !pattern.test(address.postalCode)) {
    errors.postalCode = "Enter a valid postal code"
  }

  return errors
}
`,
		"src/api/client.ts": `export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string
  ) {
    super(message)
  }
}

export async function getJSON<T>(path: string): Promise<T> {
  const response = await fetch("/api" + path, {
    headers: { Accept: "application/json" },
  })
  if (!response.ok) {
    throw new ApiError(response.status, await response.text())
  }

  return (await response.json()) as T
}
`,
	},
	"mobile-app": {
		"package.json": `{
  "name": "mobile-app",
  "private": true,
  "scripts": { "test": "jest", "lint": "eslint ." }
}
`,
		"src/auth/session.ts": `export type Session = {
  token: string
  expiresAt: number
}

const REFRESH_MARGIN_MS = 60_000

// needsRefresh renews the session a minute before it expires, so requests in
// flight never carry an expired token.
export function needsRefresh(session: Session, now: number = Date.now()): boolean {
  return session.expiresAt - now < REFRESH_MARGIN_MS
}

export async function withFreshSession<T>(
  session: Session,
  refresh: () => Promise<Session>,
  request: (session: Session) => Promise<T>
): Promise<T> {
  const current = needsRefresh(session) ? await refresh() : session
  return request(current)
}
`,
	},
	"analytics-pipeline": {
		"pyproject.toml": "[project]\nname = \"analytics-pipeline\"\nversion = \"0.1.0\"\n",
		"pipeline/sessions.py": `from dataclasses import dataclass
from datetime import datetime, timedelta

SESSION_GAP = timedelta(minutes=30)


@dataclass
class Event:
    user_id: str
    at: datetime


def sessionize(events: list[Event]) -> dict[str, int]:
    """Count sessions per user; a gap over 30 minutes starts a new session."""
    sessions: dict[str, int] = {}
    last_seen: dict[str, datetime] = {}

    for event in sorted(events, key=lambda e: (e.user_id, e.at)):
        previous = last_seen.get(event.user_id)
        if previous is None or event.at - previous > SESSION_GAP:
            sessions[event.user_id] = sessions.get(event.user_id, 0) + 1
        last_seen[event.user_id] = event.at

    return sessions
`,
	},
	"billing-worker": {
		"go.mod": "module example.com/billing-worker\n\ngo 1.26\n",
		"internal/invoices/generator.go": `package invoices

import (
	"fmt"
	"time"
)

type Invoice struct {
	CustomerID  string
	PeriodStart time.Time
	PeriodEnd   time.Time
	TotalMinor  int64
}

// PeriodFor returns the billing month that contains moment, in UTC, so
// customers in every time zone are billed for the same calendar month.
func PeriodFor(moment time.Time) (time.Time, time.Time) {
	utc := moment.UTC()
	start := time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func Generate(customerID string, usageMinor []int64, moment time.Time) (Invoice, error) {
	if customerID == "" {
		return Invoice{}, fmt.Errorf("customer id is required")
	}

	start, end := PeriodFor(moment)
	var total int64
	for _, amount := range usageMinor {
		total += amount
	}

	return Invoice{CustomerID: customerID, PeriodStart: start, PeriodEnd: end, TotalMinor: total}, nil
}
`,
		"internal/queue/consumer.go": `package queue

import (
	"context"
	"time"
)

type Message struct {
	ID       string
	Body     []byte
	Attempts int
}

type Broker interface {
	Ack(ctx context.Context, id string) error
	Retry(ctx context.Context, id string, after time.Duration) error
	DeadLetter(ctx context.Context, id string) error
}

const maxAttempts = 5

// Handle retries a failed message with growing delays and moves it to the
// dead-letter queue after maxAttempts.
func Handle(ctx context.Context, broker Broker, message Message, process func(Message) error) error {
	if err := process(message); err == nil {
		return broker.Ack(ctx, message.ID)
	}

	if message.Attempts+1 >= maxAttempts {
		return broker.DeadLetter(ctx, message.ID)
	}

	delay := time.Duration(1<<message.Attempts) * time.Second
	return broker.Retry(ctx, message.ID, delay)
}
`,
	},
}

// writeRepo creates the project directory with its files and one commit, and
// returns the commit hash used as the runs' base commit.
func writeRepo(root, name string) (string, error) {
	files, found := sampleRepos[name]
	if !found {
		files, found = extraRepos[name]
	}
	if !found {
		return "", fmt.Errorf("unknown sample repo %q", name)
	}

	for path, content := range files {
		target := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return "", err
		}
	}

	steps := [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"add", "--all"},
		{"-c", "user.name=Istok Demo", "-c", "user.email=demo@example.com", "commit", "--quiet", "-m", "Initial commit"},
	}
	for _, step := range steps {
		if err := git(root, step...); err != nil {
			return "", err
		}
	}

	output, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("read demo commit: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func git(dir string, args ...string) error {
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, output)
	}

	return nil
}
