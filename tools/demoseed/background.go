package main

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/vtimame/istok.sh/internal/run"
)

// extraRepos are quieter projects that only carry background history.
var extraRepos = map[string]map[string]string{
	"auth-service": {
		"go.mod": "module example.com/auth-service\n\ngo 1.26\n",
		"internal/tokens/issuer.go": `package tokens

import (
	"crypto/rand"
	"encoding/base64"
	"time"
)

type Token struct {
	Value     string
	ExpiresAt time.Time
}

// Issue creates an opaque session token with 256 bits of randomness.
func Issue(ttl time.Duration) (Token, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return Token{}, err
	}

	return Token{Value: base64.RawURLEncoding.EncodeToString(buffer), ExpiresAt: time.Now().Add(ttl)}, nil
}
`,
	},
	"admin-dashboard": {
		"package.json": "{\n  \"name\": \"admin-dashboard\",\n  \"private\": true\n}\n",
		"src/reports/revenue.ts": `export type DailyRevenue = { day: string; totalMinor: number }

// weeklyTotals groups daily revenue into ISO weeks for the overview chart.
export function weeklyTotals(days: DailyRevenue[]): Map<string, number> {
  const weeks = new Map<string, number>()
  for (const day of days) {
    const date = new Date(day.day)
    const monday = new Date(date)
    monday.setUTCDate(date.getUTCDate() - ((date.getUTCDay() + 6) % 7))
    const key = monday.toISOString().slice(0, 10)
    weeks.set(key, (weeks.get(key) ?? 0) + day.totalMinor)
  }
  return weeks
}
`,
	},
	"search-indexer": {
		"go.mod": "module example.com/search-indexer\n\ngo 1.26\n",
		"internal/index/tokenize.go": `package index

import (
	"strings"
	"unicode"
)

// Tokenize lowercases text and splits it on anything that is not a letter or
// a digit, dropping one-letter tokens.
func Tokenize(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	tokens := fields[:0]
	for _, field := range fields {
		if len([]rune(field)) > 1 {
			tokens = append(tokens, field)
		}
	}
	return tokens
}
`,
	},
	"notifications": {
		"package.json": "{\n  \"name\": \"notifications\",\n  \"private\": true\n}\n",
		"src/digest.ts": `export type Notification = { userId: string; title: string; createdAt: number }

// groupDigest bundles notifications per user so each person gets one email.
export function groupDigest(items: Notification[]): Map<string, Notification[]> {
  const byUser = new Map<string, Notification[]>()
  for (const item of items) {
    byUser.set(item.userId, [...(byUser.get(item.userId) ?? []), item])
  }
  return byUser
}
`,
	},
}

