package main

import (
	"time"

	contextmodel "github.com/vtimame/istok.sh/internal/context"
	"github.com/vtimame/istok.sh/internal/knowledge"
	"github.com/vtimame/istok.sh/internal/run"
	"github.com/vtimame/istok.sh/internal/task"
)

// seedScenario tells one story across three fictional projects: Claude and
// Codex take tasks, run tests, sometimes fail, hand work over, and one run is
// still in progress while another went stale.
func seedScenario(w *world) {
	now := time.Now()
	ago := func(days, hours int) time.Time {
		return now.Add(-time.Duration(days)*24*time.Hour - time.Duration(hours)*time.Hour)
	}

	acme := w.newProject("acme-api", ago(9, 2))
	store := w.newProject("storefront-web", ago(8, 5))
	billing := w.newProject("billing-worker", ago(7, 1))
	mobile := w.newProject("mobile-app", ago(12, 3))
	analytics := w.newProject("analytics-pipeline", ago(15, 6))

	projects := map[string]demoProject{
		"acme-api": acme, "storefront-web": store, "billing-worker": billing,
		"mobile-app": mobile, "analytics-pipeline": analytics,
	}
	for _, name := range []string{"auth-service", "admin-dashboard", "search-indexer", "notifications"} {
		projects[name] = w.newProject(name, ago(30, 0))
	}

	// A month of background work first, so the story below stays the newest.
	seedBackground(w, projects, now)

	seedContext(w, acme, store, billing)

	// acme-api: idempotent orders, a failed then fixed 404, a live retry task.
	idempotency := w.newTask(acme, taskSpec{
		title:       "Make order creation idempotent",
		description: "A retried `POST /orders` charges the customer twice when the first response times out.\n\nUse the order ID as the idempotency key and return the stored order on retries.",
		criteria:    "- A repeated request with the same order ID returns the existing order\n- `PaymentGateway.Charge` is called once per order\n- `go test ./...` passes",
	}, alex, ago(4, 6))
	runIdem := w.claim(acme, idempotency, claude, ago(3, 5))
	w.progress(idempotency, claude, "`CreateOrder` now looks the order up first and only charges unknown IDs.", ago(3, 4))
	idemTests := w.verify(acme, runIdem, claude, checkSpec{argv: []string{"go", "test", "./..."}, duration: 8400 * time.Millisecond, summary: "All packages pass, including a new test that retries `CreateOrder` with the same ID."})
	w.verify(acme, runIdem, claude, checkSpec{argv: []string{"go", "vet", "./..."}, duration: 2100 * time.Millisecond, summary: "No issues."})
	runIdem = w.finish(runIdem, claude, run.StatusSucceeded, "`CreateOrder` is idempotent: retries with the same order ID return the stored order and never charge twice. Covered by `TestCreateOrderRetry`.")
	w.complete(idempotency, runIdem, idemTests, claude, "Idempotent order creation is merged and tested.")

	notFound := w.newTask(acme, taskSpec{
		title:       "Return 404 for missing orders",
		description: "`GET /orders/{id}` answers 500 when the order does not exist, which pages the on-call engineer.",
		criteria:    "- Unknown orders return 404\n- Other errors still return 500",
	}, alex, ago(3, 2))
	runNF1 := w.claim(acme, notFound, codex, ago(2, 7))
	w.verify(acme, runNF1, codex, checkSpec{argv: []string{"go", "test", "./internal/httpapi/..."}, exitCode: 1, duration: 3300 * time.Millisecond, summary: "`TestGetOrderNotFound` fails: the handler compares errors with `==`, and the repository wraps `ErrNotFound`."})
	w.finish(runNF1, codex, run.StatusFailed, "Handler change is not enough: wrapped `ErrNotFound` still maps to 500. Needs `errors.Is`; handing over.")
	w.comment(notFound, codex, "The repository wraps `ErrNotFound`, so the handler has to use `errors.Is`.", ago(2, 6))
	runNF2 := w.claim(acme, notFound, claude, ago(1, 9))
	nfTests := w.verify(acme, runNF2, claude, checkSpec{argv: []string{"go", "test", "./..."}, duration: 7900 * time.Millisecond, summary: "All packages pass; `TestGetOrderNotFound` covers wrapped errors."})
	runNF2 = w.finish(runNF2, claude, run.StatusSucceeded, "`GetOrder` maps `orders.ErrNotFound` (wrapped or not) to 404 with `errors.Is`; other errors stay 500.")
	w.complete(notFound, runNF2, nfTests, claude, "Missing orders now return 404.")

	rateLimit := w.newTask(acme, taskSpec{
		title:       "Rate limit the checkout endpoint",
		description: "Limit checkout attempts per customer to stop card testing.",
	}, alex, ago(2, 1))
	w.block(rateLimit, alex, "Waiting for the security team to agree on limits per customer and per IP.", ago(1, 5))

	// storefront-web: a persisted cart, a stale run, an open migration.
	cart := w.newTask(store, taskSpec{
		title:       "Persist the cart across sessions",
		description: "Shoppers lose their cart after closing the tab. Keep it in `localStorage` and survive private mode.",
		criteria:    "- The cart survives reloads and new sessions\n- Private mode keeps working with an in-memory cart",
	}, alex, ago(2, 8))
	runCart := w.claim(store, cart, codex, ago(1, 4))
	cartTests := w.verify(store, runCart, codex, checkSpec{argv: []string{"pnpm", "test"}, duration: 5200 * time.Millisecond, summary: "18 tests pass, including storage failures in private mode."})
	w.verify(store, runCart, codex, checkSpec{argv: []string{"pnpm", "lint"}, duration: 3100 * time.Millisecond, summary: "No lint errors."})
	runCart = w.finish(runCart, codex, run.StatusSucceeded, "Cart is saved to `localStorage` on every change and restored on load; storage errors fall back to the in-memory cart.")
	w.complete(cart, runCart, cartTests, codex, "Cart persistence shipped.")

	inline := w.newTask(store, taskSpec{
		title:       "Show inline validation on the checkout form",
		description: "Show address errors next to each field instead of one banner at the top.",
	}, alex, ago(3, 1))
	w.claim(store, inline, codex, ago(2, 3)) // never finished: the lease expires and the run goes stale

	w.newTask(store, taskSpec{
		title:       "Migrate the API client to typed responses",
		description: "Replace `any` in `getJSON` callers with generated types.",
	}, alex, ago(0, 5))

	// billing-worker: two finished runs and an open alerting task.
	utc := w.newTask(billing, taskSpec{
		title:       "Bill customers by UTC calendar month",
		description: "Customers east of UTC get invoices for the wrong month on the 1st.",
		criteria:    "- Billing periods are UTC calendar months\n- Tests cover time zones on both sides of UTC",
	}, alex, ago(6, 2))
	runUTC := w.claim(billing, utc, claude, ago(5, 3))
	utcTests := w.verify(billing, runUTC, claude, checkSpec{argv: []string{"go", "test", "./..."}, duration: 4100 * time.Millisecond, summary: "Pass; new cases for UTC+14 and UTC-12 on month boundaries."})
	runUTC = w.finish(runUTC, claude, run.StatusSucceeded, "`PeriodFor` converts to UTC before computing the month, so every customer is billed for the same calendar month.")
	w.complete(utc, runUTC, utcTests, claude, "UTC billing periods are live.")

	queue := w.newTask(billing, taskSpec{
		title:       "Retry failed queue messages before dead-lettering",
		description: "Failed messages go straight to the dead-letter queue. Retry them a few times with growing delays first.",
		criteria:    "- Up to 5 attempts with exponential delays\n- After the last attempt the message is dead-lettered",
	}, alex, ago(1, 6))
	runQueue := w.claim(billing, queue, claude, now.Add(-3*time.Hour))
	queueTests := w.verify(billing, runQueue, claude, checkSpec{argv: []string{"go", "test", "./..."}, duration: 3800 * time.Millisecond, summary: "All tests pass."})
	w.verify(billing, runQueue, claude, checkSpec{argv: []string{"go", "test", "-race", "./internal/queue/..."}, duration: 9200 * time.Millisecond, summary: "No races."})
	w.verify(billing, runQueue, claude, checkSpec{argv: []string{"go", "vet", "./..."}, duration: 1900 * time.Millisecond, summary: "No issues."})
	runQueue = w.finish(runQueue, claude, run.StatusSucceeded, "`Handle` retries failures with 1s, 2s, 4s… delays and dead-letters after 5 attempts.")
	w.complete(queue, runQueue, queueTests, claude, "Retries before dead-lettering are in place.")

	w.newTask(billing, taskSpec{
		title:       "Alert when the dead-letter queue grows",
		description: "Page on-call when more than 50 messages land in the dead-letter queue within an hour.",
	}, alex, now.Add(-2*time.Hour))

	// mobile-app and analytics-pipeline: quieter projects with older history.
	refresh := w.newTask(mobile, taskSpec{
		title:       "Refresh the session before it expires",
		description: "Requests fail with 401 when the token expires mid-flight.",
		criteria:    "- Sessions refresh a minute before expiry\n- No request is sent with an expired token",
	}, alex, ago(6, 4))
	runRefresh := w.claim(mobile, refresh, codex, ago(5, 7))
	refreshTests := w.verify(mobile, runRefresh, codex, checkSpec{argv: []string{"pnpm", "test"}, duration: 6100 * time.Millisecond, summary: "Session tests pass, including the refresh margin."})
	runRefresh = w.finish(runRefresh, codex, run.StatusSucceeded, "`withFreshSession` refreshes tokens a minute before expiry, so in-flight requests never use an expired token.")
	w.complete(refresh, runRefresh, refreshTests, codex, "Session refresh shipped.")
	w.newTask(mobile, taskSpec{title: "Support biometric unlock", description: "Unlock the app with Face ID / fingerprint after the first sign-in."}, alex, ago(4, 2))

	sessions := w.newTask(analytics, taskSpec{
		title:       "Split user sessions after 30 minutes of inactivity",
		description: "Session counts are inflated because every event starts a new session.",
		criteria:    "- A gap over 30 minutes starts a new session\n- Events are processed in time order per user",
	}, alex, ago(10, 2))
	runSessions := w.claim(analytics, sessions, claude, ago(9, 4))
	sessionTests := w.verify(analytics, runSessions, claude, checkSpec{argv: []string{"pytest", "-q"}, duration: 2400 * time.Millisecond, summary: "12 passed."})
	runSessions = w.finish(runSessions, claude, run.StatusSucceeded, "`sessionize` sorts events per user and starts a new session after a 30-minute gap.")
	w.complete(sessions, runSessions, sessionTests, claude, "Sessionization fixed.")
	w.newTask(analytics, taskSpec{title: "Backfill sessions for the last 90 days", description: "Recompute daily session counts after the sessionization fix."}, alex, ago(8, 1))

	seedKnowledge(w, acme, store, idempotency, notFound, cart, ago)

	// The live run comes last, so its snapshot includes the knowledge above.
	retries := w.newTask(acme, taskSpec{
		title:       "Retry temporary payment failures with backoff",
		description: "The payment provider returns transient 503s a few times a day. Retry them with exponential backoff, reusing the order ID as the idempotency key.",
		criteria:    "- Up to 3 attempts with exponential backoff\n- Only `ErrTemporary` is retried\n- Context cancellation stops retries",
	}, alex, ago(1, 3))
	runRetry := w.claim(acme, retries, claude, now.Add(-14*time.Minute))
	w.progress(retries, claude, "Added a retry loop to `Gateway.Charge`; writing tests for cancellation next.", now.Add(-6*time.Minute))
	w.verify(acme, runRetry, claude, checkSpec{argv: []string{"go", "test", "./internal/payments/..."}, duration: 2600 * time.Millisecond, summary: "Retry and backoff tests pass."})
	w.keepAlive(runRetry, claude, 4*time.Hour)

	refunds := w.newTask(acme, taskSpec{
		title:       "Document the refund flow",
		description: "Write a runbook for partial and full refunds once payment retries land.",
	}, alex, ago(1, 2))
	w.dependsOn(refunds, retries, alex, ago(1, 2))

	w.settleTaskTimes()
}