// backlog lists plausible past work per project; each entry becomes a task.
var backlog = map[string][]string{
	"acme-api": {
		"Add pagination to the orders list", "Validate currency codes on order creation", "Expose order status webhooks",
		"Log request IDs in every handler", "Return 409 for duplicate SKUs", "Add OpenAPI spec for orders",
		"Cache product prices for 60 seconds", "Reject negative quantities", "Add health check endpoint",
		"Migrate order IDs to UUIDv7", "Index orders by customer", "Add structured error responses",
		"Support partial refunds", "Time out slow provider calls", "Add metrics for checkout latency",
		"Remove legacy v1 order endpoints",
	},
	"storefront-web": {
		"Lazy-load product images", "Add skeletons to the product grid", "Fix focus trap in the cart drawer",
		"Show stock levels on product pages", "Add wishlist button", "Support dark mode",
		"Prefetch checkout on cart open", "Localize prices by region", "Fix layout shift in the header",
		"Add keyboard shortcuts for search", "Track add-to-cart events",
	},
	"billing-worker": {
		"Generate PDF invoices", "Send invoice emails in batches", "Round taxes per line item",
		"Handle prorated plan changes", "Retry failed card charges", "Export invoices to CSV",
		"Add idempotency to invoice generation", "Alert on failed billing runs",
	},
	"mobile-app": {
		"Add pull to refresh on orders", "Cache images offline", "Support push notification deep links",
		"Fix crash on Android 15 share sheet", "Add order tracking screen", "Reduce cold start time",
	},
	"analytics-pipeline": {
		"Deduplicate events by ID", "Partition tables by day", "Add late-arriving events window",
		"Compute weekly retention cohorts", "Backfill revenue metrics",
	},
	"auth-service": {
		"Rotate signing keys monthly", "Add TOTP two-factor login", "Rate limit password resets",
		"Expire sessions after inactivity", "Hash passwords with Argon2id", "Add OAuth login with GitHub",
		"Audit log for admin sign-ins",
	},
	"admin-dashboard": {
		"Add revenue chart by week", "Filter orders by status", "Export customer list",
		"Show refund reasons", "Add role-based access", "Paginate audit log",
	},
	"search-indexer": {
		"Stem English tokens", "Boost exact title matches", "Reindex on product updates",
		"Add synonyms for common queries", "Drop stop words", "Benchmark query latency",
	},
	"notifications": {
		"Send daily digest emails", "Respect quiet hours", "Unsubscribe link in every email",
		"Batch push notifications", "Template emails with MJML",
	},
}

var validationCommands = map[string][][]string{
	"go":     {{"go", "test", "./..."}, {"go", "vet", "./..."}, {"go", "test", "-race", "./..."}},
	"ts":     {{"pnpm", "test"}, {"pnpm", "lint"}, {"pnpm", "typecheck"}},
	"python": {{"pytest", "-q"}, {"ruff", "check", "."}},
}

var projectLanguage = map[string]string{
	"acme-api": "go", "billing-worker": "go", "auth-service": "go", "search-indexer": "go",
	"storefront-web": "ts", "mobile-app": "ts", "admin-dashboard": "ts", "notifications": "ts",
	"analytics-pipeline": "python",
}

// seedBackground plays a month of finished, open and blocked work across all
// projects. A fixed seed keeps screenshots stable between runs.
func seedBackground(w *world, projects map[string]demoProject, now time.Time) {
	random := rand.New(rand.NewPCG(2026, 10))
	agents := []agent{claude, codex}

	for _, name := range sortedNames(projects) {
		p := projects[name]
		titles := backlog[name]

		for index, title := range titles {
			createdAt := now.Add(-time.Duration(35-index)*24*time.Hour + time.Duration(random.IntN(20))*time.Hour)
			value := w.newTask(p, taskSpec{title: title}, alex, createdAt)

			// Roughly three quarters are done; the rest stay open, a few blocked.
			roll := random.IntN(100)
			switch {
			case roll < 74:
				by := agents[random.IntN(len(agents))]
				claimedAt := createdAt.Add(time.Duration(2+random.IntN(30)) * time.Hour)
				if claimedAt.After(now.Add(-6 * time.Hour)) {
					claimedAt = now.Add(-time.Duration(6+random.IntN(48)) * time.Hour)
				}

				current := w.claim(p, value, by, claimedAt)
				commands := validationCommands[projectLanguage[name]]
				checks := 1 + random.IntN(len(commands))

				var first string
				for i := 0; i < checks; i++ {
					id := w.verify(p, current, by, checkSpec{
						argv:     commands[i],
						duration: time.Duration(800+random.IntN(9000)) * time.Millisecond,
						summary:  "Passes.",
					})
					if first == "" {
						first = id
					}
				}

				current = w.finish(current, by, run.StatusSucceeded, fmt.Sprintf("%s: done and verified.", title))
				w.complete(value, current, first, by, "Done.")

			case roll < 82:
				w.block(value, alex, "Waiting on a product decision.", createdAt.Add(3*time.Hour))
			}
		}
	}
}

func sortedNames(projects map[string]demoProject) []string {
	names := make([]string, 0, len(projects))
	for name := range projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