func seedContext(w *world, acme, store, billing demoProject) {
	w.instruction(acme, contextmodel.KindInstruction, contextmodel.DeliveryAlways,
		"Run the full test suite before finishing",
		"Run `go test ./...` and `go vet ./...` and record both as validations before finishing a run.", "testing")
	w.instruction(acme, contextmodel.KindConstraint, contextmodel.DeliveryAlways,
		"Never log card data",
		"Payment requests must not log card numbers, CVC or full billing addresses. Log the order ID instead.", "security", "payments")
	w.instruction(acme, contextmodel.KindDecision, contextmodel.DeliveryRanked,
		"Money is stored in minor units",
		"All amounts are `int64` minor units (cents) with an ISO 4217 currency code. Never use floats for money.", "payments")

	w.instruction(store, contextmodel.KindInstruction, contextmodel.DeliveryAlways,
		"Run tests and lint",
		"Run `pnpm test` and `pnpm lint` before finishing.", "testing")
	w.instruction(billing, contextmodel.KindDecision, contextmodel.DeliveryRanked,
		"Billing runs in UTC",
		"Billing periods, cutoffs and invoice dates are computed in UTC.", "billing")
}

func seedKnowledge(w *world, acme, store demoProject, idempotency, notFound, cart task.Task, ago func(int, int) time.Time) {
	w.knowledgeItem(acme, claude, knowledgeSpec{
		kind:       knowledge.KindArchitecture,
		title:      "Order idempotency",
		summary:    "Order IDs double as idempotency keys from the API down to the payment provider.",
		body:       "`CreateOrder` returns the stored order for a known ID and only charges unknown IDs. The payment gateway passes the order ID as the provider's idempotency key, so retries at any layer never charge twice.",
		tags:       []string{"orders", "payments"},
		promote:    true,
		createdAt:  ago(3, 4),
		reviewedAt: ago(2, 9),
		sourceTask: idempotency,
	})
	w.knowledgeItem(acme, claude, knowledgeSpec{
		kind:       knowledge.KindRunbook,
		title:      "Mapping domain errors to HTTP statuses",
		summary:    "Handlers use errors.Is for domain errors because repositories wrap them.",
		body:       "Repositories wrap domain errors with context. Handlers must compare with `errors.Is` (`orders.ErrNotFound` → 404); everything else is 500.",
		tags:       []string{"http"},
		createdAt:  ago(1, 8),
		sourceTask: notFound,
	})
	w.knowledgeItem(store, codex, knowledgeSpec{
		kind:       knowledge.KindDecision,
		title:      "Cart persistence uses localStorage",
		summary:    "The cart lives in localStorage with an in-memory fallback for private mode.",
		body:       "`saveCart` writes on every change and `loadCart` restores on start. Storage access is wrapped in try/catch because Safari private mode throws.",
		tags:       []string{"cart"},
		promote:    true,
		createdAt:  ago(1, 3),
		reviewedAt: ago(0, 20),
		sourceTask: cart,
	})
}
